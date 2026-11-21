# go-bridge

Go backend for the OpenCode laptop bridge.

- Behavior contract: `specs/bridge-behavior-freeze-v1.md`
- Object API: `specs/bridge-object-api.md`

## Build

```powershell
go build -o bin/process-backend.exe ./cmd/process-backend
go build -o bin/go-bridge.exe ./cmd/go-bridge
```

## Test

```powershell
go test ./...
go vet ./...
```

Full local gate (gofmt + build + vet + verbose tests):

```powershell
powershell -ExecutionPolicy Bypass -File scripts/check.ps1
```

```bash
bash scripts/check.sh
```

CI runs the same checks on Windows and Linux in `.github/workflows/ci.yml`.

## Packaging

```powershell
powershell -ExecutionPolicy Bypass -File scripts/package.ps1 -Version 0.1.0
```

This creates `dist/go-bridge-<version>-windows-amd64.zip` and
`dist/go-bridge-<version>-linux-amd64.tar.gz`. Each package contains both
binaries, `README.md`, `specs/`, `deploy/`, `.env.example`, and
`opencode.example.json`. On Linux, run `bash start.sh`; it restores the
executable bits and starts the bridge. Alternatively run
`chmod +x go-bridge process-backend`.

## Binaries

- `process-backend.exe`: MCP stdio server exposing the five process tools. Reads `BRIDGE_WORKSPACE` and `BRIDGE_SHELL`.
- `go-bridge.exe`: MCP Streamable HTTP server exposing all 15 bridge tools: process, workspace files, staging, system info, clipboard, and screenshot. Also serves the optional HTTP file service.

## Run

```powershell
.\bin\go-bridge.exe
```

The CLI loads `<cwd>/.env`, reads the environment, and runs the Bridge object. Stop it with Ctrl+C.

## Go API

`go-bridge` exposes the runtime as a library:

```go
import "go-bridge/bridge"

instance := bridge.NewBridge(bridge.Options{
    Config:  bridgeConfig,
    Secrets: bridge.Secrets{MCPToken: mcpToken, FilesToken: filesToken},
    Env:     environment,
})
if err := instance.Start(ctx); err != nil {
    // handle startup failure
}
defer instance.Stop(context.Background())
```

`Start`, `Stop`, `Health`, `Diagnostics`, `Addresses`, `WaitForReady`, `ShellSummary`, and `On` follow `specs/bridge-object-api.md`.

## Configuration

Copy `.env.example` to `.env` and set `LAPTOP_MCP_TOKEN`. Uncomment
`BRIDGE_FILES_TOKEN` to enable the HTTP file service.

`go-bridge.exe` requires `LAPTOP_MCP_TOKEN`.

| Variable | Default | Notes |
| --- | --- | --- |
| `BRIDGE_PORT` | `8765` | MCP loopback port |
| `BRIDGE_DATA_DIR` | `<cwd>/data` | Data root |
| `BRIDGE_WORKSPACE` | `<data>/workspace` | File-tool root and default process workdir |
| `BRIDGE_FILES_PORT` | `8766` | Must differ from `BRIDGE_PORT` |
| `BRIDGE_FILES_TOKEN` | unset | Unset disables the HTTP file service; must differ from the MCP token |
| `BRIDGE_MAX_FILE_BYTES` | `536870912` | HTTP upload size limit |
| `BRIDGE_STAGING_QUOTA_BYTES` | `2147483648` | Inbox plus outbox quota; must be at least the upload limit |
| `BRIDGE_SCREENSHOT_BACKEND` | `auto` | `auto`, `ffmpeg`, or `powershell` |
| `BRIDGE_SHELL` | platform default | Validated shell alias |

Endpoints:

- MCP: `http://127.0.0.1:8765/mcp`
- Files: `http://127.0.0.1:8766/files` (only when `BRIDGE_FILES_TOKEN` is set)

Screenshots are saved under `<data>/screenshots` with 7-day retention and are inlined up to 4 MiB. Clipboard and screenshot tools depend on the host session; use `get_system_info` to inspect the detected backends before calling them.

The default workspace moved from `<cwd>/workspace` to `<cwd>/data/workspace`; set `BRIDGE_WORKSPACE` explicitly to keep another location. Both services bind loopback only and tokens never appear in logs.

## Deployment

- `deploy/tailscale.md`: Tailscale Serve routing, ACLs, and verification.
- `deploy/security.md`: trust boundary, token handling, file service, and desktop capabilities.
- `opencode.example.json`: recommended permissions for the 15 `laptop_*` tools.
