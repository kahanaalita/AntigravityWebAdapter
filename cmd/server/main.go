package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/mark3labs/mcp-go/server"

	"agy-gemini-web-mcp/commands"
	"agy-gemini-web-mcp/internal/auth"
	"agy-gemini-web-mcp/internal/config"
	"agy-gemini-web-mcp/internal/geminiweb"
	"agy-gemini-web-mcp/internal/mcp"
	"agy-gemini-web-mcp/internal/media"
)

func main() {
	var (
		cookieStorePath       string
		cdpURL                string
		workDir               string
		defaultHydrogenEffort string
		defaultArgonEffort    string
		hydrogenHexID         string
		argonHexID            string
		rotateMinutes         int
		installCommands       bool
		harvestCookies        bool
		promptArg             string
		modelArg              string
		effortArg             string
	)

	defaultCookiePath := os.Getenv("COOKIE_STORE_PATH")
	if defaultCookiePath == "" {
		homeDir, _ := os.UserHomeDir()
		defaultCookiePath = filepath.Join(homeDir, ".config", "agy-gemini-web", "cookies.json")
	}

	defaultCDP := os.Getenv("CHROME_CDP_URL")
	if defaultCDP == "" {
		defaultCDP = "http://127.0.0.1:9222"
	}

	defaultWorkDir := os.Getenv("ANTIGRAVITY_WORKSPACE_ROOT")
	if defaultWorkDir == "" {
		defaultWorkDir = os.Getenv("WORKSPACE_DIR")
	}

	flag.StringVar(&cookieStorePath, "cookie-store", defaultCookiePath, "Path to cookies.json storage")
	flag.StringVar(&cdpURL, "cdp-url", defaultCDP, "Chrome DevTools Protocol URL for session harvest")
	flag.StringVar(&workDir, "work-dir", defaultWorkDir, "Working directory for relative path resolution")
	flag.StringVar(&defaultHydrogenEffort, "default-hydrogen-effort", "middle", "Default effort for Hydrogen 4 (low|middle|high)")
	flag.StringVar(&defaultArgonEffort, "default-argon-effort", "medium", "Default effort for Argon 4 (low|medium|high)")
	flag.StringVar(&hydrogenHexID, "hydrogen-model-id", "hydrogen-4", "Model ID / Hex ID for Hydrogen 4")
	flag.StringVar(&argonHexID, "argon-model-id", "argon-4", "Model ID / Hex ID for Argon 4")
	flag.IntVar(&rotateMinutes, "rotate-interval", 8, "Interval in minutes for background cookie rotation")
	flag.BoolVar(&installCommands, "install-commands", false, "Install hydrogen.toml and argon.toml slash commands to Antigravity commands directory")
	flag.BoolVar(&harvestCookies, "harvest-cookies", false, "Connect to Chrome CDP immediately to harvest session cookies and exit")
	flag.StringVar(&promptArg, "prompt", "", "Direct prompt execution mode (bypass MCP stdio)")
	flag.StringVar(&modelArg, "model", "hydrogen-4", "Model for direct prompt execution")
	flag.StringVar(&effortArg, "effort", "middle", "Effort for direct prompt execution")
	flag.Parse()

	if workDir == "" {
		if envRoot := os.Getenv("ANTIGRAVITY_WORKSPACE_ROOT"); envRoot != "" {
			workDir = envRoot
		} else if envDir := os.Getenv("WORKSPACE_DIR"); envDir != "" {
			workDir = envDir
		}
	}

	if installCommands {
		if err := runInstallCommands(workDir); err != nil {
			log.Fatalf("Error installing commands: %v", err)
		}
		os.Exit(0)
	}

	session, err := auth.NewSessionManager(cookieStorePath, cdpURL)
	if err != nil {
		log.Fatalf("Failed to initialize session manager: %v", err)
	}

	if harvestCookies {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		fmt.Printf("Attempting cookie harvest from CDP at %s...\n", cdpURL)
		if err := session.HarvestCookiesFromCDP(ctx); err != nil {
			log.Fatalf("Harvest failed: %v", err)
		}
		fmt.Printf("Successfully harvested cookies into %s\n", cookieStorePath)
		os.Exit(0)
	}

	if promptArg != "" {
		client := geminiweb.NewClient(session)
		scotty := media.NewScottyUploader(session)
		resolver := media.NewImageResolver(workDir)
		customHexIDs := map[config.ModelType]string{
			config.ModelHydrogen4: hydrogenHexID,
			config.ModelArgon4:    argonHexID,
		}
		modelCfg, err := config.ResolveModelConfig(modelArg, effortArg, customHexIDs)
		if err != nil {
			log.Fatalf("Invalid model/effort: %v", err)
		}
		cleanedPrompt, resolvedImages, err := resolver.ResolveFromPromptAndList(promptArg, nil)
		if err != nil {
			log.Fatalf("Resolve images error: %v", err)
		}
		var attachments []geminiweb.ScottyAttachment
		if len(resolvedImages) > 0 {
			ctxUpload, cancelUpload := context.WithTimeout(context.Background(), 60*time.Second)
			defer cancelUpload()
			attList, err := scotty.UploadImages(ctxUpload, resolvedImages)
			if err != nil {
				log.Fatalf("Scotty image upload: %v", err)
			}
			attachments = attList
		}
		ctxGen, cancelGen := context.WithTimeout(context.Background(), modelCfg.Timeout)
		defer cancelGen()
		resp, err := client.Generate(ctxGen, geminiweb.StreamGenerateRequest{
			ModelConfig: modelCfg,
			Prompt:      cleanedPrompt,
			Attachments: attachments,
		})
		if err != nil {
			log.Fatalf("Generate error: %v", err)
		}
		fmt.Println(resp.FormattedOutput())
		os.Exit(0)
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	// Launch background cookie rotator
	rotator := auth.NewCookieRotator(session, time.Duration(rotateMinutes)*time.Minute)
	rotator.Start(ctx)
	defer rotator.Stop()

	// Initialize components
	client := geminiweb.NewClient(session)
	scotty := media.NewScottyUploader(session)
	resolver := media.NewImageResolver(workDir)

	customHexIDs := map[config.ModelType]string{
		config.ModelHydrogen4: hydrogenHexID,
		config.ModelArgon4:    argonHexID,
	}

	handler := mcp.NewServerHandler(client, scotty, resolver, customHexIDs)

	s := server.NewMCPServer(
		"google-ai-pro-web",
		"1.0.0",
		server.WithToolCapabilities(true),
		server.WithPromptCapabilities(true),
	)

	handler.Register(s)

	if err := server.ServeStdio(s); err != nil {
		log.Printf("Server exited: %v", err)
	}
}

func runInstallCommands(workDir string) error {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return err
	}

	destDirs := []string{
		filepath.Join(homeDir, ".antigravity", "commands"),
		filepath.Join(homeDir, ".gemini", "commands"),
	}

	if workDir != "" {
		destDirs = append(destDirs, filepath.Join(workDir, ".antigravity", "commands"))
	}

	for _, dir := range destDirs {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return fmt.Errorf("create dir %s: %w", dir, err)
		}

		hPath := filepath.Join(dir, "hydrogen.toml")
		if err := os.WriteFile(hPath, []byte(commands.HydrogenToml), 0644); err != nil {
			return fmt.Errorf("write %s: %w", hPath, err)
		}

		aPath := filepath.Join(dir, "argon.toml")
		if err := os.WriteFile(aPath, []byte(commands.ArgonToml), 0644); err != nil {
			return fmt.Errorf("write %s: %w", aPath, err)
		}

		fmt.Printf("✓ Installed hydrogen.toml and argon.toml to %s\n", dir)
	}

	execPath, _ := os.Executable()
	fmt.Printf("\nДля подключения сервера добавьте следующую конфигурацию в settings.json:\n\n")
	fmt.Printf(`{
  "mcpServers": {
    "google-ai-pro-web": {
      "command": %q,
      "args": ["--cookie-store", "%s/.config/agy-gemini-web/cookies.json"],
      "env": {
        "CHROME_CDP_URL": "http://127.0.0.1:9222",
        "DEFAULT_HYDROGEN_EFFORT": "middle",
        "DEFAULT_ARGON_EFFORT": "medium"
      },
      "timeoutSeconds": 600
    }
  }
}
`, execPath, homeDir)

	return nil
}
