package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/kahanaalita/dsh-gemini-web-adapter-go/internal/engine"
	"github.com/kahanaalita/dsh-gemini-web-adapter-go/internal/mcp"
	"github.com/kahanaalita/dsh-gemini-web-adapter-go/internal/server"
)

func main() {
	portFlag := flag.Int("port", 5690, "HTTP gateway listen port")
	baseFlag := flag.String("base", "", "Base directory for profile and temp files")
	chromeFlag := flag.String("chrome", "", "Path to Google Chrome executable")
	headlessFlag := flag.Bool("headless", false, "Run Chrome in headless mode")
	proxyFlag := flag.String("proxy", "", "Proxy server (e.g. socks5://127.0.0.1:10808)")
	mcpMode := flag.Bool("mcp", false, "Run in MCP stdio mode for Antigravity integration")
	flag.Parse()

	baseDir := *baseFlag
	if baseDir == "" {
		home, err := os.UserHomeDir()
		if err == nil {
			baseDir = filepath.Join(home, ".dsh-gemini-web-adapter-go")
		} else {
			baseDir = "./runtime"
		}
	}

	if err := os.MkdirAll(baseDir, 0755); err != nil {
		log.Fatalf("Failed to initialize base directory %s: %v", baseDir, err)
	}

	driverCfg := engine.DriverConfig{
		BaseDir:     baseDir,
		ChromePath:  *chromeFlag,
		Headless:    *headlessFlag,
		ProxyServer: *proxyFlag,
	}

	driver := engine.NewDriver(driverCfg)

	// If MCP mode requested, run stdio loop
	if *mcpMode {
		log.SetOutput(os.Stderr)
		if err := mcp.RunStdio(driver); err != nil {
			log.Fatalf("MCP server exited with error: %v", err)
		}
		return
	}

	// Normal HTTP/SSE Gateway mode
	srv := server.NewServer(driver, baseDir, *portFlag)

	// Handle graceful shutdown
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)

	go func() {
		<-sigCh
		fmt.Println("\nShutting down Go Gemini Gateway...")
		os.Exit(0)
	}()

	if err := srv.ListenAndServe(); err != nil {
		log.Fatalf("Server failed: %v", err)
	}
}
