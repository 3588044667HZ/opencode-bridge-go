package process

import (
	"os"
	"runtime"
	"testing"
)

func TestDetectAndResolveShell(t *testing.T) {
	set, err := DetectShells()
	if err != nil {
		t.Fatal(err)
	}
	if set.Default == "" {
		t.Fatal("default shell is empty")
	}
	shell, err := ResolveShell("", set)
	if err != nil {
		t.Fatal(err)
	}
	if shell.Alias != set.Default {
		t.Fatalf("expected %q, got %q", set.Default, shell.Alias)
	}
	if runtime.GOOS == "windows" {
		if _, err := ResolveShell("bash", set); err == nil {
			t.Fatal("bare bash must be rejected on Windows")
		}
	}
}

func TestShellArgs(t *testing.T) {
	cases := map[string][]string{
		"powershell": {"-NoLogo", "-NoProfile", "-Command", "echo x"},
		"pwsh":       {"-NoLogo", "-NoProfile", "-Command", "echo x"},
		"cmd":        {"/c", "echo x"},
		"bash":       {"-lc", "echo x"},
		"sh":         {"-c", "echo x"},
	}
	for alias, expected := range cases {
		actual := ShellArgs(Shell{Alias: alias}, "echo x")
		if len(actual) != len(expected) {
			t.Fatalf("%s: unexpected args %#v", alias, actual)
		}
		for i := range expected {
			if actual[i] != expected[i] {
				t.Fatalf("%s: unexpected args %#v", alias, actual)
			}
		}
	}
}

func TestDetectShellsWithEnv(t *testing.T) {
	fromEnv, err := DetectShellsWithEnv(os.Environ())
	if err != nil {
		t.Fatal(err)
	}
	direct, err := DetectShells()
	if err != nil {
		t.Fatal(err)
	}
	if fromEnv.Default != direct.Default || fromEnv.PathStyle != direct.PathStyle {
		t.Fatalf("explicit env detection mismatch: %#v vs %#v", fromEnv, direct)
	}
	if runtime.GOOS == "windows" {
		if _, err := DetectShellsWithEnv(nil); err == nil {
			t.Fatal("expected empty environment to fail shell detection on Windows")
		}
	}
}

func TestShellSummaryOrder(t *testing.T) {
	set, err := DetectShells()
	if err != nil {
		t.Fatal(err)
	}
	for _, alias := range set.Order {
		if _, ok := set.Available[alias]; !ok {
			t.Fatalf("order contains unknown alias %q", alias)
		}
	}
	cfg := DefaultConfig()
	cfg.Workspace = t.TempDir()
	manager, err := NewManager(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defaultAlias, available, pathStyle := manager.ShellSummary()
	found := false
	for _, alias := range available {
		if alias == defaultAlias {
			found = true
		}
	}
	if !found {
		t.Fatalf("default shell %q is not in available list %v", defaultAlias, available)
	}
	if pathStyle != set.PathStyle {
		t.Fatalf("unexpected path style: %s", pathStyle)
	}
}

func TestCommandWithWorkdir(t *testing.T) {
	cases := []struct {
		name    string
		alias   string
		workdir string
		command string
		want    string
	}{
		{"powershell", "powershell", `C:\a b`, "Get-Location", `Set-Location -LiteralPath 'C:\a b' -ErrorAction Stop; Get-Location`},
		{"powershell quote", "pwsh", `C:\it's`, "Get-Location", `Set-Location -LiteralPath 'C:\it''s' -ErrorAction Stop; Get-Location`},
		{"cmd", "cmd", `C:\a b`, "dir", `cd /d "C:\a b" && dir`},
		{"bash", "bash", `/tmp/a b`, "pwd", `cd -- '/tmp/a b' && pwd`},
		{"bash quote", "zsh", `/tmp/a'b`, "pwd", `cd -- '/tmp/a'\''b' && pwd`},
		{"sh", "sh", `/tmp/a b`, "pwd", `cd '/tmp/a b' && pwd`},
		{"fish", "fish", `/tmp/a b`, "pwd", `cd -- '/tmp/a b'; and pwd`},
		{"git-bash", "git-bash", `C:\a b`, "pwd", `cd -- 'C:/a b' && pwd`},
		{"git-bash UNC", "git-bash", `\\server\share`, "pwd", `cd -- '//server/share' && pwd`},
		{"empty workdir", "bash", "", "pwd", "pwd"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			actual, err := commandWithWorkdir(Shell{Alias: tc.alias}, tc.workdir, tc.command)
			if err != nil {
				t.Fatal(err)
			}
			if actual != tc.want {
				t.Fatalf("expected %q, got %q", tc.want, actual)
			}
		})
	}
}

func TestCommandWithWorkdirRejectsCmdPercent(t *testing.T) {
	_, err := commandWithWorkdir(Shell{Alias: "cmd"}, `C:\bad%path`, "dir")
	processErr, ok := err.(*Error)
	if !ok || processErr.Code != "workdir_unsupported_character" {
		t.Fatalf("expected workdir_unsupported_character, got %v", err)
	}
}
