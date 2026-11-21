package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"sort"
	"strings"
	"time"

	"go-bridge/bridge"
	"go-bridge/internal/bridgecfg"
)

func main() {
	log.SetFlags(0)
	log.SetOutput(os.Stderr)

	root, err := os.Getwd()
	if err != nil {
		log.Fatalf("cannot determine working directory: %v", err)
	}
	if err := bridgecfg.ApplyDotEnv(root); err != nil {
		log.Fatalf("cannot load .env: %v", err)
	}
	cfg, secrets, err := bridgecfg.Load(os.Getenv, root)
	if err != nil {
		log.Fatalf("%v", err)
	}
	instance := bridge.NewBridge(bridge.Options{
		Config: bridge.Config{
			Version:   1,
			Root:      cfg.Root,
			DataDir:   cfg.DataDir,
			Workspace: cfg.Workspace,
			MCP: bridge.MCPConfig{
				Host: bridgecfg.FilesHost,
				Port: cfg.MCPPort,
				Path: "/mcp",
			},
			Files: bridge.FilesConfig{
				Enabled:           cfg.FilesEnabled,
				Host:              bridgecfg.FilesHost,
				Port:              cfg.FilesPort,
				Path:              bridgecfg.FilesPath,
				MaxFileBytes:      cfg.MaxFileBytes,
				StagingQuotaBytes: cfg.StagingQuotaBytes,
			},
			Screenshot: bridge.ScreenshotConfig{Backend: cfg.ScreenshotBackend},
			Shell:      bridge.ShellConfig{Default: strings.TrimSpace(os.Getenv("BRIDGE_SHELL"))},
		},
		Secrets: bridge.Secrets{
			MCPToken:   secrets.MCPToken,
			FilesToken: secrets.FilesToken,
		},
		Env:    environment(os.Environ()),
		Logger: newLogger(),
	})
	ctx, stop := signal.NotifyContext(context.Background(), shutdownSignals()...)
	defer stop()
	if err := instance.Start(ctx); err != nil {
		log.Fatalf("%v", err)
	}
	logStartup(instance)
	<-ctx.Done()
	stopCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := instance.Stop(stopCtx); err != nil {
		log.Printf("stop failed: %v", err)
	}
}

func newLogger() bridge.Logger {
	allowed := map[string]bool{
		"component":  true,
		"member":     true,
		"port":       true,
		"path":       true,
		"state":      true,
		"durationMs": true,
		"code":       true,
	}
	return func(level bridge.LogLevel, message string, fields map[string]any) {
		keys := make([]string, 0, len(fields))
		for key := range fields {
			if allowed[key] {
				keys = append(keys, key)
			}
		}
		sort.Strings(keys)
		var builder strings.Builder
		for _, key := range keys {
			fmt.Fprintf(&builder, " %s=%v", key, fields[key])
		}
		log.Printf("%s: %s%s", level, message, builder.String())
	}
}

func logStartup(instance *bridge.Bridge) {
	if url := instance.MCPURL(); url != nil {
		log.Printf("MCP URL: %s", *url)
	}
	if url := instance.FilesURL(); url != nil {
		log.Printf("Files URL: %s", *url)
	} else {
		log.Printf("Files URL: disabled")
	}
	cfg := instance.Config()
	log.Printf("Workspace: %s", cfg.Workspace)
	log.Printf("Tools: %s", strings.Join(instance.ExpectedTools(), ", "))
	shell := instance.ShellSummary()
	log.Printf("Shell: %s (available: %s)", shell.Default, strings.Join(shell.Available, ", "))
}

func environment(entries []string) map[string]string {
	env := make(map[string]string, len(entries))
	for _, entry := range entries {
		key, value, ok := strings.Cut(entry, "=")
		if !ok {
			continue
		}
		env[key] = value
	}
	return env
}
