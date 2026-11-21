package process

import (
	"strings"
	"testing"
)

func TestOutputBufferReadAndCursor(t *testing.T) {
	b := newOutputBuffer(100)
	b.Append([]byte("a\nb\n"))
	cursor := 0
	out, total, returned, truncated := b.Read(0, 10, &cursor)
	if out != "a\nb" || total != 2 || returned != 2 || truncated {
		t.Fatalf("unexpected first read: %q total=%d returned=%d truncated=%v", out, total, returned, truncated)
	}
	b.Append([]byte("c\n"))
	out, _, _, _ = b.Read(0, 10, &cursor)
	if out != "c" {
		t.Fatalf("unexpected second read: %q", out)
	}
	out, _, _, _ = b.Read(1, 1, &cursor)
	if out != "a" {
		t.Fatalf("unexpected absolute read: %q", out)
	}
	out, _, _, _ = b.Read(-1, 1, &cursor)
	if out != "c" {
		t.Fatalf("unexpected tail read: %q", out)
	}
}

func TestOutputBufferPending(t *testing.T) {
	b := newOutputBuffer(100)
	b.Append([]byte("prompt> "))
	cursor := 0
	out, total, _, _ := b.Read(0, 10, &cursor)
	if !strings.Contains(out, "prompt>") || total != 1 {
		t.Fatalf("unexpected pending read: %q total=%d", out, total)
	}
	b.Flush()
	out, total, _, _ = b.Read(0, 10, &cursor)
	if out != "prompt> " || total != 1 {
		t.Fatalf("unexpected flushed read: %q total=%d", out, total)
	}
}
