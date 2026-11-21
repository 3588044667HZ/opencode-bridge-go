package screenshot

import (
	"context"
	"errors"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func requireCode(t *testing.T, err error, code string) {
	t.Helper()
	var screenshotErr *Error
	if !errors.As(err, &screenshotErr) {
		t.Fatalf("expected %s, got %v", code, err)
	}
	if screenshotErr.Code != code {
		t.Fatalf("expected %s, got %s (%v)", code, screenshotErr.Code, err)
	}
}

func TestBuildCaptureResult(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "shot.png")
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	picture := image.NewRGBA(image.Rect(0, 0, 2, 3))
	if err := png.Encode(file, picture); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	result, err := buildCaptureResult(path, "ffmpeg")
	if err != nil {
		t.Fatal(err)
	}
	if result.Width != 2 || result.Height != 3 || result.Bytes == 0 || !result.Inlined || result.MIMEType != "image/png" || result.Backend != "ffmpeg" {
		t.Fatalf("unexpected result: %#v", result)
	}
	if int64(len(result.Inline)) != result.Bytes {
		t.Fatalf("inline size mismatch: %d vs %d", len(result.Inline), result.Bytes)
	}
}

func TestBuildCaptureResultTooLarge(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "large.png")
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := file.Truncate(MaxInlineBytes + 1); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	result, err := buildCaptureResult(path, "powershell")
	if err != nil {
		t.Fatal(err)
	}
	if result.Inlined || result.Inline != nil || result.Note == "" || result.Bytes != MaxInlineBytes+1 {
		t.Fatalf("unexpected result: %#v", result)
	}
}

func TestCaptureUnavailable(t *testing.T) {
	service := &Service{dir: t.TempDir(), reason: "no backend"}
	_, err := service.Capture(context.Background())
	requireCode(t, err, "screenshot_unavailable")
}

func TestCapability(t *testing.T) {
	available := &Service{configured: "auto", backend: "ffmpeg"}
	capability := available.Capability()
	if !capability.Available || capability.DetectedBackend == nil || *capability.DetectedBackend != "ffmpeg" || capability.Reason != nil || capability.ConfiguredBackend != "auto" {
		t.Fatalf("unexpected capability: %#v", capability)
	}
	unavailable := &Service{configured: "ffmpeg", reason: "ffmpeg is not available"}
	capability = unavailable.Capability()
	if capability.Available || capability.DetectedBackend != nil || capability.Reason == nil || *capability.Reason == "" {
		t.Fatalf("unexpected capability: %#v", capability)
	}
}

func TestCleanup(t *testing.T) {
	dir := t.TempDir()
	now := time.Now()
	write := func(name string, age time.Duration) string {
		t.Helper()
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte("data"), 0o644); err != nil {
			t.Fatal(err)
		}
		stamp := now.Add(-age)
		if err := os.Chtimes(path, stamp, stamp); err != nil {
			t.Fatal(err)
		}
		return path
	}
	oldFile := write("old.png", 8*24*time.Hour)
	recentFile := write("recent.png", 6*24*time.Hour)
	oldDir := filepath.Join(dir, "old-dir")
	if err := os.Mkdir(oldDir, 0o755); err != nil {
		t.Fatal(err)
	}
	stats, err := (&Service{dir: dir}).Cleanup(now)
	if err != nil {
		t.Fatal(err)
	}
	if stats.Files != 1 || stats.Bytes != 4 {
		t.Fatalf("unexpected stats: %#v", stats)
	}
	if _, err := os.Stat(oldFile); !os.IsNotExist(err) {
		t.Fatalf("old file still exists: %v", err)
	}
	if _, err := os.Stat(recentFile); err != nil {
		t.Fatalf("recent file removed: %v", err)
	}
	if _, err := os.Stat(oldDir); err != nil {
		t.Fatalf("directory removed: %v", err)
	}
}

func TestNewCreatesDirectory(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "shots")
	service, err := New(dir, "auto")
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !info.IsDir() {
		t.Fatalf("not a directory: %s", dir)
	}
	capability := service.Capability()
	if capability.ConfiguredBackend != "auto" {
		t.Fatalf("unexpected backend: %#v", capability)
	}
}

func TestDetectShape(t *testing.T) {
	backend, reason, _ := detect("auto", os.Environ())
	if backend == "" {
		if reason == "" {
			t.Fatal("unavailable backend must have a reason")
		}
		return
	}
	if reason != "" {
		t.Fatalf("available backend has reason: %q", reason)
	}
}
