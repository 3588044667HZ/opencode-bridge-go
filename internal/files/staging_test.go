package files

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestStagingListAndDelete(t *testing.T) {
	service, _ := newTestService(t)
	inbox, err := service.StagingPath("inbox")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(inbox, "a.txt"), []byte("one"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(inbox, "b.bin"), []byte("two"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(inbox, "bad name.txt"), []byte("bad"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(inbox, "partial.part"), []byte("part"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(inbox, "dir"), 0o755); err != nil {
		t.Fatal(err)
	}
	linkCreated := false
	if err := os.Symlink(filepath.Join(inbox, "a.txt"), filepath.Join(inbox, "link.txt")); err == nil {
		linkCreated = true
	}
	list, err := service.ListStaged("inbox")
	if err != nil {
		t.Fatal(err)
	}
	if list.Direction != "inbox" || list.Path != inbox || list.TotalBytes != 6 || list.IgnoredInvalid != 1 || list.Truncated {
		t.Fatalf("unexpected list: %#v", list)
	}
	if len(list.Files) != 2 || list.Files[0].Name != "a.txt" || list.Files[1].Name != "b.bin" {
		t.Fatalf("unexpected files: %#v", list.Files)
	}
	if _, err := service.DeleteStaged("inbox", "a.txt"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(inbox, "a.txt")); !os.IsNotExist(err) {
		t.Fatalf("file still exists: %v", err)
	}
	_, err = service.DeleteStaged("inbox", "a.txt")
	requireCode(t, err, "not_found")
	_, err = service.DeleteStaged("inbox", "dir")
	requireCode(t, err, "not_a_file")
	_, err = service.DeleteStaged("inbox", "bad name.txt")
	requireCode(t, err, "invalid_name")
	_, err = service.DeleteStaged("sideways", "a.txt")
	requireCode(t, err, "invalid_direction")
	_, err = service.ListStaged("sideways")
	requireCode(t, err, "invalid_direction")
	if linkCreated {
		if _, err := service.DeleteStaged("inbox", "link.txt"); err != nil {
			t.Fatal(err)
		}
		if _, err := os.Stat(filepath.Join(inbox, "a.txt")); !os.IsNotExist(err) {
			t.Fatalf("symlink target should already be missing: %v", err)
		}
	}
}

func TestStagingQuota(t *testing.T) {
	root := t.TempDir()
	service, err := New(Config{
		WorkspaceRoot:     filepath.Join(root, "workspace"),
		StagingInbox:      filepath.Join(root, "staging", "inbox"),
		StagingOutbox:     filepath.Join(root, "staging", "outbox"),
		MaxFileBytes:      10,
		StagingQuotaBytes: 20,
	})
	if err != nil {
		t.Fatal(err)
	}
	upload := func(name, content string) (*UploadResult, error) {
		return service.Upload("inbox", name, int64(len(content)), strings.NewReader(content), "")
	}
	if _, err := upload("a.txt", "1234567890"); err != nil {
		t.Fatal(err)
	}
	if _, err := upload("b.txt", "1234567890"); err != nil {
		t.Fatal(err)
	}
	_, err = upload("c.txt", "1")
	requireCode(t, err, "quota_exceeded")
	if _, err := upload("a.txt", "abcdefghij"); err != nil {
		t.Fatal(err)
	}
	_, err = upload("d.txt", "12345678901")
	requireCode(t, err, "file_too_large")
	list, err := service.ListStaged("inbox")
	if err != nil {
		t.Fatal(err)
	}
	if list.TotalBytes != 20 {
		t.Fatalf("unexpected total bytes: %d", list.TotalBytes)
	}
}

func TestConcurrentUploadQuota(t *testing.T) {
	root := t.TempDir()
	service, err := New(Config{
		WorkspaceRoot:     filepath.Join(root, "workspace"),
		StagingInbox:      filepath.Join(root, "staging", "inbox"),
		StagingOutbox:     filepath.Join(root, "staging", "outbox"),
		MaxFileBytes:      10,
		StagingQuotaBytes: 10,
	})
	if err != nil {
		t.Fatal(err)
	}
	const uploads = 5
	var wg sync.WaitGroup
	results := make(chan error, uploads)
	for index := 0; index < uploads; index++ {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			_, err := service.Upload("inbox", fmt.Sprintf("f%d.txt", index), 4, strings.NewReader("1234"), "")
			results <- err
		}(index)
	}
	wg.Wait()
	close(results)
	successes := 0
	quotaErrors := 0
	for err := range results {
		if err == nil {
			successes++
			continue
		}
		var fileErr *Error
		if !errors.As(err, &fileErr) || fileErr.Code != "quota_exceeded" {
			t.Fatalf("unexpected upload error: %v", err)
		}
		quotaErrors++
	}
	if successes != 2 || quotaErrors != 3 {
		t.Fatalf("unexpected results: successes=%d quotaErrors=%d", successes, quotaErrors)
	}
	list, err := service.ListStaged("inbox")
	if err != nil {
		t.Fatal(err)
	}
	if list.TotalBytes != 8 {
		t.Fatalf("unexpected total bytes: %d", list.TotalBytes)
	}
}

func TestUploadChecksumAndLength(t *testing.T) {
	service, _ := newTestService(t)
	content := "hello"
	sum := sha256.Sum256([]byte(content))
	encoded := hex.EncodeToString(sum[:])
	result, err := service.Upload("inbox", "a.txt", int64(len(content)), strings.NewReader(content), strings.ToUpper(encoded))
	if err != nil {
		t.Fatal(err)
	}
	if result.Size != int64(len(content)) || result.SHA256 != encoded || result.Replaced {
		t.Fatalf("unexpected upload: %#v", result)
	}
	_, err = service.Upload("inbox", "bad.txt", int64(len(content)), strings.NewReader(content), strings.Repeat("0", 64))
	requireCode(t, err, "checksum_mismatch")
	_, err = service.Upload("inbox", "short.txt", 5, io.LimitReader(strings.NewReader(content), 3), "")
	requireCode(t, err, "length_mismatch")
	inbox, err := service.StagingPath("inbox")
	if err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(inbox)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.HasSuffix(strings.ToLower(entry.Name()), ".part") {
			t.Fatalf("temporary file was not removed: %s", entry.Name())
		}
	}
	replaced, err := service.Upload("inbox", "a.txt", 2, strings.NewReader("xy"), "")
	if err != nil {
		t.Fatal(err)
	}
	if !replaced.Replaced {
		t.Fatalf("expected replacement: %#v", replaced)
	}
}

func TestOpenOutbox(t *testing.T) {
	service, _ := newTestService(t)
	outbox, err := service.StagingPath("outbox")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(outbox, "result.bin"), []byte("data"), 0o644); err != nil {
		t.Fatal(err)
	}
	file, info, err := service.OpenOutbox("result.bin")
	if err != nil {
		t.Fatal(err)
	}
	data, err := io.ReadAll(file)
	if err != nil {
		t.Fatal(err)
	}
	_ = file.Close()
	if string(data) != "data" || info.Size() != 4 {
		t.Fatalf("unexpected open result: data=%q size=%d", data, info.Size())
	}
	if err := os.Mkdir(filepath.Join(outbox, "dir"), 0o755); err != nil {
		t.Fatal(err)
	}
	_, _, err = service.OpenOutbox("dir")
	requireCode(t, err, "not_found")
	if err := os.WriteFile(filepath.Join(outbox, "x.part"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, _, err = service.OpenOutbox("x.part")
	requireCode(t, err, "not_found")
	if err := os.Symlink(filepath.Join(outbox, "result.bin"), filepath.Join(outbox, "link.bin")); err == nil {
		_, _, err = service.OpenOutbox("link.bin")
		requireCode(t, err, "not_found")
	}
}

func TestCleanup(t *testing.T) {
	service, _ := newTestService(t)
	inbox, err := service.StagingPath("inbox")
	if err != nil {
		t.Fatal(err)
	}
	outbox, err := service.StagingPath("outbox")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	write := func(dir, name, content string, age time.Duration) string {
		t.Helper()
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
		stamp := now.Add(-age)
		if err := os.Chtimes(path, stamp, stamp); err != nil {
			t.Fatal(err)
		}
		return path
	}
	oldFile := write(inbox, "old.txt", "old", 8*24*time.Hour)
	recentFile := write(inbox, "recent.txt", "recent", 6*24*time.Hour)
	oldOutbox := write(outbox, "old-out.txt", "old-out", 8*24*time.Hour)
	oldPart := write(inbox, "stale.part", "part", 2*time.Hour)
	freshPart := write(outbox, "fresh.part", "part", 30*time.Minute)
	stats, err := service.Cleanup(now)
	if err != nil {
		t.Fatal(err)
	}
	if stats.Files != 2 || stats.Bytes != int64(len("old")+len("old-out")) || stats.PartFiles != 1 || stats.PartBytes != int64(len("part")) {
		t.Fatalf("unexpected cleanup stats: %#v", stats)
	}
	for _, path := range []string{oldFile, oldOutbox, oldPart} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("expected removal: %s (%v)", path, err)
		}
	}
	for _, path := range []string{recentFile, freshPart} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("unexpected removal: %s (%v)", path, err)
		}
	}
}
