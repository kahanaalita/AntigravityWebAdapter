package cdp

import (
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
)

// ChromeTarget represents an individual target page from /json/list.
type ChromeTarget struct {
	ID                   string `json:"id"`
	Title                string `json:"title"`
	Type                 string `json:"type"`
	URL                  string `json:"url"`
	WebSocketDebuggerURL string `json:"webSocketDebuggerUrl"`
}

// LauncherConfig holds configuration for Chrome launching and debugging.
type LauncherConfig struct {
	ChromePath  string
	BaseDir     string
	Headless    bool
	ProxyServer string
}

// ChromeInstance holds process information and DevTools port.
type ChromeInstance struct {
	Cmd      *exec.Cmd
	Port     int
	Profile  string
	ProxyURL string
}

// DetectChromePath locates the Google Chrome binary on the system.
func DetectChromePath(override string) (string, error) {
	if override != "" {
		if _, err := os.Stat(override); err == nil {
			return override, nil
		}
	}

	if env := os.Getenv("CHROME_PATH"); env != "" {
		if _, err := os.Stat(env); err == nil {
			return env, nil
		}
	}

	var candidates []string
	switch runtime.GOOS {
	case "darwin":
		candidates = []string{
			"/Applications/Google Chrome.app/Contents/MacOS/Google Chrome",
			"/Applications/Google Chrome Canary.app/Contents/MacOS/Google Chrome Canary",
			"/Applications/Chromium.app/Contents/MacOS/Chromium",
		}
	case "linux":
		candidates = []string{
			"google-chrome",
			"google-chrome-stable",
			"chromium",
			"chromium-browser",
		}
	case "windows":
		candidates = []string{
			`C:\Program Files\Google\Chrome\Application\chrome.exe`,
			`C:\Program Files (x86)\Google\Chrome\Application\chrome.exe`,
			os.ExpandEnv(`${LOCALAPPDATA}\Google\Chrome\Application\chrome.exe`),
		}
	}

	for _, c := range candidates {
		if filepath.IsAbs(c) {
			if _, err := os.Stat(c); err == nil {
				return c, nil
			}
		} else {
			if p, err := exec.LookPath(c); err == nil {
				return p, nil
			}
		}
	}

	return "", fmt.Errorf("google chrome binary not found on %s", runtime.GOOS)
}

// DetectLocalProxy probes common proxy ports on localhost.
func DetectLocalProxy() string {
	ports := []int{10808, 7890, 1080, 10809}
	for _, port := range ports {
		addr := fmt.Sprintf("127.0.0.1:%d", port)
		conn, err := net.DialTimeout("tcp", addr, 150*time.Millisecond)
		if err == nil {
			conn.Close()
			return fmt.Sprintf("socks5://127.0.0.1:%d", port)
		}
	}
	return ""
}

// ReadActivePort attempts to read the active port from DevToolsActivePort file.
func ReadActivePort(profileDir string) (int, error) {
	activePortFile := filepath.Join(profileDir, "DevToolsActivePort")
	data, err := os.ReadFile(activePortFile)
	if err != nil {
		return 0, err
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) < 1 {
		return 0, fmt.Errorf("DevToolsActivePort file empty")
	}
	port, err := strconv.Atoi(strings.TrimSpace(lines[0]))
	if err != nil {
		return 0, fmt.Errorf("invalid port in DevToolsActivePort: %w", err)
	}

	// Verify port is accepting TCP connections
	conn, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", port), 300*time.Millisecond)
	if err != nil {
		return 0, fmt.Errorf("port %d in DevToolsActivePort not responding", port)
	}
	conn.Close()

	return port, nil
}

// LaunchChrome starts or connects to a Chrome instance with remote debugging enabled.
func LaunchChrome(cfg LauncherConfig) (*ChromeInstance, error) {
	chromePath, err := DetectChromePath(cfg.ChromePath)
	if err != nil {
		return nil, err
	}

	profileDir := filepath.Join(cfg.BaseDir, "chrome_profile")
	if err := os.MkdirAll(profileDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create profile dir: %w", err)
	}

	// Check if already running
	if port, err := ReadActivePort(profileDir); err == nil && port > 0 {
		return &ChromeInstance{
			Port:     port,
			Profile:  profileDir,
			ProxyURL: cfg.ProxyServer,
		}, nil
	}

	proxyServer := cfg.ProxyServer
	if proxyServer == "" {
		proxyServer = DetectLocalProxy()
	}

	args := []string{
		"--remote-debugging-port=0",
		fmt.Sprintf("--user-data-dir=%s", profileDir),
		"--no-first-run",
		"--no-default-browser-check",
		"--disable-blink-features=AutomationControlled",
		"--password-store=basic",
		"--disable-features=Translate,OptimizationHints,MediaRouter",
		"--window-size=1280,900",
		"about:blank",
	}

	if proxyServer != "" {
		args = append(args, fmt.Sprintf("--proxy-server=%s", proxyServer))
	}

	if cfg.Headless {
		args = append(args, "--headless=new")
	}

	cmd := exec.Command(chromePath, args...)
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("failed to start chrome: %w", err)
	}

	// Wait up to 10 seconds for DevToolsActivePort
	var port int
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		p, err := ReadActivePort(profileDir)
		if err == nil && p > 0 {
			port = p
			break
		}
		time.Sleep(100 * time.Millisecond)
	}

	if port == 0 {
		_ = cmd.Process.Kill()
		return nil, fmt.Errorf("chrome started but DevToolsActivePort was not created in time")
	}

	return &ChromeInstance{
		Cmd:      cmd,
		Port:     port,
		Profile:  profileDir,
		ProxyURL: proxyServer,
	}, nil
}

// FetchTargets returns the list of open browser tabs/targets from /json/list.
func (c *ChromeInstance) FetchTargets() ([]ChromeTarget, error) {
	url := fmt.Sprintf("http://127.0.0.1:%d/json/list", c.Port)
	resp, err := http.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var targets []ChromeTarget
	if err := json.Unmarshal(body, &targets); err != nil {
		return nil, fmt.Errorf("failed to parse targets json: %w", err)
	}

	return targets, nil
}

// NewTarget creates a new tab at the specified URL and returns its target metadata.
func (c *ChromeInstance) NewTarget(targetURL string) (*ChromeTarget, error) {
	endpoint := fmt.Sprintf("http://127.0.0.1:%d/json/new?%s", c.Port, targetURL)
	req, err := http.NewRequest(http.MethodPut, endpoint, nil)
	if err != nil {
		return nil, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var target ChromeTarget
	if err := json.Unmarshal(body, &target); err != nil {
		return nil, fmt.Errorf("failed to parse new target json: %w", err)
	}

	return &target, nil
}
