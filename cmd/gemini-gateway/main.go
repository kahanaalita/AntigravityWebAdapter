package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/kahanaalita/AntigravityWebAdapter/internal/bundler"
	"github.com/kahanaalita/AntigravityWebAdapter/internal/engine"
	"github.com/kahanaalita/AntigravityWebAdapter/internal/mcp"
	"github.com/kahanaalita/AntigravityWebAdapter/internal/server"
)

func main() {
	portFlag := flag.Int("port", 5690, "HTTP gateway listen port")
	baseFlag := flag.String("base", "", "Base directory for profile and temp files")
	chromeFlag := flag.String("chrome", "", "Path to Google Chrome executable")
	headlessFlag := flag.Bool("headless", false, "Run Chrome in headless mode")
	proxyFlag := flag.String("proxy", "", "Proxy server (e.g. socks5://127.0.0.1:10808)")
	mcpMode := flag.Bool("mcp", false, "Run in MCP stdio mode for Antigravity integration")
	bundleFlag := flag.String("bundle", "", "Path to project directory to bundle into Markdown")
	outFlag := flag.String("out", "", "Output markdown file path for bundled project")
	flag.Parse()

	if *bundleFlag != "" {
		b := bundler.New(bundler.Config{RootDir: *bundleFlag})
		dump, stats, err := b.Bundle()
		if err != nil {
			log.Fatalf("Bundle failed: %v", err)
		}
		if *outFlag != "" {
			if err := os.MkdirAll(filepath.Dir(*outFlag), 0755); err != nil && filepath.Dir(*outFlag) != "." {
				log.Fatalf("Failed to create output directory: %v", err)
			}
			if err := os.WriteFile(*outFlag, []byte(dump), 0644); err != nil {
				log.Fatalf("Failed to write bundle output to %s: %v", *outFlag, err)
			}
		} else {
			fmt.Print(dump)
		}
		fmt.Fprintf(os.Stderr, "Codebase bundled successfully: %d files, ~%d tokens, %d bytes\n", stats.TotalFiles, stats.EstimatedTokens, stats.TotalBytes)
		os.Exit(0)
	}

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
		defer func() {
			_ = driver.Close()
		}()
		if err := mcp.RunStdio(driver); err != nil {
			log.Fatalf("MCP server exited with error: %v", err)
		}
		return
	}

	// Normal HTTP/SSE Gateway mode
	srv := server.NewServer(driver, baseDir, *portFlag)
	defer func() {
		_ = driver.Close()
	}()

	// Handle graceful shutdown
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)

	go func() {
		<-sigCh
		fmt.Println("\nShutting down Go Gemini Gateway...")
		_ = driver.Close()
		os.Exit(0)
	}()

	if err := srv.ListenAndServe(); err != nil {
		log.Fatalf("Server failed: %v", err)
	}
}
