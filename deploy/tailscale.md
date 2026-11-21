# Tailscale deployment

The bridge binds only to loopback:

- MCP: `127.0.0.1:8765` (`/mcp`, plus unauthenticated `/healthz`)
- Files: `127.0.0.1:8766` (`/files`, only when `BRIDGE_FILES_TOKEN` is set)

Do not change the bind address, add port forwarding, or enable Tailscale Funnel.

## Recommended: separate HTTPS ports

Expose MCP on the default Tailscale HTTPS port and files on an independent port:

```bash
tailscale serve --bg --https=443 http://127.0.0.1:8765
tailscale serve --bg --https=8443 http://127.0.0.1:8766
```

Only run the 8443 command when the file service is enabled.

Workstation URLs:

```text
https://<laptop>.<tailnet>.ts.net/mcp
https://<laptop>.<tailnet>.ts.net:8443/files/inbox/<name>
https://<laptop>.<tailnet>.ts.net:8443/files/outbox/<name>
```

Tailscale ACLs must allow the workstation to reach the laptop on `tcp:443` and,
when files are enabled, `tcp:8443`. The bridge itself only returns loopback URLs
from `get_system_info.fileService.baseUrl`; construct the public URL from the
laptop name and port.

## Verification

```bash
tailscale serve status
curl -fsS https://<laptop>.<tailnet>.ts.net/healthz
curl -i -H "Authorization: Bearer $BRIDGE_FILES_TOKEN" \
  https://<laptop>.<tailnet>.ts.net:8443/files/outbox
```

Use an MCP client (or OpenCode) with `Authorization: Bearer $LAPTOP_MCP_TOKEN`
for the `/mcp` endpoint. The MCP token must be rejected by the file service and
the file token must be rejected by MCP.

## Tradeoffs

- The MCP endpoint and `/healthz` share port 443. `/healthz` is intentionally
  unauthenticated and only reports `ok`; it must not be used as an
  authorization signal.
- The file service uses a separate HTTPS port so the MCP path is not rewritten
  by Tailscale Serve. If you experiment with path-based routing, verify that the
  full `/mcp` and `/files` paths reach the bridge before relying on it.
- Rotate either token by editing `.env` and restarting the bridge. There is no
  hot reload.
- Run the bridge as a normal interactive user. A Windows Session 0 service has
  no interactive desktop for screenshots and may not have a usable clipboard.
