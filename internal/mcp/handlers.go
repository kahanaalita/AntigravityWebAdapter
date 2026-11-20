package mcp

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	"agy-gemini-web-mcp/internal/auth"
	"agy-gemini-web-mcp/internal/config"
	"agy-gemini-web-mcp/internal/geminiweb"
	"agy-gemini-web-mcp/internal/media"
)

// ServerHandler encapsulates MCP tool and prompt registrations.
type ServerHandler struct {
	client       *geminiweb.Client
	scotty       *media.ScottyUploader
	resolver     *media.ImageResolver
	customHexIDs map[config.ModelType]string
}

// NewServerHandler creates a ServerHandler.
func NewServerHandler(client *geminiweb.Client, scotty *media.ScottyUploader, resolver *media.ImageResolver, customHexIDs map[config.ModelType]string) *ServerHandler {
	return &ServerHandler{
		client:       client,
		scotty:       scotty,
		resolver:     resolver,
		customHexIDs: customHexIDs,
	}
}

// Register registers all tools and prompts onto the MCP server.
func (h *ServerHandler) Register(s *server.MCPServer) {
	// 1. Primary unified tool: gemini_pro_web_ask
	s.AddTool(
		mcp.NewTool(
			"gemini_pro_web_ask",
			mcp.WithDescription("Unified invocation of Google AI Pro web models (Hydrogen 4 and Neon 4) with granular reasoning effort and multimodal image upload."),
			mcp.WithString("model", mcp.Required(), mcp.Description("Target model: 'hydrogen-4' or 'neon-4' (or aliases)")),
			mcp.WithString("prompt", mcp.Required(), mcp.Description("User query or instructions")),
			mcp.WithString("effort", mcp.Description("Reasoning effort level: 'low', 'middle', 'medium', or 'high'")),
			mcp.WithArray("images", mcp.Description("List of image paths, data URIs, or '@clipboard'")),
			mcp.WithString("conversation_id", mcp.Description("Optional existing conversation ID to continue")),
			mcp.WithNumber("timeout_seconds", mcp.Description("Optional execution timeout in seconds")),
		),
		h.handleUnifiedAsk,
	)

	// 2. Six atomic alias tools
	registerAtomicTool := func(name, model, effort, desc string) {
		s.AddTool(
			mcp.NewTool(
				name,
				mcp.WithDescription(desc),
				mcp.WithString("prompt", mcp.Required(), mcp.Description("User prompt")),
				mcp.WithArray("images", mcp.Description("Optional image paths, URLs, or '@clipboard'")),
				mcp.WithString("conversation_id", mcp.Description("Optional conversation ID to continue")),
			),
			func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
				return h.executeAsk(ctx, model, effort, req.GetArguments(), false)
			},
		)
	}

	registerAtomicTool("hydrogen_low", "hydrogen-4", "low", "Hydrogen 4 with low reasoning effort (fast response, 20m streaming timeout, 5m idle)")
	registerAtomicTool("hydrogen_middle", "hydrogen-4", "middle", "Hydrogen 4 with middle reasoning effort (balanced analysis, 20m streaming timeout, 5m idle)")
	registerAtomicTool("hydrogen_high", "hydrogen-4", "high", "Hydrogen 4 with high reasoning effort (deep analysis, 20m streaming timeout, 5m idle)")

	registerAtomicTool("neon_low", "neon-4", "low", "Neon 4 with low reasoning effort (fast response, 20m streaming timeout, 5m idle)")
	registerAtomicTool("neon_medium", "neon-4", "medium", "Neon 4 with medium reasoning effort (balanced synthesis, 20m streaming timeout, 5m idle)")
	registerAtomicTool("neon_high", "neon-4", "high", "Neon 4 with high reasoning effort (deep synthesis, 20m streaming timeout, 5m idle)")

	// Backward compatibility aliases for argon
	registerAtomicTool("argon_low", "neon-4", "low", "Argon 4 alias (migrated to Neon 4 with low reasoning effort)")
	registerAtomicTool("argon_medium", "neon-4", "medium", "Argon 4 alias (migrated to Neon 4 with medium reasoning effort)")
	registerAtomicTool("argon_high", "neon-4", "high", "Argon 4 alias (migrated to Neon 4 with high reasoning effort)")

	// 3. MCP Prompts: hydrogen and neon
	s.AddPrompt(
		mcp.NewPrompt(
			"hydrogen",
			mcp.WithPromptDescription("Prompt template for Hydrogen 4 (Google AI Pro Web)"),
			mcp.WithArgument("prompt", mcp.RequiredArgument(), mcp.ArgumentDescription("Prompt text for Hydrogen 4")),
			mcp.WithArgument("effort", mcp.ArgumentDescription("Reasoning effort: low | middle | high (default: middle)")),
			mcp.WithArgument("image_paths", mcp.ArgumentDescription("Comma-separated list of image paths or @clipboard")),
		),
		func(ctx context.Context, req mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
			return h.handlePromptTemplate("hydrogen-4", "middle", req)
		},
	)

	s.AddPrompt(
		mcp.NewPrompt(
			"neon",
			mcp.WithPromptDescription("Prompt template for Neon 4 (Google AI Pro Web)"),
			mcp.WithArgument("prompt", mcp.RequiredArgument(), mcp.ArgumentDescription("Prompt text for Neon 4")),
			mcp.WithArgument("effort", mcp.ArgumentDescription("Reasoning effort: low | medium | high (default: medium)")),
			mcp.WithArgument("image_paths", mcp.ArgumentDescription("Comma-separated list of image paths or @clipboard")),
		),
		func(ctx context.Context, req mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
			return h.handlePromptTemplate("neon-4", "medium", req)
		},
	)
}

