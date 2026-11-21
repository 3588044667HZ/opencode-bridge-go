package processmcp

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

var startProcessSchema = raw(`{
  "$schema": "http://json-schema.org/draft-07/schema#",
  "type": "object",
  "properties": {
    "command": {"type": "string"},
    "workdir": {"type": "string"},
    "timeout": {"type": "integer", "minimum": -9007199254740991, "maximum": 9007199254740991},
    "timeout_ms": {"type": "integer", "minimum": -9007199254740991, "maximum": 9007199254740991},
    "shell": {"type": "string"},
    "verbose_timing": {"type": "boolean"}
  },
  "required": ["command"]
}`)

var readProcessOutputSchema = raw(`{
  "$schema": "http://json-schema.org/draft-07/schema#",
  "type": "object",
  "properties": {
    "pid": {"type": "number"},
    "timeout_ms": {"type": "number"},
    "offset": {"type": "number"},
    "length": {"type": "number"},
    "verbose_timing": {"type": "boolean"}
  },
  "required": ["pid"]
}`)

var interactWithProcessSchema = raw(`{
  "$schema": "http://json-schema.org/draft-07/schema#",
  "type": "object",
  "properties": {
    "pid": {"type": "number"},
    "input": {"type": "string"},
    "timeout_ms": {"type": "number"},
    "wait_for_prompt": {"type": "boolean"},
    "verbose_timing": {"type": "boolean"}
  },
  "required": ["pid", "input"]
}`)

var forceTerminateSchema = raw(`{
  "$schema": "http://json-schema.org/draft-07/schema#",
  "type": "object",
  "properties": {
    "pid": {"type": "number"}
  },
  "required": ["pid"]
}`)

var listSessionsSchema = raw(`{
  "$schema": "http://json-schema.org/draft-07/schema#",
  "type": "object",
  "properties": {}
}`)

var startProcessTool = &mcp.Tool{
	Name:        "start_process",
	Title:       "Start bridge process",
	Description: "Start a shell process on the bridge host. workdir defaults to BRIDGE_WORKSPACE.",
	InputSchema: startProcessSchema,
	Annotations: &mcp.ToolAnnotations{
		DestructiveHint: boolPtr(true),
		OpenWorldHint:   boolPtr(true),
		ReadOnlyHint:    false,
	},
}

var readProcessOutputTool = &mcp.Tool{
	Name:        "read_process_output",
	Title:       "Read process output",
	Description: "Read new or historical output from a process session.",
	InputSchema: readProcessOutputSchema,
	Annotations: &mcp.ToolAnnotations{
		DestructiveHint: boolPtr(false),
		OpenWorldHint:   boolPtr(false),
		ReadOnlyHint:    true,
	},
}

var interactWithProcessTool = &mcp.Tool{
	Name:        "interact_with_process",
	Title:       "Interact with process",
	Description: "Send input to a running process session and return newly available output.",
	InputSchema: interactWithProcessSchema,
	Annotations: &mcp.ToolAnnotations{
		DestructiveHint: boolPtr(true),
		OpenWorldHint:   boolPtr(true),
		ReadOnlyHint:    false,
	},
}

var forceTerminateTool = &mcp.Tool{
	Name:        "force_terminate",
	Title:       "Force terminate process",
	Description: "Terminate a process session and its process tree.",
	InputSchema: forceTerminateSchema,
	Annotations: &mcp.ToolAnnotations{
		DestructiveHint: boolPtr(true),
		OpenWorldHint:   boolPtr(false),
		ReadOnlyHint:    false,
	},
}

var listSessionsTool = &mcp.Tool{
	Name:        "list_sessions",
	Title:       "List process sessions",
	Description: "List active process sessions.",
	InputSchema: listSessionsSchema,
	Annotations: &mcp.ToolAnnotations{
		DestructiveHint: boolPtr(false),
		OpenWorldHint:   boolPtr(false),
		ReadOnlyHint:    true,
	},
}
