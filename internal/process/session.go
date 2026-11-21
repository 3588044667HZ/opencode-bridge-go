package process

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"go-bridge/internal/envutil"
)

const (
	StateRunning    = "running"
	StateFinished   = "finished"
	StateTerminated = "terminated"
)

type Manager struct {
	cfg          Config
	shells       ShellSet
	defaultShell Shell
	mu           sync.Mutex
	sessions     map[int]*session
	closed       bool
}

func NewManager(cfg Config) (*Manager, error) {
	if cfg.MaxBufferLines <= 0 {
		cfg.MaxBufferLines = 50000
	}
	if cfg.DefaultReadLines <= 0 {
		cfg.DefaultReadLines = 1000
	}
	if cfg.MaxReadLines <= 0 {
		cfg.MaxReadLines = 10000
	}
	if cfg.InitialQuiet <= 0 {
		cfg.InitialQuiet = 150 * time.Millisecond
	}
	if cfg.SessionTTL <= 0 {
		cfg.SessionTTL = 10 * time.Minute
	}
	if strings.TrimSpace(cfg.Workspace) == "" {
		return nil, NewError("invalid_config", "BRIDGE_WORKSPACE is required")
	}
	workspace, err := filepath.Abs(cfg.Workspace)
	if err != nil {
		return nil, NewError("invalid_config", "invalid BRIDGE_WORKSPACE: %v", err)
	}
	if err := os.MkdirAll(workspace, 0o755); err != nil {
		return nil, NewError("invalid_config", "cannot create BRIDGE_WORKSPACE: %v", err)
	}
	cfg.Workspace = workspace
	var shells ShellSet
	if cfg.Env == nil {
		shells, err = DetectShells()
	} else {
		shells, err = DetectShellsWithEnv(cfg.Env)
	}
	if err != nil {
		return nil, err
	}
	shell, err := ResolveShell(cfg.ShellAlias, shells)
	if err != nil {
		return nil, err
	}
	return &Manager{
		cfg:          cfg,
		shells:       shells,
		defaultShell: shell,
		sessions:     map[int]*session{},
	}, nil
}

func (m *Manager) Start(opts StartOptions) (*StartResult, error) {
	if strings.TrimSpace(opts.Command) == "" {
		return nil, NewError("invalid_arguments", "command must be a non-empty string")
	}
	if strings.ContainsRune(opts.Command, 0) {
		return nil, NewError("invalid_arguments", "command must not contain NUL")
	}
	m.mu.Lock()
	closed := m.closed
	m.mu.Unlock()
	if closed {
		return nil, NewError("process_backend_unavailable", "process manager is closed")
	}
	if opts.Timeout <= 0 {
		opts.Timeout = 30 * time.Second
	}
	workdir, err := m.resolveWorkdir(opts.Workdir)
	if err != nil {
		return nil, err
	}
	shell, err := m.resolveShell(opts.ShellAlias)
	if err != nil {
		return nil, err
	}

	s := &session{
		state:   StateRunning,
		started: time.Now(),
		notify:  make(chan struct{}),
		done:    make(chan struct{}),
		buffer:  newOutputBuffer(m.cfg.MaxBufferLines),
		shell:   shell,
		workdir: workdir,
	}
	command, err := commandWithWorkdir(shell, workdir, opts.Command)
	if err != nil {
		return nil, err
	}
	cmd := exec.Command(shell.Path, ShellArgs(shell, command)...)
	cmd.Dir = workdir
	env := m.cfg.Env
	if env == nil {
		env = os.Environ()
	}
	cmd.Env = envutil.Clean(env)
	prepareCommand(cmd)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, NewError("process_backend_unavailable", "cannot create stdin pipe: %v", err)
	}
	s.cmd = cmd
	s.stdin = stdin
	cmd.Stdout = sessionWriter{s: s}
	cmd.Stderr = sessionWriter{s: s}
	if err := cmd.Start(); err != nil {
		_ = stdin.Close()
		return nil, NewError("process_backend_unavailable", "cannot start shell: %v", err)
	}
	s.pid = cmd.Process.Pid
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		_ = stdin.Close()
		killTree(s.pid)
		return nil, NewError("process_backend_unavailable", "process manager is closed")
	}
	m.sessions[s.pid] = s
	m.mu.Unlock()
	go s.wait()

	s.waitInitial(m.cfg.InitialQuiet, opts.Timeout)
	s.mu.Lock()
	output, _, _, _ := s.buffer.Read(0, m.cfg.MaxReadLines, &s.readCursor)
	state := s.state
	exitCode := s.exitCode
	pid := s.pid
	s.mu.Unlock()
	return &StartResult{
		PID:      pid,
		State:    state,
		ExitCode: exitCode,
		Shell:    shell,
		Workdir:  workdir,
		Output:   output,
	}, nil
}

