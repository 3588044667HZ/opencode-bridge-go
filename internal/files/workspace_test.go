package files

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func newTestService(t *testing.T) (*Service, string) {
	t.Helper()
	root := t.TempDir()
	service, err := New(Config{
		WorkspaceRoot:     filepath.Join(root, "workspace"),
		StagingInbox:      filepath.Join(root, "staging", "inbox"),
		StagingOutbox:     filepath.Join(root, "staging", "outbox"),
		MaxFileBytes:      1 << 20,
		StagingQuotaBytes: 4 << 20,
	})
	if err != nil {
		t.Fatal(err)
	}
	return service, root
}

func requireCode(t *testing.T, err error, code string) {
	t.Helper()
	var fileErr *Error
	if !errors.As(err, &fileErr) {
		t.Fatalf("expected %s, got %v", code, err)
	}
	if fileErr.Code != code {
		t.Fatalf("expected %s, got %s (%v)", code, fileErr.Code, err)
	}
}

func TestWriteReadFile(t *testing.T) {
	service, _ := newTestService(t)
	content := "第一行\nsecond\nthird\n"
	write, err := service.WriteFile(WriteFileOptions{
		Path:       "notes/today.txt",
		Content:    content,
		CreateDirs: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if write.Bytes != len(content) || !write.Created || write.Replaced {
		t.Fatalf("unexpected write result: %#v", write)
	}
	read, err := service.ReadFile(ReadFileOptions{Path: "notes/today.txt"})
	if err != nil {
		t.Fatal(err)
	}
	if read.TotalLines != 3 || read.ReturnedLines != 3 || read.Truncated {
		t.Fatalf("unexpected read result: %#v", read)
	}
	if read.Content != "第一行\nsecond\nthird" {
		t.Fatalf("unexpected content: %q", read.Content)
	}
	if read.Offset != 1 || read.Limit != DefaultReadLines {
		t.Fatalf("unexpected defaults: %#v", read)
	}
	page, err := service.ReadFile(ReadFileOptions{Path: "notes/today.txt", Offset: 2, Limit: 1})
	if err != nil {
		t.Fatal(err)
	}
	if page.Content != "second" || page.ReturnedLines != 1 || !page.Truncated {
		t.Fatalf("unexpected page: %#v", page)
	}
	empty, err := service.ReadFile(ReadFileOptions{Path: "notes/today.txt", Offset: 10, Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if empty.Content != "" || empty.ReturnedLines != 0 || empty.Truncated {
		t.Fatalf("unexpected empty page: %#v", empty)
	}
	write, err = service.WriteFile(WriteFileOptions{Path: "notes/today.txt", Content: "x"})
	if err != nil {
		t.Fatal(err)
	}
	if write.Created || !write.Replaced {
		t.Fatalf("unexpected replace result: %#v", write)
	}
}

func TestReadFileLineSemantics(t *testing.T) {
	service, _ := newTestService(t)
	cases := []struct {
		name    string
		content string
		lines   int
		want    string
	}{
		{"empty", "", 0, ""},
		{"one", "a\n", 1, "a"},
		{"blank tail", "a\n\n", 2, "a\n"},
		{"no trailing", "a\nb", 2, "a\nb"},
		{"crlf", "a\r\nb\rc\n", 3, "a\nb\nc"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(service.WorkspaceRoot(), tc.name+".txt")
			if err := os.WriteFile(path, []byte(tc.content), 0o644); err != nil {
				t.Fatal(err)
			}
			result, err := service.ReadFile(ReadFileOptions{Path: tc.name + ".txt"})
			if err != nil {
				t.Fatal(err)
			}
			if result.TotalLines != tc.lines || result.Content != tc.want {
				t.Fatalf("total=%d content=%q, want total=%d content=%q", result.TotalLines, result.Content, tc.lines, tc.want)
			}
		})
	}
}

func TestReadFileBOMAndBinary(t *testing.T) {
	service, _ := newTestService(t)
	bomPath := filepath.Join(service.WorkspaceRoot(), "bom.txt")
	if err := os.WriteFile(bomPath, append([]byte{0xEF, 0xBB, 0xBF}, []byte("a\nb")...), 0o644); err != nil {
		t.Fatal(err)
	}
	result, err := service.ReadFile(ReadFileOptions{Path: "bom.txt"})
	if err != nil {
		t.Fatal(err)
	}
	if result.Content != "a\nb" {
		t.Fatalf("BOM not stripped: %q", result.Content)
	}
	invalid := filepath.Join(service.WorkspaceRoot(), "invalid.txt")
	if err := os.WriteFile(invalid, []byte{0xff, 0xfe}, 0o644); err != nil {
		t.Fatal(err)
	}
	_, err = service.ReadFile(ReadFileOptions{Path: "invalid.txt"})
	requireCode(t, err, "binary_file")
	nul := filepath.Join(service.WorkspaceRoot(), "nul-bytes.txt")
	if err := os.WriteFile(nul, []byte("a\x00b"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err = service.ReadFile(ReadFileOptions{Path: "nul-bytes.txt"})
	requireCode(t, err, "binary_file")
}

func TestReadFileLimits(t *testing.T) {
	service, _ := newTestService(t)
	largePath := filepath.Join(service.WorkspaceRoot(), "large.txt")
	file, err := os.Create(largePath)
	if err != nil {
		t.Fatal(err)
	}
	if err := file.Truncate(MaxTextFileBytes + 1); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	_, err = service.ReadFile(ReadFileOptions{Path: "large.txt"})
	requireCode(t, err, "file_too_large")

	_, err = service.WriteFile(WriteFileOptions{Path: "too-big.txt", Content: strings.Repeat("a", MaxTextResultBytes+1)})
	requireCode(t, err, "content_too_large")

	line := strings.Repeat("a", 3*1024*1024)
	byteLimited := filepath.Join(service.WorkspaceRoot(), "byte-limit.txt")
	if err := os.WriteFile(byteLimited, []byte(line+"\n"+line), 0o644); err != nil {
		t.Fatal(err)
	}
	result, err := service.ReadFile(ReadFileOptions{Path: "byte-limit.txt"})
	if err != nil {
		t.Fatal(err)
	}
	if result.Content != line || result.ReturnedLines != 1 || !result.TruncatedByBytes || !result.Truncated {
		t.Fatalf("unexpected byte-limit result: returned=%d byBytes=%v truncated=%v", result.ReturnedLines, result.TruncatedByBytes, result.Truncated)
	}

	longLine := strings.Repeat("b", MaxTextResultBytes+10)
	longPath := filepath.Join(service.WorkspaceRoot(), "long-line.txt")
	if err := os.WriteFile(longPath, []byte(longLine), 0o644); err != nil {
		t.Fatal(err)
	}
	result, err = service.ReadFile(ReadFileOptions{Path: "long-line.txt"})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Content) != MaxTextResultBytes || !result.TruncatedLine || result.TruncatedByBytes || !result.Truncated {
		t.Fatalf("unexpected long-line result: bytes=%d line=%v byBytes=%v truncated=%v", len(result.Content), result.TruncatedLine, result.TruncatedByBytes, result.Truncated)
	}
}

func TestPathSafety(t *testing.T) {
	service, root := newTestService(t)
	outside := filepath.Join(root, "outside.txt")
	if err := os.WriteFile(outside, []byte("outside"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := service.ReadFile(ReadFileOptions{Path: "../outside.txt"})
	requireCode(t, err, "path_escape")
	_, err = service.ReadFile(ReadFileOptions{Path: outside})
	requireCode(t, err, "path_escape")
	_, err = service.WriteFile(WriteFileOptions{Path: "../evil.txt", Content: "x"})
	requireCode(t, err, "path_escape")
	_, err = service.WriteFile(WriteFileOptions{Path: filepath.Join(root, "evil.txt"), Content: "x"})
	requireCode(t, err, "path_escape")
	if runtime.GOOS == "windows" {
		volume := filepath.VolumeName(service.WorkspaceRoot())
		if volume != "" {
			_, err = service.ReadFile(ReadFileOptions{Path: volume + "outside.txt"})
			requireCode(t, err, "path_escape")
		}
	}
	if err := os.WriteFile(filepath.Join(service.WorkspaceRoot(), "inside.txt"), []byte("inside"), 0o644); err != nil {
		t.Fatal(err)
	}
	absolute := filepath.Join(service.WorkspaceRoot(), "inside.txt")
	result, err := service.ReadFile(ReadFileOptions{Path: absolute})
	if err != nil {
		t.Fatal(err)
	}
	if result.Content != "inside" {
		t.Fatalf("unexpected absolute read: %q", result.Content)
	}
}

func TestSymlinkSafety(t *testing.T) {
	service, root := newTestService(t)
	outside := filepath.Join(root, "outside.txt")
	if err := os.WriteFile(outside, []byte("outside"), 0o644); err != nil {
		t.Fatal(err)
	}
	escape := filepath.Join(service.WorkspaceRoot(), "escape.txt")
	if err := os.Symlink(outside, escape); err != nil {
		t.Skipf("cannot create symlink: %v", err)
	}
	_, err := service.ReadFile(ReadFileOptions{Path: "escape.txt"})
	requireCode(t, err, "path_escape")
	_, err = service.StatFile("escape.txt")
	requireCode(t, err, "path_escape")

	realPath := filepath.Join(service.WorkspaceRoot(), "real.txt")
	if err := os.WriteFile(realPath, []byte("real"), 0o644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(service.WorkspaceRoot(), "link.txt")
	if err := os.Symlink(realPath, link); err != nil {
		t.Skipf("cannot create symlink: %v", err)
	}
	result, err := service.ReadFile(ReadFileOptions{Path: "link.txt"})
	if err != nil {
		t.Fatal(err)
	}
	if result.Content != "real" {
		t.Fatalf("unexpected symlink read: %q", result.Content)
	}
	stat, err := service.StatFile("link.txt")
	if err != nil {
		t.Fatal(err)
	}
	if !stat.IsSymlink || stat.Type != "file" || !strings.HasSuffix(stat.RealPath, "real.txt") {
		t.Fatalf("unexpected symlink stat: %#v", stat)
	}
	write, err := service.WriteFile(WriteFileOptions{Path: "link.txt", Content: "replaced"})
	if err != nil {
		t.Fatal(err)
	}
	if !write.Replaced {
		t.Fatalf("expected replacement: %#v", write)
	}
	original, err := os.ReadFile(realPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(original) != "real" {
		t.Fatalf("symlink target was modified: %q", original)
	}
	info, err := os.Lstat(link)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		t.Fatal("symlink was not replaced")
	}
}

func TestSymlinkParentWriteEscape(t *testing.T) {
	service, root := newTestService(t)
	outsideDir := filepath.Join(root, "outside-dir")
	if err := os.MkdirAll(outsideDir, 0o755); err != nil {
		t.Fatal(err)
	}
	linkDir := filepath.Join(service.WorkspaceRoot(), "linkdir")
	if err := os.Symlink(outsideDir, linkDir); err != nil {
		t.Skipf("cannot create symlink: %v", err)
	}
	_, err := service.WriteFile(WriteFileOptions{Path: "linkdir/file.txt", Content: "x"})
	requireCode(t, err, "path_escape")
	_, err = service.WriteFile(WriteFileOptions{Path: "linkdir/sub/file.txt", Content: "x", CreateDirs: true})
	requireCode(t, err, "path_escape")
	if _, err := os.Stat(filepath.Join(outsideDir, "file.txt")); !os.IsNotExist(err) {
		t.Fatalf("write escaped through symlink: %v", err)
	}
}

func TestListSymlinkEntry(t *testing.T) {
	service, _ := newTestService(t)
	root := service.WorkspaceRoot()
	if err := os.WriteFile(filepath.Join(root, "target.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(root, "target.txt"), filepath.Join(root, "link.txt")); err != nil {
		t.Skipf("cannot create symlink: %v", err)
	}
	result, err := service.ListDirectory(ListDirectoryOptions{})
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range result.Entries {
		if entry.Name == "link.txt" {
			if entry.Type != "symlink" {
				t.Fatalf("unexpected symlink type: %#v", entry)
			}
			return
		}
	}
	t.Fatal("symlink entry not listed")
}

func TestWriteFileParentsAndValidation(t *testing.T) {
	service, _ := newTestService(t)
	_, err := service.WriteFile(WriteFileOptions{Path: "missing/file.txt", Content: "x"})
	requireCode(t, err, "parent_not_found")
	if _, err := service.WriteFile(WriteFileOptions{Path: "missing/file.txt", Content: "x", CreateDirs: true}); err != nil {
		t.Fatal(err)
	}
	_, err = service.WriteFile(WriteFileOptions{Path: ".", Content: "x"})
	requireCode(t, err, "not_a_file")
	_, err = service.WriteFile(WriteFileOptions{Path: "bad.txt", Content: "a\x00b"})
	requireCode(t, err, "invalid_text")
	_, err = service.WriteFile(WriteFileOptions{Path: "bad.txt", Content: string([]byte{0xff})})
	requireCode(t, err, "invalid_text")
	if _, err := service.WriteFile(WriteFileOptions{Path: "empty.txt", Content: ""}); err != nil {
		t.Fatal(err)
	}
	read, err := service.ReadFile(ReadFileOptions{Path: "empty.txt"})
	if err != nil {
		t.Fatal(err)
	}
	if read.TotalLines != 0 || read.Content != "" {
		t.Fatalf("unexpected empty file: %#v", read)
	}
}

func TestListDirectory(t *testing.T) {
	service, _ := newTestService(t)
	root := service.WorkspaceRoot()
	for _, dir := range []string{"zdir", "adir"} {
		if err := os.MkdirAll(filepath.Join(root, dir), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	for _, file := range []string{"b.txt", "A.txt", "c.txt"} {
		if err := os.WriteFile(filepath.Join(root, file), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	result, err := service.ListDirectory(ListDirectoryOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if result.Path != "." || result.Total != 5 || result.Truncated {
		t.Fatalf("unexpected list result: %#v", result)
	}
	names := make([]string, 0, len(result.Entries))
	for _, entry := range result.Entries {
		names = append(names, entry.Name)
	}
	want := []string{"adir", "zdir", "A.txt", "b.txt", "c.txt"}
	if strings.Join(names, ",") != strings.Join(want, ",") {
		t.Fatalf("unexpected order: %v", names)
	}
	for _, entry := range result.Entries {
		if entry.Type == "directory" {
			if entry.Size != nil || entry.ModifiedAt != nil {
				t.Fatalf("directory should have null metadata: %#v", entry)
			}
		} else if entry.Size == nil || entry.ModifiedAt == nil {
			t.Fatalf("file should have metadata: %#v", entry)
		}
	}
	_, err = service.ListDirectory(ListDirectoryOptions{Path: "A.txt"})
	requireCode(t, err, "not_a_directory")
	_, err = service.ListDirectory(ListDirectoryOptions{Path: "missing"})
	requireCode(t, err, "not_found")
}

func TestListDirectoryTruncation(t *testing.T) {
	service, _ := newTestService(t)
	dir := filepath.Join(service.WorkspaceRoot(), "many")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	for index := 0; index < MaxDirectoryEntries+1; index++ {
		name := filepath.Join(dir, fmt.Sprintf("f%04d", index))
		if err := os.WriteFile(name, nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	result, err := service.ListDirectory(ListDirectoryOptions{Path: "many"})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Entries) != MaxDirectoryEntries || result.Total != MaxDirectoryEntries+1 || !result.Truncated {
		t.Fatalf("unexpected truncation: entries=%d total=%d truncated=%v", len(result.Entries), result.Total, result.Truncated)
	}
}

func TestStatFile(t *testing.T) {
	service, _ := newTestService(t)
	if err := os.WriteFile(filepath.Join(service.WorkspaceRoot(), "file.txt"), []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	result, err := service.StatFile("file.txt")
	if err != nil {
		t.Fatal(err)
	}
	if result.Type != "file" || result.Size != 5 || result.IsSymlink {
		t.Fatalf("unexpected stat: %#v", result)
	}
	if result.CreatedAt == "" || result.ModifiedAt == "" {
		t.Fatalf("missing times: %#v", result)
	}
	_, err = service.StatFile("missing")
	requireCode(t, err, "not_found")
	dirResult, err := service.StatFile(".")
	if err != nil {
		t.Fatal(err)
	}
	if dirResult.Type != "directory" {
		t.Fatalf("unexpected directory stat: %#v", dirResult)
	}
}