func (h *ServerHandler) handleUnifiedAsk(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args := req.GetArguments()
	model, _ := args["model"].(string)
	effort, _ := args["effort"].(string)
	return h.executeAsk(ctx, model, effort, args, true)
}

func (h *ServerHandler) prepareAskRequest(rawModel, rawEffort string, args map[string]any, allowSlashOverride bool) (*geminiweb.StreamGenerateRequest, []media.ResolvedImage, error) {
	rawPrompt, _ := args["prompt"].(string)
	if strings.TrimSpace(rawPrompt) == "" {
		return nil, nil, fmt.Errorf("prompt cannot be empty")
	}

	convID, _ := args["conversation_id"].(string)

	var customTimeout time.Duration
	if timeoutSec, ok := args["timeout_seconds"].(float64); ok && timeoutSec > 0 {
		customTimeout = time.Duration(timeoutSec) * time.Second
	}

	// Parse images argument
	var explicitImages []string
	if imgArg, ok := args["images"]; ok && imgArg != nil {
		switch v := imgArg.(type) {
		case []any:
			for _, item := range v {
				if s, ok := item.(string); ok && s != "" {
					explicitImages = append(explicitImages, s)
				}
			}
		case []string:
			explicitImages = v
		case string:
			if strings.TrimSpace(v) != "" {
				explicitImages = append(explicitImages, v)
			}
		}
	}

	// Route slash command only if allowSlashOverride is true (e.g. gemini_pro_web_ask)
	if allowSlashOverride {
		slashRes, err := ParseSlashCommand(rawPrompt, "", "")
		if err == nil && slashRes.Model != "" {
			rawModel = string(slashRes.Model)
			if slashRes.Effort != "" {
				rawEffort = string(slashRes.Effort)
			}
			rawPrompt = slashRes.Prompt
		}
	}

	if rawModel == "" {
		rawModel = "hydrogen-4"
	}

	modelCfg, err := config.ResolveModelConfig(rawModel, rawEffort, h.customHexIDs)
	if err != nil {
		return nil, nil, fmt.Errorf("invalid model or effort: %w", err)
	}

	// Resolve media images from prompt text (@path, @clipboard) and images array
	cleanedPrompt, resolvedImages, err := h.resolver.ResolveFromPromptAndList(rawPrompt, explicitImages)
	if err != nil {
		return nil, nil, fmt.Errorf("resolve images: %w", err)
	}

	return &geminiweb.StreamGenerateRequest{
		ModelConfig:    modelCfg,
		Prompt:         cleanedPrompt,
		ConversationID: convID,
		CustomTimeout:  customTimeout,
	}, resolvedImages, nil
}

