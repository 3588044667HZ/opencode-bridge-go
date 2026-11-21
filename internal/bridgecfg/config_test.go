package bridgecfg

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func loadWith(t *testing.T, root string, values map[string]string) (Config, Secrets, error) {
	t.Helper()
	return Load(func(key string) string { return values[key] }, root)
}

func TestLoadDefaults(t *testing.T) {
	root := t.TempDir()
	cfg, secrets, err := loadWith(t, root, map[string]string{"LAPTOP_MCP_TOKEN": "mcp-token"})
	if err != nil {
		t.Fatal(err)
	}
	if secrets.MCPToken != "mcp-token" || secrets.FilesToken != "" {
		t.Fatalf("unexpected secrets: %#v", secrets)
	}
	if cfg.DataDir != filepath.Join(root, "data") {
		t.Fatalf("unexpected data dir: %s", cfg.DataDir)
	}
	if cfg.Workspace != filepath.Join(root, "data", "workspace") {
		t.Fatalf("unexpected workspace: %s", cfg.Workspace)
	}
	if cfg.MCPPort != DefaultMCPPort || cfg.FilesPort != DefaultFilesPort || cfg.FilesEnabled {
		t.Fatalf("unexpected ports: %#v", cfg)
	}
	if cfg.MaxFileBytes != DefaultMaxFileBytes || cfg.StagingQuotaBytes != DefaultStagingQuota {
		t.Fatalf("unexpected limits: %#v", cfg)
	}
	if cfg.ScreenshotBackend != "auto" {
		t.Fatalf("unexpected screenshot backend: %s", cfg.ScreenshotBackend)
	}
	if cfg.StagingInbox() != filepath.Join(root, "data", "staging", "inbox") {
		t.Fatalf("unexpected inbox: %s", cfg.StagingInbox())
	}
	if cfg.StagingOutbox() != filepath.Join(root, "data", "staging", "outbox") {
		t.Fatalf("unexpected outbox: %s", cfg.StagingOutbox())
	}
	if cfg.ScreenshotsDir() != filepath.Join(root, "data", "screenshots") {
		t.Fatalf("unexpected screenshots dir: %s", cfg.ScreenshotsDir())
	}
	if cfg.MCPAddr() != "127.0.0.1:8765" || cfg.FilesAddr() != "127.0.0.1:8766" {
		t.Fatalf("unexpected addresses: %s %s", cfg.MCPAddr(), cfg.FilesAddr())
	}
}

func TestLoadRelativePaths(t *testing.T) {
	root := t.TempDir()
	cfg, _, err := loadWith(t, root, map[string]string{
		"LAPTOP_MCP_TOKEN":   "mcp-token",
		"BRIDGE_DATA_DIR":    "state",
		"BRIDGE_WORKSPACE":   "work",
		"BRIDGE_FILES_TOKEN": "file-token",
	})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.DataDir != filepath.Join(root, "state") || cfg.Workspace != filepath.Join(root, "work") {
		t.Fatalf("unexpected paths: %#v", cfg)
	}
	if !cfg.FilesEnabled {
		t.Fatal("file service should be enabled")
	}
}

func TestLoadErrors(t *testing.T) {
	root := t.TempDir()
	cases := []struct {
		name   string
		values map[string]string
		part   string
	}{
		{"missing token", map[string]string{}, "LAPTOP_MCP_TOKEN"},
		{"same ports", map[string]string{"LAPTOP_MCP_TOKEN": "mcp", "BRIDGE_PORT": "9000", "BRIDGE_FILES_PORT": "9000"}, "BRIDGE_FILES_PORT"},
		{"invalid mcp port", map[string]string{"LAPTOP_MCP_TOKEN": "mcp", "BRIDGE_PORT": "0"}, "BRIDGE_PORT"},
		{"invalid files port", map[string]string{"LAPTOP_MCP_TOKEN": "mcp", "BRIDGE_FILES_PORT": "abc"}, "BRIDGE_FILES_PORT"},
		{"invalid max bytes", map[string]string{"LAPTOP_MCP_TOKEN": "mcp", "BRIDGE_MAX_FILE_BYTES": "0"}, "BRIDGE_MAX_FILE_BYTES"},
		{"invalid quota", map[string]string{"LAPTOP_MCP_TOKEN": "mcp", "BRIDGE_STAGING_QUOTA_BYTES": "abc"}, "BRIDGE_STAGING_QUOTA_BYTES"},
		{"quota below max", map[string]string{"LAPTOP_MCP_TOKEN": "mcp", "BRIDGE_MAX_FILE_BYTES": "100", "BRIDGE_STAGING_QUOTA_BYTES": "50"}, "BRIDGE_STAGING_QUOTA_BYTES"},
		{"invalid backend", map[string]string{"LAPTOP_MCP_TOKEN": "mcp", "BRIDGE_SCREENSHOT_BACKEND": "magic"}, "BRIDGE_SCREENSHOT_BACKEND"},
		{"equal tokens", map[string]string{"LAPTOP_MCP_TOKEN": "same", "BRIDGE_FILES_TOKEN": "same"}, "BRIDGE_FILES_TOKEN"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, _, err := loadWith(t, root, tc.values)
			if err == nil {
				t.Fatal("expected error")
			}
			if !strings.Contains(err.Error(), tc.part) {
				t.Fatalf("expected error containing %q, got %v", tc.part, err)
			}
		})
	}
}

func TestEnsureDirs(t *testing.T) {
	root := t.TempDir()
	cfg, _, err := loadWith(t, root, map[string]string{"LAPTOP_MCP_TOKEN": "mcp"})
	if err != nil {
		t.Fatal(err)
	}
	if err := cfg.EnsureDirs(); err != nil {
		t.Fatal(err)
	}
	for _, dir := range []string{cfg.DataDir, cfg.Workspace, cfg.StagingInbox(), cfg.StagingOutbox(), cfg.ScreenshotsDir()} {
		info, err := os.Stat(dir)
		if err != nil {
			t.Fatal(err)
		}
		if !info.IsDir() {
			t.Fatalf("not a directory: %s", dir)
		}
	}
	blocked := filepath.Join(root, "blocked")
	if err := os.WriteFile(blocked, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, _, err = loadWith(t, root, map[string]string{
		"LAPTOP_MCP_TOKEN": "mcp",
		"BRIDGE_WORKSPACE": "blocked",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := cfg.EnsureDirs(); err == nil {
		t.Fatal("expected workspace file to fail EnsureDirs")
	}
}

func TestWorkspaceCoversDataDir(t *testing.T) {
	root := t.TempDir()
	cfg, _, err := loadWith(t, root, map[string]string{"LAPTOP_MCP_TOKEN": "mcp"})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.WorkspaceCoversDataDir() {
		t.Fatal("default workspace should not cover data dir")
	}
	cfg, _, err = loadWith(t, root, map[string]string{
		"LAPTOP_MCP_TOKEN": "mcp",
		"BRIDGE_WORKSPACE": "data",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.WorkspaceCoversDataDir() {
		t.Fatal("workspace equal to data dir should be detected")
	}
	cfg, _, err = loadWith(t, root, map[string]string{
		"LAPTOP_MCP_TOKEN": "mcp",
		"BRIDGE_WORKSPACE": ".",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.WorkspaceCoversDataDir() {
		t.Fatal("workspace ancestor of data dir should be detected")
	}
}
