package clipboard

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
)

func requireCode(t *testing.T, err error, code string) {
	t.Helper()
	var clipboardErr *Error
	if !errors.As(err, &clipboardErr) {
		t.Fatalf("expected %s, got %v", code, err)
	}
	if clipboardErr.Code != code {
		t.Fatalf("expected %s, got %s (%v)", code, clipboardErr.Code, err)
	}
}

func TestCapabilityUnavailable(t *testing.T) {
	service := &Service{reason: "no backend"}
	capability := service.Capability()
	if capability.Available || capability.Backend != nil {
		t.Fatalf("unexpected capability: %#v", capability)
	}
	if capability.Reason == nil || *capability.Reason != "no backend" {
		t.Fatalf("unexpected reason: %#v", capability.Reason)
	}
	_, err := service.Read(context.Background())
	requireCode(t, err, "clipboard_unavailable")
	_, err = service.Write(context.Background(), "x")
	requireCode(t, err, "clipboard_unavailable")
}

func TestCapabilityAvailable(t *testing.T) {
	service := &Service{backend: &Backend{Name: "test"}}
	capability := service.Capability()
	if !capability.Available || capability.Backend == nil || *capability.Backend != "test" || capability.Reason != nil {
		t.Fatalf("unexpected capability: %#v", capability)
	}
}

func TestWriteValidation(t *testing.T) {
	service := &Service{}
	_, err := service.Write(context.Background(), "a\x00b")
	requireCode(t, err, "invalid_text")
	_, err = service.Write(context.Background(), string([]byte{0xff}))
	requireCode(t, err, "invalid_text")
	_, err = service.Write(context.Background(), strings.Repeat("a", MaxBytes+1))
	requireCode(t, err, "clipboard_too_large")
}

func TestDetectShape(t *testing.T) {
	backend, reason := detect(os.Environ())
	if backend == nil {
		if reason == "" {
			t.Fatal("unavailable backend must have a reason")
		}
		return
	}
	if backend.Name == "" || backend.ReadPath == "" || backend.WritePath == "" || reason != "" {
		t.Fatalf("unexpected backend: %#v reason=%q", backend, reason)
	}
}
