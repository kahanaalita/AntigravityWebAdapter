package auth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestParseWizGlobalData(t *testing.T) {
	html := `<!doctype html><html><head><script>
window.WIZ_global_data = {
  "SNlM0e":"sec_token_abcdef_12345",
  "cfb2h":"boq_assistant-bard-web-server_20261113.00_p0",
  "FdrFJe":"session_sid_998877"
};
</script></head><body></body></html>`

	wiz, err := ParseWizGlobalData(html)
	if err != nil {
		t.Fatalf("ParseWizGlobalData failed: %v", err)
	}

	if wiz.At != "sec_token_abcdef_12345" {
		t.Errorf("Expected At 'sec_token_abcdef_12345', got %q", wiz.At)
	}
	if wiz.Bl != "boq_assistant-bard-web-server_20261113.00_p0" {
		t.Errorf("Expected Bl 'boq_assistant-bard-web-server_20261113.00_p0', got %q", wiz.Bl)
	}
	if wiz.Sid != "session_sid_998877" {
		t.Errorf("Expected Sid 'session_sid_998877', got %q", wiz.Sid)
	}

	// Missing SNlM0e should return error
	missingHTML := `<html><body>No tokens here</body></html>`
	_, err = ParseWizGlobalData(missingHTML)
	if err == nil {
		t.Errorf("Expected error when SNlM0e is missing")
	}
}

func TestCookieStore_Formats(t *testing.T) {
	tmpDir := t.TempDir()

	// Format 1: Map
	mapPath := filepath.Join(tmpDir, "map.json")
	if err := os.WriteFile(mapPath, []byte(`{"__Secure-1PSID":"val1","__Secure-1PSIDTS":"val2"}`), 0600); err != nil {
		t.Fatal(err)
	}
	store1 := NewCookieStore(mapPath)
	if err := store1.Load(); err != nil {
		t.Fatalf("Load map failed: %v", err)
	}
	v, ok := store1.Get("__Secure-1PSID")
	if !ok || v != "val1" {
		t.Errorf("Expected val1, got %v", v)
	}

	// Format 2: Array of cookie objects
	arrPath := filepath.Join(tmpDir, "arr.json")
	if err := os.WriteFile(arrPath, []byte(`[{"name":"__Secure-1PSID","value":"valA"},{"name":"SID","value":"valB"}]`), 0600); err != nil {
		t.Fatal(err)
	}
	store2 := NewCookieStore(arrPath)
	if err := store2.Load(); err != nil {
		t.Fatalf("Load array failed: %v", err)
	}
	v, ok = store2.Get("__Secure-1PSID")
	if !ok || v != "valA" {
		t.Errorf("Expected valA, got %v", v)
	}
	v, ok = store2.Get("SID")
	if !ok || v != "valB" {
		t.Errorf("Expected valB, got %v", v)
	}

	// Format 3: Raw string
	rawPath := filepath.Join(tmpDir, "raw.txt")
	if err := os.WriteFile(rawPath, []byte(`__Secure-1PSID=valX; __Secure-1PSIDTS=valY`), 0600); err != nil {
		t.Fatal(err)
	}
	store3 := NewCookieStore(rawPath)
	if err := store3.Load(); err != nil {
		t.Fatalf("Load raw failed: %v", err)
	}
	v, ok = store3.Get("__Secure-1PSID")
	if !ok || v != "valX" {
		t.Errorf("Expected valX, got %v", v)
	}

	// Test Save & CookieHeader
	savePath := filepath.Join(tmpDir, "save.json")
	store4 := NewCookieStore(savePath)
	store4.Set("A", "1")
	store4.Set("B", "2")
	if err := store4.Save(); err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	hdr := store4.CookieHeader()
	if !strings.Contains(hdr, "A=1") || !strings.Contains(hdr, "B=2") {
		t.Errorf("CookieHeader missing entries: %q", hdr)
	}
}

func TestCreateDialer_Default(t *testing.T) {
	d := createDialer("")
	if d.Resolver != nil {
		t.Errorf("expected default dialer to have nil Resolver, got %v", d.Resolver)
	}
	if d.Timeout != 30*time.Second {
		t.Errorf("expected 30s timeout, got %v", d.Timeout)
	}
}

func TestCreateDialer_CustomDNS(t *testing.T) {
	d1 := createDialer("1.1.1.1")
	if d1.Resolver == nil {
		t.Fatalf("expected custom resolver, got nil")
	}
	if !d1.Resolver.PreferGo {
		t.Errorf("expected PreferGo = true")
	}

	d2 := createDialer("8.8.8.8:5353")
	if d2.Resolver == nil {
		t.Fatalf("expected custom resolver with port, got nil")
	}
}

