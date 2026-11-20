package auth

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/url"
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

// launchDedicatedCDPChrome launches an isolated Chrome instance with remote debugging enabled
// and waits up to 5 seconds for http://127.0.0.1:<port>/json/version to be ready.
func launchDedicatedCDPChrome(ctx context.Context, endpoint string) error {
	chromePath := "/Applications/Google Chrome.app/Contents/MacOS/Google Chrome"
	if envPath := os.Getenv("CHROME_PATH"); envPath != "" {
		chromePath = envPath
	} else if envBin := os.Getenv("CHROME_BIN"); envBin != "" {
		chromePath = envBin
	}

	if _, err := os.Stat(chromePath); err != nil {
		return fmt.Errorf("chrome executable not found at %s: %w", chromePath, err)
	}

	port := "9222"
	if endpoint != "" {
		if u, err := url.Parse(endpoint); err == nil && u.Port() != "" {
			port = u.Port()
		}
	}

	profileDir := "/tmp/agy-chrome-cdp-profile"
	if envProfile := os.Getenv("AGY_CHROME_PROFILE_DIR"); envProfile != "" {
		profileDir = envProfile
	}

	args := []string{
		fmt.Sprintf("--remote-debugging-port=%s", port),
		fmt.Sprintf("--user-data-dir=%s", profileDir),
		"--no-first-run",
		"--no-default-browser-check",
		"https://gemini.google.com",
	}

	cmd := exec.Command(chromePath, args...)
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to start dedicated Chrome CDP process: %w", err)
	}

	versionURL := fmt.Sprintf("http://127.0.0.1:%s/json/version", port)
	readyCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	client := &http.Client{Timeout: 500 * time.Millisecond}
	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()

	for {
		req, err := http.NewRequestWithContext(readyCtx, "GET", versionURL, nil)
		if err == nil {
			resp, err := client.Do(req)
			if err == nil && resp.StatusCode == http.StatusOK {
				_ = resp.Body.Close()
				log.Printf("[auto_harvest] dedicated Chrome CDP is ready at %s", versionURL)
				return nil
			}
			if resp != nil {
				_ = resp.Body.Close()
			}
		}

		select {
		case <-readyCtx.Done():
			return fmt.Errorf("timeout waiting for dedicated Chrome CDP at %s: %w", versionURL, readyCtx.Err())
		case <-ticker.C:
		}
	}
}

// isTCCError checks if an error is caused by macOS TCC permissions restriction.
func isTCCError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "tcc") ||
		strings.Contains(msg, "full disk access") ||
		strings.Contains(msg, "permission denied") ||
		strings.Contains(msg, "operation not permitted")
}

// isDefaultCDPEndpoint returns true if endpoint is unset or specifies standard port 9222.
func isDefaultCDPEndpoint(endpoint string) bool {
	if endpoint == "" {
		return true
	}
	u, err := url.Parse(endpoint)
	if err != nil {
		return false
	}
	port := u.Port()
	return port == "" || port == "9222"
}

// AutoHarvest attempts to retrieve valid session cookies from Chrome CDP,
// launching Chrome with debugging port if needed, or falling back to Safari.
func (s *SessionManager) AutoHarvest(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.AutoHarvestLocked(ctx)
}