type session struct {
	mu         sync.Mutex
	pid        int
	cmd        *exec.Cmd
	stdin      io.WriteCloser
	shell      Shell
	workdir    string
	started    time.Time
	state      string
	exitCode   *int
	buffer     *outputBuffer
	readCursor int
	notify     chan struct{}
	done       chan struct{}
	outputSeen bool
	lastOutput time.Time
}

type sessionWriter struct {
	s *session
}

func (w sessionWriter) Write(p []byte) (int, error) {
	s := w.s
	s.mu.Lock()
	s.buffer.Append(p)
	if len(p) > 0 {
		s.outputSeen = true
		s.lastOutput = time.Now()
		s.signalLocked()
	}
	s.mu.Unlock()
	return len(p), nil
}

func (s *session) signalLocked() {
	if s.notify != nil {
		close(s.notify)
	}
	s.notify = make(chan struct{})
}

func (s *session) wait() {
	err := s.cmd.Wait()
	code := 0
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			code = exitErr.ExitCode()
		} else {
			code = -1
		}
	}
	s.mu.Lock()
	if s.state == StateRunning {
		s.state = StateFinished
	}
	s.exitCode = &code
	s.buffer.Flush()
	s.signalLocked()
	close(s.done)
	s.mu.Unlock()
}

func (s *session) waitInitial(quiet, timeout time.Duration) {
	deadline := time.Now().Add(timeout)
	for {
		s.mu.Lock()
		state := s.state
		seen := s.outputSeen
		last := s.lastOutput
		ch := s.notify
		s.mu.Unlock()
		if state != StateRunning {
			return
		}
		if seen && time.Since(last) >= quiet {
			return
		}
		remaining := time.Until(deadline)
		if remaining <= 0 {
			return
		}
		wait := 50 * time.Millisecond
		if remaining < wait {
			wait = remaining
		}
		timer := time.NewTimer(wait)
		select {
		case <-ch:
			if !timer.Stop() {
				<-timer.C
			}
		case <-timer.C:
		}
	}
}

func (s *session) waitForOutput(timeout time.Duration) bool {
	if timeout <= 0 {
		return false
	}
	deadline := time.Now().Add(timeout)
	for {
		s.mu.Lock()
		state := s.state
		available := s.buffer.NewAvailable(s.readCursor)
		ch := s.notify
		s.mu.Unlock()
		if state != StateRunning || available {
			return true
		}
		remaining := time.Until(deadline)
		if remaining <= 0 {
			return false
		}
		wait := 50 * time.Millisecond
		if remaining < wait {
			wait = remaining
		}
		timer := time.NewTimer(wait)
		select {
		case <-ch:
			if !timer.Stop() {
				<-timer.C
			}
		case <-timer.C:
		}
	}
}

func (m *Manager) Read(pid int, opts ReadOptions) (*ReadResult, error) {
	s, err := m.find(pid)
	if err != nil {
		return nil, err
	}
	if opts.Length <= 0 {
		opts.Length = m.cfg.DefaultReadLines
	}
	if opts.Length > m.cfg.MaxReadLines {
		opts.Length = m.cfg.MaxReadLines
	}
	if opts.Timeout <= 0 {
		opts.Timeout = 5 * time.Second
	}
	s.waitForOutput(opts.Timeout)
	s.mu.Lock()
	output, total, returned, truncated := s.buffer.Read(opts.Offset, opts.Length, &s.readCursor)
	state := s.state
	exitCode := s.exitCode
	s.mu.Unlock()
	return &ReadResult{
		PID:           pid,
		State:         state,
		ExitCode:      exitCode,
		Output:        output,
		TotalLines:    total,
		ReturnedLines: returned,
		Truncated:     truncated,
	}, nil
}

func (m *Manager) Interact(pid int, opts InteractOptions) (*InteractResult, error) {
	if strings.ContainsRune(opts.Input, 0) {
		return nil, NewError("invalid_arguments", "input must not contain NUL")
	}
	s, err := m.find(pid)
	if err != nil {
		return nil, err
	}
	if opts.Timeout <= 0 {
		opts.Timeout = 8 * time.Second
	}
	s.mu.Lock()
	state := s.state
	stdin := s.stdin
	s.mu.Unlock()
	if state != StateRunning {
		return nil, NewError("process_not_running", "process %d is not running", pid)
	}
	s.mu.Lock()
	s.buffer.Discard(&s.readCursor)
	s.mu.Unlock()
	if opts.Input != "" {
		if _, err := io.WriteString(stdin, opts.Input); err != nil {
			return nil, NewError("io_error", "cannot write to process %d: %v", pid, err)
		}
	}
	if s.waitForOutput(opts.Timeout) {
		s.waitForQuiet(120*time.Millisecond, opts.Timeout)
	}
	s.mu.Lock()
	output, _, _, _ := s.buffer.Read(0, m.cfg.MaxReadLines, &s.readCursor)
	state = s.state
	exitCode := s.exitCode
	s.mu.Unlock()
	return &InteractResult{PID: pid, State: state, ExitCode: exitCode, Output: output}, nil
}

