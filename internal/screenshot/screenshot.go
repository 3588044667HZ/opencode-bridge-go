package screenshot

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"go-bridge/internal/envutil"
	"go-bridge/internal/procutil"
)

const (
	MaxInlineBytes = 4 * 1024 * 1024
	Retention      = 7 * 24 * time.Hour
)

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

type Capability struct {
	Available         bool    `json:"available"`
	ConfiguredBackend string  `json:"configuredBackend"`
	DetectedBackend   *string `json:"detectedBackend"`
	Reason            *string `json:"reason"`
}

type CaptureResult struct {
	Path     string `json:"path"`
	Width    int    `json:"width"`
	Height   int    `json:"height"`
	Bytes    int64  `json:"bytes"`
	Inlined  bool   `json:"inlined"`
	MIMEType string `json:"mimeType"`
	Backend  string `json:"backend"`
	Note     string `json:"note,omitempty"`
	Inline   []byte `json:"-"`
}

type CleanupStats struct {
	Files int
	Bytes int64
}

type Service struct {
	dir            string
	configured     string
	backend        string
	reason         string
	powerShellPath string
	env            []string
}

func New(dir, configured string) (*Service, error) {
	return NewWithEnv(dir, configured, nil)
}

func NewWithEnv(dir, configured string, env []string) (*Service, error) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return nil, NewError("io_error", "cannot resolve screenshots directory: %v", err)
	}
	abs = filepath.Clean(abs)
	if err := os.MkdirAll(abs, 0o755); err != nil {
		return nil, NewError("io_error", "cannot create screenshots directory: %v", err)
	}
	configured = strings.ToLower(strings.TrimSpace(configured))
	if configured == "" {
		configured = "auto"
	}
	backend, reason, powerShellPath := detect(configured, env)
	if backend == "" && reason == "" {
		reason = "screenshot is not available"
	}
	return &Service{
		dir:            abs,
		configured:     configured,
		backend:        backend,
		reason:         reason,
		powerShellPath: powerShellPath,
		env:            env,
	}, nil
}

func (s *Service) commandEnv() []string {
	return envutil.Clean(serviceEnv(s.env))
}

func (s *Service) Capability() Capability {
	capability := Capability{
		Available:         s.backend != "",
		ConfiguredBackend: s.configured,
	}
	if s.backend != "" {
		backend := s.backend
		capability.DetectedBackend = &backend
	}
	if s.reason != "" {
		reason := s.reason
		capability.Reason = &reason
	}
	return capability
}

func (s *Service) Capture(ctx context.Context) (*CaptureResult, error) {
	if s.backend == "" {
		return nil, NewError("screenshot_unavailable", "%s", s.reason)
	}
	if err := os.MkdirAll(s.dir, 0o755); err != nil {
		return nil, NewError("screenshot_error", "cannot create screenshots directory: %v", err)
	}
	name, err := newCaptureName()
	if err != nil {
		return nil, err
	}
	path := filepath.Join(s.dir, name)
	backend := s.backend
	err = s.captureWith(ctx, backend, path)
	if err != nil && backend == "ffmpeg" && s.configured == "auto" && s.powerShellPath != "" {
		_ = os.Remove(path)
		backend = "powershell"
		err = s.captureWith(ctx, backend, path)
	}
	if err != nil {
		_ = os.Remove(path)
		return nil, err
	}
	result, err := buildCaptureResult(path, backend)
	if err != nil {
		_ = os.Remove(path)
		return nil, err
	}
	return result, nil
}

func (s *Service) Cleanup(now time.Time) (CleanupStats, error) {
	var stats CleanupStats
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		return stats, NewError("io_error", "cannot read screenshots directory: %v", err)
	}
	for _, entry := range entries {
		info, err := entry.Info()
		if err != nil {
			continue
		}
		if !info.Mode().IsRegular() {
			continue
		}
		if now.Sub(info.ModTime()) <= Retention {
			continue
		}
		path := filepath.Join(s.dir, entry.Name())
		if os.Remove(path) == nil {
			stats.Files++
			stats.Bytes += info.Size()
		}
	}
	return stats, nil
}

