package systemmcp

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"go-bridge/internal/clipboard"
	"go-bridge/internal/jsonutil"
	"go-bridge/internal/screenshot"
	"go-bridge/internal/sysinfo"
)

type setClipboardArgs struct {
	Text *string `json:"text"`
}

func Register(server *mcp.Server, system *sysinfo.System, clip *clipboard.Service, shot *screenshot.Service) {
	server.AddTool(getSystemInfoTool, func(_ context.Context, _ *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		info, err := system.Get()
		if err != nil {
			return errorResult("get_system_info", "system_error", err), nil
		}
		return jsonResult("get_system_info", "system_error", info), nil
	})

	server.AddTool(getClipboardTool, func(ctx context.Context, _ *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		result, err := clip.Read(ctx)
		if err != nil {
			return errorResult("get_clipboard", "clipboard_error", err), nil
		}
		return jsonResult("get_clipboard", "clipboard_error", result), nil
	})

	server.AddTool(setClipboardTool, func(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		var args setClipboardArgs
		if err := json.Unmarshal(req.Params.Arguments, &args); err != nil {
			return errorResult("set_clipboard", "invalid_arguments", clipboard.NewError("invalid_arguments", "%v", err)), nil
		}
		if args.Text == nil {
			return errorResult("set_clipboard", "invalid_arguments", clipboard.NewError("invalid_arguments", "text is required")), nil
		}
		result, err := clip.Write(ctx, *args.Text)
		if err != nil {
			return errorResult("set_clipboard", "clipboard_error", err), nil
		}
		return jsonResult("set_clipboard", "clipboard_error", result), nil
	})

	server.AddTool(captureScreenshotTool, func(ctx context.Context, _ *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		result, err := shot.Capture(ctx)
		if err != nil {
			return errorResult("capture_screenshot", "screenshot_error", err), nil
		}
		data, err := jsonutil.MarshalIndent(result)
		if err != nil {
			return errorResult("capture_screenshot", "screenshot_error", screenshot.NewError("screenshot_error", "cannot encode result: %v", err)), nil
		}
		content := []mcp.Content{&mcp.TextContent{Text: string(data)}}
		if result.Inlined && len(result.Inline) > 0 {
			content = append(content, &mcp.ImageContent{
				Data:     []byte(base64.StdEncoding.EncodeToString(result.Inline)),
				MIMEType: result.MIMEType,
			})
		}
		return &mcp.CallToolResult{Content: content}, nil
	})
}

func jsonResult(tool, fallback string, value any) *mcp.CallToolResult {
	data, err := jsonutil.MarshalIndent(value)
	if err != nil {
		return errorResult(tool, fallback, fmt.Errorf("cannot encode result: %w", err))
	}
	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: string(data)}},
	}
}

func errorResult(tool, fallback string, err error) *mcp.CallToolResult {
	code := fallback
	message := err.Error()
	var clipboardErr *clipboard.Error
	var screenshotErr *screenshot.Error
	var systemErr *sysinfo.Error
	switch {
	case errors.As(err, &clipboardErr):
		code = clipboardErr.Code
		message = clipboardErr.Message
	case errors.As(err, &screenshotErr):
		code = screenshotErr.Code
		message = screenshotErr.Message
	case errors.As(err, &systemErr):
		code = systemErr.Code
		message = systemErr.Message
	}
	return &mcp.CallToolResult{
		IsError: true,
		Content: []mcp.Content{&mcp.TextContent{Text: fmt.Sprintf("%s failed: %s: %s", tool, code, message)}},
	}
}
