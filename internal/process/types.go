package process

import (
	"fmt"
	"time"
)

type Config struct {
	Workspace        string
	ShellAlias       string
	Env              []string
	MaxBufferLines   int
	DefaultReadLines int
	MaxReadLines     int
	InitialQuiet     time.Duration
	SessionTTL       time.Duration
}

func DefaultConfig() Config {
	return Config{
		MaxBufferLines:   50000,
		DefaultReadLines: 1000,
		MaxReadLines:     10000,
		InitialQuiet:     150 * time.Millisecond,
		SessionTTL:       10 * time.Minute,
	}
}

type Shell struct {
	Alias     string `json:"alias"`
	Kind      string `json:"kind"`
	Path      string `json:"path"`
	PathStyle string `json:"pathStyle"`
}

type ShellSet struct {
	Default   string
	Available map[string]Shell
	Order     []string
	PathStyle string
}

type StartOptions struct {
	Command       string
	Workdir       string
	Timeout       time.Duration
	ShellAlias    string
	VerboseTiming bool
}

type StartResult struct {
	PID      int
	State    string
	ExitCode *int
	Shell    Shell
	Workdir  string
	Output   string
}

type ReadOptions struct {
	Offset  int
	Length  int
	Timeout time.Duration
}

type ReadResult struct {
	PID           int
	State         string
	ExitCode      *int
	Output        string
	TotalLines    int
	ReturnedLines int
	Truncated     bool
}

type InteractOptions struct {
	Input         string
	Timeout       time.Duration
	WaitForPrompt bool
}

type InteractResult struct {
	PID      int
	State    string
	ExitCode *int
	Output   string
}

type SessionInfo struct {
	PID       int       `json:"pid"`
	Shell     string    `json:"shell"`
	Workdir   string    `json:"workdir"`
	State     string    `json:"state"`
	StartedAt time.Time `json:"startedAt"`
	ExitCode  *int      `json:"exitCode,omitempty"`
}

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
