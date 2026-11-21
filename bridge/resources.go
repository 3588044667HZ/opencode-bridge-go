package bridge

import (
	"context"
	"crypto/subtle"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"go-bridge/internal/clipboard"
	"go-bridge/internal/files"
	"go-bridge/internal/fileserver"
	"go-bridge/internal/filesmcp"
	"go-bridge/internal/process"
	"go-bridge/internal/processmcp"
	"go-bridge/internal/screenshot"
	"go-bridge/internal/sysinfo"
	"go-bridge/internal/systemmcp"
)

type resources struct {
	manager     *process.Manager
	fileService *files.Service
	fileServer  *fileserver.Server
	mcpListener net.Listener
	httpServer  *http.Server
	clipboard   *clipboard.Service
	screenshot  *screenshot.Service
	system      *sysinfo.System
	shell       ShellSummary
	cleanupStop context.CancelFunc
	cleanupDone chan struct{}
	failedMu    sync.Mutex
	fileFailed  bool
}

func (r *resources) markFileServerFailed() {
	if r == nil {
		return
	}
	r.failedMu.Lock()
	r.fileFailed = true
	r.failedMu.Unlock()
}

func (r *resources) filesAvailable() bool {
	if r == nil {
		return false
	}
	r.failedMu.Lock()
	defer r.failedMu.Unlock()
	return !r.fileFailed
}

