package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/charlesolinsky/local-llm/internal/config"
	"github.com/charlesolinsky/local-llm/internal/server"
	"github.com/charlesolinsky/local-llm/internal/tui"
)

var version = "0.1.0"

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(2)
	}

	switch os.Args[1] {
	case "serve":
		os.Exit(runServe(os.Args[2:]))
	case "tui", "ui":
		os.Exit(runTUI(os.Args[2:]))
	case "status":
		os.Exit(runStatus(os.Args[2:]))
	case "version":
		fmt.Println(version)
	case "help", "-h", "--help":
		printUsage()
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q\n\n", os.Args[1])
		printUsage()
		os.Exit(2)
	}
}

func printUsage() {
	fmt.Fprintf(os.Stderr, `local-llm — authenticated OpenAI-compatible gateway for local Ollama

Usage:
  local-llm serve  [--config path] [--env-file path]
  local-llm tui    [--config path] [--env-file path]
  local-llm status [--config path] [--env-file path]
  local-llm version

  tui (alias: ui)  Dashboard: start/stop, install models, live logs
  serve            Headless gateway for LAN / launchd

Environment:
  LOCAL_LLM_API_KEY   Required bearer token for /v1 requests
`)
}

func runTUI(args []string) int {
	fs := flag.NewFlagSet("tui", flag.ExitOnError)
	cfgPath := fs.String("config", defaultConfigPath(), "path to models.yaml")
	envFile := fs.String("env-file", defaultEnvPath(), "optional .env file to load")
	_ = fs.Parse(args)

	if err := loadEnvFile(*envFile); err != nil {
		fmt.Fprintf(os.Stderr, "warning: %v\n", err)
	}

	store, err := config.Open(*cfgPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "config error: %v\n", err)
		return 1
	}

	if err := tui.Run(store, *envFile); err != nil {
		fmt.Fprintf(os.Stderr, "tui error: %v\n", err)
		return 1
	}
	return 0
}

func runServe(args []string) int {
	fs := flag.NewFlagSet("serve", flag.ExitOnError)
	cfgPath := fs.String("config", defaultConfigPath(), "path to models.yaml")
	envFile := fs.String("env-file", defaultEnvPath(), "optional .env file to load")
	_ = fs.Parse(args)

	if err := loadEnvFile(*envFile); err != nil {
		fmt.Fprintf(os.Stderr, "warning: %v\n", err)
	}

	store, err := config.Open(*cfgPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "config error: %v\n", err)
		return 1
	}

	srv, err := server.New(store)
	if err != nil {
		fmt.Fprintf(os.Stderr, "server error: %v\n", err)
		return 1
	}

	errCh := make(chan error, 1)
	go func() {
		errCh <- srv.ListenAndServe()
	}()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	select {
	case err := <-errCh:
		if err != nil && err != http.ErrServerClosed {
			fmt.Fprintf(os.Stderr, "listen error: %v\n", err)
			return 1
		}
	case sig := <-sigCh:
		fmt.Fprintf(os.Stderr, "received %s, shutting down…\n", sig)
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = srv.Shutdown(ctx)
	}
	return 0
}

func runStatus(args []string) int {
	fs := flag.NewFlagSet("status", flag.ExitOnError)
	cfgPath := fs.String("config", defaultConfigPath(), "path to models.yaml")
	envFile := fs.String("env-file", defaultEnvPath(), "optional .env file to load")
	_ = fs.Parse(args)

	_ = loadEnvFile(*envFile)

	cfg, err := config.Load(*cfgPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "config error: %v\n", err)
		return 1
	}

	host := cfg.Listen
	if len(host) >= 7 && host[:7] == "0.0.0.0" {
		host = "127.0.0.1" + host[7:]
	}

	client := &http.Client{Timeout: 3 * time.Second}
	healthURL := "http://" + host + "/health"
	resp, err := client.Get(healthURL)
	if err != nil {
		fmt.Printf("gateway: DOWN (%v)\n", err)
		fmt.Printf("config:  %s\n", *cfgPath)
		fmt.Printf("listen:  %s\n", cfg.Listen)
		fmt.Printf("ollama:  %s\n", cfg.OllamaBase)
		fmt.Printf("models:  %v\n", cfg.AliasNames())
		return 1
	}
	defer resp.Body.Close()

	var body map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&body)
	fmt.Printf("gateway: UP (HTTP %d)\n", resp.StatusCode)
	fmt.Printf("health:  %v\n", body)
	fmt.Printf("config:  %s\n", *cfgPath)
	fmt.Printf("models:  %v\n", cfg.AliasNames())
	if resp.StatusCode >= 300 {
		return 1
	}
	return 0
}

func defaultConfigPath() string {
	if p := os.Getenv("LOCAL_LLM_CONFIG"); p != "" {
		return p
	}
	return filepath.Join(findRepoRoot(), "config", "models.yaml")
}

func defaultEnvPath() string {
	return filepath.Join(findRepoRoot(), ".env")
}

func findRepoRoot() string {
	// Prefer directory containing the binary's sibling config/, else cwd.
	exe, err := os.Executable()
	if err == nil {
		dir := filepath.Dir(exe)
		// bin/local-llm → repo root
		candidate := filepath.Clean(filepath.Join(dir, ".."))
		if fileExists(filepath.Join(candidate, "config", "models.yaml")) {
			return candidate
		}
	}
	cwd, err := os.Getwd()
	if err != nil {
		return "."
	}
	return cwd
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// loadEnvFile loads KEY=VALUE lines from a .env file into the process env
// without overriding variables that are already set.
func loadEnvFile(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("read env file %s: %w", path, err)
	}
	lines := splitLines(string(data))
	for _, line := range lines {
		line = trimSpace(line)
		if line == "" || line[0] == '#' {
			continue
		}
		key, val, ok := splitKV(line)
		if !ok {
			continue
		}
		if os.Getenv(key) == "" {
			_ = os.Setenv(key, val)
		}
	}
	return nil
}

func splitLines(s string) []string {
	var out []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			out = append(out, s[start:i])
			start = i + 1
		}
	}
	if start < len(s) {
		out = append(out, s[start:])
	}
	return out
}

func trimSpace(s string) string {
	for len(s) > 0 && (s[0] == ' ' || s[0] == '\t' || s[0] == '\r') {
		s = s[1:]
	}
	for len(s) > 0 {
		c := s[len(s)-1]
		if c == ' ' || c == '\t' || c == '\r' {
			s = s[:len(s)-1]
			continue
		}
		break
	}
	return s
}

func splitKV(line string) (string, string, bool) {
	for i := 0; i < len(line); i++ {
		if line[i] == '=' {
			key := trimSpace(line[:i])
			val := trimSpace(line[i+1:])
			if len(val) >= 2 {
				if (val[0] == '"' && val[len(val)-1] == '"') || (val[0] == '\'' && val[len(val)-1] == '\'') {
					val = val[1 : len(val)-1]
				}
			}
			if key == "" {
				return "", "", false
			}
			return key, val, true
		}
	}
	return "", "", false
}