func (h *ServerHandler) executeAsk(ctx context.Context, rawModel, rawEffort string, args map[string]any, allowSlashOverride bool) (*mcp.CallToolResult, error) {
	genReq, resolvedImages, err := h.prepareAskRequest(rawModel, rawEffort, args, allowSlashOverride)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	// Upload images via Scotty Resumable Upload
	if len(resolvedImages) > 0 {
		attList, err := h.scotty.UploadImages(ctx, resolvedImages)
		if err != nil {
			if errors.Is(err, geminiweb.ErrAuthenticationRequired) || isAuthError(err) {
				return mcp.NewToolResultError(FormatAuthRequiredHelp(err)), nil
			}
			return mcp.NewToolResultError(fmt.Sprintf("scotty image upload: %v", err)), nil
		}
		genReq.Attachments = attList
	}

	// Execute Generation
	resp, err := h.client.Generate(ctx, *genReq)
	if err != nil {
		if errors.Is(err, geminiweb.ErrAuthenticationRequired) || isAuthError(err) {
			return mcp.NewToolResultError(FormatAuthRequiredHelp(err)), nil
		}
		return mcp.NewToolResultError(fmt.Sprintf("gemini web rpc generation: %v", err)), nil
	}

	formatted := resp.FormattedOutput()
	return mcp.NewToolResultText(formatted), nil
}

func isAuthError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, geminiweb.ErrAuthenticationRequired) || errors.Is(err, auth.ErrAuthenticationRequired) {
		return true
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "accounts.google.com") ||
		strings.Contains(msg, "servicelogin") ||
		strings.Contains(msg, "session expired") ||
		strings.Contains(msg, "authentication required") ||
		strings.Contains(msg, "status 401") ||
		(strings.Contains(msg, "status 403") && !strings.Contains(msg, "google api error"))
}

// FormatAuthRequiredHelp formats a structured instruction guide for re-authenticating with Google Gemini.
func FormatAuthRequiredHelp(err error) string {
	var b strings.Builder
	b.WriteString("🔐 ТРЕБУЕТСЯ АВТОРИЗАЦИЯ В GOOGLE GEMINI (AUTH_REQUIRED)\n\n")
	b.WriteString("Сессия Gemini недействительна или истекли куки авторизации.\n\n")
	b.WriteString("Для восстановления доступа выполните одно из следующих действий:\n\n")
	b.WriteString("1. Запуск браузера Google Chrome с открытым портом отладки (CDP) [Рекомендуется]:\n")
	b.WriteString("   Выполните в терминале команду:\n")
	b.WriteString("     macOS:\n")
	b.WriteString("       /Applications/Google\\ Chrome.app/Contents/MacOS/Google\\ Chrome \\\n")
	b.WriteString("         --remote-debugging-port=9222 \\\n")
	b.WriteString("         --user-data-dir=\"/tmp/agy-chrome-cdp-profile\" \\\n")
	b.WriteString("         \"https://gemini.google.com\"\n")
	b.WriteString("     Linux:\n")
	b.WriteString("       google-chrome --remote-debugging-port=9222 --user-data-dir=\"/tmp/agy-chrome-cdp-profile\" \"https://gemini.google.com\"\n\n")
	b.WriteString("   После открытия окна выполните вход в свой Google-аккаунт на https://gemini.google.com.\n\n")
	b.WriteString("2. Обновление файла cookies вручную:\n")
	b.WriteString("   Экспортируйте актуальные куки (__Secure-1PSID, __Secure-1PSIDTS) для домена gemini.google.com\n")
	b.WriteString("   в файл ~/.config/agy-gemini-web/cookies.json.\n\n")
	if err != nil {
		b.WriteString(fmt.Sprintf("Детали ошибки: %v\n", err))
	}
	return b.String()
}

func (h *ServerHandler) handlePromptTemplate(model, defaultEffort string, req mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
	args := req.Params.Arguments
	userPrompt := args["prompt"]
	effort := args["effort"]
	if effort == "" {
		effort = defaultEffort
	}
	imagePaths := args["image_paths"]

	var text strings.Builder
	text.WriteString(fmt.Sprintf("Вызови MCP-инструмент `gemini_pro_web_ask` со следующими параметрами:\n"))
	text.WriteString(fmt.Sprintf("- `model`: %q\n", model))
	text.WriteString(fmt.Sprintf("- `effort`: %q\n", effort))
	if imagePaths != "" {
		text.WriteString(fmt.Sprintf("- `images`: [%s]\n", imagePaths))
	}
	text.WriteString(fmt.Sprintf("- `prompt`: %s\n\n", userPrompt))
	text.WriteString("Верни ответ модели без сокращений, сохраняя блок рассуждений (Thinking Process) в свернутом виде.")

	msg := mcp.NewPromptMessage(mcp.RoleUser, mcp.NewTextContent(text.String()))
	return mcp.NewGetPromptResult(fmt.Sprintf("Execute %s with effort %s", model, effort), []mcp.PromptMessage{msg}), nil
}
