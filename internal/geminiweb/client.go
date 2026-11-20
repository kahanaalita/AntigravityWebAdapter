package geminiweb

import (
	"container/list"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"math/rand"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	fhttp "github.com/bogdanfinn/fhttp"

	"agy-gemini-web-mcp/internal/auth"
	"agy-gemini-web-mcp/internal/config"
)

// ErrAuthenticationRequired indicates that Gemini session credentials have expired
// or authentication is required (e.g. redirected to accounts.google.com).
var ErrAuthenticationRequired = errors.New("authentication required: gemini session expired or redirected to accounts.google.com")

// isAuthError checks whether an error is caused by expired cookies or login redirection.
func isAuthError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, ErrAuthenticationRequired) {
		return true
	}
	msg := err.Error()
	return strings.Contains(msg, "accounts.google.com") ||
		strings.Contains(msg, "ServiceLogin") ||
		strings.Contains(msg, "session expired") ||
		strings.Contains(msg, "authentication failed") ||
		strings.Contains(msg, "ErrAuthenticationRequired") ||
		strings.Contains(msg, "AUTH_REQUIRED")
}

// ConversationEntry holds a cached conversation context with its last update timestamp.
type ConversationEntry struct {
	Key       string              `json:"key"`
	Context   ConversationContext `json:"context"`
	UpdatedAt time.Time           `json:"updated_at"`
}

type diskConversationData struct {
	Version       int                 `json:"version"`
	Conversations []ConversationEntry `json:"conversations"`
}

// ConversationStore is a thread-safe LRU cache with capacity limit, TTL expiration,
// and file persistence for multi-turn conversations across server restarts.
type ConversationStore struct {
	mu       sync.RWMutex
	capacity int
	ttl      time.Duration
	filePath string
	entries  map[string]*list.Element
	ll       *list.List
}

// NewConversationStore creates a new ConversationStore, optionally loading saved state from disk.
func NewConversationStore(capacity int, ttl time.Duration, filePath string) *ConversationStore {
	if capacity <= 0 {
		capacity = 1000
	}
	if ttl <= 0 {
		ttl = 7 * 24 * time.Hour
	}
	if filePath == "" {
		filePath = defaultConversationsPath()
	} else {
		filePath = expandPath(filePath)
	}

	cs := &ConversationStore{
		capacity: capacity,
		ttl:      ttl,
		filePath: filePath,
		entries:  make(map[string]*list.Element),
		ll:       list.New(),
	}

	cs.loadFromFile()
	return cs
}

func defaultConversationsPath() string {
	if envPath := os.Getenv("AGY_CONVERSATIONS_FILE"); envPath != "" {
		return expandPath(envPath)
	}
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		home = "."
	}
	return filepath.Join(home, ".config", "agy-gemini-web", "conversations.json")
}

func expandPath(p string) string {
	if strings.HasPrefix(p, "~/") {
		if home, err := os.UserHomeDir(); err == nil && home != "" {
			return filepath.Join(home, p[2:])
		}
	}
	return p
}

// Get returns the conversation context for convID, if present and not expired.
func (cs *ConversationStore) Get(convID string) (ConversationContext, bool) {
	if convID == "" {
		return ConversationContext{}, false
	}

	cs.mu.Lock()
	defer cs.mu.Unlock()

	elem, exists := cs.entries[convID]
	if !exists {
		return ConversationContext{}, false
	}

	entry := elem.Value.(*ConversationEntry)
	if time.Since(entry.UpdatedAt) > cs.ttl {
		cs.removeElement(elem)
		return ConversationContext{}, false
	}

	cs.ll.MoveToFront(elem)
	return entry.Context, true
}

// Put saves or updates the conversation context for convID, evicting the oldest if capacity is reached.
func (cs *ConversationStore) Put(convID string, ctx ConversationContext) {
	if convID == "" {
		return
	}

	cs.mu.Lock()
	defer cs.mu.Unlock()

	now := time.Now()
	if elem, exists := cs.entries[convID]; exists {
		entry := elem.Value.(*ConversationEntry)
		entry.Context = ctx
		entry.UpdatedAt = now
		cs.ll.MoveToFront(elem)
	} else {
		for cs.ll.Len() >= cs.capacity {
			oldest := cs.ll.Back()
			if oldest != nil {
				cs.removeElement(oldest)
			} else {
				break
			}
		}
		entry := &ConversationEntry{
			Key:       convID,
			Context:   ctx,
			UpdatedAt: now,
		}
		elem := cs.ll.PushFront(entry)
		cs.entries[convID] = elem
	}

	cs.saveToFileLocked()
}

// Len returns the current number of cached conversations.
func (cs *ConversationStore) Len() int {
	cs.mu.RLock()
	defer cs.mu.RUnlock()
	return cs.ll.Len()
}

func (cs *ConversationStore) removeElement(elem *list.Element) {
	entry := elem.Value.(*ConversationEntry)
	delete(cs.entries, entry.Key)
	cs.ll.Remove(elem)
}

