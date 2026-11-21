package processmcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"go-bridge/internal/process"
)

type startArgs struct {
	Command       string   `json:"command"`
	Workdir       string   `json:"workdir"`
	Timeout       *float64 `json:"timeout"`
	TimeoutMS     *float64 `json:"timeout_ms"`
	Shell         string   `json:"shell"`
	VerboseTiming *bool    `json:"verbose_timing"`
}

type readArgs struct {
	PID           float64  `json:"pid"`
	TimeoutMS     *float64 `json:"timeout_ms"`
	Offset        *float64 `json:"offset"`
	Length        *float64 `json:"length"`
	VerboseTiming *bool    `json:"verbose_timing"`
}

type interactArgs struct {
	PID           float64  `json:"pid"`
	Input         string   `json:"input"`
	TimeoutMS     *float64 `json:"timeout_ms"`
	WaitForPrompt *bool    `json:"wait_for_prompt"`
	VerboseTiming *bool    `json:"verbose_timing"`
}

type forceArgs struct {
	PID float64 `json:"pid"`
}

func Register(server *mcp.Server, manager *process.Manager) {
	server.AddTool(startProcessTool, func(_ context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		var args startArgs
		if err := json.Unmarshal(req.Params.Arguments, &args); err != nil {
			return errorResult("start_process", process.NewError("invalid_arguments", "%v", err)), nil
		}
		if args.Command == "" {
			return errorResult("start_process", process.NewError("invalid_arguments", "command must be a non-empty string")), nil
		}
		if args.Timeout != nil && args.TimeoutMS != nil {
			return errorResult("start_process", process.NewError("conflicting_timeout", "timeout and timeout_ms are mutually exclusive")), nil
		}
		timeoutMS := 30000.0
		if args.Timeout != nil {
			timeoutMS = *args.Timeout
		} else if args.TimeoutMS != nil {
			timeoutMS = *args.TimeoutMS
		}
		if !isInteger(timeoutMS) || timeoutMS < 1 || timeoutMS > 3600000 {
			return errorResult("start_process", process.NewError("invalid_timeout", "timeout must be an integer between 1 and 3600000")), nil
		}
		result, err := manager.Start(process.StartOptions{
			Command:    args.Command,
			Workdir:    args.Workdir,
			Timeout:    time.Duration(timeoutMS) * time.Millisecond,
			ShellAlias: args.Shell,
		})
		if err != nil {
			return errorResult("start_process", err), nil
		}
		return successResult(result.Text(), map[string]any{
			"pid":      result.PID,
			"state":    result.State,
			"exitCode": result.ExitCode,
			"shell":    result.Shell.Alias,
			"workdir":  result.Workdir,
			"output":   result.Output,
		}), nil
	})

	server.AddTool(readProcessOutputTool, func(_ context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		var args readArgs
		if err := json.Unmarshal(req.Params.Arguments, &args); err != nil {
			return errorResult("read_process_output", process.NewError("invalid_arguments", "%v", err)), nil
		}
		pid, err := parsePID(args.PID)
		if err != nil {
			return errorResult("read_process_output", err), nil
		}
		timeout, err := parseTimeout(args.TimeoutMS, 5000)
		if err != nil {
			return errorResult("read_process_output", err), nil
		}
		offset, err := parseOptionalInt(args.Offset, 0, "offset")
		if err != nil {
			return errorResult("read_process_output", err), nil
		}
		length, err := parseOptionalInt(args.Length, 0, "length")
		if err != nil {
			return errorResult("read_process_output", err), nil
		}
		result, err := manager.Read(pid, process.ReadOptions{Offset: offset, Length: length, Timeout: timeout})
		if err != nil {
			return errorResult("read_process_output", err), nil
		}
		return successResult(result.Text(), map[string]any{
			"pid":           result.PID,
			"state":         result.State,
			"exitCode":      result.ExitCode,
			"output":        result.Output,
			"totalLines":    result.TotalLines,
			"returnedLines": result.ReturnedLines,
			"truncated":     result.Truncated,
		}), nil
	})

	server.AddTool(interactWithProcessTool, func(_ context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		var args interactArgs
		if err := json.Unmarshal(req.Params.Arguments, &args); err != nil {
			return errorResult("interact_with_process", process.NewError("invalid_arguments", "%v", err)), nil
		}
		pid, err := parsePID(args.PID)
		if err != nil {
			return errorResult("interact_with_process", err), nil
		}
		timeout, err := parseTimeout(args.TimeoutMS, 8000)
		if err != nil {
			return errorResult("interact_with_process", err), nil
		}
		result, err := manager.Interact(pid, process.InteractOptions{Input: args.Input, Timeout: timeout, WaitForPrompt: args.WaitForPrompt != nil && *args.WaitForPrompt})
		if err != nil {
			return errorResult("interact_with_process", err), nil
		}
		return successResult(result.Text(), map[string]any{
			"pid":      result.PID,
			"state":    result.State,
			"exitCode": result.ExitCode,
			"output":   result.Output,
		}), nil
	})

	server.AddTool(forceTerminateTool, func(_ context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		var args forceArgs
		if err := json.Unmarshal(req.Params.Arguments, &args); err != nil {
			return errorResult("force_terminate", process.NewError("invalid_arguments", "%v", err)), nil
		}
		pid, err := parsePID(args.PID)
		if err != nil {
			return errorResult("force_terminate", err), nil
		}
		if err := manager.Terminate(pid); err != nil {
			return errorResult("force_terminate", err), nil
		}
		return successResult(fmt.Sprintf("Successfully initiated termination of session %d.", pid), map[string]any{
			"pid":        pid,
			"terminated": true,
		}), nil
	})

	server.AddTool(listSessionsTool, func(_ context.Context, _ *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		sessions := manager.List()
		return successResult(process.FormatSessions(sessions), map[string]any{"sessions": sessions}), nil
	})
}

func successResult(text string, structured any) *mcp.CallToolResult {
	return &mcp.CallToolResult{
		Content:           []mcp.Content{&mcp.TextContent{Text: text}},
		StructuredContent: structured,
	}
}

func errorResult(tool string, err error) *mcp.CallToolResult {
	code := "io_error"
	message := err.Error()
	var processErr *process.Error
	if errors.As(err, &processErr) {
		code = processErr.Code
		message = processErr.Message
	}
	return &mcp.CallToolResult{
		IsError: true,
		Content: []mcp.Content{&mcp.TextContent{Text: fmt.Sprintf("%s failed: %s: %s", tool, code, message)}},
	}
}

func parsePID(value float64) (int, error) {
	if !isInteger(value) || value <= 0 || value > math.MaxInt32 {
		return 0, process.NewError("invalid_arguments", "pid must be a positive integer")
	}
	return int(value), nil
}

func parseTimeout(value *float64, defaultValue float64) (time.Duration, error) {
	if value == nil {
		value = &defaultValue
	}
	if !isInteger(*value) || *value < 0 || *value > 3600000 {
		return 0, process.NewError("invalid_arguments", "timeout_ms must be an integer between 0 and 3600000")
	}
	return time.Duration(*value) * time.Millisecond, nil
}

func parseOptionalInt(value *float64, defaultValue int, name string) (int, error) {
	if value == nil {
		return defaultValue, nil
	}
	if !isInteger(*value) || *value < math.MinInt32 || *value > math.MaxInt32 {
		return 0, process.NewError("invalid_arguments", "%s must be an integer", name)
	}
	return int(*value), nil
}

func isInteger(value float64) bool {
	return !math.IsNaN(value) && !math.IsInf(value, 0) && math.Trunc(value) == value
}