func TestNewSessionManager_Init(t *testing.T) {
	tmpDir := t.TempDir()
	cookieFile := filepath.Join(tmpDir, "cookies.json")
	sm, err := NewSessionManager(cookieFile, "http://127.0.0.1:9222")
	if err != nil {
		t.Fatalf("NewSessionManager failed: %v", err)
	}
	if sm == nil {
		t.Fatal("expected non-nil SessionManager")
	}
	if sm.CDPURL() != "http://127.0.0.1:9222" {
		t.Errorf("expected cdpURL, got %s", sm.CDPURL())
	}
	if sm.Store() == nil {
		t.Errorf("expected non-nil store")
	}
	if sm.TLSClient() == nil {
		t.Errorf("expected non-nil tls client")
	}
}

func TestNewSessionManager_TLSTimeoutEnv(t *testing.T) {
	tmpDir := t.TempDir()
	cookieFile := filepath.Join(tmpDir, "cookies.json")

	t.Setenv("TLS_CLIENT_TIMEOUT", "1800")
	sm, err := NewSessionManager(cookieFile, "")
	if err != nil {
		t.Fatalf("NewSessionManager with custom TLS_CLIENT_TIMEOUT failed: %v", err)
	}
	if sm == nil || sm.TLSClient() == nil {
		t.Fatalf("expected non-nil SessionManager and TLSClient")
	}
}

func TestIsChromeRunning(t *testing.T) {
	// Should execute without panicking
	_ = isChromeRunning(t.Context())
}

func TestCookieRotator_EmptyStore(t *testing.T) {
	tmpDir := t.TempDir()
	cookieFile := filepath.Join(tmpDir, "empty_cookies.json")
	sm, err := NewSessionManager(cookieFile, "")
	if err != nil {
		t.Fatalf("NewSessionManager failed: %v", err)
	}

	rotator := NewCookieRotator(sm, 5*time.Minute)
	if err := rotator.RotateOnce(t.Context()); err != nil {
		t.Errorf("expected nil error for empty store, got %v", err)
	}
}

func TestRefreshWizData_LocalizationRedirect(t *testing.T) {
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/app" && r.URL.RawQuery == "" {
			// Simulate 302 redirect for localization
			http.SetCookie(w, &http.Cookie{
				Name:  "TEST_SESSION_COOKIE",
				Value: "cookie_value_from_redirect",
				Path:  "/",
			})
			w.Header().Set("Location", "/app?hl=ru")
			w.WriteHeader(http.StatusFound)
			return
		}
		if r.URL.Path == "/app" && r.URL.Query().Get("hl") == "ru" {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`<!doctype html><html><head><script>
window.WIZ_global_data = {
  "SNlM0e":"token_loc_123",
  "cfb2h":"bl_loc_456",
  "FdrFJe":"sid_loc_789"
};
</script></head><body></body></html>`))
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	tmpDir := t.TempDir()
	cookieFile := filepath.Join(tmpDir, "cookies.json")
	sm, err := NewSessionManager(cookieFile, "")
	if err != nil {
		t.Fatalf("NewSessionManager failed: %v", err)
	}
	sm.baseURL = srv.URL

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	wiz, err := sm.RefreshWizData(ctx)
	if err != nil {
		t.Fatalf("RefreshWizData failed with localization redirect: %v", err)
	}

	if wiz.At != "token_loc_123" {
		t.Errorf("expected At 'token_loc_123', got %q", wiz.At)
	}
	if wiz.Bl != "bl_loc_456" {
		t.Errorf("expected Bl 'bl_loc_456', got %q", wiz.Bl)
	}
	if wiz.Sid != "sid_loc_789" {
		t.Errorf("expected Sid 'sid_loc_789', got %q", wiz.Sid)
	}

	// Verify cookie persistence from redirect
	val, ok := sm.Store().Get("TEST_SESSION_COOKIE")
	if !ok || val != "cookie_value_from_redirect" {
		t.Errorf("expected cookie TEST_SESSION_COOKIE to be saved, got %v (ok=%v)", val, ok)
	}

	// Verify cookie was saved to disk
	savedStore := NewCookieStore(cookieFile)
	if err := savedStore.Load(); err != nil {
		t.Fatalf("failed to load saved store: %v", err)
	}
	valDisk, okDisk := savedStore.Get("TEST_SESSION_COOKIE")
	if !okDisk || valDisk != "cookie_value_from_redirect" {
		t.Errorf("expected cookie saved on disk, got %v (ok=%v)", valDisk, okDisk)
	}
}