func (cs *ConversationStore) saveToFileLocked() {
	if cs.filePath == "" {
		return
	}

	var listToSave []ConversationEntry
	now := time.Now()
	for elem := cs.ll.Front(); elem != nil; elem = elem.Next() {
		entry := elem.Value.(*ConversationEntry)
		if now.Sub(entry.UpdatedAt) <= cs.ttl {
			listToSave = append(listToSave, *entry)
		}
	}

	data := diskConversationData{
		Version:       1,
		Conversations: listToSave,
	}

	bytes, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		log.Printf("[WARN] Failed to marshal conversation store: %v", err)
		return
	}

	dir := filepath.Dir(cs.filePath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		log.Printf("[WARN] Failed to create directory for conversation store %s: %v", dir, err)
		return
	}

	tmpFile := fmt.Sprintf("%s.tmp.%d", cs.filePath, time.Now().UnixNano())
	if err := os.WriteFile(tmpFile, bytes, 0600); err != nil {
		log.Printf("[WARN] Failed to write temporary conversation store: %v", err)
		return
	}

	if err := os.Rename(tmpFile, cs.filePath); err != nil {
		_ = os.Remove(tmpFile)
		log.Printf("[WARN] Failed to replace conversation store file %s: %v", cs.filePath, err)
	}
}

func (cs *ConversationStore) loadFromFile() {
	if cs.filePath == "" {
		return
	}

	bytes, err := os.ReadFile(cs.filePath)
	if err != nil {
		if !os.IsNotExist(err) {
			log.Printf("[WARN] Failed to read conversation store %s: %v", cs.filePath, err)
		}
		return
	}

	var data diskConversationData
	if err := json.Unmarshal(bytes, &data); err != nil {
		log.Printf("[WARN] Failed to unmarshal conversation store %s: %v", cs.filePath, err)
		return
	}

	now := time.Now()
	// Reverse iteration so that the newest entries end up at the front of the list
	for i := len(data.Conversations) - 1; i >= 0; i-- {
		item := data.Conversations[i]
		if item.Key == "" {
			continue
		}
		if now.Sub(item.UpdatedAt) > cs.ttl {
			continue
		}
		if cs.ll.Len() >= cs.capacity {
			continue
		}
		entryCopy := item
		elem := cs.ll.PushFront(&entryCopy)
		cs.entries[entryCopy.Key] = elem
	}
}

// Client sends prompt and multimodal generation requests to Gemini Web RPC.
type Client struct {
	session   *auth.SessionManager
	convStore *ConversationStore
}

// NewClient creates a new Gemini Web RPC Client with default conversation store.
func NewClient(session *auth.SessionManager) *Client {
	return NewClientWithStore(session, NewConversationStore(1000, 7*24*time.Hour, ""))
}

// NewClientWithStore creates a new Client with a custom ConversationStore.
func NewClientWithStore(session *auth.SessionManager, store *ConversationStore) *Client {
	if store == nil {
		store = NewConversationStore(1000, 7*24*time.Hour, "")
	}
	return &Client{
		session:   session,
		convStore: store,
	}
}

// ConvStore returns the underlying ConversationStore.
func (c *Client) ConvStore() *ConversationStore {
	return c.convStore
}

// GetConversationContext returns cached conversation context or empty.
func (c *Client) GetConversationContext(convID string) ConversationContext {
	if c.convStore == nil {
		return ConversationContext{}
	}
	ctx, _ := c.convStore.Get(convID)
	return ctx
}

// SaveConversationContext stores conversation context for continuation.
func (c *Client) SaveConversationContext(convID string, ctx ConversationContext) {
	if c.convStore != nil {
		c.convStore.Put(convID, ctx)
	}
}

// StreamGenerateRequest holds all parameters for a generation call.
type StreamGenerateRequest struct {
	ModelConfig    *config.ModelConfig
	Prompt         string
	Attachments    []ScottyAttachment
	ConversationID string
	CustomTimeout  time.Duration
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
	if err != nil && (strings.Contains(err.Error(), "401") || strings.Contains(err.Error(), "403") || strings.Contains(err.Error(), "CSRF") || strings.Contains(err.Error(), "302") || isAuthError(err)) {
		// Do not retry on explicit Google API errors, rate limits, or browser 429/403
		if c.session != nil && !strings.Contains(err.Error(), "google api error") && !strings.Contains(err.Error(), "RESOURCE_EXHAUSTED") && !strings.Contains(err.Error(), "HTTP 429") && !strings.Contains(err.Error(), "429") {
			// Session might be expired, auto harvest fresh cookies and retry
			_ = c.session.AutoHarvest(callCtx)
			_, refreshErr := c.session.GetWizData(callCtx, true)
			if refreshErr == nil {
				resp, err = c.doGenerate(callCtx, req, true)
			}
		}
	}

	if err != nil && isAuthError(err) && !errors.Is(err, ErrAuthenticationRequired) {
		err = fmt.Errorf("%w: %v. Please re-authenticate at https://gemini.google.com or start Chrome with --remote-debugging-port=9222", ErrAuthenticationRequired, err)
	}

	return resp, err
}

