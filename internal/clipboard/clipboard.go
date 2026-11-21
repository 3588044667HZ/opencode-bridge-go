package clipboard

import (
	"context"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"time"
	"unicode/utf8"

	"go-bridge/internal/envutil"
	"go-bridge/internal/procutil"
)

const MaxBytes = 1 << 20

type Error struct {
	Code    string
	Message string
}

func (e *Error) Error() string {
	return fmt.Sprintf("%s: %s", e.Code, e.Message)
}

func NewError(code, format string, args ...any) *Error {
	return &Error{Code: code, Message: fmt.Sprintf(format, args...)}
}

type Backend struct {
	Name      string
	ReadPath  string
	ReadArgs  []string
	WritePath string
	WriteArgs []string
}

type Capability struct {
	Available bool    `json:"available"`
	Backend   *string `json:"backend"`
	Reason    *string `json:"reason"`
}

type ReadResult struct {
	Text    string `json:"text"`
	Bytes   int    `json:"bytes"`
	Backend string `json:"backend"`
}

type WriteResult struct {
	OK      bool   `json:"ok"`
	Bytes   int    `json:"bytes"`
	Backend string `json:"backend"`
}

type Service struct {
	backend *Backend
	reason  string
	env     []string
}

func New() *Service {
	return NewWithEnv(nil)
}

func NewWithEnv(env []string) *Service {
	backend, reason := detect(env)
	return &Service{backend: backend, reason: reason, env: env}
}

func (s *Service) commandEnv() []string {
	return envutil.Clean(serviceEnv(s.env))
}

func (s *Service) Capability() Capability {
	capability := Capability{Available: s.backend != nil}
	if s.backend != nil {
		name := s.backend.Name
		capability.Backend = &name
	}
	if s.reason != "" {
		reason := s.reason
		capability.Reason = &reason
	}
	return capability
}

func (s *Service) Read(ctx context.Context) (*ReadResult, error) {
	if s.backend == nil {
		return nil, NewError("clipboard_unavailable", "%s", s.reason)
	}
	data, err := readClipboard(ctx, s.backend, s.commandEnv())
	if err != nil {
		return nil, err
	}
	return &ReadResult{Text: string(data), Bytes: len(data), Backend: s.backend.Name}, nil
}

func (s *Service) Write(ctx context.Context, text string) (*WriteResult, error) {
	if strings.ContainsRune(text, 0) {
		return nil, NewError("invalid_text", "text must not contain NUL")
	}
	if !utf8.ValidString(text) {
		return nil, NewError("invalid_text", "text must be valid UTF-8")
	}
	if len(text) > MaxBytes {
		return nil, NewError("clipboard_too_large", "text exceeds %d bytes", MaxBytes)
	}
	if s.backend == nil {
		return nil, NewError("clipboard_unavailable", "%s", s.reason)
	}
	if err := writeClipboard(ctx, s.backend, text, s.commandEnv()); err != nil {
		return nil, err
	}
	return &WriteResult{OK: true, Bytes: len(text), Backend: s.backend.Name}, nil
}

func readClipboard(ctx context.Context, backend *Backend, env []string) ([]byte, error) {
	operationCtx, cancel := operationContext(ctx)
	defer cancel()
	command := exec.CommandContext(operationCtx, backend.ReadPath, backend.ReadArgs...)
	command.Env = env
	procutil.Prepare(command)
	stdout, err := command.StdoutPipe()
	if err != nil {
		return nil, NewError("clipboard_error", "cannot start clipboard read")
	}
	if err := command.Start(); err != nil {
		return nil, NewError("clipboard_error", "cannot start clipboard read")
	}
	data, readErr := io.ReadAll(io.LimitReader(stdout, MaxBytes+1))
	if len(data) > MaxBytes {
		_ = command.Process.Kill()
		_ = command.Wait()
		return nil, NewError("clipboard_too_large", "clipboard text exceeds %d bytes", MaxBytes)
	}
	if readErr != nil {
		_ = command.Process.Kill()
		_ = command.Wait()
		return nil, NewError("clipboard_error", "cannot read clipboard output")
	}
	if err := command.Wait(); err != nil {
		if operationCtx.Err() != nil {
			return nil, NewError("clipboard_error", "clipboard read timed out")
		}
		return nil, NewError("clipboard_error", "clipboard read failed")
	}
	if !utf8.Valid(data) {
		return nil, NewError("clipboard_error", "clipboard output is not valid UTF-8")
	}
	return data, nil
}

func writeClipboard(ctx context.Context, backend *Backend, text string, env []string) error {
	operationCtx, cancel := operationContext(ctx)
	defer cancel()
	command := exec.CommandContext(operationCtx, backend.WritePath, backend.WriteArgs...)
	command.Env = env
	command.Stdin = strings.NewReader(text)
	command.Stdout = io.Discard
	command.Stderr = io.Discard
	procutil.Prepare(command)
	if err := command.Run(); err != nil {
		if operationCtx.Err() != nil {
			return NewError("clipboard_error", "clipboard write timed out")
		}
		return NewError("clipboard_error", "clipboard write failed")
	}
	return nil
}

func operationContext(ctx context.Context) (context.Context, context.CancelFunc) {
	if ctx == nil {
		ctx = context.Background()
	}
	if _, ok := ctx.Deadline(); ok {
		return context.WithCancel(ctx)
	}
	return context.WithTimeout(ctx, 10*time.Second)
}
