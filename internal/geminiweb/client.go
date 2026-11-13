package geminiweb

import (
	"context"
	"fmt"
	"io"
	"math/rand"
	"strings"
	"sync"
	"time"

	fhttp "github.com/bogdanfinn/fhttp"

	"agy-gemini-web-mcp/internal/auth"
	"agy-gemini-web-mcp/internal/config"
)

// Client sends prompt and multimodal generation requests to Gemini Web RPC.
type Client struct {
	session *auth.SessionManager
	mu      sync.RWMutex
	convs   map[string]ConversationContext
}

// NewClient creates a new Gemini Web RPC Client.
func NewClient(session *auth.SessionManager) *Client {
	return &Client{
		session: session,
		convs:   make(map[string]ConversationContext),
	}
}

// GetConversationContext returns cached conversation context or empty.
func (c *Client) GetConversationContext(convID string) ConversationContext {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.convs[convID]
}

// SaveConversationContext stores conversation context for continuation.
func (c *Client) SaveConversationContext(convID string, ctx ConversationContext) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.convs[convID] = ctx
}

// StreamGenerateRequest holds all parameters for a generation call.
type StreamGenerateRequest struct {
	ModelConfig     *config.ModelConfig
	Prompt          string
	Attachments     []ScottyAttachment
	ConversationID  string
	CustomTimeout   time.Duration
}

// Generate executes the RPC call and returns the parsed response.
func (c *Client) Generate(ctx context.Context, req StreamGenerateRequest) (*ParsedResponse, error) {
	timeout := req.ModelConfig.Timeout
	if req.CustomTimeout > 0 {
		timeout = req.CustomTimeout
	}

	callCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	resp, err := c.doGenerate(callCtx, req, false)
	if err != nil && (strings.Contains(err.Error(), "401") || strings.Contains(err.Error(), "403") || strings.Contains(err.Error(), "CSRF")) {
		// Session might be expired, force refresh WizData and retry once
		_, refreshErr := c.session.GetWizData(callCtx, true)
		if refreshErr == nil {
			return c.doGenerate(callCtx, req, true)
		}
	}

	return resp, err
}

func (c *Client) doGenerate(ctx context.Context, req StreamGenerateRequest, isRetry bool) (*ParsedResponse, error) {
	wiz, err := c.session.GetWizData(ctx, isRetry)
	if err != nil {
		return nil, fmt.Errorf("retrieve session auth tokens: %w", err)
	}

	var convCtx ConversationContext
	if req.ConversationID != "" {
		convCtx = c.GetConversationContext(req.ConversationID)
		if convCtx.ConversationID == "" {
			convCtx.ConversationID = req.ConversationID
		}
	}

	body, err := BuildStreamGenerateBody(req.Prompt, req.Attachments, convCtx, req.ModelConfig.EffortTier, wiz.At)
	if err != nil {
		return nil, fmt.Errorf("build request payload: %w", err)
	}

	// If Chrome CDP is active, execute directly in browser context (bypasses cookie rotation/fingerprint 401s)
	if c.session.CDPURL() != "" {
		respBody, bridgeErr := c.session.BridgeGenerate(ctx, body, wiz.Bl, wiz.Sid)
		if bridgeErr == nil {
			parsed, parseErr := ParseStream(strings.NewReader(respBody))
			if parseErr == nil && parsed != nil {
				if parsed.ConversationID != "" {
					c.SaveConversationContext(parsed.ConversationID, ConversationContext{
						ConversationID: parsed.ConversationID,
						ResponseID:     parsed.ResponseID,
						ChoiceID:       parsed.ChoiceID,
					})
				}
				return parsed, nil
			}
		}
	}

	reqID := 100000 + rand.Intn(900000)
	targetURL := fmt.Sprintf("https://gemini.google.com/_/BardChatUi/data/assistant.lamda.BardFrontendService/StreamGenerate?bl=%s&_reqid=%d&rt=c", wiz.Bl, reqID)
	if wiz.Sid != "" {
		targetURL += fmt.Sprintf("&f.sid=%s", wiz.Sid)
	}

	httpReq, err := fhttp.NewRequestWithContext(ctx, "POST", targetURL, strings.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create stream generate request: %w", err)
	}

	cookieHdr := c.session.Store().CookieHeader()
	if cookieHdr != "" {
		httpReq.Header.Set("Cookie", cookieHdr)
	}

	httpReq.Header.Set("Content-Type", "application/x-www-form-urlencoded;charset=UTF-8")
	httpReq.Header.Set("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36")
	httpReq.Header.Set("Origin", "https://gemini.google.com")
	reqReferer := "https://gemini.google.com/"
	if convCtx.ConversationID != "" {
		reqReferer = fmt.Sprintf("https://gemini.google.com/app/%s", convCtx.ConversationID)
	}
	httpReq.Header.Set("Referer", reqReferer)
	httpReq.Header.Set("X-Same-Domain", "1")

	// Model & Effort Extension Header
	extHeader := BuildExtensionHeader(req.ModelConfig.ModelHexID, req.ModelConfig.EffortTier)
	httpReq.Header.Set("x-goog-ext-525001261-jspb", extHeader)

	httpResp, err := c.session.TLSClient().Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("send request: %w", err)
	}
	defer httpResp.Body.Close()

	if httpResp.StatusCode != 200 {
		respSnippet, _ := io.ReadAll(io.LimitReader(httpResp.Body, 512))
		return nil, fmt.Errorf("unexpected status code %d from Gemini Web RPC: %s", httpResp.StatusCode, string(respSnippet))
	}

	parsed, err := ParseStream(httpResp.Body)
	if err != nil {
		return nil, fmt.Errorf("parse stream response: %w", err)
	}

	// Update cookie store if response returned new cookies
	for _, cookie := range httpResp.Cookies() {
		if cookie.Value != "" {
			c.session.Store().Set(cookie.Name, cookie.Value)
		}
	}

	// Store conversation context for continuation
	if parsed.ConversationID != "" {
		savedCtx := ConversationContext{
			ConversationID: parsed.ConversationID,
			ResponseID:     parsed.ResponseID,
			ChoiceID:       parsed.ChoiceID,
		}
		c.SaveConversationContext(parsed.ConversationID, savedCtx)
		if req.ConversationID != "" && req.ConversationID != parsed.ConversationID {
			c.SaveConversationContext(req.ConversationID, savedCtx)
		}
	}

	return parsed, nil
}
