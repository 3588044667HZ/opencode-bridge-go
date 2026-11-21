package filesmcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"go-bridge/internal/files"
	"go-bridge/internal/jsonutil"
)

type readFileArgs struct {
	Path   string   `json:"path"`
	Offset *float64 `json:"offset"`
	Limit  *float64 `json:"limit"`
}

type writeFileArgs struct {
	Path       string  `json:"path"`
	Content    *string `json:"content"`
	CreateDirs *bool   `json:"create_dirs"`
}

type listDirectoryArgs struct {
	Path string `json:"path"`
}

type statFileArgs struct {
	Path string `json:"path"`
}

type listStagedFilesArgs struct {
	Direction string `json:"direction"`
}

type deleteStagedFileArgs struct {
	Direction string `json:"direction"`
	Name      string `json:"name"`
}

func Register(server *mcp.Server, service *files.Service) {
	server.AddTool(readFileTool, func(_ context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		var args readFileArgs
		if err := json.Unmarshal(req.Params.Arguments, &args); err != nil {
			return errorResult("read_file", files.NewError("invalid_arguments", "%v", err)), nil
		}
		if args.Path == "" {
			return errorResult("read_file", files.NewError("invalid_arguments", "path is required")), nil
		}
		offset, err := parseOptionalInt(args.Offset, 1, "offset", 1, math.MaxInt32)
		if err != nil {
			return errorResult("read_file", err), nil
		}
		limit, err := parseOptionalInt(args.Limit, files.DefaultReadLines, "limit", 1, files.MaxReadLines)
		if err != nil {
			return errorResult("read_file", err), nil
		}
		result, err := service.ReadFile(files.ReadFileOptions{Path: args.Path, Offset: offset, Limit: limit})
		if err != nil {
			return errorResult("read_file", err), nil
		}
		return jsonResult("read_file", result), nil
	})

	server.AddTool(writeFileTool, func(_ context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		var args writeFileArgs
		if err := json.Unmarshal(req.Params.Arguments, &args); err != nil {
			return errorResult("write_file", files.NewError("invalid_arguments", "%v", err)), nil
		}
		if args.Path == "" {
			return errorResult("write_file", files.NewError("invalid_arguments", "path is required")), nil
		}
		if args.Content == nil {
			return errorResult("write_file", files.NewError("invalid_arguments", "content is required")), nil
		}
		createDirs := args.CreateDirs != nil && *args.CreateDirs
		result, err := service.WriteFile(files.WriteFileOptions{
			Path:       args.Path,
			Content:    *args.Content,
			CreateDirs: createDirs,
		})
		if err != nil {
			return errorResult("write_file", err), nil
		}
		return jsonResult("write_file", result), nil
	})

	server.AddTool(listDirectoryTool, func(_ context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		var args listDirectoryArgs
		if len(req.Params.Arguments) != 0 {
			if err := json.Unmarshal(req.Params.Arguments, &args); err != nil {
				return errorResult("list_directory", files.NewError("invalid_arguments", "%v", err)), nil
			}
		}
		result, err := service.ListDirectory(files.ListDirectoryOptions{Path: args.Path})
		if err != nil {
			return errorResult("list_directory", err), nil
		}
		return jsonResult("list_directory", result), nil
	})

	server.AddTool(statFileTool, func(_ context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		var args statFileArgs
		if err := json.Unmarshal(req.Params.Arguments, &args); err != nil {
			return errorResult("stat_file", files.NewError("invalid_arguments", "%v", err)), nil
		}
		if args.Path == "" {
			return errorResult("stat_file", files.NewError("invalid_arguments", "path is required")), nil
		}
		result, err := service.StatFile(args.Path)
		if err != nil {
			return errorResult("stat_file", err), nil
		}
		return jsonResult("stat_file", result), nil
	})

	server.AddTool(listStagedFilesTool, func(_ context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		var args listStagedFilesArgs
		if err := json.Unmarshal(req.Params.Arguments, &args); err != nil {
			return errorResult("list_staged_files", files.NewError("invalid_arguments", "%v", err)), nil
		}
		if args.Direction == "" {
			return errorResult("list_staged_files", files.NewError("invalid_arguments", "direction is required")), nil
		}
		result, err := service.ListStaged(args.Direction)
		if err != nil {
			return errorResult("list_staged_files", err), nil
		}
		return jsonResult("list_staged_files", result), nil
	})

	server.AddTool(deleteStagedFileTool, func(_ context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		var args deleteStagedFileArgs
		if err := json.Unmarshal(req.Params.Arguments, &args); err != nil {
			return errorResult("delete_staged_file", files.NewError("invalid_arguments", "%v", err)), nil
		}
		if args.Direction == "" {
			return errorResult("delete_staged_file", files.NewError("invalid_arguments", "direction is required")), nil
		}
		if args.Name == "" {
			return errorResult("delete_staged_file", files.NewError("invalid_arguments", "name is required")), nil
		}
		result, err := service.DeleteStaged(args.Direction, args.Name)
		if err != nil {
			return errorResult("delete_staged_file", err), nil
		}
		return jsonResult("delete_staged_file", result), nil
	})
}

func parseOptionalInt(value *float64, fallback int, name string, minimum, maximum int) (int, error) {
	if value == nil {
		return fallback, nil
	}
	if math.IsNaN(*value) || math.IsInf(*value, 0) || math.Trunc(*value) != *value {
		return 0, files.NewError("invalid_arguments", "%s must be an integer", name)
	}
	if *value < float64(minimum) || *value > float64(maximum) {
		return 0, files.NewError("invalid_arguments", "%s must be between %d and %d", name, minimum, maximum)
	}
	return int(*value), nil
}

func jsonResult(tool string, value any) *mcp.CallToolResult {
	data, err := jsonutil.MarshalIndent(value)
	if err != nil {
		return errorResult(tool, files.NewError("io_error", "cannot encode result: %v", err))
	}
	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: string(data)}},
	}
}

func errorResult(tool string, err error) *mcp.CallToolResult {
	code := "io_error"
	message := err.Error()
	var fileErr *files.Error
	if errors.As(err, &fileErr) {
		code = fileErr.Code
		message = fileErr.Message
	}
	return &mcp.CallToolResult{
		IsError: true,
		Content: []mcp.Content{&mcp.TextContent{Text: fmt.Sprintf("%s failed: %s: %s", tool, code, message)}},
	}
}