// AutoHarvestLocked is called when s.mu is already held.
// It checks whether Chrome CDP is active and harvests cookies,
// launches dedicated CDP Chrome if unavailable on port, or falls back to Safari.
func (s *SessionManager) AutoHarvestLocked(ctx context.Context) error {
	endpoint := s.cdpURL
	if endpoint == "" {
		endpoint = "http://127.0.0.1:9222"
	}

	// 1. Проверяем, доступен ли уже работающий CDP
	client := &http.Client{Timeout: 1 * time.Second}
	req, reqErr := http.NewRequestWithContext(ctx, "GET", strings.TrimRight(endpoint, "/")+"/json/version", nil)
	cdpActive := false
	if reqErr == nil {
		resp, err := client.Do(req)
		if err == nil && resp.StatusCode == http.StatusOK {
			_ = resp.Body.Close()
			cdpActive = true
		} else if resp != nil {
			_ = resp.Body.Close()
		}
	}

	if cdpActive {
		if harvestErr := s.harvestCookiesFromCDPLocked(ctx); harvestErr == nil {
			log.Printf("[auto_harvest] successfully harvested cookies from active Chrome CDP session")
			return nil
		} else {
			log.Printf("[auto_harvest] Chrome CDP is active at %s but cookie harvest failed: %v", endpoint, harvestErr)
		}
	} else if isDefaultCDPEndpoint(endpoint) {
		log.Printf("[auto_harvest] Chrome CDP is not active at %s, launching dedicated CDP Chrome...", endpoint)
		if launchErr := launchDedicatedCDPChrome(ctx, endpoint); launchErr == nil {
			if harvestErr := s.harvestCookiesFromCDPLocked(ctx); harvestErr == nil {
				log.Printf("[auto_harvest] successfully harvested cookies from dedicated Chrome CDP session")
				return nil
			} else {
				log.Printf("[auto_harvest] dedicated Chrome CDP started, but cookie harvest failed: %v", harvestErr)
			}
		} else {
			log.Printf("[auto_harvest] launch dedicated Chrome CDP failed: %v", launchErr)
		}
	}

	// 2. Попытка Safari fallback
	log.Printf("[auto_harvest] attempting Safari cookies fallback...")
	if safariErr := s.harvestCookiesFromSafariLocked(ctx); safariErr == nil {
		log.Printf("[auto_harvest] successfully harvested cookies from Safari binarycookies")
		return nil
	} else {
		if isTCCError(safariErr) {
			log.Printf("[auto_harvest] Safari cookies access restricted by macOS TCC (Full Disk Access needed in System Settings -> Privacy & Security -> Full Disk Access). Proceeding with CDP mode.")
		} else {
			log.Printf("[auto_harvest] Safari cookies fallback failed: %v", safariErr)
		}
	}

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

	candidatePaths := []string{
		filepath.Join(homeDir, "Library", "Containers", "com.apple.Safari", "Data", "Library", "Cookies", "Cookies.binarycookies"),
		filepath.Join(homeDir, "Library", "Cookies", "Cookies.binarycookies"),
	}

	var foundPath string
	var lastErr error

	for _, p := range candidatePaths {
		fi, err := os.Stat(p)
		if err != nil {
			if errors.Is(err, os.ErrPermission) || strings.Contains(strings.ToLower(err.Error()), "permission denied") || strings.Contains(strings.ToLower(err.Error()), "operation not permitted") {
				return fmt.Errorf("access to Safari cookies denied by macOS TCC (Full Disk Access required): %w. Please grant Full Disk Access to your terminal or application in System Settings -> Privacy & Security -> Full Disk Access", err)
			}
			lastErr = err
			continue
		}
		if fi.Size() == 0 {
			lastErr = fmt.Errorf("safari cookies file is empty at %s", p)
			continue
		}

		// Verify read access via Open
		if f, err := os.Open(p); err != nil {
			if errors.Is(err, os.ErrPermission) || strings.Contains(strings.ToLower(err.Error()), "permission denied") || strings.Contains(strings.ToLower(err.Error()), "operation not permitted") {
				return fmt.Errorf("access to Safari cookies denied by macOS TCC (Full Disk Access required): %w. Please grant Full Disk Access to your terminal or application in System Settings -> Privacy & Security -> Full Disk Access", err)
			}
			lastErr = fmt.Errorf("open safari cookies file: %w", err)
			continue
		} else {
			_ = f.Close()
		}

		foundPath = p
		break
	}

	if foundPath == "" {
		if lastErr != nil {
			return fmt.Errorf("safari cookies not found: %w", lastErr)
		}
		return errors.New("safari cookies file not found in candidate paths")
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
`, foundPath)

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
