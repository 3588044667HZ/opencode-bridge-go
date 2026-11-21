package sysinfo

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"

	"go-bridge/internal/clipboard"
	"go-bridge/internal/screenshot"
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

type Config struct {
	Workspace      string
	DataDir        string
	StagingInbox   string
	StagingOutbox  string
	FilesEnabled   bool
	FilesHost      string
	FilesPort      int
	FilesPath      string
	ShellDefault   string
	ShellAvailable []string
	ShellPathStyle string
}

type System struct {
	cfg                  Config
	screenshotCapability screenshot.Capability
	clipboardCapability  clipboard.Capability
}

type OSInfo struct {
	Platform string `json:"platform"`
	Type     string `json:"type"`
	Release  string `json:"release"`
	Version  string `json:"version"`
}

type ShellInfo struct {
	Default   string   `json:"default"`
	Available []string `json:"available"`
	PathStyle string   `json:"pathStyle"`
}

type StagingInfo struct {
	Inbox  string `json:"inbox"`
	Outbox string `json:"outbox"`
}

type PathsInfo struct {
	DefaultWorkdir string      `json:"defaultWorkdir"`
	WorkspaceRoot  string      `json:"workspaceRoot"`
	DataDir        string      `json:"dataDir"`
	Staging        StagingInfo `json:"staging"`
}

type FileServiceInfo struct {
	Enabled bool    `json:"enabled"`
	Host    string  `json:"host"`
	Port    int     `json:"port"`
	BaseURL *string `json:"baseUrl"`
}

type CapabilitiesInfo struct {
	Screenshot screenshot.Capability `json:"screenshot"`
	Clipboard  clipboard.Capability  `json:"clipboard"`
}

type Info struct {
	OS           OSInfo           `json:"os"`
	Hostname     string           `json:"hostname"`
	Arch         string           `json:"arch"`
	Shell        ShellInfo        `json:"shell"`
	Paths        PathsInfo        `json:"paths"`
	FileService  FileServiceInfo  `json:"fileService"`
	Capabilities CapabilitiesInfo `json:"capabilities"`
}

func New(cfg Config, screenshotCapability screenshot.Capability, clipboardCapability clipboard.Capability) *System {
	return &System{
		cfg:                  cfg,
		screenshotCapability: screenshotCapability,
		clipboardCapability:  clipboardCapability,
	}
}

func (s *System) Get() (*Info, error) {
	if s.cfg.ShellDefault == "" {
		return nil, NewError("system_error", "default shell is not available")
	}
	defaultFound := false
	for _, alias := range s.cfg.ShellAvailable {
		if alias == s.cfg.ShellDefault {
			defaultFound = true
			break
		}
	}
	if !defaultFound {
		return nil, NewError("system_error", "default shell is not in available shells")
	}
	hostname, err := os.Hostname()
	if err != nil {
		return nil, NewError("system_error", "cannot determine hostname: %v", err)
	}
	workspace := absolutePath(s.cfg.Workspace)
	info := &Info{
		OS: OSInfo{
			Platform: platformName(),
			Type:     osType(),
			Release:  osRelease(),
			Version:  osVersion(),
		},
		Hostname: hostname,
		Arch:     archName(),
		Shell: ShellInfo{
			Default:   s.cfg.ShellDefault,
			Available: append([]string{}, s.cfg.ShellAvailable...),
			PathStyle: s.cfg.ShellPathStyle,
		},
		Paths: PathsInfo{
			DefaultWorkdir: workspace,
			WorkspaceRoot:  workspace,
			DataDir:        absolutePath(s.cfg.DataDir),
			Staging: StagingInfo{
				Inbox:  absolutePath(s.cfg.StagingInbox),
				Outbox: absolutePath(s.cfg.StagingOutbox),
			},
		},
		FileService: FileServiceInfo{
			Enabled: s.cfg.FilesEnabled,
			Host:    s.cfg.FilesHost,
			Port:    s.cfg.FilesPort,
		},
		Capabilities: CapabilitiesInfo{
			Screenshot: s.screenshotCapability,
			Clipboard:  s.clipboardCapability,
		},
	}
	if s.cfg.FilesEnabled {
		baseURL := fmt.Sprintf("http://%s:%d%s", s.cfg.FilesHost, s.cfg.FilesPort, s.cfg.FilesPath)
		info.FileService.BaseURL = &baseURL
	}
	return info, nil
}

func absolutePath(path string) string {
	if path == "" {
		return path
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return path
	}
	return filepath.Clean(abs)
}

func platformName() string {
	switch runtime.GOOS {
	case "windows":
		return "win32"
	case "darwin":
		return "darwin"
	case "linux":
		return "linux"
	case "freebsd":
		return "freebsd"
	case "openbsd":
		return "openbsd"
	case "netbsd":
		return "netbsd"
	case "solaris":
		return "sunos"
	default:
		return runtime.GOOS
	}
}

func archName() string {
	switch runtime.GOARCH {
	case "amd64":
		return "x64"
	case "386":
		return "ia32"
	case "arm":
		return "arm"
	case "arm64":
		return "arm64"
	case "ppc64":
		return "ppc64"
	case "ppc64le":
		return "ppc64"
	case "s390x":
		return "s390x"
	case "mips", "mipsle":
		return "mips"
	default:
		return runtime.GOARCH
	}
}
