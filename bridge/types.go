package bridge

import "fmt"

const (
	APIVersion      = 1
	ContractVersion = "bridge-v1"
)

type State string

const (
	StateNew      State = "new"
	StateStarting State = "starting"
	StateRunning  State = "running"
	StateStopping State = "stopping"
	StateStopped  State = "stopped"
	StateFailed   State = "failed"
)

type LogLevel string

const (
	LevelDebug LogLevel = "debug"
	LevelInfo  LogLevel = "info"
	LevelWarn  LogLevel = "warn"
	LevelError LogLevel = "error"
)

type Logger func(level LogLevel, message string, fields map[string]any)

const (
	CodeInvalidConfig  = "invalid_config"
	CodeInvalidSecret  = "invalid_secret"
	CodeAlreadyStarted = "already_started"
	CodeStopInProgress = "stop_in_progress"
	CodeNotStarted     = "not_started"
	CodeStartFailed    = "start_failed"
	CodeStopFailed     = "stop_failed"
	CodePortInUse      = "port_in_use"
	CodeMemberFailed   = "member_failed"
	CodeTimeout        = "timeout"
	CodeUnsupported    = "unsupported_platform"
)

type BridgeError struct {
	Code    string
	Message string
}

func (e *BridgeError) Error() string {
	return fmt.Sprintf("%s: %s", e.Code, e.Message)
}

func newBridgeError(code, format string, args ...any) *BridgeError {
	return &BridgeError{Code: code, Message: fmt.Sprintf(format, args...)}
}

type MCPConfig struct {
	Host string `json:"host"`
	Port int    `json:"port"`
	Path string `json:"path"`
}

type FilesConfig struct {
	Enabled           bool   `json:"enabled"`
	Host              string `json:"host"`
	Port              int    `json:"port"`
	Path              string `json:"path"`
	MaxFileBytes      int64  `json:"maxFileBytes"`
	StagingQuotaBytes int64  `json:"stagingQuotaBytes"`
}

type ScreenshotConfig struct {
	Backend string `json:"backend"`
}

type ShellConfig struct {
	Default string `json:"default,omitempty"`
}

type Config struct {
	Version    int              `json:"version"`
	Root       string           `json:"root"`
	DataDir    string           `json:"dataDir"`
	Workspace  string           `json:"workspace"`
	MCP        MCPConfig        `json:"mcp"`
	Files      FilesConfig      `json:"files"`
	Screenshot ScreenshotConfig `json:"screenshot"`
	Shell      ShellConfig      `json:"shell"`
}

type PublicConfig = Config

type Secrets struct {
	MCPToken   string
	FilesToken string
}

type Options struct {
	Config  Config
	Secrets Secrets
	Env     map[string]string
	Logger  Logger
}

type Address struct {
	Host string `json:"host"`
	Port int    `json:"port"`
	Path string `json:"path"`
}

type Addresses struct {
	MCP   Address  `json:"mcp"`
	Files *Address `json:"files"`
}

type Health struct {
	State    State       `json:"state"`
	UptimeMs int64       `json:"uptimeMs"`
	MCP      HealthMCP   `json:"mcp"`
	Files    HealthFiles `json:"files"`
}

type HealthMCP struct {
	OK    bool     `json:"ok"`
	URL   *string  `json:"url"`
	Tools []string `json:"tools"`
}

type HealthFiles struct {
	Enabled bool    `json:"enabled"`
	OK      *bool   `json:"ok"`
	URL     *string `json:"url"`
}

type Diagnostics struct {
	State         State      `json:"state"`
	PID           *int       `json:"pid"`
	StartedAt     *string    `json:"startedAt"`
	UptimeMs      int64      `json:"uptimeMs"`
	Config        Config     `json:"config"`
	Addresses     *Addresses `json:"addresses"`
	ExpectedTools []string   `json:"expectedTools"`
}

type ShellSummary struct {
	Default   string   `json:"default"`
	Available []string `json:"available"`
	PathStyle string   `json:"pathStyle"`
}

type Event string

const (
	EventReady            Event = "ready"
	EventStopped          Event = "stopped"
	EventError            Event = "error"
	EventMemberError      Event = "member-error"
	EventFileServiceError Event = "file-service-error"
)

type ReadyEvent struct {
	Addresses *Addresses `json:"addresses"`
	MCPURL    *string    `json:"mcpUrl"`
	FilesURL  *string    `json:"filesUrl"`
}

type ErrorEvent struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type StoppedEvent struct {
	Reason string `json:"reason,omitempty"`
}

type MemberErrorEvent struct {
	Member  string `json:"member"`
	Code    string `json:"code"`
	Message string `json:"message"`
}

type FileServiceErrorEvent struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

var expectedToolNames = []string{
	"capture_screenshot",
	"delete_staged_file",
	"force_terminate",
	"get_clipboard",
	"get_system_info",
	"interact_with_process",
	"list_directory",
	"list_sessions",
	"list_staged_files",
	"read_file",
	"read_process_output",
	"set_clipboard",
	"start_process",
	"stat_file",
	"write_file",
}