func TestRefreshWizData_AccountsLoginRedirect(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Location", "https://accounts.google.com/ServiceLogin?continue=https://gemini.google.com/app")
		w.WriteHeader(http.StatusFound)
	}))
	defer srv.Close()

	tmpDir := t.TempDir()
	cookieFile := filepath.Join(tmpDir, "cookies.json")
	sm, err := NewSessionManager(cookieFile, "http://127.0.0.1:54321")
	if err != nil {
		t.Fatalf("NewSessionManager failed: %v", err)
	}
	sm.baseURL = srv.URL

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	_, err = sm.RefreshWizData(ctx)
	if err == nil {
		t.Fatal("expected error on accounts.google.com login redirect, got nil")
	}
}

func TestAutoHarvest_NoSpamWhenCDPUnavailable(t *testing.T) {
	// Isolate HOME so Safari cookies aren't read from real user profile
	t.Setenv("HOME", t.TempDir())

	tmpDir := t.TempDir()
	cookieFile := filepath.Join(tmpDir, "cookies.json")
	sm, err := NewSessionManager(cookieFile, "http://127.0.0.1:54321")
	if err != nil {
		t.Fatalf("NewSessionManager failed: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	start := time.Now()
	err = sm.AutoHarvest(ctx)
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("expected error when CDP and Safari are unavailable, got nil")
	}
	if !strings.Contains(err.Error(), "--remote-debugging-port=9222") {
		t.Errorf("expected instruction in error, got: %v", err)
	}
	if elapsed > 3*time.Second {
		t.Errorf("AutoHarvest took too long (%v), should return promptly without opening browsers", elapsed)
	}
}

func TestRefreshWizData_MutexNotHeldDuringNetwork(t *testing.T) {
	netStarted := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(netStarted)
		time.Sleep(300 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`<!doctype html><html><head><script>
window.WIZ_global_data = {
  "SNlM0e":"token_async",
  "cfb2h":"bl_async",
  "FdrFJe":"sid_async"
};
</script></head><body></body></html>`))
	}))
	defer srv.Close()

	tmpDir := t.TempDir()
	cookieFile := filepath.Join(tmpDir, "cookies.json")
	sm, err := NewSessionManager(cookieFile, "")
	if err != nil {
		t.Fatalf("NewSessionManager failed: %v", err)
	}
	sm.baseURL = srv.URL

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		_, _ = sm.RefreshWizData(context.Background())
	}()

	// Wait for the HTTP request to begin
	<-netStarted

	// During the network request, sm.CDPURL() should be readable immediately without waiting for the 300ms network call
	readStart := time.Now()
	_ = sm.CDPURL()
	readElapsed := time.Since(readStart)

	if readElapsed > 50*time.Millisecond {
		t.Errorf("sm.CDPURL() was blocked for %v by RefreshWizData! Write lock held during network request", readElapsed)
	}

	wg.Wait()
}

func TestCookieRotator_StartupRotation(t *testing.T) {
	tmpDir := t.TempDir()
	cookieFile := filepath.Join(tmpDir, "cookies.json")
	sm, err := NewSessionManager(cookieFile, "")
	if err != nil {
		t.Fatalf("NewSessionManager failed: %v", err)
	}

	// Interval is 24 hours, but Start should immediately trigger initial rotation
	rotator := NewCookieRotator(sm, 24*time.Hour)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	rotator.Start(ctx)
	time.Sleep(50 * time.Millisecond)
	rotator.Stop()
}

func TestSafariCookies_DirectPathFallback(t *testing.T) {
	fakeHome := t.TempDir()
	t.Setenv("HOME", fakeHome)

	// Create ONLY direct path: ~/Library/Cookies/Cookies.binarycookies
	directDir := filepath.Join(fakeHome, "Library", "Cookies")
	if err := os.MkdirAll(directDir, 0755); err != nil {
		t.Fatalf("mkdir direct cookies dir failed: %v", err)
	}
	cookieFile := filepath.Join(directDir, "Cookies.binarycookies")
	// Write dummy data (non-zero size so stat succeeds)
	if err := os.WriteFile(cookieFile, []byte("dummy binary cookie content"), 0644); err != nil {
		t.Fatalf("write direct cookie file failed: %v", err)
	}

	tmpDir := t.TempDir()
	storeFile := filepath.Join(tmpDir, "store.json")
	sm, err := NewSessionManager(storeFile, "")
	if err != nil {
		t.Fatalf("NewSessionManager failed: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	// harvestCookiesFromSafariLocked should find the direct path file and attempt extraction
	err = sm.harvestCookiesFromSafariLocked(ctx)
	if err == nil {
		t.Fatal("expected python extraction error on dummy content, got nil")
	}
	// The error must NOT be "safari cookies not found" or "no such file or directory",
	// but rather the python script error indicating the file was opened and parsed
	if strings.Contains(err.Error(), "no such file or directory") || strings.Contains(err.Error(), "not found") {
		t.Errorf("expected direct file to be discovered, but got file not found error: %v", err)
	}
}
