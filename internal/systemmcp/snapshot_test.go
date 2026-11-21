package systemmcp

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"testing"
	"time"
)

var implementedSystemTools = []string{
	"get_system_info",
	"get_clipboard",
	"set_clipboard",
	"capture_screenshot",
}

func TestSystemToolSurfaceMatchesSnapshot(t *testing.T) {
	snapshot := loadToolSnapshot(t)
	actual := listRegisteredTools(t)
	if len(actual) != len(implementedSystemTools) {
		t.Fatalf("expected %d implemented tools, got %d: %v", len(implementedSystemTools), len(actual), toolNames(actual))
	}
	for _, name := range implementedSystemTools {
		got, ok := actual[name]
		if !ok {
			t.Fatalf("missing implemented tool %q", name)
		}
		want, ok := snapshot[name]
		if !ok {
			t.Fatalf("snapshot has no tool %q", name)
		}
		compareTool(t, name, got, want)
	}
}

func listRegisteredTools(t *testing.T) map[string]map[string]any {
	t.Helper()
	system, clip, shot := newTestServices(t)
	session := newTestSession(t, system, clip, shot)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	result, err := session.ListTools(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	tools := make(map[string]map[string]any, len(result.Tools))
	for _, tool := range result.Tools {
		data, err := json.Marshal(tool)
		if err != nil {
			t.Fatal(err)
		}
		var value map[string]any
		if err := json.Unmarshal(data, &value); err != nil {
			t.Fatal(err)
		}
		tools[tool.Name] = value
	}
	return tools
}

func loadToolSnapshot(t *testing.T) map[string]map[string]any {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot locate snapshot test source")
	}
	path := filepath.Join(filepath.Dir(file), "..", "..", "specs", "bridge-tools-v1.snapshot.json")
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

func compareTool(t *testing.T, name string, got, want map[string]any) {
	t.Helper()
	if got["name"] != want["name"] {
		t.Fatalf("%s name mismatch: got %v, want %v", name, got["name"], want["name"])
	}
	if got["title"] != want["title"] {
		t.Fatalf("%s title mismatch: got %v, want %v", name, got["title"], want["title"])
	}
	gotAnnotations, ok := got["annotations"].(map[string]any)
	if !ok {
		t.Fatalf("%s has no annotations", name)
	}
	wantAnnotations, ok := want["annotations"].(map[string]any)
	if !ok {
		t.Fatalf("%s snapshot has no annotations", name)
	}
	for _, key := range []string{"readOnlyHint", "destructiveHint", "openWorldHint"} {
		if gotAnnotations[key] != wantAnnotations[key] {
			t.Fatalf("%s annotation %s mismatch: got %v, want %v", name, key, gotAnnotations[key], wantAnnotations[key])
		}
	}
	if !reflect.DeepEqual(got["inputSchema"], want["inputSchema"]) {
		t.Fatalf("%s inputSchema mismatch:\n got: %#v\nwant: %#v", name, got["inputSchema"], want["inputSchema"])
	}
}

func toolNames(tools map[string]map[string]any) []string {
	names := make([]string, 0, len(tools))
	for name := range tools {
		names = append(names, name)
	}
	return names
}
