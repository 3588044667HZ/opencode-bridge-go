package filesmcp

import (
	"encoding/json"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func raw(value string) json.RawMessage {
	return json.RawMessage(value)
}

func boolPtr(value bool) *bool {
	return &value
}

var readFileSchema = raw(`{
  "$schema": "http://json-schema.org/draft-07/schema#",
  "type": "object",
  "properties": {
    "path": {"type": "string"},
    "offset": {"type": "number"},
    "limit": {"type": "number"}
  }
}`)

var writeFileSchema = raw(`{
  "$schema": "http://json-schema.org/draft-07/schema#",
  "type": "object",
  "properties": {
    "path": {"type": "string"},
    "content": {"type": "string"},
    "create_dirs": {"type": "boolean"}
  }
}`)

var listDirectorySchema = raw(`{
  "$schema": "http://json-schema.org/draft-07/schema#",
  "type": "object",
  "properties": {
    "path": {"type": "string"}
  }
}`)

var statFileSchema = raw(`{
  "$schema": "http://json-schema.org/draft-07/schema#",
  "type": "object",
  "properties": {
    "path": {"type": "string"}
  }
}`)

var listStagedFilesSchema = raw(`{
  "$schema": "http://json-schema.org/draft-07/schema#",
  "type": "object",
  "properties": {
    "direction": {"type": "string"}
  }
}`)

var deleteStagedFileSchema = raw(`{
  "$schema": "http://json-schema.org/draft-07/schema#",
  "type": "object",
  "properties": {
    "direction": {"type": "string"},
    "name": {"type": "string"}
  }
}`)

var readFileTool = &mcp.Tool{
	Name:        "read_file",
	Title:       "Read workspace text file",
	Description: "Read UTF-8 text from a file inside BRIDGE_WORKSPACE.",
	InputSchema: readFileSchema,
	Annotations: &mcp.ToolAnnotations{
		DestructiveHint: boolPtr(false),
		OpenWorldHint:   boolPtr(false),
		ReadOnlyHint:    true,
	},
}

var writeFileTool = &mcp.Tool{
	Name:        "write_file",
	Title:       "Write workspace text file",
	Description: "Write UTF-8 text to a file inside BRIDGE_WORKSPACE.",
	InputSchema: writeFileSchema,
	Annotations: &mcp.ToolAnnotations{
		DestructiveHint: boolPtr(true),
		OpenWorldHint:   boolPtr(false),
		ReadOnlyHint:    false,
	},
}

var listDirectoryTool = &mcp.Tool{
	Name:        "list_directory",
	Title:       "List workspace directory",
	Description: "List a directory inside BRIDGE_WORKSPACE without recursion.",
	InputSchema: listDirectorySchema,
	Annotations: &mcp.ToolAnnotations{
		DestructiveHint: boolPtr(false),
		OpenWorldHint:   boolPtr(false),
		ReadOnlyHint:    true,
	},
}

var statFileTool = &mcp.Tool{
	Name:        "stat_file",
	Title:       "Stat workspace path",
	Description: "Return metadata for a path inside BRIDGE_WORKSPACE.",
	InputSchema: statFileSchema,
	Annotations: &mcp.ToolAnnotations{
		DestructiveHint: boolPtr(false),
		OpenWorldHint:   boolPtr(false),
		ReadOnlyHint:    true,
	},
}

var listStagedFilesTool = &mcp.Tool{
	Name:        "list_staged_files",
	Title:       "List staged files",
	Description: "List valid regular files in the staging inbox or outbox.",
	InputSchema: listStagedFilesSchema,
	Annotations: &mcp.ToolAnnotations{
		DestructiveHint: boolPtr(false),
		OpenWorldHint:   boolPtr(false),
		ReadOnlyHint:    true,
	},
}

var deleteStagedFileTool = &mcp.Tool{
	Name:        "delete_staged_file",
	Title:       "Delete staged file",
	Description: "Delete a staged file from the staging inbox or outbox.",
	InputSchema: deleteStagedFileSchema,
	Annotations: &mcp.ToolAnnotations{
		DestructiveHint: boolPtr(true),
		OpenWorldHint:   boolPtr(false),
		ReadOnlyHint:    false,
	},
}