func (b *Bridge) startResources(ctx context.Context, cfg Config, secrets Secrets, env []string) (*resources, *Addresses, *string, *string, *BridgeError) {
	res := &resources{}
	cleanup := func(cause *BridgeError) (*resources, *Addresses, *string, *string, *BridgeError) {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		_ = b.stopResources(res, cleanupCtx)
		cancel()
		return nil, nil, nil, nil, cause
	}
	directories := []string{
		cfg.DataDir,
		cfg.Workspace,
		filepath.Join(cfg.DataDir, "staging", "inbox"),
		filepath.Join(cfg.DataDir, "staging", "outbox"),
		filepath.Join(cfg.DataDir, "screenshots"),
	}
	for _, dir := range directories {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return cleanup(newBridgeError(CodeStartFailed, "cannot create directory %s: %v", dir, err))
		}
	}
	if workspaceCovers(cfg.Workspace, cfg.DataDir) {
		b.log(LevelWarn, "workspace covers data directory", map[string]any{"component": "bridge", "path": cfg.Workspace})
	}
	processCfg := process.DefaultConfig()
	processCfg.Workspace = cfg.Workspace
	processCfg.ShellAlias = cfg.Shell.Default
	processCfg.Env = env
	manager, err := process.NewManager(processCfg)
	if err != nil {
		var processErr *process.Error
		if errors.As(err, &processErr) && processErr.Code == "invalid_shell" {
			return cleanup(newBridgeError(CodeInvalidConfig, "%s", processErr.Message))
		}
		return cleanup(newBridgeError(CodeStartFailed, "%v", err))
	}
	res.manager = manager
	defaultShell, availableShells, pathStyle := manager.ShellSummary()
	res.shell = ShellSummary{
		Default:   defaultShell,
		Available: append([]string{}, availableShells...),
		PathStyle: pathStyle,
	}
	fileService, err := files.New(files.Config{
		WorkspaceRoot:     cfg.Workspace,
		StagingInbox:      filepath.Join(cfg.DataDir, "staging", "inbox"),
		StagingOutbox:     filepath.Join(cfg.DataDir, "staging", "outbox"),
		MaxFileBytes:      cfg.Files.MaxFileBytes,
		StagingQuotaBytes: cfg.Files.StagingQuotaBytes,
	})
	if err != nil {
		return cleanup(newBridgeError(CodeStartFailed, "%v", err))
	}
	res.fileService = fileService
	screenshotService, err := screenshot.NewWithEnv(filepath.Join(cfg.DataDir, "screenshots"), cfg.Screenshot.Backend, env)
	if err != nil {
		return cleanup(newBridgeError(CodeStartFailed, "%v", err))
	}
	res.screenshot = screenshotService
	clipboardService := clipboard.NewWithEnv(env)
	res.clipboard = clipboardService

	var filesAddress *Address
	var filesURL *string
	filesPort := cfg.Files.Port
	if cfg.Files.Enabled {
		fileServer, err := fileserver.New(net.JoinHostPort(cfg.Files.Host, strconv.Itoa(cfg.Files.Port)), secrets.FilesToken, fileService)
		if err != nil {
			return cleanup(newBridgeError(CodeStartFailed, "%v", err))
		}
		res.fileServer = fileServer
		if err := fileServer.Listen(); err != nil {
			return cleanup(listenBridgeError("file service", err))
		}
		if tcp, ok := fileServer.Addr().(*net.TCPAddr); ok {
			filesPort = tcp.Port
		}
		filesAddress = &Address{Host: cfg.Files.Host, Port: filesPort, Path: cfg.Files.Path}
		url := fmt.Sprintf("http://%s:%d%s", cfg.Files.Host, filesPort, cfg.Files.Path)
		filesURL = &url
		go b.serveFileServer(res, fileServer)
	}

	systemService := sysinfo.New(sysinfo.Config{
		Workspace:      cfg.Workspace,
		DataDir:        cfg.DataDir,
		StagingInbox:   filepath.Join(cfg.DataDir, "staging", "inbox"),
		StagingOutbox:  filepath.Join(cfg.DataDir, "staging", "outbox"),
		FilesEnabled:   cfg.Files.Enabled,
		FilesHost:      cfg.Files.Host,
		FilesPort:      filesPort,
		FilesPath:      cfg.Files.Path,
		ShellDefault:   res.shell.Default,
		ShellAvailable: res.shell.Available,
		ShellPathStyle: res.shell.PathStyle,
	}, screenshotService.Capability(), clipboardService.Capability())
	res.system = systemService

	mcpServer := mcp.NewServer(&mcp.Implementation{Name: "go-bridge", Version: "0.1.0"}, nil)
	processmcp.Register(mcpServer, manager)
	filesmcp.Register(mcpServer, fileService)
	systemmcp.Register(mcpServer, systemService, clipboardService, screenshotService)

	mcpListener, err := net.Listen("tcp", net.JoinHostPort(cfg.MCP.Host, strconv.Itoa(cfg.MCP.Port)))
	if err != nil {
		return cleanup(listenBridgeError("mcp", err))
	}
	res.mcpListener = mcpListener
	mcpPort := cfg.MCP.Port
	if tcp, ok := mcpListener.Addr().(*net.TCPAddr); ok {
		mcpPort = tcp.Port
	}
	mcpAddress := Address{Host: cfg.MCP.Host, Port: mcpPort, Path: cfg.MCP.Path}
	mcpURL := fmt.Sprintf("http://%s:%d%s", cfg.MCP.Host, mcpPort, cfg.MCP.Path)
	streamable := mcp.NewStreamableHTTPHandler(
		func(*http.Request) *mcp.Server { return mcpServer },
		&mcp.StreamableHTTPOptions{SessionTimeout: 30 * time.Minute},
	)
	mux := http.NewServeMux()
	mux.Handle(cfg.MCP.Path, bearerAuth(streamable, secrets.MCPToken))
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = w.Write([]byte("ok"))
	})
	httpServer := &http.Server{
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
	}
	res.httpServer = httpServer
	go b.serveMCP(httpServer, mcpListener)

	cleanupCtx, cancel := context.WithCancel(context.Background())
	res.cleanupStop = cancel
	res.cleanupDone = make(chan struct{})
	go b.runCleanup(cleanupCtx, res)

	addresses := &Addresses{MCP: mcpAddress, Files: filesAddress}
	return res, addresses, &mcpURL, filesURL, nil
}

