package bridgecfg

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadDotEnv(t *testing.T) {
	root := t.TempDir()
	content := "# comment\n" +
		"A=1\n" +
		"export B=two\n" +
		"C=\"three\"\n" +
		"D='four'\n" +
		"E=value # comment\n" +
		"F=\n" +
		"G=a=b\n" +
		"INVALID\n"
	if err := os.WriteFile(filepath.Join(root, ".env"), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	values, err := LoadDotEnv(root)
	if err != nil {
		t.Fatal(err)
	}
	expected := map[string]string{
		"A": "1",
		"B": "two",
		"C": "three",
		"D": "four",
		"E": "value",
		"F": "",
		"G": "a=b",
	}
	for key, value := range expected {
		if values[key] != value {
			t.Fatalf("%s: got %q, want %q", key, values[key], value)
		}
	}
	if _, ok := values["INVALID"]; ok {
		t.Fatal("invalid line was accepted")
	}
}

func TestApplyDotEnvPrecedence(t *testing.T) {
	root := t.TempDir()
	content := "GO_BRIDGE_TEST_DOTENV_A=from-file\nGO_BRIDGE_TEST_DOTENV_B=from-file\n"
	if err := os.WriteFile(filepath.Join(root, ".env"), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	_ = os.Unsetenv("GO_BRIDGE_TEST_DOTENV_A")
	t.Setenv("GO_BRIDGE_TEST_DOTENV_B", "from-env")
	t.Cleanup(func() {
		_ = os.Unsetenv("GO_BRIDGE_TEST_DOTENV_A")
	})
	if err := ApplyDotEnv(root); err != nil {
		t.Fatal(err)
	}
	if value := os.Getenv("GO_BRIDGE_TEST_DOTENV_A"); value != "from-file" {
		t.Fatalf("unexpected A: %q", value)
	}
	if value := os.Getenv("GO_BRIDGE_TEST_DOTENV_B"); value != "from-env" {
		t.Fatalf("unexpected B: %q", value)
	}
}