func (s *Service) captureWith(ctx context.Context, backend, path string) error {
	lookup := envutil.NewLookup(serviceEnv(s.env))
	switch backend {
	case "ffmpeg":
		executable := lookup.LookPath("ffmpeg")
		if executable == "" {
			return NewError("screenshot_error", "ffmpeg is not available")
		}
		return runCapture(ctx, executable, []string{
			"-y",
			"-loglevel", "error",
			"-f", "gdigrab",
			"-i", "desktop",
			"-frames:v", "1",
			path,
		}, path, s.commandEnv())
	case "powershell":
		if s.powerShellPath == "" {
			return NewError("screenshot_error", "powershell is not available")
		}
		script := fmt.Sprintf(
			`Add-Type -AssemblyName System.Windows.Forms,System.Drawing; $bounds = [System.Windows.Forms.SystemInformation]::VirtualScreen; $bitmap = New-Object System.Drawing.Bitmap $bounds.Width, $bounds.Height; $graphics = [System.Drawing.Graphics]::FromImage($bitmap); $graphics.CopyFromScreen($bounds.Location, [System.Drawing.Point]::Empty, $bounds.Size); $bitmap.Save(%s, [System.Drawing.Imaging.ImageFormat]::Png); $graphics.Dispose(); $bitmap.Dispose()`,
			quotePowerShellPath(path),
		)
		return runCapture(ctx, s.powerShellPath, []string{"-NoLogo", "-NoProfile", "-STA", "-Command", script}, path, s.commandEnv())
	case "screencapture":
		executable := lookup.LookPath("screencapture")
		if executable == "" {
			return NewError("screenshot_error", "screencapture is not available")
		}
		return runCapture(ctx, executable, []string{"-x", "-t", "png", path}, path, s.commandEnv())
	case "grim":
		executable := lookup.LookPath("grim")
		if executable == "" {
			return NewError("screenshot_error", "grim is not available")
		}
		return runCapture(ctx, executable, []string{path}, path, s.commandEnv())
	case "scrot":
		executable := lookup.LookPath("scrot")
		if executable == "" {
			return NewError("screenshot_error", "scrot is not available")
		}
		return runCapture(ctx, executable, []string{"-o", path}, path, s.commandEnv())
	case "maim":
		executable := lookup.LookPath("maim")
		if executable == "" {
			return NewError("screenshot_error", "maim is not available")
		}
		return runCapture(ctx, executable, []string{path}, path, s.commandEnv())
	case "gnome-screenshot":
		executable := lookup.LookPath("gnome-screenshot")
		if executable == "" {
			return NewError("screenshot_error", "gnome-screenshot is not available")
		}
		return runCapture(ctx, executable, []string{"-f", path}, path, s.commandEnv())
	case "import":
		executable := lookup.LookPath("import")
		if executable == "" {
			return NewError("screenshot_error", "import is not available")
		}
		return runCapture(ctx, executable, []string{"-window", "root", path}, path, s.commandEnv())
	default:
		return NewError("screenshot_unavailable", "screenshot backend is not available")
	}
}

func runCapture(ctx context.Context, executable string, args []string, outputPath string, env []string) error {
	operationCtx, cancel := operationContext(ctx)
	defer cancel()
	command := exec.CommandContext(operationCtx, executable, args...)
	command.Env = env
	command.Stdout = io.Discard
	command.Stderr = io.Discard
	procutil.Prepare(command)
	if err := command.Run(); err != nil {
		if operationCtx.Err() != nil {
			return NewError("screenshot_error", "screenshot command timed out")
		}
		return NewError("screenshot_error", "screenshot command failed")
	}
	info, err := os.Stat(outputPath)
	if err != nil {
		return NewError("screenshot_error", "screenshot was not created")
	}
	if !info.Mode().IsRegular() {
		return NewError("screenshot_error", "screenshot is not a regular file")
	}
	if info.Size() == 0 {
		return NewError("screenshot_error", "screenshot is empty")
	}
	return nil
}

func buildCaptureResult(path, backend string) (*CaptureResult, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, NewError("screenshot_error", "cannot stat screenshot")
	}
	if !info.Mode().IsRegular() || info.Size() == 0 {
		return nil, NewError("screenshot_error", "screenshot is empty")
	}
	width, height := imageSize(path)
	result := &CaptureResult{
		Path:     path,
		Width:    width,
		Height:   height,
		Bytes:    info.Size(),
		MIMEType: "image/png",
		Backend:  backend,
	}
	if info.Size() <= MaxInlineBytes {
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, NewError("screenshot_error", "cannot read screenshot")
		}
		result.Inlined = true
		result.Inline = data
	} else {
		result.Note = "screenshot exceeds 4 MiB inline limit"
	}
	return result, nil
}

func imageSize(path string) (int, int) {
	file, err := os.Open(path)
	if err != nil {
		return 0, 0
	}
	defer file.Close()
	config, _, err := image.DecodeConfig(file)
	if err != nil {
		return 0, 0
	}
	return config.Width, config.Height
}

func newCaptureName() (string, error) {
	var suffix [4]byte
	if _, err := rand.Read(suffix[:]); err != nil {
		return "", NewError("screenshot_error", "cannot generate screenshot name")
	}
	return fmt.Sprintf("screenshot-%s-%s.png", time.Now().UTC().Format("20060102-150405"), hex.EncodeToString(suffix[:])), nil
}

func quotePowerShellPath(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "''") + "'"
}

func operationContext(ctx context.Context) (context.Context, context.CancelFunc) {
	if ctx == nil {
		ctx = context.Background()
	}
	if _, ok := ctx.Deadline(); ok {
		return context.WithCancel(ctx)
	}
	return context.WithTimeout(ctx, 30*time.Second)
}
