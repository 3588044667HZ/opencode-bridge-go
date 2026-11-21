package main

import (
	"context"
	"errors"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"go-bridge/internal/process"

	"go-bridge/internal/processmcp"
)

func main() {
	log.SetFlags(0)
	log.SetOutput(os.Stderr)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	workspace := strings.TrimSpace(os.Getenv("BRIDGE_WORKSPACE"))
	if workspace == "" {
		cwd, err := os.Getwd()
		if err != nil {
			log.Fatalf("cannot determine working directory: %v", err)
		}
		workspace = filepath.Join(cwd, "workspace")
	}
	cfg := process.DefaultConfig()
	cfg.Workspace = workspace
	cfg.ShellAlias = os.Getenv("BRIDGE_SHELL")

	manager, err := process.NewManager(cfg)
	if err != nil {
		log.Fatalf("%v", err)
	}
	server := mcp.NewServer(&mcp.Implementation{
		Name:    "go-bridge-process",
		Version: "0.1.0",
	}, nil)
	processmcp.Register(server, manager)
	runErr := server.Run(ctx, &mcp.StdioTransport{})
	_ = manager.Close()
	if runErr != nil && !errors.Is(runErr, context.Canceled) {
		log.Fatalf("server failed: %v", runErr)
	}
}
