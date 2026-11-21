package bridge_test

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"go-bridge/bridge"
)

type authTransport struct {
	token string
}

func (a authTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	clone := request.Clone(request.Context())
	clone.Header.Set("Authorization", "Bearer "+a.token)
	return http.DefaultTransport.RoundTrip(clone)
}

type logEntry struct {
	level   bridge.LogLevel
	message string
	fields  map[string]any
}

type logCapture struct {
	mu      sync.Mutex
	entries []logEntry
}

func (c *logCapture) log(level bridge.LogLevel, message string, fields map[string]any) {
	clone := make(map[string]any, len(fields))
	for key, value := range fields {
		clone[key] = value
	}
	c.mu.Lock()
	c.entries = append(c.entries, logEntry{level: level, message: message, fields: clone})
	c.mu.Unlock()
}

func (c *logCapture) text() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	var builder strings.Builder
	for _, entry := range c.entries {
		builder.WriteString(string(entry.level))
		builder.WriteString(":")
		builder.WriteString(entry.message)
		for key, value := range entry.fields {
			builder.WriteString(" ")
			builder.WriteString(key)
			builder.WriteString("=")
			builder.WriteString(fmt.Sprintf("%v", value))
		}
		builder.WriteString("\n")
	}
	return builder.String()
}

func testEnv() map[string]string {
	env := map[string]string{}
	for _, entry := range os.Environ() {
		key, value, ok := strings.Cut(entry, "=")
		if !ok {
			continue
		}
		switch strings.ToLower(key) {
		case "laptop_mcp_token", "bridge_files_token", "supergateway_api_key", "supergateway_api_key_file":
			continue
		}
		env[key] = value
	}
	return env
}

func newTestConfig(t *testing.T, filesEnabled bool) bridge.Config {
	t.Helper()
	root := t.TempDir()
	return bridge.Config{
		Version:   1,
		Root:      root,
		DataDir:   filepath.Join(root, "data"),
		Workspace: filepath.Join(root, "data", "workspace"),
		MCP:       bridge.MCPConfig{Host: "127.0.0.1", Port: 0, Path: "/mcp"},
		Files: bridge.FilesConfig{
			Enabled:           filesEnabled,
			Host:              "127.0.0.1",
			Port:              0,
			Path:              "/files",
			MaxFileBytes:      1 << 20,
			StagingQuotaBytes: 4 << 20,
		},
		Screenshot: bridge.ScreenshotConfig{Backend: "auto"},
	}
}

func newTestSecrets() bridge.Secrets {
	stamp := time.Now().UnixNano()
	return bridge.Secrets{
		MCPToken:   fmt.Sprintf("test-mcp-%d", stamp),
		FilesToken: fmt.Sprintf("test-files-%d", stamp),
	}
}

func newTestBridge(t *testing.T, cfg bridge.Config, secrets bridge.Secrets, env map[string]string, logger bridge.Logger) *bridge.Bridge {
	t.Helper()
	instance := bridge.NewBridge(bridge.Options{
		Config:  cfg,
		Secrets: secrets,
		Env:     env,
		Logger:  logger,
	})
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		_ = instance.Stop(ctx)
	})
	return instance
}

func connectMCP(t *testing.T, instance *bridge.Bridge, token string) *mcp.ClientSession {
	t.Helper()
	url := instance.MCPURL()
	if url == nil {
		t.Fatal("missing MCP URL")
	}
	transport := &mcp.StreamableClientTransport{
		Endpoint:             *url,
		HTTPClient:           &http.Client{Transport: authTransport{token: token}},
		DisableStandaloneSSE: true,
	}
	client := mcp.NewClient(&mcp.Implementation{Name: "bridge-test-client", Version: "0"}, nil)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	t.Cleanup(cancel)
	session, err := client.Connect(ctx, transport, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = session.Close()
	})
	return session
}

