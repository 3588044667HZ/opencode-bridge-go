package process

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func newTestManager(t *testing.T) *Manager {
	t.Helper()
	dir := t.TempDir()
	cfg := DefaultConfig()
	cfg.Workspace = dir
	manager, err := NewManager(cfg)
	if err != nil {
		t.Fatal(err)
	}
	return manager
}

func testCommand(script string) string {
	if runtime.GOOS == "windows" {
		return script
	}
	switch script {
	case "echo go-test":
		return "echo go-test"
	case "sleep 2; echo done":
		return "sleep 2; echo done"
	default:
		return script
	}
}

func TestStartReadAndTerminate(t *testing.T) {
	manager := newTestManager(t)
	windows := runtime.GOOS == "windows"
	command := "echo go-test"
	if windows {
		command = "Write-Output go-test"
	}
	result, err := manager.Start(StartOptions{Command: command, Timeout: 5 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result.Output, "go-test") {
		t.Fatalf("missing output: %q", result.Output)
	}
	if result.State != StateFinished {
		read, err := manager.Read(result.PID, ReadOptions{Timeout: 5 * time.Second})
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(read.Output, "go-test") && read.State != StateFinished {
			t.Fatalf("process did not finish: %#v", read)
		}
	}
}

func TestWorkdir(t *testing.T) {
	manager := newTestManager(t)
	sub := filepath.Join(manager.cfg.Workspace, "sub")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	command := "pwd"
	if runtime.GOOS == "windows" {
		command = "Write-Output (Get-Location).Path"
	}
	result, err := manager.Start(StartOptions{Command: command, Workdir: "sub", Timeout: 5 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result.Output, "sub") {
		t.Fatalf("workdir not applied: %q", result.Output)
	}
}

func TestShellDoesNotSeeTokens(t *testing.T) {
	manager := newTestManager(t)
	t.Setenv("LAPTOP_MCP_TOKEN", "secret-mcp")
	t.Setenv("BRIDGE_FILES_TOKEN", "secret-files")
	t.Setenv("SUPERGATEWAY_API_KEY", "secret-gateway")
	command := `printf '[%s|%s|%s]\n' "$LAPTOP_MCP_TOKEN" "$BRIDGE_FILES_TOKEN" "$SUPERGATEWAY_API_KEY"`
	if runtime.GOOS == "windows" {
		command = `Write-Output ("[" + $env:LAPTOP_MCP_TOKEN + "|" + $env:BRIDGE_FILES_TOKEN + "|" + $env:SUPERGATEWAY_API_KEY + "]")`
	}
	result, err := manager.Start(StartOptions{Command: command, Timeout: 5 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result.Output, "[||]") {
		t.Fatalf("tokens leaked into shell environment: %q", result.Output)
	}
	if strings.Contains(result.Output, "secret-") {
		t.Fatalf("token value leaked into shell output: %q", result.Output)
	}
}

func TestCloseTerminatesSessions(t *testing.T) {
	manager := newTestManager(t)
	command := "sleep 30"
	if runtime.GOOS == "windows" {
		command = "Start-Sleep -Seconds 30"
	}
	result, err := manager.Start(StartOptions{Command: command, Timeout: 100 * time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	if result.State != StateRunning {
		t.Fatalf("expected running, got %s", result.State)
	}
	if err := manager.Close(); err != nil {
		t.Fatal(err)
	}
	if sessions := manager.List(); len(sessions) != 0 {
		t.Fatalf("sessions remain after close: %#v", sessions)
	}
	if _, err := manager.Read(result.PID, ReadOptions{Timeout: time.Millisecond}); err == nil {
		t.Fatal("expected closed session to be unavailable")
	}
	_, err = manager.Start(StartOptions{Command: command, Timeout: time.Millisecond})
	processErr, ok := err.(*Error)
	if !ok || processErr.Code != "process_backend_unavailable" {
		t.Fatalf("expected process_backend_unavailable, got %v", err)
	}
}

func TestLongProcessAndTerminate(t *testing.T) {
	manager := newTestManager(t)
	command := "sleep 2; echo done"
	if runtime.GOOS == "windows" {
		command = "Start-Sleep -Seconds 2; Write-Output done"
	}
	result, err := manager.Start(StartOptions{Command: command, Timeout: 100 * time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	if result.State != StateRunning {
		t.Fatalf("expected running, got %s", result.State)
	}
	if err := manager.Terminate(result.PID); err != nil {
		t.Fatal(err)
	}
	if sessions := manager.List(); len(sessions) != 0 {
		t.Fatalf("session still listed: %#v", sessions)
	}
}
