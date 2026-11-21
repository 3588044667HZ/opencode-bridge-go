package systemmcp

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

var emptySchema = raw(`{
  "type": "object",
  "properties": {}
}`)

var setClipboardSchema = raw(`{
  "$schema": "http://json-schema.org/draft-07/schema#",
  "type": "object",
  "properties": {
    "text": {"type": "string"}
  },
  "required": ["text"]
}`)

var getSystemInfoTool = &mcp.Tool{
	Name:        "get_system_info",
	Title:       "Get bridge system information",
	Description: "Return operating system, shell, path, file service, and capability information.",
	InputSchema: emptySchema,
	Annotations: &mcp.ToolAnnotations{
		DestructiveHint: boolPtr(false),
		OpenWorldHint:   boolPtr(false),
		ReadOnlyHint:    true,
	},
}

var getClipboardTool = &mcp.Tool{
	Name:        "get_clipboard",
	Title:       "Read bridge clipboard text",
	Description: "Read UTF-8 text from the bridge host clipboard.",
	InputSchema: emptySchema,
	Annotations: &mcp.ToolAnnotations{
		DestructiveHint: boolPtr(false),
		OpenWorldHint:   boolPtr(false),
		ReadOnlyHint:    true,
	},
}

var setClipboardTool = &mcp.Tool{
	Name:        "set_clipboard",
	Title:       "Write bridge clipboard text",
	Description: "Write UTF-8 text to the bridge host clipboard.",
	InputSchema: setClipboardSchema,
	Annotations: &mcp.ToolAnnotations{
		DestructiveHint: boolPtr(true),
		OpenWorldHint:   boolPtr(false),
		ReadOnlyHint:    false,
	},
}

var captureScreenshotTool = &mcp.Tool{
	Name:        "capture_screenshot",
	Title:       "Capture bridge desktop screenshot",
	Description: "Capture the bridge host desktop and return metadata plus an optional image.",
	InputSchema: emptySchema,
	Annotations: &mcp.ToolAnnotations{
		DestructiveHint: boolPtr(false),
		OpenWorldHint:   boolPtr(false),
		ReadOnlyHint:    true,
	},
}
