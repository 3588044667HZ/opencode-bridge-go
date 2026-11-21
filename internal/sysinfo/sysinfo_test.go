package sysinfo

import (
	"encoding/json"
	"errors"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"go-bridge/internal/clipboard"
	"go-bridge/internal/screenshot"
)

func testConfig(root string) Config {
	return Config{
		Workspace:      filepath.Join(root, "workspace"),
		DataDir:        filepath.Join(root, "data"),
		StagingInbox:   filepath.Join(root, "data", "staging", "inbox"),
		StagingOutbox:  filepath.Join(root, "data", "staging", "outbox"),
		FilesEnabled:   true,
		FilesHost:      "127.0.0.1",
		FilesPort:      8766,
		FilesPath:      "/files",
		ShellDefault:   "test-shell",
		ShellAvailable: []string{"test-shell", "other-shell"},
		ShellPathStyle: "posix",
	}
}

func testCapabilities() (screenshot.Capability, clipboard.Capability) {
	backend := "ffmpeg"
	clipboardBackend := "powershell"
	return screenshot.Capability{
		Available:         true,
		ConfiguredBackend: "auto",
		DetectedBackend:   &backend,
	}, clipboard.Capability{
		Available: true,
		Backend:   &clipboardBackend,
	}
}

func TestGet(t *testing.T) {
	root := t.TempDir()
	screenshotCapability, clipboardCapability := testCapabilities()
	system := New(testConfig(root), screenshotCapability, clipboardCapability)
	info, err := system.Get()
	if err != nil {
		t.Fatal(err)
	}
	if info.Hostname == "" || info.Arch == "" || info.OS.Platform == "" || info.OS.Type == "" {
		t.Fatalf("missing system fields: %#v", info)
	}
	if info.Shell.Default != "test-shell" || len(info.Shell.Available) != 2 || info.Shell.PathStyle != "posix" {
		t.Fatalf("unexpected shell: %#v", info.Shell)
	}
	if info.Paths.DefaultWorkdir != info.Paths.WorkspaceRoot || info.Paths.DefaultWorkdir != filepath.Join(root, "workspace") {
		t.Fatalf("unexpected paths: %#v", info.Paths)
	}
	if !filepath.IsAbs(info.Paths.DataDir) || !filepath.IsAbs(info.Paths.Staging.Inbox) || !filepath.IsAbs(info.Paths.Staging.Outbox) {
		t.Fatalf("paths are not absolute: %#v", info.Paths)
	}
	if !info.FileService.Enabled || info.FileService.Host != "127.0.0.1" || info.FileService.Port != 8766 {
		t.Fatalf("unexpected file service: %#v", info.FileService)
	}
	if info.FileService.BaseURL == nil || *info.FileService.BaseURL != "http://127.0.0.1:8766/files" {
		t.Fatalf("unexpected base URL: %#v", info.FileService.BaseURL)
	}
	if !info.Capabilities.Screenshot.Available || info.Capabilities.Screenshot.DetectedBackend == nil || *info.Capabilities.Screenshot.DetectedBackend != "ffmpeg" {
		t.Fatalf("unexpected screenshot capability: %#v", info.Capabilities.Screenshot)
	}
	if !info.Capabilities.Clipboard.Available || info.Capabilities.Clipboard.Backend == nil || *info.Capabilities.Clipboard.Backend != "powershell" {
		t.Fatalf("unexpected clipboard capability: %#v", info.Capabilities.Clipboard)
	}
}

func TestFileServiceDisabled(t *testing.T) {
	root := t.TempDir()
	cfg := testConfig(root)
	cfg.FilesEnabled = false
	screenshotCapability, clipboardCapability := testCapabilities()
	info, err := New(cfg, screenshotCapability, clipboardCapability).Get()
	if err != nil {
		t.Fatal(err)
	}
	if info.FileService.Enabled || info.FileService.BaseURL != nil || info.FileService.Host != "127.0.0.1" || info.FileService.Port != 8766 {
		t.Fatalf("unexpected file service: %#v", info.FileService)
	}
}

func TestShellErrors(t *testing.T) {
	root := t.TempDir()
	screenshotCapability, clipboardCapability := testCapabilities()
	cfg := testConfig(root)
	cfg.ShellDefault = "missing"
	_, err := New(cfg, screenshotCapability, clipboardCapability).Get()
	requireCode(t, err, "system_error")
	cfg.ShellDefault = ""
	_, err = New(cfg, screenshotCapability, clipboardCapability).Get()
	requireCode(t, err, "system_error")
}

func TestJSONShape(t *testing.T) {
	root := t.TempDir()
	screenshotCapability, clipboardCapability := testCapabilities()
	info, err := New(testConfig(root), screenshotCapability, clipboardCapability).Get()
	if err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(info)
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	for _, field := range []string{
		`"os"`,
		`"hostname"`,
		`"arch"`,
		`"shell"`,
		`"paths"`,
		`"fileService"`,
		`"capabilities"`,
		`"baseUrl"`,
		`"configuredBackend"`,
		`"detectedBackend"`,
		`"backend"`,
		`"reason"`,
	} {
		if !strings.Contains(text, field) {
			t.Fatalf("missing field %s in %s", field, text)
		}
	}
}

func TestPlatformAndArchMapping(t *testing.T) {
	switch runtime.GOOS {
	case "windows":
		if platformName() != "win32" {
			t.Fatalf("unexpected platform: %s", platformName())
		}
	case "linux":
		if platformName() != "linux" {
			t.Fatalf("unexpected platform: %s", platformName())
		}
	case "darwin":
		if platformName() != "darwin" {
			t.Fatalf("unexpected platform: %s", platformName())
		}
	}
	if runtime.GOARCH == "amd64" && archName() != "x64" {
		t.Fatalf("unexpected arch: %s", archName())
	}
}

func requireCode(t *testing.T, err error, code string) {
	t.Helper()
	var systemErr *Error
	if !errors.As(err, &systemErr) || systemErr.Code != code {
		t.Fatalf("expected %s, got %v", code, err)
	}
}
