package auth

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

// AutoHarvest attempts to retrieve valid session cookies from Chrome CDP,
// launching Chrome with debugging port if needed, or falling back to Safari.
func (s *SessionManager) AutoHarvest(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.AutoHarvestLocked(ctx)
}

// AutoHarvestLocked is called when s.mu is already held.
func (s *SessionManager) AutoHarvestLocked(ctx context.Context) error {
	// 1. Try Chrome CDP directly
	if err := s.harvestCookiesFromCDPLocked(ctx); err == nil {
		return nil
	}

	// 2. Try launching Chrome with remote debugging port
	endpoint := s.cdpURL
	if endpoint == "" {
		endpoint = "http://127.0.0.1:9222"
	}

	_ = exec.Command("open", "-a", "Google Chrome", "--args", "--remote-debugging-port=9222", "https://gemini.google.com/app").Run()

	// Wait up to 3 seconds for CDP endpoint to become ready
	client := &http.Client{Timeout: 1 * time.Second}
	deadline := time.Now().Add(3 * time.Second)

	for time.Now().Before(deadline) {
		time.Sleep(300 * time.Millisecond)
		resp, err := client.Get(endpoint + "/json/version")
		if err == nil && resp.StatusCode == http.StatusOK {
			resp.Body.Close()
			if harvestErr := s.harvestCookiesFromCDPLocked(ctx); harvestErr == nil {
				return nil
			}
			break
		}
		if ctx.Err() != nil {
			break
		}
	}

	// 3. Fallback: Safari binary cookies
	if safariErr := s.harvestCookiesFromSafariLocked(ctx); safariErr == nil {
		return nil
	}

	return fmt.Errorf("unable to harvest session cookies from Chrome CDP or Safari")
}

// HarvestCookiesFromSafari extracts Google session cookies from Safari binarycookies.
func (s *SessionManager) HarvestCookiesFromSafari(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.harvestCookiesFromSafariLocked(ctx)
}

// harvestCookiesFromSafariLocked extracts cookies without acquiring s.mu.
func (s *SessionManager) harvestCookiesFromSafariLocked(ctx context.Context) error {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	safariCookiePath := filepath.Join(homeDir, "Library", "Containers", "com.apple.Safari", "Data", "Library", "Cookies", "Cookies.binarycookies")
	if _, err := os.Stat(safariCookiePath); err != nil {
		return fmt.Errorf("safari cookies not found at %s: %w", safariCookiePath, err)
	}

	pyScript := fmt.Sprintf(`
import struct, json, os

path = %q
if not os.path.exists(path):
    exit(1)

with open(path, 'rb') as f:
    magic = f.read(4)
    if magic != b'cook': exit(1)
    num_pages = struct.unpack('>I', f.read(4))[0]
    page_sizes = [struct.unpack('>I', f.read(4))[0] for _ in range(num_pages)]
    cookies = {}
    for size in page_sizes:
        page_bytes = f.read(size)
        if page_bytes[:4] != b'\x00\x00\x01\x00': continue
        num_cookies = struct.unpack('<I', page_bytes[4:8])[0]
        cookie_offsets = [struct.unpack('<I', page_bytes[8+i*4:12+i*4])[0] for i in range(num_cookies)]
        for offset in cookie_offsets:
            c_bytes = page_bytes[offset:]
            url_offset = struct.unpack('<I', c_bytes[16:20])[0]
            name_offset = struct.unpack('<I', c_bytes[20:24])[0]
            val_offset = struct.unpack('<I', c_bytes[28:32])[0]
            def get_str(start):
                end = c_bytes.find(b'\x00', start)
                return c_bytes[start:end].decode('utf-8', errors='ignore')
            domain = get_str(url_offset)
            name = get_str(name_offset)
            val = get_str(val_offset)
            if domain in ['.google.com', 'google.com', 'gemini.google.com']:
                cookies[name] = val

google_auth = ['__Secure-1PSID', '__Secure-1PSIDTS', 'SID']
if any(k in cookies for k in google_auth):
    print(json.dumps(cookies))
else:
    exit(2)
`, safariCookiePath)

	cmd := exec.CommandContext(ctx, "python3", "-c", pyScript)
	out, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("safari cookies extraction failed: %w", err)
	}

	var cookies map[string]string
	if err := json.Unmarshal(out, &cookies); err != nil {
		return fmt.Errorf("unmarshal safari cookies: %w", err)
	}

	for k, v := range cookies {
		s.store.Set(k, v)
	}
	return s.store.Save()
}