func (m *Manager) Terminate(pid int) error {
	s, err := m.find(pid)
	if err != nil {
		return err
	}
	s.mu.Lock()
	state := s.state
	stdin := s.stdin
	s.mu.Unlock()
	if state == StateRunning {
		killTree(s.cmd.Process.Pid)
	}
	if stdin != nil {
		_ = stdin.Close()
	}
	select {
	case <-s.done:
	case <-time.After(5 * time.Second):
	}
	s.mu.Lock()
	if s.state == StateRunning {
		s.state = StateTerminated
		s.signalLocked()
	}
	s.mu.Unlock()
	m.mu.Lock()
	delete(m.sessions, pid)
	m.mu.Unlock()
	return nil
}

func (m *Manager) Close() error {
	m.mu.Lock()
	m.closed = true
	pids := make([]int, 0, len(m.sessions))
	for pid := range m.sessions {
		pids = append(pids, pid)
	}
	m.mu.Unlock()
	var wg sync.WaitGroup
	for _, pid := range pids {
		wg.Add(1)
		go func(pid int) {
			defer wg.Done()
			_ = m.Terminate(pid)
		}(pid)
	}
	wg.Wait()
	return nil
}

func (m *Manager) ShellSummary() (string, []string, string) {
	available := make([]string, 0, len(m.shells.Order))
	seen := make(map[string]bool, len(m.shells.Order))
	for _, alias := range m.shells.Order {
		if _, ok := m.shells.Available[alias]; ok {
			available = append(available, alias)
			seen[alias] = true
		}
	}
	if !seen[m.defaultShell.Alias] {
		available = append(available, m.defaultShell.Alias)
	}
	return m.defaultShell.Alias, available, m.shells.PathStyle
}

func (m *Manager) List() []SessionInfo {
	m.mu.Lock()
	defer m.mu.Unlock()
	now := time.Now()
	result := make([]SessionInfo, 0, len(m.sessions))
	for pid, s := range m.sessions {
		s.mu.Lock()
		if s.state == StateRunning {
			result = append(result, SessionInfo{
				PID:       pid,
				Shell:     s.shell.Alias,
				Workdir:   s.workdir,
				State:     s.state,
				StartedAt: s.started,
				ExitCode:  s.exitCode,
			})
		} else if now.Sub(s.started) > m.cfg.SessionTTL {
			delete(m.sessions, pid)
		}
		s.mu.Unlock()
	}
	sort.Slice(result, func(i, j int) bool { return result[i].PID < result[j].PID })
	return result
}

func (m *Manager) find(pid int) (*session, error) {
	m.mu.Lock()
	s := m.sessions[pid]
	m.mu.Unlock()
	if s == nil {
		return nil, NewError("session_not_found", "no session with pid %d", pid)
	}
	return s, nil
}

func (m *Manager) resolveWorkdir(workdir string) (string, error) {
	if strings.ContainsRune(workdir, 0) {
		return "", NewError("invalid_arguments", "workdir must not contain NUL")
	}
	base := m.cfg.Workspace
	if strings.TrimSpace(workdir) == "" {
		return base, nil
	}
	target := workdir
	if !filepath.IsAbs(target) {
		target = filepath.Join(base, target)
	}
	target, err := filepath.Abs(target)
	if err != nil {
		return "", NewError("workdir_not_found", "cannot resolve workdir %q: %v", workdir, err)
	}
	info, err := os.Stat(target)
	if err != nil {
		if os.IsNotExist(err) {
			return "", NewError("workdir_not_found", "workdir does not exist: %s", target)
		}
		return "", NewError("workdir_not_found", "cannot stat workdir %s: %v", target, err)
	}
	if !info.IsDir() {
		return "", NewError("workdir_not_directory", "workdir is not a directory: %s", target)
	}
	return target, nil
}

func (m *Manager) resolveShell(alias string) (Shell, error) {
	if strings.TrimSpace(alias) == "" {
		return m.defaultShell, nil
	}
	return ResolveShell(alias, m.shells)
}

func stateText(state string, exitCode *int) string {
	if state == StateFinished {
		if exitCode != nil {
			return fmt.Sprintf("Process has finished execution (exit code %d).", *exitCode)
		}
		return "Process has finished execution."
	}
	if state == StateTerminated {
		return "Process was terminated."
	}
	return "Process is running."
}

func (s *session) waitForQuiet(quiet, timeout time.Duration) {
	if quiet <= 0 {
		return
	}
	deadline := time.Now().Add(timeout)
	for {
		s.mu.Lock()
		state := s.state
		last := s.lastOutput
		ch := s.notify
		s.mu.Unlock()
		if state != StateRunning {
			return
		}
		if !last.IsZero() && time.Since(last) >= quiet {
			return
		}
		remaining := time.Until(deadline)
		if remaining <= 0 {
			return
		}
		wait := 50 * time.Millisecond
		if remaining < wait {
			wait = remaining
		}
		timer := time.NewTimer(wait)
		select {
		case <-ch:
			if !timer.Stop() {
				<-timer.C
			}
		case <-timer.C:
		}
	}
}