func callTool(t *testing.T, session *mcp.ClientSession, name string, args map[string]any) *mcp.CallToolResult {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
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

func callText(t *testing.T, session *mcp.ClientSession, name string, args map[string]any) string {
	t.Helper()
	result := callTool(t, session, name, args)
	if result.IsError {
		t.Fatalf("%s failed: %s", name, toolText(t, result))
	}
	return toolText(t, result)
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

func jsonObject(t *testing.T, text string) map[string]any {
	t.Helper()
	var value map[string]any
	if err := json.Unmarshal([]byte(text), &value); err != nil {
		t.Fatalf("cannot decode JSON %q: %v", text, err)
	}
	return value
}

func jsonString(t *testing.T, object map[string]any, key string) string {
	t.Helper()
	value, ok := object[key].(string)
	if !ok {
		t.Fatalf("field %s is not a string: %#v", key, object[key])
	}
	return value
}

func jsonNumber(t *testing.T, object map[string]any, key string) int {
	t.Helper()
	value, ok := object[key].(float64)
	if !ok {
		t.Fatalf("field %s is not a number: %#v", key, object[key])
	}
	return int(value)
}

func jsonBool(t *testing.T, object map[string]any, key string) bool {
	t.Helper()
	value, ok := object[key].(bool)
	if !ok {
		t.Fatalf("field %s is not a bool: %#v", key, object[key])
	}
	return value
}

func httpRequest(t *testing.T, method, url, token string, body io.Reader, headers map[string]string) (int, http.Header, []byte) {
	t.Helper()
	request, err := http.NewRequest(method, url, body)
	if err != nil {
		t.Fatal(err)
	}
	if token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	for key, value := range headers {
		request.Header.Set(key, value)
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	data, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	return response.StatusCode, response.Header, data
}

func requireBridgeError(t *testing.T, err error, code string) {
	t.Helper()
	var bridgeErr *bridge.BridgeError
	if !errors.As(err, &bridgeErr) {
		t.Fatalf("expected %s, got %v", code, err)
	}
	if bridgeErr.Code != code {
		t.Fatalf("expected %s, got %s (%v)", code, bridgeErr.Code, err)
	}
}

func echoCommand() string {
	if runtime.GOOS == "windows" {
		return "Write-Output bridge-ok"
	}
	return "echo bridge-ok"
}

func longCommand() string {
	if runtime.GOOS == "windows" {
		return "Start-Sleep -Seconds 30"
	}
	return "sleep 30"
}

func interactiveCommand() string {
	if runtime.GOOS == "windows" {
		return `$line = [Console]::In.ReadLine(); Write-Output ("got:" + $line)`
	}
	return `read line; echo "got:$line"`
}

func envPrintCommand() string {
	if runtime.GOOS == "windows" {
		return `Write-Output ("[" + $env:LAPTOP_MCP_TOKEN + "|" + $env:BRIDGE_FILES_TOKEN + "|" + $env:SUPERGATEWAY_API_KEY + "]")`
	}
	return `printf "[%s|%s|%s]\n" "$LAPTOP_MCP_TOKEN" "$BRIDGE_FILES_TOKEN" "$SUPERGATEWAY_API_KEY"`
}

func loadSnapshot(t *testing.T) map[string]map[string]any {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot locate bridge test source")
	}
	path := filepath.Join(filepath.Dir(file), "..", "specs", "bridge-tools-v1.snapshot.json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var raw struct {
		ContractVersion string           `json:"contractVersion"`
		Tools           []map[string]any `json:"tools"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatal(err)
	}
	if raw.ContractVersion != "bridge-v1" {
		t.Fatalf("unexpected contract version %q", raw.ContractVersion)
	}
	tools := make(map[string]map[string]any, len(raw.Tools))
	for _, tool := range raw.Tools {
		name, _ := tool["name"].(string)
		if name == "" {
			t.Fatalf("snapshot tool has no name: %#v", tool)
		}
		tools[name] = tool
	}
	return tools
}

func verifyToolSurface(t *testing.T, session *mcp.ClientSession) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	result, err := session.ListTools(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	actual := make(map[string]map[string]any, len(result.Tools))
	for _, tool := range result.Tools {
		data, err := json.Marshal(tool)
		if err != nil {
			t.Fatal(err)
		}
		var value map[string]any
		if err := json.Unmarshal(data, &value); err != nil {
			t.Fatal(err)
		}
		actual[tool.Name] = value
	}
	snapshot := loadSnapshot(t)
	if len(actual) != len(snapshot) || len(actual) != 15 {
		t.Fatalf("expected 15 tools, got %d: %v", len(actual), toolNames(actual))
	}
	for name, want := range snapshot {
		got, ok := actual[name]
		if !ok {
			t.Fatalf("missing tool %q", name)
		}
		if got["title"] != want["title"] {
			t.Fatalf("%s title mismatch: got %v, want %v", name, got["title"], want["title"])
		}
		gotAnnotations, _ := got["annotations"].(map[string]any)
		wantAnnotations, _ := want["annotations"].(map[string]any)
		for _, key := range []string{"readOnlyHint", "destructiveHint", "openWorldHint"} {
			if gotAnnotations[key] != wantAnnotations[key] {
				t.Fatalf("%s annotation %s mismatch: got %v, want %v", name, key, gotAnnotations[key], wantAnnotations[key])
			}
		}
		if !reflect.DeepEqual(got["inputSchema"], want["inputSchema"]) {
			t.Fatalf("%s inputSchema mismatch:\n got: %#v\nwant: %#v", name, got["inputSchema"], want["inputSchema"])
		}
	}
}

func toolNames(tools map[string]map[string]any) []string {
	names := make([]string, 0, len(tools))
	for name := range tools {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

type systemInfo struct {
	OS struct {
		Platform string `json:"platform"`
		Type     string `json:"type"`
		Release  string `json:"release"`
		Version  string `json:"version"`
	} `json:"os"`
	Hostname string `json:"hostname"`
	Arch     string `json:"arch"`
	Shell    struct {
		Default   string   `json:"default"`
		Available []string `json:"available"`
		PathStyle string   `json:"pathStyle"`
	} `json:"shell"`
	Paths struct {
		DefaultWorkdir string `json:"defaultWorkdir"`
		WorkspaceRoot  string `json:"workspaceRoot"`
		DataDir        string `json:"dataDir"`
		Staging        struct {
			Inbox  string `json:"inbox"`
			Outbox string `json:"outbox"`
		} `json:"staging"`
	} `json:"paths"`
	FileService struct {
		Enabled bool    `json:"enabled"`
		Host    string  `json:"host"`
		Port    int     `json:"port"`
		BaseURL *string `json:"baseUrl"`
	} `json:"fileService"`
	Capabilities struct {
		Screenshot struct {
			Available         bool    `json:"available"`
			ConfiguredBackend string  `json:"configuredBackend"`
			DetectedBackend   *string `json:"detectedBackend"`
			Reason            *string `json:"reason"`
		} `json:"screenshot"`
		Clipboard struct {
			Available bool    `json:"available"`
			Backend   *string `json:"backend"`
			Reason    *string `json:"reason"`
		} `json:"clipboard"`
	} `json:"capabilities"`
}

func getSystemInfo(t *testing.T, session *mcp.ClientSession) systemInfo {
	t.Helper()
	text := callText(t, session, "get_system_info", nil)
	var info systemInfo
	if err := json.Unmarshal([]byte(text), &info); err != nil {
		t.Fatal(err)
	}
	return info
}

func TestBridgeLifecycle(t *testing.T) {
	cfg := newTestConfig(t, true)
	secrets := newTestSecrets()
	env := testEnv()
	env["LAPTOP_MCP_TOKEN"] = "env-mcp-secret"
	env["BRIDGE_FILES_TOKEN"] = "env-files-secret"
	env["SUPERGATEWAY_API_KEY"] = "env-gateway-secret"
	logs := &logCapture{}
	instance := newTestBridge(t, cfg, secrets, env, logs.log)
	if instance.State() != bridge.StateNew {
		t.Fatalf("unexpected state: %s", instance.State())
	}
	if instance.APIVersion() != 1 || instance.ContractVersion() != "bridge-v1" {
		t.Fatalf("unexpected versions: %d %s", instance.APIVersion(), instance.ContractVersion())
	}
	if len(instance.ExpectedTools()) != 15 {
		t.Fatalf("unexpected expected tools: %v", instance.ExpectedTools())
	}
	if instance.Addresses() != nil || instance.MCPURL() != nil || instance.FilesURL() != nil {
		t.Fatal("fresh bridge should have no addresses")
	}
	ready := make(chan *bridge.ReadyEvent, 2)
	stopped := make(chan *bridge.StoppedEvent, 2)
	errorsCh := make(chan *bridge.ErrorEvent, 2)
	instance.On(bridge.EventReady, func(payload any) {
		ready <- payload.(*bridge.ReadyEvent)
	})
	instance.On(bridge.EventStopped, func(payload any) {
		stopped <- payload.(*bridge.StoppedEvent)
	})
	instance.On(bridge.EventError, func(payload any) {
		errorsCh <- payload.(*bridge.ErrorEvent)
	})
	if err := instance.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	if instance.State() != bridge.StateRunning {
		t.Fatalf("unexpected state: %s", instance.State())
	}
	select {
	case event := <-ready:
		if event.Addresses == nil || event.Addresses.Files == nil || event.MCPURL == nil || event.FilesURL == nil {
			t.Fatalf("unexpected ready event: %#v", event)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("ready event not emitted")
	}
	addresses := instance.Addresses()
	if addresses == nil || addresses.Files == nil {
		t.Fatalf("missing addresses: %#v", addresses)
	}
	if addresses.MCP.Port == 0 || addresses.Files.Port == 0 {
		t.Fatalf("expected ephemeral ports: %#v", addresses)
	}
	if instance.MCPURL() == nil || instance.FilesURL() == nil {
		t.Fatal("missing URLs")
	}
	status, _, _ := httpRequest(t, http.MethodGet, *instance.MCPURL(), "", nil, nil)
	if status != http.StatusUnauthorized {
		t.Fatalf("expected unauthenticated MCP request to fail, got %d", status)
	}
	status, _, _ = httpRequest(t, http.MethodGet, *instance.MCPURL(), secrets.FilesToken, nil, nil)
	if status != http.StatusUnauthorized {
		t.Fatalf("expected file token to be rejected by MCP, got %d", status)
	}
	if err := instance.WaitForReady(context.Background()); err != nil {
		t.Fatalf("wait ready failed: %v", err)
	}
	if err := instance.Start(context.Background()); err != nil {
		t.Fatalf("idempotent start failed: %v", err)
	}
	select {
	case event := <-ready:
		t.Fatalf("unexpected second ready event: %#v", event)
	default:
	}
	health := instance.Health(context.Background())
	if health.State != bridge.StateRunning || !health.MCP.OK || len(health.MCP.Tools) != 15 {
		t.Fatalf("unexpected health: %#v", health)
	}
	if !health.Files.Enabled || health.Files.OK == nil || !*health.Files.OK || health.Files.URL == nil {
		t.Fatalf("unexpected files health: %#v", health.Files)
	}
	diagnostics := instance.Diagnostics()
	if diagnostics.State != bridge.StateRunning || diagnostics.PID == nil || diagnostics.StartedAt == nil {
		t.Fatalf("unexpected diagnostics: %#v", diagnostics)
	}
	if len(diagnostics.ExpectedTools) != 15 || diagnostics.Addresses == nil {
		t.Fatalf("unexpected diagnostics: %#v", diagnostics)
	}
	configJSON, err := json.Marshal(diagnostics.Config)
	if err != nil {
		t.Fatal(err)
	}
	diagnosticsJSON, err := json.Marshal(diagnostics)
	if err != nil {
		t.Fatal(err)
	}
	healthJSON, err := json.Marshal(health)
	if err != nil {
		t.Fatal(err)
	}
	for _, token := range []string{secrets.MCPToken, secrets.FilesToken} {
		if strings.Contains(string(configJSON), token) || strings.Contains(string(diagnosticsJSON), token) || strings.Contains(string(healthJSON), token) {
			t.Fatalf("token leaked into bridge state")
		}
		if strings.Contains(logs.text(), token) {
			t.Fatalf("token leaked into logs")
		}
	}

	session := connectMCP(t, instance, secrets.MCPToken)
	verifyToolSurface(t, session)
	info := getSystemInfo(t, session)
	if info.FileService.Enabled != true || info.FileService.Port != addresses.Files.Port {
		t.Fatalf("unexpected system info file service: %#v", info.FileService)
	}
	if info.FileService.BaseURL == nil || *info.FileService.BaseURL != *instance.FilesURL() {
		t.Fatalf("unexpected file service URL: %#v", info.FileService.BaseURL)
	}
	if info.Paths.WorkspaceRoot != cfg.Workspace || info.Paths.DataDir != cfg.DataDir {
		t.Fatalf("unexpected paths: %#v", info.Paths)
	}
	if info.Shell.Default == "" || info.Shell.PathStyle == "" {
		t.Fatalf("unexpected shell: %#v", info.Shell)
	}

	t.Run("files", func(t *testing.T) {
		write := jsonObject(t, callText(t, session, "write_file", map[string]any{
			"path":        "notes/test.txt",
			"content":     "第一行\nsecond\n",
			"create_dirs": true,
		}))
		if jsonNumber(t, write, "bytes") != len("第一行\nsecond\n") || !jsonBool(t, write, "created") {
			t.Fatalf("unexpected write: %#v", write)
		}
		read := jsonObject(t, callText(t, session, "read_file", map[string]any{"path": "notes/test.txt"}))
		if jsonString(t, read, "content") != "第一行\nsecond" || jsonNumber(t, read, "totalLines") != 2 {
			t.Fatalf("unexpected read: %#v", read)
		}
		list := jsonObject(t, callText(t, session, "list_directory", map[string]any{"path": "notes"}))
		entries, _ := list["entries"].([]any)
		if len(entries) != 1 {
			t.Fatalf("unexpected list: %#v", list)
		}
		stat := jsonObject(t, callText(t, session, "stat_file", map[string]any{"path": "notes/test.txt"}))
		if jsonString(t, stat, "type") != "file" {
			t.Fatalf("unexpected stat: %#v", stat)
		}
		requireToolError(t, session, "read_file", map[string]any{"path": "../escape.txt"}, "path_escape")
	})

	t.Run("process", func(t *testing.T) {
		startText := callText(t, session, "start_process", map[string]any{"command": echoCommand(), "timeout": 5000})
		if !strings.Contains(startText, "bridge-ok") {
			t.Fatalf("missing output: %s", startText)
		}
		quick := callTool(t, session, "list_sessions", nil)
		if quick.IsError {
			t.Fatalf("list_sessions failed: %s", toolText(t, quick))
		}
		longText := callText(t, session, "start_process", map[string]any{"command": longCommand(), "timeout": 200})
		pid := parsePID(t, longText)
		sessionsText := callText(t, session, "list_sessions", nil)
		if !strings.Contains(sessionsText, strconv.Itoa(pid)) {
			t.Fatalf("session %d missing from %s", pid, sessionsText)
		}
		callText(t, session, "read_process_output", map[string]any{"pid": pid, "timeout_ms": 100})
		callText(t, session, "force_terminate", map[string]any{"pid": pid})
		if text := callText(t, session, "list_sessions", nil); text != "No active sessions" {
			t.Fatalf("session still listed: %s", text)
		}
		interactiveText := callText(t, session, "start_process", map[string]any{"command": interactiveCommand(), "timeout": 200})
		interactivePID := parsePID(t, interactiveText)
		interactText := callText(t, session, "interact_with_process", map[string]any{
			"pid":             interactivePID,
			"input":           "hello\n",
			"timeout_ms":      5000,
			"wait_for_prompt": false,
		})
		if !strings.Contains(interactText, "got:hello") {
			t.Fatalf("interactive output missing: %s", interactText)
		}
		callText(t, session, "force_terminate", map[string]any{"pid": interactivePID})
		envText := callText(t, session, "start_process", map[string]any{"command": envPrintCommand(), "timeout": 5000})
		if !strings.Contains(envText, "[||]") {
			t.Fatalf("token env leaked: %s", envText)
		}
		if strings.Contains(envText, "env-mcp-secret") || strings.Contains(envText, "env-files-secret") || strings.Contains(envText, "env-gateway-secret") {
			t.Fatalf("token value leaked: %s", envText)
		}
	})

	t.Run("staging", func(t *testing.T) {
		list := jsonObject(t, callText(t, session, "list_staged_files", map[string]any{"direction": "inbox"}))
		if files, _ := list["files"].([]any); len(files) != 0 {
			t.Fatalf("unexpected staged files: %#v", list)
		}
		requireToolError(t, session, "delete_staged_file", map[string]any{"direction": "inbox", "name": "missing.bin"}, "not_found")
	})

	t.Run("file service", func(t *testing.T) {
		status, _, _ := httpRequest(t, http.MethodGet, *instance.FilesURL()+"/outbox", "", nil, nil)
		if status != http.StatusUnauthorized {
			t.Fatalf("expected 401, got %d", status)
		}
		status, _, _ = httpRequest(t, http.MethodGet, *instance.FilesURL()+"/outbox", secrets.MCPToken, nil, nil)
		if status != http.StatusUnauthorized {
			t.Fatalf("expected MCP token to be rejected, got %d", status)
		}
		status, _, body := httpRequest(t, http.MethodPut, *instance.FilesURL()+"/inbox/blackbox.txt", secrets.FilesToken, strings.NewReader("payload"), nil)
		if status != http.StatusCreated {
			t.Fatalf("expected 201, got %d: %s", status, body)
		}
		list := jsonObject(t, callText(t, session, "list_staged_files", map[string]any{"direction": "inbox"}))
		files, _ := list["files"].([]any)
		if len(files) != 1 {
			t.Fatalf("unexpected inbox list: %#v", list)
		}
		if err := os.Rename(
			filepath.Join(cfg.DataDir, "staging", "inbox", "blackbox.txt"),
			filepath.Join(cfg.DataDir, "staging", "outbox", "blackbox.txt"),
		); err != nil {
			t.Fatal(err)
		}
		status, headers, body := httpRequest(t, http.MethodGet, *instance.FilesURL()+"/outbox/blackbox.txt", secrets.FilesToken, nil, nil)
		if status != http.StatusOK || string(body) != "payload" {
			t.Fatalf("unexpected download: %d %q", status, body)
		}
		if !strings.Contains(headers.Get("Content-Disposition"), "attachment;") {
			t.Fatalf("missing disposition: %q", headers.Get("Content-Disposition"))
		}
		status, _, _ = httpRequest(t, http.MethodDelete, *instance.FilesURL()+"/outbox/blackbox.txt", secrets.FilesToken, nil, nil)
		if status != http.StatusNoContent {
			t.Fatalf("expected 204, got %d", status)
		}
	})

	t.Run("clipboard", func(t *testing.T) {
		if !info.Capabilities.Clipboard.Available {
			requireToolError(t, session, "get_clipboard", nil, "clipboard_unavailable")
			t.Skip("clipboard backend is not available")
		}
		initial := callTool(t, session, "get_clipboard", nil)
		if initial.IsError {
			t.Skipf("clipboard backend is not usable: %s", toolText(t, initial))
		}
		var original struct {
			Text string `json:"text"`
		}
		if err := json.Unmarshal([]byte(toolText(t, initial)), &original); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			_ = callTool(t, session, "set_clipboard", map[string]any{"text": original.Text})
		})
		text := "bridge 黑盒测试 " + time.Now().Format(time.RFC3339Nano) + "\n第二行"
		setResult := callTool(t, session, "set_clipboard", map[string]any{"text": text})
		if setResult.IsError {
			t.Skipf("clipboard write failed: %s", toolText(t, setResult))
		}
		var read struct {
			Text  string `json:"text"`
			Bytes int    `json:"bytes"`
		}
		if err := json.Unmarshal([]byte(callText(t, session, "get_clipboard", nil)), &read); err != nil {
			t.Fatal(err)
		}
		if read.Text != text || read.Bytes != len(text) {
			t.Fatalf("unexpected clipboard round trip: %#v", read)
		}
		emptySet := callTool(t, session, "set_clipboard", map[string]any{"text": ""})
		if emptySet.IsError {
			t.Fatalf("empty clipboard write failed: %s", toolText(t, emptySet))
		}
		var emptyRead struct {
			Text  string `json:"text"`
			Bytes int    `json:"bytes"`
		}
		if err := json.Unmarshal([]byte(callText(t, session, "get_clipboard", nil)), &emptyRead); err != nil {
			t.Fatal(err)
		}
		if emptyRead.Text != "" || emptyRead.Bytes != 0 {
			t.Fatalf("unexpected empty clipboard: %#v", emptyRead)
		}
	})

	t.Run("screenshot", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		result, callErr := session.CallTool(ctx, &mcp.CallToolParams{Name: "capture_screenshot"})
		cancel()
		if callErr != nil {
			t.Skipf("screenshot call failed: %v", callErr)
		}
		if !info.Capabilities.Screenshot.Available {
			if !result.IsError || !strings.Contains(toolText(t, result), "screenshot_unavailable") {
				t.Fatalf("expected screenshot_unavailable, got %s", toolText(t, result))
			}
			t.Skip("screenshot backend is not available")
		}
		if result.IsError {
			message := toolText(t, result)
			if strings.Contains(message, "screenshot_error") {
				t.Skipf("screenshot unavailable in this environment: %s", message)
			}
			t.Fatalf("screenshot failed: %s", message)
		}
		var metadata struct {
			Path    string `json:"path"`
			Width   int    `json:"width"`
			Height  int    `json:"height"`
			Bytes   int64  `json:"bytes"`
			Inlined bool   `json:"inlined"`
		}
		if err := json.Unmarshal([]byte(toolText(t, result)), &metadata); err != nil {
			t.Fatal(err)
		}
		if metadata.Width <= 0 || metadata.Height <= 0 || metadata.Bytes <= 0 {
			t.Fatalf("unexpected metadata: %#v", metadata)
		}
		infoOnDisk, err := os.Stat(metadata.Path)
		if err != nil {
			t.Fatal(err)
		}
		if infoOnDisk.Size() != metadata.Bytes {
			t.Fatalf("size mismatch: %d vs %d", infoOnDisk.Size(), metadata.Bytes)
		}
		if metadata.Inlined {
			if len(result.Content) != 2 {
				t.Fatalf("expected image content, got %d", len(result.Content))
			}
			image, ok := result.Content[1].(*mcp.ImageContent)
			if !ok {
				t.Fatalf("unexpected image type: %T", result.Content[1])
			}
			if image.MIMEType != "image/png" {
				t.Fatalf("unexpected mime type: %s", image.MIMEType)
			}
			decoded, err := base64.StdEncoding.DecodeString(string(image.Data))
			if err != nil {
				t.Fatal(err)
			}
			expected, err := os.ReadFile(metadata.Path)
			if err != nil {
				t.Fatal(err)
			}
			if len(decoded) != len(expected) {
				t.Fatalf("inline image size mismatch: %d vs %d", len(decoded), len(expected))
			}
		}
	})

	if err := instance.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
	if instance.State() != bridge.StateStopped {
		t.Fatalf("unexpected state: %s", instance.State())
	}
	if instance.Addresses() != nil || instance.MCPURL() != nil || instance.FilesURL() != nil {
		t.Fatal("addresses should be cleared after stop")
	}
	select {
	case <-stopped:
	case <-time.After(2 * time.Second):
		t.Fatal("stopped event not emitted")
	}
	requireBridgeError(t, instance.WaitForReady(context.Background()), bridge.CodeNotStarted)
	stoppedDiagnostics := instance.Diagnostics()
	if stoppedDiagnostics.State != bridge.StateStopped || stoppedDiagnostics.PID != nil || stoppedDiagnostics.StartedAt != nil || stoppedDiagnostics.Addresses != nil {
		t.Fatalf("unexpected stopped diagnostics: %#v", stoppedDiagnostics)
	}
	stoppedHealth := instance.Health(context.Background())
	if stoppedHealth.State != bridge.StateStopped || stoppedHealth.MCP.OK || stoppedHealth.Files.OK == nil || *stoppedHealth.Files.OK || stoppedHealth.Files.URL != nil {
		t.Fatalf("unexpected stopped health: %#v", stoppedHealth)
	}
	if err := instance.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
	select {
	case event := <-stopped:
		t.Fatalf("unexpected second stopped event: %#v", event)
	default:
	}
	select {
	case event := <-errorsCh:
		t.Fatalf("unexpected error event: %#v", event)
	default:
	}

	if err := instance.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	second := instance.Addresses()
	if second == nil || second.Files == nil {
		t.Fatalf("missing second addresses: %#v", second)
	}
	if second.MCP.Port == addresses.MCP.Port || second.Files.Port == addresses.Files.Port {
		t.Fatalf("ephemeral ports were reused: first=%#v second=%#v", addresses, second)
	}
	if err := instance.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func parsePID(t *testing.T, text string) int {
	t.Helper()
	match := regexp.MustCompile(`PID (\d+)`).FindStringSubmatch(text)
	if len(match) != 2 {
		t.Fatalf("cannot find PID in %q", text)
	}
	pid, err := strconv.Atoi(match[1])
	if err != nil {
		t.Fatal(err)
	}
	return pid
}

func TestBridgeFilesDisabled(t *testing.T) {
	cfg := newTestConfig(t, false)
	secrets := newTestSecrets()
	secrets.FilesToken = ""
	instance := newTestBridge(t, cfg, secrets, testEnv(), nil)
	if err := instance.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	if instance.FilesURL() != nil {
		t.Fatal("files URL should be nil")
	}
	addresses := instance.Addresses()
	if addresses == nil || addresses.Files != nil {
		t.Fatalf("unexpected addresses: %#v", addresses)
	}
	health := instance.Health(context.Background())
	if health.Files.Enabled || health.Files.OK != nil || health.Files.URL != nil {
		t.Fatalf("unexpected files health: %#v", health.Files)
	}
	session := connectMCP(t, instance, secrets.MCPToken)
	info := getSystemInfo(t, session)
	if info.FileService.Enabled || info.FileService.BaseURL != nil {
		t.Fatalf("unexpected system info: %#v", info.FileService)
	}
	if err := os.WriteFile(filepath.Join(cfg.DataDir, "staging", "inbox", "offline.bin"), []byte("data"), 0o644); err != nil {
		t.Fatal(err)
	}
	list := jsonObject(t, callText(t, session, "list_staged_files", map[string]any{"direction": "inbox"}))
	files, _ := list["files"].([]any)
	if len(files) != 1 {
		t.Fatalf("unexpected staging list: %#v", list)
	}
	callText(t, session, "delete_staged_file", map[string]any{"direction": "inbox", "name": "offline.bin"})
	if err := instance.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestBridgeValidation(t *testing.T) {
	baseSecrets := newTestSecrets()
	cases := []struct {
		name   string
		code   string
		mutate func(*bridge.Config, *bridge.Secrets)
	}{
		{"version", bridge.CodeInvalidConfig, func(cfg *bridge.Config, _ *bridge.Secrets) { cfg.Version = 0 }},
		{"root", bridge.CodeInvalidConfig, func(cfg *bridge.Config, _ *bridge.Secrets) { cfg.Root = "relative" }},
		{"data dir", bridge.CodeInvalidConfig, func(cfg *bridge.Config, _ *bridge.Secrets) { cfg.DataDir = "data" }},
		{"workspace", bridge.CodeInvalidConfig, func(cfg *bridge.Config, _ *bridge.Secrets) { cfg.Workspace = "workspace" }},
		{"mcp host", bridge.CodeInvalidConfig, func(cfg *bridge.Config, _ *bridge.Secrets) { cfg.MCP.Host = "0.0.0.0" }},
		{"mcp path", bridge.CodeInvalidConfig, func(cfg *bridge.Config, _ *bridge.Secrets) { cfg.MCP.Path = "/other" }},
		{"mcp port", bridge.CodeInvalidConfig, func(cfg *bridge.Config, _ *bridge.Secrets) { cfg.MCP.Port = 70000 }},
		{"files host", bridge.CodeInvalidConfig, func(cfg *bridge.Config, _ *bridge.Secrets) { cfg.Files.Host = "0.0.0.0" }},
		{"files path", bridge.CodeInvalidConfig, func(cfg *bridge.Config, _ *bridge.Secrets) { cfg.Files.Path = "/other" }},
		{"files port", bridge.CodeInvalidConfig, func(cfg *bridge.Config, _ *bridge.Secrets) { cfg.Files.Port = 70000 }},
		{"same ports", bridge.CodeInvalidConfig, func(cfg *bridge.Config, _ *bridge.Secrets) { cfg.MCP.Port = 12345; cfg.Files.Port = 12345 }},
		{"max bytes", bridge.CodeInvalidConfig, func(cfg *bridge.Config, _ *bridge.Secrets) { cfg.Files.MaxFileBytes = 0 }},
		{"quota", bridge.CodeInvalidConfig, func(cfg *bridge.Config, _ *bridge.Secrets) { cfg.Files.StagingQuotaBytes = 1 }},
		{"screenshot", bridge.CodeInvalidConfig, func(cfg *bridge.Config, _ *bridge.Secrets) { cfg.Screenshot.Backend = "magic" }},
		{"shell", bridge.CodeInvalidConfig, func(cfg *bridge.Config, _ *bridge.Secrets) { cfg.Shell.Default = "definitely-not-a-shell" }},
		{"mcp token", bridge.CodeInvalidSecret, func(_ *bridge.Config, secrets *bridge.Secrets) { secrets.MCPToken = "" }},
		{"files token", bridge.CodeInvalidSecret, func(_ *bridge.Config, secrets *bridge.Secrets) { secrets.FilesToken = "" }},
		{"equal tokens", bridge.CodeInvalidSecret, func(_ *bridge.Config, secrets *bridge.Secrets) {
			secrets.MCPToken = "same"
			secrets.FilesToken = "same"
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := newTestConfig(t, true)
			secrets := baseSecrets
			tc.mutate(&cfg, &secrets)
			instance := newTestBridge(t, cfg, secrets, testEnv(), nil)
			errorEvents := make(chan *bridge.ErrorEvent, 1)
			instance.On(bridge.EventError, func(payload any) {
				errorEvents <- payload.(*bridge.ErrorEvent)
			})
			err := instance.Start(context.Background())
			requireBridgeError(t, err, tc.code)
			select {
			case event := <-errorEvents:
				if event.Code != tc.code {
					t.Fatalf("unexpected error event: %#v", event)
				}
			default:
				t.Fatal("error event not emitted")
			}
			if instance.State() != bridge.StateFailed {
				t.Fatalf("unexpected state: %s", instance.State())
			}
			if instance.Addresses() != nil {
				t.Fatal("addresses should be nil after failed start")
			}
			if stopErr := instance.Stop(context.Background()); stopErr != nil {
				t.Fatal(stopErr)
			}
		})
	}
}

func TestBridgePortInUse(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	cfg := newTestConfig(t, true)
	cfg.MCP.Port = port
	instance := newTestBridge(t, cfg, newTestSecrets(), testEnv(), nil)
	err = instance.Start(context.Background())
	requireBridgeError(t, err, bridge.CodePortInUse)
	if instance.State() != bridge.StateFailed || instance.Addresses() != nil {
		t.Fatalf("unexpected failed state: %s %#v", instance.State(), instance.Addresses())
	}
	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}
	if err := instance.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	addresses := instance.Addresses()
	if addresses == nil || addresses.MCP.Port != port {
		t.Fatalf("unexpected retry addresses: %#v", addresses)
	}
	if err := instance.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}

	filesListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	filesPort := filesListener.Addr().(*net.TCPAddr).Port
	cfg = newTestConfig(t, true)
	cfg.Files.Port = filesPort
	filesBridge := newTestBridge(t, cfg, newTestSecrets(), testEnv(), nil)
	err = filesBridge.Start(context.Background())
	requireBridgeError(t, err, bridge.CodePortInUse)
	_ = filesBridge.Stop(context.Background())
	_ = filesListener.Close()
}

func TestBridgeStopFromNew(t *testing.T) {
	instance := newTestBridge(t, newTestConfig(t, true), newTestSecrets(), testEnv(), nil)
	if err := instance.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
	if instance.State() != bridge.StateStopped || instance.Addresses() != nil {
		t.Fatalf("unexpected stopped state: %s %#v", instance.State(), instance.Addresses())
	}
	if err := instance.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
}