func (b *Bridge) stopResources(res *resources, ctx context.Context) error {
	if res == nil {
		return nil
	}
	if res.cleanupStop != nil {
		res.cleanupStop()
	}
	if res.cleanupDone != nil {
		select {
		case <-res.cleanupDone:
		case <-ctx.Done():
		}
	}
	var firstErr error
	if res.fileServer != nil {
		if err := res.fileServer.Shutdown(ctx); err != nil && !errors.Is(err, http.ErrServerClosed) {
			firstErr = err
		}
		_ = res.fileServer.Close()
	}
	if res.httpServer != nil {
		if err := res.httpServer.Shutdown(ctx); err != nil && !errors.Is(err, http.ErrServerClosed) && firstErr == nil {
			firstErr = err
		}
	}
	if res.mcpListener != nil {
		_ = res.mcpListener.Close()
	}
	if res.manager != nil {
		_ = res.manager.Close()
	}
	return firstErr
}

func (b *Bridge) serveMCP(server *http.Server, listener net.Listener) {
	if err := server.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
		b.log(LevelError, "mcp server stopped", map[string]any{
			"component": "gateway",
			"member":    "process",
			"code":      CodeMemberFailed,
		})
		b.emit(EventError, &ErrorEvent{Code: CodeMemberFailed, Message: "mcp server stopped"})
	}
}

func (b *Bridge) serveFileServer(res *resources, server *fileserver.Server) {
	if err := server.Serve(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		res.markFileServerFailed()
		b.log(LevelError, "file service stopped", map[string]any{
			"component": "files",
			"code":      CodeMemberFailed,
		})
		b.emit(EventFileServiceError, &FileServiceErrorEvent{Code: "file_service_failed", Message: "file service stopped"})
	}
}

func (b *Bridge) runCleanup(ctx context.Context, res *resources) {
	defer close(res.cleanupDone)
	b.cleanupOnce(res)
	ticker := time.NewTicker(time.Hour)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			b.cleanupOnce(res)
		}
	}
}

func (b *Bridge) cleanupOnce(res *resources) {
	if res.fileService != nil {
		if stats, err := res.fileService.Cleanup(time.Now()); err != nil {
			b.log(LevelWarn, "staging cleanup failed", map[string]any{"component": "files", "code": "io_error"})
		} else if stats.Files > 0 || stats.PartFiles > 0 {
			b.log(LevelInfo, fmt.Sprintf(
				"staging cleanup: files=%d bytes=%d parts=%d partBytes=%d",
				stats.Files,
				stats.Bytes,
				stats.PartFiles,
				stats.PartBytes,
			), map[string]any{"component": "files"})
		}
	}
	if res.screenshot != nil {
		if stats, err := res.screenshot.Cleanup(time.Now()); err != nil {
			b.log(LevelWarn, "screenshot cleanup failed", map[string]any{"component": "screenshot", "code": "io_error"})
		} else if stats.Files > 0 {
			b.log(LevelInfo, fmt.Sprintf(
				"screenshot cleanup: files=%d bytes=%d",
				stats.Files,
				stats.Bytes,
			), map[string]any{"component": "screenshot"})
		}
	}
}

func listenBridgeError(component string, err error) *BridgeError {
	if isAddrInUse(err) {
		return newBridgeError(CodePortInUse, "%s port is already in use", component)
	}
	return newBridgeError(CodeStartFailed, "%s cannot listen: %v", component, err)
}

func bearerAuth(next http.Handler, token string) http.Handler {
	expected := []byte("Bearer " + token)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		actual := []byte(r.Header.Get("Authorization"))
		if len(actual) != len(expected) || subtle.ConstantTimeCompare(actual, expected) != 1 {
			w.Header().Set("WWW-Authenticate", `Bearer realm="go-bridge"`)
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}
