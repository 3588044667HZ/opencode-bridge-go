# Security notes

## Trust boundary

The MCP token is equivalent to an interactive login as the bridge user. The 15
tools can run arbitrary commands, read and write workspace files, and access the
bridge desktop. The workspace file tools are not a sandbox; `start_process` can
read any file the user can read.

## Tokens

- `LAPTOP_MCP_TOKEN` is required. Use a long random value.
- `BRIDGE_FILES_TOKEN` is optional. When set, the HTTP file service is enabled.
  It must be different from the MCP token.
- Tokens are accepted only as Bearer credentials. MCP and file tokens are not
  interchangeable.
- Tokens are removed from the environment of bridged shells, clipboard commands,
  and screenshot commands.
- Tokens must not appear in logs, diagnostics, health output, or `get_system_info`.
- Keep `.env` private. Do not commit it; `.token` is also ignored by `.gitignore`.

## Network

- Keep both services bound to `127.0.0.1`. Use Tailscale Serve for remote access
  as described in `deploy/tailscale.md`.
- Do not use Tailscale Funnel, router port forwarding, or a public reverse proxy.
- The MCP token does not replace Tailscale ACLs; use both.

## File service

- `inbox` is for workstation uploads and `outbox` is for downloads.
- Uploaded files are never executed, parsed, or auto-extracted.
- Names are validated; symlinks and directories are not served.
- Staging files expire after 7 days. `.part` uploads are cleaned after 1 hour.
- Quotas and the per-request size limit are configured with
  `BRIDGE_STAGING_QUOTA_BYTES` and `BRIDGE_MAX_FILE_BYTES`.

## Workspace and data

- Workspace file tools reject `..`, drive-relative paths, absolute paths outside
  the workspace, and symlink escapes.
- `BRIDGE_WORKSPACE` is not a security boundary against the MCP client, because
  the process tools have full user privileges.
- If the workspace contains the data directory, the bridge logs a warning
  because staging and screenshots may become readable through the file tools.
- Screenshots are stored under `<data>/screenshots` and retained for 7 days.

## Desktop capabilities

- Clipboard and screenshot tools are best-effort. Headless, locked, or
  non-interactive sessions may return `clipboard_unavailable`,
  `clipboard_error`, `screenshot_unavailable`, or `screenshot_error`.
- Treat clipboard contents and screenshots as sensitive. They are returned to
  the MCP client and must not be logged.
- Configure OpenCode permissions so process, clipboard, screenshot, and write
  tools require confirmation. See `opencode.example.json`.

## Operations

- Run as a normal user, not root or Administrator.
- Rotate tokens by editing `.env` and restarting the bridge.
- Keep the Go module dependencies updated and rerun `scripts/check.ps1` or
  `scripts/check.sh` before deployment.