func (c *Client) doGenerate(ctx context.Context, req StreamGenerateRequest, isRetry bool) (*ParsedResponse, error) {
	if c.session == nil {
		return nil, ErrAuthenticationRequired
	}
	wiz, err := c.session.GetWizData(ctx, isRetry)
	if err != nil {
		if isAuthError(err) {
			return nil, fmt.Errorf("%w: %v. Please re-authenticate at https://gemini.google.com or start Chrome with --remote-debugging-port=9222", ErrAuthenticationRequired, err)
		}
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

	// Model & Effort Extension Header
	extHeader := BuildExtensionHeader(req.ModelConfig.ModelHexID, req.ModelConfig.EffortTier)

	// If Chrome CDP is active, execute directly in browser context (bypasses cookie rotation/fingerprint 401s)
	cdpAttempted := false
	if c.session.CDPURL() != "" {
		cdpAttempted = true
		timeout := req.ModelConfig.Timeout
		if req.CustomTimeout > 0 {
			timeout = req.CustomTimeout
		}
		respBody, bridgeErr := c.session.BridgeGenerate(ctx, auth.BridgeGenerateParams{
			Body:            body,
			Bl:              wiz.Bl,
			Sid:             wiz.Sid,
			ExtensionHeader: extHeader,
			Timeout:         timeout,
		})
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
			// If parser returned explicit Google API / quota error, do not fallback to TLS client
			if parseErr != nil && (strings.Contains(parseErr.Error(), "google api error") || strings.Contains(parseErr.Error(), "RESOURCE_EXHAUSTED") || strings.Contains(parseErr.Error(), "429")) {
				return nil, parseErr
			}
		} else {
			// If CDP error contains HTTP 429/403 or google api error, do not fallback to TLS client
			errMsg := bridgeErr.Error()
			if strings.Contains(errMsg, "HTTP 429") || strings.Contains(errMsg, "HTTP 403") || strings.Contains(errMsg, "google api error") || strings.Contains(errMsg, "429") || strings.Contains(errMsg, "403") {
				return nil, bridgeErr
			}
		}
	}

	// Synchronization of token 'at' on fallback from CDP Bridge to TLS Client:
	// When CDP Bridge failed, the session tokens might have drifted.
	// Force refresh wiz data and reconstruct body with the latest CSRF token.
	if cdpAttempted {
		if freshWiz, refreshErr := c.session.GetWizData(ctx, true); refreshErr == nil && freshWiz != nil {
			wiz = freshWiz
			if freshBody, bodyErr := BuildStreamGenerateBody(req.Prompt, req.Attachments, convCtx, req.ModelConfig.EffortTier, wiz.At); bodyErr == nil {
				body = freshBody
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
	httpReq.Header.Set("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.5 Safari/605.1.15")
	httpReq.Header.Set("Origin", "https://gemini.google.com")
	reqReferer := "https://gemini.google.com/"
	if convCtx.ConversationID != "" {
		reqReferer = fmt.Sprintf("https://gemini.google.com/app/%s", convCtx.ConversationID)
	}
	httpReq.Header.Set("Referer", reqReferer)
	httpReq.Header.Set("X-Same-Domain", "1")
	httpReq.Header.Set("x-goog-ext-525001261-jspb", extHeader)

	httpResp, err := c.session.TLSClient().Do(httpReq)
	if err != nil {
		if isAuthError(err) {
			return nil, fmt.Errorf("%w: %v. Please re-authenticate at https://gemini.google.com", ErrAuthenticationRequired, err)
		}
		return nil, fmt.Errorf("send request: %w", err)
	}
	defer httpResp.Body.Close()

	if httpResp.StatusCode >= 300 && httpResp.StatusCode < 400 {
		loc := httpResp.Header.Get("Location")
		if strings.Contains(loc, "accounts.google.com") || strings.Contains(loc, "ServiceLogin") {
			return nil, fmt.Errorf("%w: redirected to %s. Please re-authenticate at https://gemini.google.com", ErrAuthenticationRequired, loc)
		}
	}

	if httpResp.StatusCode == 401 || httpResp.StatusCode == 403 {
		respSnippet, _ := io.ReadAll(io.LimitReader(httpResp.Body, 512))
		return nil, fmt.Errorf("%w: status %d from Gemini Web RPC: %s. Please re-authenticate at https://gemini.google.com", ErrAuthenticationRequired, httpResp.StatusCode, string(respSnippet))
	}

	if httpResp.StatusCode != 200 {
		respSnippet, _ := io.ReadAll(io.LimitReader(httpResp.Body, 512))
		return nil, fmt.Errorf("unexpected status code %d from Gemini Web RPC: %s", httpResp.StatusCode, string(respSnippet))
	}

	parsed, err := ParseStream(httpResp.Body)
	if err != nil {
		return nil, fmt.Errorf("parse stream response: %w", err)
	}

	// Update cookie store if response returned new cookies
	var cookiesUpdated bool
	for _, cookie := range httpResp.Cookies() {
		if cookie.Value != "" {
			c.session.Store().Set(cookie.Name, cookie.Value)
			cookiesUpdated = true
		}
	}
	if cookiesUpdated {
		_ = c.session.Store().Save()
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
