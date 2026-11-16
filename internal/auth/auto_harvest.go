package auth

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// isChromeRunning checks if Google Chrome is currently running via pgrep.
func isChromeRunning(ctx context.Context) bool {
	cmd := exec.CommandContext(ctx, "pgrep", "-x", "Google Chrome")
	return cmd.Run() == nil
}

// AutoHarvest attempts to retrieve valid session cookies from Chrome CDP,
// launching Chrome with debugging port if needed, or falling back to Safari.
func (s *SessionManager) AutoHarvest(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.AutoHarvestLocked(ctx)
}

// AutoHarvestLocked is called when s.mu is already held.
// It checks whether Chrome CDP is active and harvests cookies, or falls back to Safari.
// It DOES NOT launch background Chrome processes or empty profile windows.
func (s *SessionManager) AutoHarvestLocked(ctx context.Context) error {
	endpoint := s.cdpURL
	if endpoint == "" {
		endpoint = "http://127.0.0.1:9222"
	}

	// 1. Проверяем, доступен ли уже работающий CDP
	client := &http.Client{Timeout: 1 * time.Second}
	req, reqErr := http.NewRequestWithContext(ctx, "GET", strings.TrimRight(endpoint, "/")+"/json/version", nil)
	if reqErr == nil {
		resp, err := client.Do(req)
		if err == nil && resp.StatusCode == http.StatusOK {
			_ = resp.Body.Close()
			if harvestErr := s.harvestCookiesFromCDPLocked(ctx); harvestErr == nil {
				log.Printf("[auto_harvest] successfully harvested cookies from active Chrome CDP session")
				return nil
			} else {
				log.Printf("[auto_harvest] Chrome CDP is active at %s but cookie harvest failed: %v", endpoint, harvestErr)
			}
		} else if resp != nil {
			_ = resp.Body.Close()
		}
	}

	// 2. Попытка Safari fallback
	log.Printf("[auto_harvest] attempting Safari cookies fallback...")
	if safariErr := s.harvestCookiesFromSafariLocked(ctx); safariErr == nil {
		log.Printf("[auto_harvest] successfully harvested cookies from Safari binarycookies")
		return nil
	} else {
		log.Printf("[auto_harvest] Safari cookies fallback failed: %v", safariErr)
	}

	// НЕ открываем пустые окна /tmp! Выводим четкую инструкцию для пользователя.
	return fmt.Errorf("Chrome CDP is not active at %s and Safari cookies inaccessible. To enable automatic harvest, start Chrome with: open -a \"Google Chrome\" --args --remote-debugging-port=9222", endpoint)
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
		return fmt.Errorf("resolve user home directory: %w", err)
	}
	safariCookiePath := filepath.Join(homeDir, "Library", "Containers", "com.apple.Safari", "Data", "Library", "Cookies", "Cookies.binarycookies")

	// Check existence and permissions via Stat
	if fi, err := os.Stat(safariCookiePath); err != nil {
		if errors.Is(err, os.ErrPermission) || strings.Contains(strings.ToLower(err.Error()), "permission denied") || strings.Contains(strings.ToLower(err.Error()), "operation not permitted") {
			return fmt.Errorf("access to Safari cookies denied by macOS TCC (Full Disk Access required): %w. Please grant Full Disk Access to your terminal or application in System Settings -> Privacy & Security -> Full Disk Access", err)
		}
		if os.IsNotExist(err) {
			return fmt.Errorf("safari cookies not found at %s: %w", safariCookiePath, err)
		}
		return fmt.Errorf("stat safari cookies: %w", err)
	} else if fi.Size() == 0 {
		return fmt.Errorf("safari cookies file is empty at %s", safariCookiePath)
	}

	// Verify read access via Open
	if f, err := os.Open(safariCookiePath); err != nil {
		if errors.Is(err, os.ErrPermission) || strings.Contains(strings.ToLower(err.Error()), "permission denied") || strings.Contains(strings.ToLower(err.Error()), "operation not permitted") {
			return fmt.Errorf("access to Safari cookies denied by macOS TCC (Full Disk Access required): %w. Please grant Full Disk Access to your terminal or application in System Settings -> Privacy & Security -> Full Disk Access", err)
		}
		return fmt.Errorf("open safari cookies file: %w", err)
	} else {
		_ = f.Close()
	}

	pyScript := fmt.Sprintf(`
import struct, json, os, sys

path = %q
if not os.path.exists(path):
    sys.exit(1)

try:
    with open(path, 'rb') as f:
        magic = f.read(4)
        if magic != b'cook':
            sys.exit(1)
        num_pages = struct.unpack('>I', f.read(4))[0]
        page_sizes = [struct.unpack('>I', f.read(4))[0] for _ in range(num_pages)]
        cookies = {}
        for size in page_sizes:
            page_bytes = f.read(size)
            if page_bytes[:4] != b'\x00\x00\x01\x00':
                continue
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
        sys.exit(2)
except PermissionError as e:
    sys.stderr.write(f"TCC_PERMISSION_DENIED: {e}\n")
    sys.exit(3)
except Exception as e:
    sys.stderr.write(f"EXTRACTION_ERROR: {e}\n")
    sys.exit(4)
`, safariCookiePath)

	var stdout, stderr bytes.Buffer
	cmd := exec.CommandContext(ctx, "python3", "-c", pyScript)
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		stderrStr := strings.TrimSpace(stderr.String())
		if strings.Contains(stderrStr, "TCC_PERMISSION_DENIED") || strings.Contains(stderrStr, "PermissionError") || strings.Contains(stderrStr, "Operation not permitted") {
			return fmt.Errorf("safari cookies extraction blocked by macOS TCC (Full Disk Access required): %s. Please grant Full Disk Access in System Settings -> Privacy & Security -> Full Disk Access", stderrStr)
		}
		if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() == 2 {
			return errors.New("no Google authentication cookies found in Safari binarycookies")
		}
		return fmt.Errorf("safari cookies extraction failed (%w): %s", err, stderrStr)
	}

	var cookies map[string]string
	if err := json.Unmarshal(stdout.Bytes(), &cookies); err != nil {
		return fmt.Errorf("unmarshal safari cookies: %w", err)
	}

	for k, v := range cookies {
		s.store.Set(k, v)
	}
	return s.store.Save()
}
