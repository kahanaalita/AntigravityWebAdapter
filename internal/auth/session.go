package auth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
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
	baseURL      string
	chromePath   string
	lastWizFetch time.Time
	cdpSem       chan struct{}
}

// createDialer creates a net.Dialer with a 30-second timeout.
// By default, it uses system DNS resolution.
// If customDNS is specified (e.g. from CUSTOM_DNS_RESOLVER env var),
// it resolves via that server with fallback to the default resolver.
func createDialer(customDNS string) net.Dialer {
	dialer := net.Dialer{
		Timeout: 30 * time.Second,
	}

	if customDNS = strings.TrimSpace(customDNS); customDNS != "" {
		dnsAddr := customDNS
		if !strings.Contains(dnsAddr, ":") {
			dnsAddr = net.JoinHostPort(dnsAddr, "53")
		}

		dialer.Resolver = &net.Resolver{
			PreferGo: true,
			Dial: func(ctx context.Context, network, address string) (net.Conn, error) {
				d := net.Dialer{Timeout: 5 * time.Second}
				conn, err := d.DialContext(ctx, "udp", dnsAddr)
				if err != nil {
					// Fallback to default resolver
					if net.DefaultResolver != nil && net.DefaultResolver.Dial != nil {
						return net.DefaultResolver.Dial(ctx, network, address)
					}
					return d.DialContext(ctx, network, address)
				}
				return conn, nil
			},
		}
	}

	return dialer
}

// NewSessionManager creates a SessionManager.
func NewSessionManager(cookiePath, cdpURL string) (*SessionManager, error) {
	jar := tls_client.NewCookieJar()

	dialer := createDialer(os.Getenv("CUSTOM_DNS_RESOLVER"))

	// 1260 секунд (21 минута) — 20 минут серверного лимита потока + 60 секунд буфер на сетевые задержки
	timeoutSeconds := 1260
	if v := os.Getenv("TLS_CLIENT_TIMEOUT"); v != "" {
		if sec, err := strconv.Atoi(v); err == nil && sec > 0 {
			timeoutSeconds = sec
		}
	}

	options := []tls_client.HttpClientOption{
		tls_client.WithTimeoutSeconds(timeoutSeconds),
		tls_client.WithClientProfile(profiles.Safari_16_0),
		tls_client.WithCookieJar(jar),
		tls_client.WithNotFollowRedirects(),
		tls_client.WithDialer(dialer),
	}

	if proxyURL := os.Getenv("HTTPS_PROXY"); proxyURL != "" {
		options = append(options, tls_client.WithProxyUrl(proxyURL))
	} else if proxyURL := os.Getenv("HTTP_PROXY"); proxyURL != "" {
		options = append(options, tls_client.WithProxyUrl(proxyURL))
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
		cdpSem:     make(chan struct{}, 3),
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
// Network requests are performed OUTSIDE s.mu to avoid blocking concurrent callers.
func (s *SessionManager) RefreshWizData(ctx context.Context) (*WizData, error) {
	// 1. Try Chrome CDP first if available — it holds live authenticated tokens from browser tab
	if cdpURL := s.CDPURL(); cdpURL != "" {
		if wiz, err := s.FetchWizDataViaCDP(ctx); err == nil && wiz != nil && wiz.At != "" {
			s.mu.Lock()
			s.wizData = wiz
			s.lastWizFetch = time.Now()
			s.mu.Unlock()
			return wiz, nil
		}
	}

	// 2. Fetch gemini app page via HTTP client (outside s.mu.Lock)
	wiz, err := s.fetchWizDataViaHTTP(ctx)
	if err != nil {
		// If cookies expired or redirected to accounts.google.com, attempt AutoHarvest
		if harvestErr := s.AutoHarvest(ctx); harvestErr == nil {
			return s.refreshWizDataWithNewCookies(ctx)
		}
		return nil, err
	}

	s.mu.Lock()
	s.wizData = wiz
	s.lastWizFetch = time.Now()
	s.mu.Unlock()

	return wiz, nil
}

func (s *SessionManager) refreshWizDataWithNewCookies(ctx context.Context) (*WizData, error) {
	wiz, err := s.fetchWizDataViaHTTP(ctx)
	if err != nil {
		return nil, fmt.Errorf("retry fetch WIZ_global_data: %w", err)
	}

	s.mu.Lock()
	s.wizData = wiz
	s.lastWizFetch = time.Now()
	s.mu.Unlock()

	return wiz, nil
}

func (s *SessionManager) fetchWizDataViaHTTP(ctx context.Context) (*WizData, error) {
	baseURL := s.baseURL
	if baseURL == "" {
		baseURL = "https://gemini.google.com"
	}
	currentURL := baseURL + "/app"
	maxRedirects := 5

	for redirectCount := 0; redirectCount < maxRedirects; redirectCount++ {
		req, err := fhttp.NewRequestWithContext(ctx, "GET", currentURL, nil)
		if err != nil {
			return nil, err
		}

		cookieHdr := s.store.CookieHeader()
		if cookieHdr != "" {
			req.Header.Set("Cookie", cookieHdr)
		}
		req.Header.Set("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.5 Safari/605.1.15")
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

		// Update cookies from response
		var cookiesUpdated bool
		for _, c := range resp.Cookies() {
			if c.Value != "" {
				s.store.Set(c.Name, c.Value)
				cookiesUpdated = true
			}
		}
		if cookiesUpdated {
			_ = s.store.Save()
		}

		// Handle HTTP redirects (301, 302, 303, 307, 308)
		if resp.StatusCode >= 300 && resp.StatusCode < 400 {
			loc := resp.Header.Get("Location")
			_ = resp.Body.Close()

			if strings.Contains(loc, "accounts.google.com") || strings.Contains(loc, "ServiceLogin") {
				return nil, errors.New("gemini session expired: redirected to accounts.google.com")
			}

			if loc == "" {
				return nil, fmt.Errorf("redirect status %d without Location header", resp.StatusCode)
			}

			if strings.HasPrefix(loc, "/") {
				currentURL = baseURL + loc
			} else {
				currentURL = loc
			}
			continue
		}

		if resp.StatusCode == 401 || resp.StatusCode == 403 {
			_ = resp.Body.Close()
			return nil, fmt.Errorf("gemini authentication failed with status %d", resp.StatusCode)
		}

		bodyBytes, err := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if err != nil {
			return nil, fmt.Errorf("read gemini response body: %w", err)
		}

		wiz, err := ParseWizGlobalData(string(bodyBytes))
		if err != nil {
			return nil, fmt.Errorf("parse WIZ_global_data (status %d): %w", resp.StatusCode, err)
		}

		return wiz, nil
	}

	return nil, errors.New("too many redirects fetching gemini app")
}
