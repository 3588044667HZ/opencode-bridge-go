package systemmcp

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"go-bridge/internal/clipboard"
	"go-bridge/internal/screenshot"
	"go-bridge/internal/sysinfo"
)

func newTestServices(t *testing.T) (*sysinfo.System, *clipboard.Service, *screenshot.Service) {
	t.Helper()
	root := t.TempDir()
	clip := clipboard.New()
	shot, err := screenshot.New(filepath.Join(root, "screenshots"), "auto")
	if err != nil {
		t.Fatal(err)
	}
	system := sysinfo.New(sysinfo.Config{
		Workspace:      filepath.Join(root, "workspace"),
		DataDir:        filepath.Join(root, "data"),
		StagingInbox:   filepath.Join(root, "data", "staging", "inbox"),
		StagingOutbox:  filepath.Join(root, "data", "staging", "outbox"),
		FilesEnabled:   true,
		FilesHost:      "127.0.0.1",
		FilesPort:      8766,
		FilesPath:      "/files",
		ShellDefault:   "test-shell",
		ShellAvailable: []string{"test-shell"},
		ShellPathStyle: "posix",
	}, shot.Capability(), clip.Capability())
	return system, clip, shot
}

func newTestSession(t *testing.T, system *sysinfo.System, clip *clipboard.Service, shot *screenshot.Service) *mcp.ClientSession {
	t.Helper()
	server := mcp.NewServer(&mcp.Implementation{Name: "system-test", Version: "0"}, nil)
	Register(server, system, clip, shot)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	t.Cleanup(cancel)
	serverTransport, clientTransport := mcp.NewInMemoryTransports()
	serverSession, err := server.Connect(ctx, serverTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	client := mcp.NewClient(&mcp.Implementation{Name: "system-client", Version: "0"}, nil)
	clientSession, err := client.Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = clientSession.Close()
		_ = serverSession.Close()
	})
	return clientSession
}

func callTool(t *testing.T, session *mcp.ClientSession, name string, args map[string]any) *mcp.CallToolResult {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	result, err := session.CallTool(ctx, &mcp.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func toolText(t *testing.T, result *mcp.CallToolResult) string {
	t.Helper()
	if len(result.Content) == 0 {
		t.Fatal("tool returned no content")
	}
	text, ok := result.Content[0].(*mcp.TextContent)
	if !ok {
		t.Fatalf("unexpected content type: %T", result.Content[0])
	}
	return text.Text
}

func TestSystemInfoTool(t *testing.T) {
	t.Setenv("LAPTOP_MCP_TOKEN", "secret-system-info-token")
	t.Setenv("BRIDGE_FILES_TOKEN", "secret-system-info-files-token")
	system, clip, shot := newTestServices(t)
	session := newTestSession(t, system, clip, shot)
	result := callTool(t, session, "get_system_info", nil)
	if result.IsError {
		t.Fatalf("get_system_info failed: %s", toolText(t, result))
	}
	text := toolText(t, result)
	if !strings.Contains(text, "\n  \"os\"") {
		t.Fatalf("expected pretty JSON, got %s", text)
	}
	if strings.Contains(text, "secret-system-info-token") || strings.Contains(text, "secret-system-info-files-token") {
		t.Fatal("system info leaked a token")
	}
	var info sysinfo.Info
	if err := json.Unmarshal([]byte(text), &info); err != nil {
		t.Fatal(err)
	}
	if info.Hostname == "" || info.Shell.Default != "test-shell" || info.FileService.BaseURL == nil {
		t.Fatalf("unexpected system info: %#v", info)
	}
}

func TestClipboardTool(t *testing.T) {
	system, clip, shot := newTestServices(t)
	session := newTestSession(t, system, clip, shot)

	result := callTool(t, session, "set_clipboard", nil)
	if !result.IsError || !strings.Contains(toolText(t, result), "invalid_arguments") {
		t.Fatalf("expected invalid_arguments, got %s", toolText(t, result))
	}
	result = callTool(t, session, "set_clipboard", map[string]any{"text": "a\x00b"})
	if !result.IsError || !strings.Contains(toolText(t, result), "invalid_text") {
		t.Fatalf("expected invalid_text, got %s", toolText(t, result))
	}

	capability := clip.Capability()
	if !capability.Available {
		result = callTool(t, session, "get_clipboard", nil)
		if !result.IsError || !strings.Contains(toolText(t, result), "clipboard_unavailable") {
			t.Fatalf("expected clipboard_unavailable, got %s", toolText(t, result))
		}
		return
	}
	result = callTool(t, session, "get_clipboard", nil)
	if result.IsError {
		message := toolText(t, result)
		if strings.Contains(message, "clipboard_error") {
			t.Skipf("clipboard backend is not usable in this environment: %s", message)
		}
		t.Fatalf("get_clipboard failed: %s", message)
	}
	var read clipboard.ReadResult
	if err := json.Unmarshal([]byte(toolText(t, result)), &read); err != nil {
		t.Fatal(err)
	}
	if read.Backend == "" || read.Bytes != len(read.Text) {
		t.Fatalf("unexpected read result: %#v", read)
	}
}

func TestCaptureScreenshotTool(t *testing.T) {
	system, clip, shot := newTestServices(t)
	session := newTestSession(t, system, clip, shot)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	result, callErr := session.CallTool(ctx, &mcp.CallToolParams{Name: "capture_screenshot"})
	cancel()
	if callErr != nil {
		t.Skipf("screenshot call failed: %v", callErr)
	}
	capability := shot.Capability()
	if !capability.Available {
		if !result.IsError || !strings.Contains(toolText(t, result), "screenshot_unavailable") {
			t.Fatalf("expected screenshot_unavailable, got %s", toolText(t, result))
		}
		return
	}
	if result.IsError {
		message := toolText(t, result)
		if strings.Contains(message, "screenshot_error") {
			t.Skipf("screenshot capture is not usable in this environment: %s", message)
		}
		t.Fatalf("capture_screenshot failed: %s", message)
	}
	var capture screenshot.CaptureResult
	if err := json.Unmarshal([]byte(toolText(t, result)), &capture); err != nil {
		t.Fatal(err)
	}
	if capture.Bytes <= 0 || capture.Width <= 0 || capture.Height <= 0 {
		t.Fatalf("unexpected capture result: %#v", capture)
	}
	info, err := os.Stat(capture.Path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Size() != capture.Bytes {
		t.Fatalf("size mismatch: %d vs %d", info.Size(), capture.Bytes)
	}
	if capture.Inlined {
		if len(result.Content) != 2 {
			t.Fatalf("expected image content, got %d items", len(result.Content))
		}
		image, ok := result.Content[1].(*mcp.ImageContent)
		if !ok {
			t.Fatalf("unexpected image content type: %T", result.Content[1])
		}
		if image.MIMEType != "image/png" {
			t.Fatalf("unexpected MIME type: %s", image.MIMEType)
		}
		decoded, err := base64.StdEncoding.DecodeString(string(image.Data))
		if err != nil {
			t.Fatal(err)
		}
		expected, err := os.ReadFile(capture.Path)
		if err != nil {
			t.Fatal(err)
		}
		if string(decoded) != string(expected) {
			t.Fatal("inlined image does not match saved screenshot")
		}
	} else if len(result.Content) != 1 {
		t.Fatalf("unexpected content for non-inlined screenshot: %d", len(result.Content))
	}
}
