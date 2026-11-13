package auth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	fhttp "github.com/bogdanfinn/fhttp"
	tls_client "github.com/bogdanfinn/tls-client"
	"github.com/bogdanfinn/tls-client/profiles"
)

var (
	reSNlM0e = regexp.MustCompile(`"SNlM0e":\s*"([^"]+)"`)
	reCfb2h  = regexp.MustCompile(`"cfb2h":\s*"([^"]+)"`)
	reFdrFJe = regexp.MustCompile(`"FdrFJe":\s*"([^"]+)"`)
)

// WizData holds dynamic tokens extracted from gemini.google.com/app.
type WizData struct {
	At  string // SNlM0e
	Bl  string // cfb2h
	Sid string // FdrFJe
}

// CookieStore manages session cookies with disk persistence.
type CookieStore struct {
	mu      sync.RWMutex
	path    string
	cookies map[string]string
}

// NewCookieStore creates a new CookieStore.
func NewCookieStore(path string) *CookieStore {
	return &CookieStore{
		path:    path,
		cookies: make(map[string]string),
	}
}

// Load loads cookies from file supporting key-value JSON, array of cookie objects, or raw cookie strings.
func (cs *CookieStore) Load() error {
	cs.mu.Lock()
	defer cs.mu.Unlock()

	if cs.path == "" {
		return errors.New("cookie path is empty")
	}

	data, err := os.ReadFile(cs.path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("read cookie file: %w", err)
	}

	trimmed := strings.TrimSpace(string(data))
	if trimmed == "" {
		return nil
	}

	// Try 1: Map[string]string {"__Secure-1PSID": "..."}
	var kvMap map[string]string
	if err := json.Unmarshal(data, &kvMap); err == nil && len(kvMap) > 0 {
		cs.cookies = kvMap
		return nil
	}

	// Try 2: Array of cookie objects [{"name": "...", "value": "..."}, ...]
	type cookieObj struct {
		Name  string `json:"name"`
		Value string `json:"value"`
	}
	var cookieArr []cookieObj
	if err := json.Unmarshal(data, &cookieArr); err == nil && len(cookieArr) > 0 {
		for _, c := range cookieArr {
			if c.Name != "" {
				cs.cookies[c.Name] = c.Value
			}
		}
		return nil
	}

	// Try 3: Raw cookie string format: key1=val1; key2=val2
	parts := strings.Split(trimmed, ";")
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		eq := strings.Index(p, "=")
		if eq > 0 {
			k := strings.TrimSpace(p[:eq])
			v := strings.TrimSpace(p[eq+1:])
			cs.cookies[k] = v
		}
	}

	return nil
}

// Save atomically writes cookies to the JSON file.
func (cs *CookieStore) Save() error {
	cs.mu.RLock()
	defer cs.mu.RUnlock()

	if cs.path == "" {
		return nil
	}

	dir := filepath.Dir(cs.path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("create cookie dir: %w", err)
	}

	data, err := json.MarshalIndent(cs.cookies, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal cookies: %w", err)
	}

	tmpFile := fmt.Sprintf("%s.tmp.%d", cs.path, time.Now().UnixNano())
	if err := os.WriteFile(tmpFile, data, 0600); err != nil {
		return fmt.Errorf("write tmp cookie file: %w", err)
	}

	if err := os.Rename(tmpFile, cs.path); err != nil {
		_ = os.Remove(tmpFile)
		return fmt.Errorf("rename cookie file: %w", err)
	}

	return nil
}

// Set sets a cookie.
func (cs *CookieStore) Set(name, value string) {
	cs.mu.Lock()
	defer cs.mu.Unlock()
	cs.cookies[name] = value
}

// Get gets a cookie.
func (cs *CookieStore) Get(name string) (string, bool) {
	cs.mu.RLock()
	defer cs.mu.RUnlock()
	val, ok := cs.cookies[name]
	return val, ok
}

// All returns a copy of all cookies.
func (cs *CookieStore) All() map[string]string {
	cs.mu.RLock()
	defer cs.mu.RUnlock()
	res := make(map[string]string, len(cs.cookies))
	for k, v := range cs.cookies {
		res[k] = v
	}
	return res
}

// CookieHeader formats cookies as an HTTP Cookie header string.
func (cs *CookieStore) CookieHeader() string {
	cs.mu.RLock()
	defer cs.mu.RUnlock()
	var b strings.Builder
	first := true
	for k, v := range cs.cookies {
		if !first {
			b.WriteString("; ")
		}
		b.WriteString(k)
		b.WriteString("=")
		b.WriteString(v)
		first = false
	}
	return b.String()
}

// SessionManager manages web session tokens and HTTP client.
type SessionManager struct {
	mu           sync.RWMutex
	store        *CookieStore
	httpClient   tls_client.HttpClient
	wizData      *WizData
	cdpURL       string
	chromePath   string
	lastWizFetch time.Time
}

