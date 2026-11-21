package bridge

import (
	"context"
	"net"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

type Bridge struct {
	opts   Options
	logger Logger

	opMu sync.Mutex

	mu             sync.RWMutex
	state          State
	config         Config
	addresses      *Addresses
	mcpURL         *string
	filesURL       *string
	startedAt      time.Time
	readyCh        chan struct{}
	readyErr       *BridgeError
	res            *resources
	shellDefault   string
	shellAvailable []string
	shellPathStyle string

	eventMu      sync.Mutex
	listeners    map[Event][]listenerEntry
	nextListener int
}

type listenerEntry struct {
	id int
	fn func(any)
}

func NewBridge(opts Options) *Bridge {
	logger := opts.Logger
	if logger == nil {
		logger = func(LogLevel, string, map[string]any) {}
	}
	env := make(map[string]string, len(opts.Env))
	for key, value := range opts.Env {
		env[key] = value
	}
	opts.Env = env
	return &Bridge{
		opts:      opts,
		logger:    logger,
		state:     StateNew,
		config:    opts.Config,
		listeners: map[Event][]listenerEntry{},
	}
}

func (b *Bridge) APIVersion() int {
	return APIVersion
}

func (b *Bridge) ContractVersion() string {
	return ContractVersion
}

func (b *Bridge) State() State {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.state
}

func (b *Bridge) Config() Config {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.config
}

func (b *Bridge) ExpectedTools() []string {
	return append([]string{}, expectedToolNames...)
}

func (b *Bridge) Addresses() *Addresses {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return cloneAddresses(b.addresses)
}

func (b *Bridge) MCPURL() *string {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return cloneString(b.mcpURL)
}

func (b *Bridge) FilesURL() *string {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return cloneString(b.filesURL)
}

func (b *Bridge) ShellSummary() ShellSummary {
	b.mu.RLock()
	defer b.mu.RUnlock()
	summary := ShellSummary{
		Default:   b.shellDefault,
		Available: append([]string{}, b.shellAvailable...),
		PathStyle: b.shellPathStyle,
	}
	return summary
}

func (b *Bridge) Start(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	b.mu.RLock()
	state := b.state
	b.mu.RUnlock()
	if state == StateStarting {
		return newBridgeError(CodeAlreadyStarted, "bridge is already starting")
	}
	if state == StateStopping {
		return newBridgeError(CodeStopInProgress, "bridge is stopping")
	}
	if state == StateRunning {
		return nil
	}

	b.opMu.Lock()
	defer b.opMu.Unlock()
	b.mu.Lock()
	switch b.state {
	case StateRunning:
		b.mu.Unlock()
		return nil
	case StateStarting:
		b.mu.Unlock()
		return newBridgeError(CodeAlreadyStarted, "bridge is already starting")
	case StateStopping:
		b.mu.Unlock()
		return newBridgeError(CodeStopInProgress, "bridge is stopping")
	}
	readyCh := make(chan struct{})
	b.state = StateStarting
	b.readyCh = readyCh
	b.readyErr = nil
	b.mu.Unlock()

	begin := time.Now()
	b.log(LevelInfo, "starting bridge", map[string]any{"component": "bridge", "state": string(StateStarting)})
	cfg, validationErr := normalizeConfig(b.opts.Config, b.opts.Secrets)
	if validationErr != nil {
		b.failStart(readyCh, validationErr, begin)
		return validationErr
	}
	b.mu.Lock()
	b.config = cfg
	b.mu.Unlock()
	if err := ctx.Err(); err != nil {
		cause := newBridgeError(CodeStartFailed, "start canceled")
		b.failStart(readyCh, cause, begin)
		return cause
	}
	res, addresses, mcpURL, filesURL, err := b.startResources(ctx, cfg, b.opts.Secrets, envSlice(b.opts.Env))
	if err != nil {
		b.failStart(readyCh, err, begin)
		return err
	}
	b.mu.Lock()
	b.res = res
	b.addresses = addresses
	b.mcpURL = mcpURL
	b.filesURL = filesURL
	b.shellDefault = res.shell.Default
	b.shellAvailable = append([]string{}, res.shell.Available...)
	b.shellPathStyle = res.shell.PathStyle
	b.state = StateRunning
	b.startedAt = time.Now()
	b.readyErr = nil
	b.mu.Unlock()
	close(readyCh)
	b.log(LevelInfo, "bridge started", map[string]any{
		"component":  "bridge",
		"state":      string(StateRunning),
		"durationMs": time.Since(begin).Milliseconds(),
	})
	b.emit(EventReady, &ReadyEvent{
		Addresses: cloneAddresses(addresses),
		MCPURL:    cloneString(mcpURL),
		FilesURL:  cloneString(filesURL),
	})
	return nil
}

func (b *Bridge) Stop(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	b.opMu.Lock()
	defer b.opMu.Unlock()
	b.mu.RLock()
	state := b.state
	b.mu.RUnlock()
	if state == StateStopped {
		return nil
	}
	if state == StateNew {
		b.mu.Lock()
		b.state = StateStopped
		b.addresses = nil
		b.mcpURL = nil
		b.filesURL = nil
		b.startedAt = time.Time{}
		b.readyCh = nil
		b.readyErr = nil
		b.mu.Unlock()
		b.emit(EventStopped, &StoppedEvent{})
		return nil
	}
	b.mu.Lock()
	b.state = StateStopping
	res := b.res
	b.mu.Unlock()
	b.log(LevelInfo, "stopping bridge", map[string]any{"component": "bridge", "state": string(StateStopping)})
	stopErr := b.stopResources(res, ctx)
	b.mu.Lock()
	b.state = StateStopped
	b.res = nil
	b.addresses = nil
	b.mcpURL = nil
	b.filesURL = nil
	b.startedAt = time.Time{}
	b.readyCh = nil
	b.readyErr = nil
	b.shellDefault = ""
	b.shellAvailable = nil
	b.shellPathStyle = ""
	b.mu.Unlock()
	b.emit(EventStopped, &StoppedEvent{})
	if stopErr != nil {
		return newBridgeError(CodeStopFailed, "%v", stopErr)
	}
	return nil
}

func (b *Bridge) WaitForReady(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	b.mu.RLock()
	state := b.state
	readyCh := b.readyCh
	readyErr := b.readyErr
	b.mu.RUnlock()
	switch state {
	case StateRunning:
		return nil
	case StateFailed:
		if readyErr != nil {
			return readyErr
		}
		return newBridgeError(CodeStartFailed, "bridge failed to start")
	case StateStarting:
		if readyCh == nil {
			return newBridgeError(CodeNotStarted, "bridge is not starting")
		}
		select {
		case <-readyCh:
			b.mu.RLock()
			state = b.state
			err := b.readyErr
			b.mu.RUnlock()
			if state == StateRunning {
				return nil
			}
			if err != nil {
				return err
			}
			return newBridgeError(CodeStartFailed, "bridge failed to start")
		case <-ctx.Done():
			return newBridgeError(CodeTimeout, "timed out waiting for bridge to become ready")
		}
	default:
		return newBridgeError(CodeNotStarted, "bridge is not started")
	}
}

func (b *Bridge) Health(ctx context.Context) *Health {
	if ctx == nil {
		ctx = context.Background()
	}
	b.mu.RLock()
	state := b.state
	startedAt := b.startedAt
	addresses := cloneAddresses(b.addresses)
	mcpURL := cloneString(b.mcpURL)
	filesURL := cloneString(b.filesURL)
	cfg := b.config
	res := b.res
	b.mu.RUnlock()
	health := &Health{
		State: state,
		MCP: HealthMCP{
			URL:   mcpURL,
			Tools: []string{},
		},
		Files: HealthFiles{
			Enabled: cfg.Files.Enabled,
			URL:     filesURL,
		},
	}
	if state == StateRunning && !startedAt.IsZero() {
		health.UptimeMs = time.Since(startedAt).Milliseconds()
	}
	if state == StateRunning && addresses != nil {
		health.MCP.OK = dialable(ctx, addresses.MCP)
		health.MCP.Tools = append([]string{}, expectedToolNames...)
		if cfg.Files.Enabled {
			filesOK := res != nil && res.filesAvailable() && addresses.Files != nil && dialable(ctx, *addresses.Files)
			health.Files.OK = &filesOK
		}
	} else if cfg.Files.Enabled {
		filesOK := false
		health.Files.OK = &filesOK
	}
	if !cfg.Files.Enabled {
		health.Files.OK = nil
		health.Files.URL = nil
	}
	return health
}

func (b *Bridge) Diagnostics() *Diagnostics {
	b.mu.RLock()
	state := b.state
	startedAt := b.startedAt
	cfg := b.config
	addresses := cloneAddresses(b.addresses)
	b.mu.RUnlock()
	diagnostics := &Diagnostics{
		State:         state,
		Config:        cfg,
		Addresses:     addresses,
		ExpectedTools: append([]string{}, expectedToolNames...),
	}
	if state == StateRunning && !startedAt.IsZero() {
		pid := os.Getpid()
		started := startedAt.Format(time.RFC3339)
		diagnostics.PID = &pid
		diagnostics.StartedAt = &started
		diagnostics.UptimeMs = time.Since(startedAt).Milliseconds()
	}
	return diagnostics
}

func (b *Bridge) On(event Event, listener func(any)) func() {
	if listener == nil {
		return func() {}
	}
	b.eventMu.Lock()
	b.nextListener++
	id := b.nextListener
	b.listeners[event] = append(b.listeners[event], listenerEntry{id: id, fn: listener})
	b.eventMu.Unlock()
	return func() {
		b.eventMu.Lock()
		defer b.eventMu.Unlock()
		entries := b.listeners[event]
		for index, entry := range entries {
			if entry.id == id {
				b.listeners[event] = append(entries[:index], entries[index+1:]...)
				break
			}
		}
	}
}

func (b *Bridge) emit(event Event, payload any) {
	b.eventMu.Lock()
	entries := append([]listenerEntry(nil), b.listeners[event]...)
	b.eventMu.Unlock()
	for _, entry := range entries {
		func() {
			defer func() {
				_ = recover()
			}()
			entry.fn(payload)
		}()
	}
}

func (b *Bridge) log(level LogLevel, message string, fields map[string]any) {
	if b.logger == nil {
		return
	}
	b.logger(level, message, fields)
}

func (b *Bridge) failStart(readyCh chan struct{}, cause *BridgeError, begin time.Time) {
	b.mu.Lock()
	b.state = StateFailed
	b.res = nil
	b.addresses = nil
	b.mcpURL = nil
	b.filesURL = nil
	b.startedAt = time.Time{}
	b.readyErr = cause
	b.readyCh = readyCh
	b.mu.Unlock()
	if readyCh != nil {
		close(readyCh)
	}
	b.log(LevelError, "bridge start failed", map[string]any{
		"component":  "bridge",
		"state":      string(StateFailed),
		"code":       cause.Code,
		"durationMs": time.Since(begin).Milliseconds(),
	})
	b.emit(EventError, &ErrorEvent{Code: cause.Code, Message: cause.Message})
}

func normalizeConfig(cfg Config, secrets Secrets) (Config, *BridgeError) {
	if cfg.Version != 1 {
		return Config{}, newBridgeError(CodeInvalidConfig, "config.version must be 1")
	}
	if strings.TrimSpace(cfg.Root) == "" || !filepath.IsAbs(cfg.Root) {
		return Config{}, newBridgeError(CodeInvalidConfig, "config.root must be an absolute path")
	}
	cfg.Root = filepath.Clean(cfg.Root)
	if cfg.DataDir == "" {
		cfg.DataDir = filepath.Join(cfg.Root, "data")
	} else if !filepath.IsAbs(cfg.DataDir) {
		return Config{}, newBridgeError(CodeInvalidConfig, "config.dataDir must be an absolute path")
	}
	cfg.DataDir = filepath.Clean(cfg.DataDir)
	if cfg.Workspace == "" {
		cfg.Workspace = filepath.Join(cfg.DataDir, "workspace")
	} else if !filepath.IsAbs(cfg.Workspace) {
		return Config{}, newBridgeError(CodeInvalidConfig, "config.workspace must be an absolute path")
	}
	cfg.Workspace = filepath.Clean(cfg.Workspace)
	if cfg.MCP.Host == "" {
		cfg.MCP.Host = "127.0.0.1"
	}
	if cfg.MCP.Host != "127.0.0.1" {
		return Config{}, newBridgeError(CodeInvalidConfig, "config.mcp.host must be 127.0.0.1")
	}
	if cfg.MCP.Path == "" {
		cfg.MCP.Path = "/mcp"
	}
	if cfg.MCP.Path != "/mcp" {
		return Config{}, newBridgeError(CodeInvalidConfig, "config.mcp.path must be /mcp")
	}
	if cfg.MCP.Port < 0 || cfg.MCP.Port > 65535 {
		return Config{}, newBridgeError(CodeInvalidConfig, "config.mcp.port must be between 0 and 65535")
	}
	if cfg.Files.Host == "" {
		cfg.Files.Host = "127.0.0.1"
	}
	if cfg.Files.Host != "127.0.0.1" {
		return Config{}, newBridgeError(CodeInvalidConfig, "config.files.host must be 127.0.0.1")
	}
	if cfg.Files.Path == "" {
		cfg.Files.Path = "/files"
	}
	if cfg.Files.Path != "/files" {
		return Config{}, newBridgeError(CodeInvalidConfig, "config.files.path must be /files")
	}
	if cfg.Files.Port < 0 || cfg.Files.Port > 65535 {
		return Config{}, newBridgeError(CodeInvalidConfig, "config.files.port must be between 0 and 65535")
	}
	if cfg.Files.Port != 0 && cfg.MCP.Port != 0 && cfg.Files.Port == cfg.MCP.Port {
		return Config{}, newBridgeError(CodeInvalidConfig, "config.files.port must differ from config.mcp.port")
	}
	if cfg.Files.MaxFileBytes <= 0 {
		return Config{}, newBridgeError(CodeInvalidConfig, "config.files.maxFileBytes must be positive")
	}
	if cfg.Files.StagingQuotaBytes < cfg.Files.MaxFileBytes {
		return Config{}, newBridgeError(CodeInvalidConfig, "config.files.stagingQuotaBytes must be at least maxFileBytes")
	}
	cfg.Screenshot.Backend = strings.ToLower(strings.TrimSpace(cfg.Screenshot.Backend))
	if cfg.Screenshot.Backend == "" {
		cfg.Screenshot.Backend = "auto"
	}
	switch cfg.Screenshot.Backend {
	case "auto", "ffmpeg", "powershell":
	default:
		return Config{}, newBridgeError(CodeInvalidConfig, "config.screenshot.backend must be auto, ffmpeg, or powershell")
	}
	cfg.Shell.Default = strings.TrimSpace(cfg.Shell.Default)
	if secrets.MCPToken == "" {
		return Config{}, newBridgeError(CodeInvalidSecret, "mcpToken is required")
	}
	if cfg.Files.Enabled {
		if secrets.FilesToken == "" {
			return Config{}, newBridgeError(CodeInvalidSecret, "filesToken is required when files are enabled")
		}
		if secrets.FilesToken == secrets.MCPToken {
			return Config{}, newBridgeError(CodeInvalidSecret, "filesToken must differ from mcpToken")
		}
	}
	return cfg, nil
}

func dialable(ctx context.Context, address Address) bool {
	if address.Port <= 0 {
		return false
	}
	timeout := 500 * time.Millisecond
	if deadline, ok := ctx.Deadline(); ok {
		remaining := time.Until(deadline)
		if remaining <= 0 {
			return false
		}
		if remaining < timeout {
			timeout = remaining
		}
	}
	conn, err := (&net.Dialer{Timeout: timeout}).DialContext(ctx, "tcp", net.JoinHostPort(address.Host, strconv.Itoa(address.Port)))
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}

func envSlice(env map[string]string) []string {
	keys := make([]string, 0, len(env))
	for key := range env {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	result := make([]string, 0, len(keys))
	for _, key := range keys {
		result = append(result, key+"="+env[key])
	}
	return result
}

func cloneAddresses(addresses *Addresses) *Addresses {
	if addresses == nil {
		return nil
	}
	clone := &Addresses{MCP: addresses.MCP}
	if addresses.Files != nil {
		files := *addresses.Files
		clone.Files = &files
	}
	return clone
}

func cloneString(value *string) *string {
	if value == nil {
		return nil
	}
	clone := *value
	return &clone
}

func workspaceCovers(workspace, dataDir string) bool {
	resolvedWorkspace := workspace
	if resolved, err := filepath.EvalSymlinks(workspace); err == nil {
		resolvedWorkspace = resolved
	}
	resolvedData := dataDir
	if resolved, err := filepath.EvalSymlinks(dataDir); err == nil {
		resolvedData = resolved
	}
	rel, err := filepath.Rel(resolvedWorkspace, resolvedData)
	if err != nil {
		return false
	}
	if rel == "." {
		return true
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}
