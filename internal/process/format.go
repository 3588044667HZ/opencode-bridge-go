package process

import (
	"fmt"
	"strings"
)

func (r *StartResult) Text() string {
	var b strings.Builder
	fmt.Fprintf(&b, "Process started with PID %d (shell: %s)\n", r.PID, r.Shell.Path)
	if r.Output != "" {
		b.WriteString("Initial output:\n")
		b.WriteString(r.Output)
		b.WriteString("\n\n")
	} else {
		b.WriteString("\n")
	}
	b.WriteString(processStateLine(r.PID, r.State, r.ExitCode))
	return b.String()
}

func (r *ReadResult) Text() string {
	var b strings.Builder
	if r.Output != "" {
		b.WriteString(r.Output)
		b.WriteString("\n\n")
	} else {
		fmt.Fprintf(&b, "No new output for process %d.\n", r.PID)
	}
	b.WriteString(processStateLine(r.PID, r.State, r.ExitCode))
	return b.String()
}

func (r *InteractResult) Text() string {
	var b strings.Builder
	if r.Output != "" {
		b.WriteString(r.Output)
		b.WriteString("\n\n")
	} else {
		fmt.Fprintf(&b, "No new output for process %d.\n", r.PID)
	}
	b.WriteString(processStateLine(r.PID, r.State, r.ExitCode))
	return b.String()
}

func FormatSessions(sessions []SessionInfo) string {
	if len(sessions) == 0 {
		return "No active sessions"
	}
	var b strings.Builder
	b.WriteString("Active sessions:\n")
	for _, session := range sessions {
		fmt.Fprintf(&b, "PID %d | %s | %s | %s", session.PID, session.Shell, session.Workdir, session.State)
		if session.ExitCode != nil {
			fmt.Fprintf(&b, " | exit code %d", *session.ExitCode)
		}
		b.WriteString("\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

func processStateLine(pid int, state string, exitCode *int) string {
	switch state {
	case StateFinished:
		if exitCode != nil {
			return fmt.Sprintf("Process %d has finished execution (exit code %d).", pid, *exitCode)
		}
		return fmt.Sprintf("Process %d has finished execution.", pid)
	case StateTerminated:
		return fmt.Sprintf("Process %d was terminated.", pid)
	default:
		return fmt.Sprintf("Process %d is running. Use read_process_output to get more output.", pid)
	}
}