// NewSessionManager creates a SessionManager.
func NewSessionManager(cookiePath, cdpURL string) (*SessionManager, error) {
	jar := tls_client.NewCookieJar()
	options := []tls_client.HttpClientOption{
		tls_client.WithTimeoutSeconds(120),
		tls_client.WithClientProfile(profiles.Chrome_131),
		tls_client.WithCookieJar(jar),
	}

	client, err := tls_client.NewHttpClient(tls_client.NewNoopLogger(), options...)
	if err != nil {
		return nil, fmt.Errorf("init tls client: %w", err)
	}

	store := NewCookieStore(cookiePath)
	if err := store.Load(); err != nil {
		// Log or ignore if file doesn't exist yet
	}

	return &SessionManager{
		store:      store,
		httpClient: client,
		cdpURL:     cdpURL,
	}, nil
}

// Store returns the underlying CookieStore.
func (s *SessionManager) CDPURL() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.cdpURL
}

func (s *SessionManager) Store() *CookieStore {
	return s.store
}

// TLSClient returns the underlying tls_client.HttpClient.
func (s *SessionManager) TLSClient() tls_client.HttpClient {
	return s.httpClient
}

// ParseWizGlobalData extracts SNlM0e, cfb2h, and FdrFJe from HTML.
func ParseWizGlobalData(html string) (*WizData, error) {
	atMatch := reSNlM0e.FindStringSubmatch(html)
	if len(atMatch) < 2 {
		return nil, errors.New("SNlM0e (CSRF token) not found in response HTML")
	}

	data := &WizData{
		At: atMatch[1],
	}

	blMatch := reCfb2h.FindStringSubmatch(html)
	if len(blMatch) >= 2 {
		data.Bl = blMatch[1]
	} else {
		data.Bl = "boq_assistant-bard-web-server_20261113.00_p0"
	}

	sidMatch := reFdrFJe.FindStringSubmatch(html)
	if len(sidMatch) >= 2 {
		data.Sid = sidMatch[1]
	}

	return data, nil
}

// GetWizData returns cached or fresh WizData.
func (s *SessionManager) GetWizData(ctx context.Context, forceRefresh bool) (*WizData, error) {
	s.mu.RLock()
	if !forceRefresh && s.wizData != nil && time.Since(s.lastWizFetch) < 30*time.Minute {
		data := s.wizData
		s.mu.RUnlock()
		return data, nil
	}
	s.mu.RUnlock()

	return s.RefreshWizData(ctx)
}

// RefreshWizData queries https://gemini.google.com/app or Chrome CDP to extract SNlM0e, cfb2h, and FdrFJe.
func (s *SessionManager) RefreshWizData(ctx context.Context) (*WizData, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Try Chrome CDP first if available — it holds live authenticated tokens from browser tab
	if s.cdpURL != "" {
		if wiz, err := s.FetchWizDataViaCDP(ctx); err == nil && wiz != nil && wiz.At != "" {
			s.wizData = wiz
			s.lastWizFetch = time.Now()
			return wiz, nil
		}
	}

	req, err := fhttp.NewRequestWithContext(ctx, "GET", "https://gemini.google.com/app", nil)
	if err != nil {
		return nil, err
	}

	cookieHdr := s.store.CookieHeader()
	if cookieHdr != "" {
		req.Header.Set("Cookie", cookieHdr)
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36")
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,image/avif,image/webp,*/*;q=0.8")
	req.Header.Set("Accept-Language", "en-US,en;q=0.9,ru;q=0.8")
	req.Header.Set("Sec-Fetch-Dest", "document")
	req.Header.Set("Sec-Fetch-Mode", "navigate")
	req.Header.Set("Sec-Fetch-Site", "none")
	req.Header.Set("Sec-Fetch-User", "?1")

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch gemini app: %w", err)
	}
	defer resp.Body.Close()

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read gemini response body: %w", err)
	}
	html := string(bodyBytes)

	// Update any cookies returned in response
	for _, c := range resp.Cookies() {
		s.store.Set(c.Name, c.Value)
	}
	_ = s.store.Save()

	wiz, err := ParseWizGlobalData(html)
	if err != nil {
		// If cookies expired or redirected to accounts.google.com, attempt CDP harvest
		if s.cdpURL != "" {
			if cdpErr := s.harvestCookiesFromCDPLocked(ctx); cdpErr == nil {
				// Retry with freshly harvested cookies
				return s.refreshWizDataWithNewCookies(ctx)
			}
		}
		return nil, fmt.Errorf("parse WIZ_global_data (status %d): %w", resp.StatusCode, err)
	}

	s.wizData = wiz
	s.lastWizFetch = time.Now()
	return wiz, nil
}

func (s *SessionManager) refreshWizDataWithNewCookies(ctx context.Context) (*WizData, error) {
	req, err := fhttp.NewRequestWithContext(ctx, "GET", "https://gemini.google.com/app", nil)
	if err != nil {
		return nil, err
	}

	cookieHdr := s.store.CookieHeader()
	if cookieHdr != "" {
		req.Header.Set("Cookie", cookieHdr)
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36")
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("retry fetch gemini app: %w", err)
	}
	defer resp.Body.Close()

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("retry read body: %w", err)
	}

	wiz, err := ParseWizGlobalData(string(bodyBytes))
	if err != nil {
		return nil, fmt.Errorf("retry parse WIZ_global_data: %w", err)
	}

	s.wizData = wiz
	s.lastWizFetch = time.Now()
	return wiz, nil
}
