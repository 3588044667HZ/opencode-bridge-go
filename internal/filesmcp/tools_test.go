package filesmcp

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"go-bridge/internal/files"
)

func newTestService(t *testing.T) *files.Service {
	t.Helper()
	root := t.TempDir()
	service, err := files.New(files.Config{
		WorkspaceRoot:     filepath.Join(root, "workspace"),
		StagingInbox:      filepath.Join(root, "staging", "inbox"),
		StagingOutbox:     filepath.Join(root, "staging", "outbox"),
		MaxFileBytes:      1 << 20,
		StagingQuotaBytes: 4 << 20,
	})
	if err != nil {
		t.Fatal(err)
	}
	return service
}

func newTestSession(t *testing.T, service *files.Service) *mcp.ClientSession {
	t.Helper()
	server := mcp.NewServer(&mcp.Implementation{Name: "files-test", Version: "0"}, nil)
	Register(server, service)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	t.Cleanup(cancel)
	serverTransport, clientTransport := mcp.NewInMemoryTransports()
	serverSession, err := server.Connect(ctx, serverTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	client := mcp.NewClient(&mcp.Implementation{Name: "files-client", Version: "0"}, nil)
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
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	result, err := session.CallTool(ctx, &mcp.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func callText(t *testing.T, session *mcp.ClientSession, name string, args map[string]any) string {
	t.Helper()
	result := callTool(t, session, name, args)
	if result.IsError {
		t.Fatalf("%s failed: %s", name, toolText(t, result))
	}
	return toolText(t, result)
}

func callInto(t *testing.T, session *mcp.ClientSession, name string, args map[string]any, target any) {
	t.Helper()
	text := callText(t, session, name, args)
	if err := json.Unmarshal([]byte(text), target); err != nil {
		t.Fatalf("cannot decode %s result: %v\n%s", name, err, text)
	}
}

func requireToolError(t *testing.T, session *mcp.ClientSession, name string, args map[string]any, code string) {
	t.Helper()
	result := callTool(t, session, name, args)
	if !result.IsError {
		t.Fatalf("%s unexpectedly succeeded: %s", name, toolText(t, result))
	}
	text := toolText(t, result)
	if !strings.Contains(text, code) {
		t.Fatalf("%s error %q does not contain %q", name, text, code)
	}
}

func toolText(t *testing.T, result *mcp.CallToolResult) string {
	t.Helper()
	if len(result.Content) != 1 {
		t.Fatalf("unexpected content count: %d", len(result.Content))
	}
	text, ok := result.Content[0].(*mcp.TextContent)
	if !ok {
		t.Fatalf("unexpected content type: %T", result.Content[0])
	}
	return text.Text
}

func TestFileToolsRoundTrip(t *testing.T) {
	session := newTestSession(t, newTestService(t))
	var write files.WriteFileResult
	callInto(t, session, "write_file", map[string]any{
		"path":        "notes/a.txt",
		"content":     "hello\nworld\n",
		"create_dirs": true,
	}, &write)
	if write.Bytes != 12 || !write.Created || write.Replaced {
		t.Fatalf("unexpected write: %#v", write)
	}
	var read files.ReadFileResult
	callInto(t, session, "read_file", map[string]any{"path": "notes/a.txt"}, &read)
	if read.TotalLines != 2 || read.ReturnedLines != 2 || read.Content != "hello\nworld" {
		t.Fatalf("unexpected read: %#v", read)
	}
	var page files.ReadFileResult
	callInto(t, session, "read_file", map[string]any{"path": "notes/a.txt", "offset": 1, "limit": 1}, &page)
	if page.Content != "hello" || !page.Truncated {
		t.Fatalf("unexpected page: %#v", page)
	}
	var list files.ListDirectoryResult
	callInto(t, session, "list_directory", map[string]any{"path": "notes"}, &list)
	if len(list.Entries) != 1 || list.Entries[0].Name != "a.txt" || list.Entries[0].Type != "file" {
		t.Fatalf("unexpected list: %#v", list)
	}
	var rootList files.ListDirectoryResult
	callInto(t, session, "list_directory", nil, &rootList)
	if rootList.Path != "." || len(rootList.Entries) != 1 || rootList.Entries[0].Name != "notes" || rootList.Entries[0].Type != "directory" {
		t.Fatalf("unexpected root list: %#v", rootList)
	}
	var stat files.StatFileResult
	callInto(t, session, "stat_file", map[string]any{"path": "notes/a.txt"}, &stat)
	if stat.Type != "file" || stat.Size != 12 || stat.IsSymlink {
		t.Fatalf("unexpected stat: %#v", stat)
	}
	var empty files.WriteFileResult
	callInto(t, session, "write_file", map[string]any{"path": "empty.txt", "content": ""}, &empty)
	if empty.Bytes != 0 || !empty.Created {
		t.Fatalf("unexpected empty write: %#v", empty)
	}
	requireToolError(t, session, "write_file", map[string]any{"path": "missing.txt"}, "invalid_arguments")
	requireToolError(t, session, "read_file", map[string]any{"path": "../outside.txt"}, "path_escape")
	requireToolError(t, session, "read_file", map[string]any{"offset": 1}, "invalid_arguments")
}

func TestStagingTools(t *testing.T) {
	service := newTestService(t)
	inbox, err := service.StagingPath("inbox")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(inbox, "firmware.bin"), []byte("data"), 0o644); err != nil {
		t.Fatal(err)
	}
	session := newTestSession(t, service)
	var list files.StagingList
	callInto(t, session, "list_staged_files", map[string]any{"direction": "inbox"}, &list)
	if list.Direction != "inbox" || len(list.Files) != 1 || list.Files[0].Name != "firmware.bin" || list.TotalBytes != 4 {
		t.Fatalf("unexpected staging list: %#v", list)
	}
	var deleted files.DeleteStagedFileResult
	callInto(t, session, "delete_staged_file", map[string]any{"direction": "inbox", "name": "firmware.bin"}, &deleted)
	if !deleted.Deleted || deleted.Name != "firmware.bin" {
		t.Fatalf("unexpected delete: %#v", deleted)
	}
	requireToolError(t, session, "delete_staged_file", map[string]any{"direction": "inbox", "name": "firmware.bin"}, "not_found")
	requireToolError(t, session, "list_staged_files", map[string]any{"direction": "sideways"}, "invalid_direction")
	requireToolError(t, session, "delete_staged_file", map[string]any{"direction": "inbox", "name": "bad name"}, "invalid_name")
}
