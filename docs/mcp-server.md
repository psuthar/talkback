# TalkBack MCP server (`talkback-mcp`)

Stdio [Model Context Protocol](https://modelcontextprotocol.io) server for agents (Cursor, Claude Code, etc.). **Stdout** carries the MCP JSON-RPC stream; **stderr** is used for operational logs (tool name and duration).

## Build

```bash
go build -o talkback-mcp ./cmd/talkback-mcp
```

## Authentication (SCRUM-33)

Every **`tools/call`** request must present an API key that matches one of the secrets in **`TALKBACK_MCP_API_KEY`** on the server (comma-separated for rotation). Other MCP methods (e.g. `initialize`, `tools/list`) are not gated by this key.

**Server environment**

| Variable | Required | Purpose |
|----------|----------|---------|
| `TALKBACK_MCP_API_KEY` | Yes | One or more comma-separated shared secrets the server accepts. |
| `TALKBACK_MCP_ACTING_USER_ID` | No | Optional UUID of the TalkBack user (or bot) to attach to the MCP session context for future ACL; same spirit as REST session rules. |

**Client → server:** pass the key in `tools/call` params metadata (preferred for stdio):

```json
{
  "name": "health_check",
  "arguments": {},
  "_meta": {
    "talkback": { "apiKey": "same-secret-as-TALKBACK_MCP_API_KEY" }
  }
}
```

Alternatives: `_meta.talkbackApiKey`, `_meta.authorization` as `Bearer <token>`, or HTTP `Authorization: Bearer` / `X-API-Key` when using a transport that populates [RequestExtra](https://pkg.go.dev/github.com/modelcontextprotocol/go-sdk/mcp#RequestExtra) headers.

Invalid or missing keys produce an **unauthorized** error for `tools/call` (nothing sensitive is logged).

## Run (local)

```bash
export TALKBACK_MCP_API_KEY=dev-shared-secret
./talkback-mcp
# optional:
./talkback-mcp -version=1.0.0
```

## Tools (SCRUM-32+)

| Tool | Description |
|------|-------------|
| `health_check` | Returns JSON `status`, `service`, `version` — connectivity only; no TalkBack session data. |

## Cursor

Add to your MCP config (e.g. **Cursor Settings → MCP** or `~/.cursor/mcp.json`), adjusting the binary path:

```json
"talkback": {
  "command": "/absolute/path/to/talkback/talkback-mcp",
  "args": ["-version=dev"],
  "env": {
    "TALKBACK_MCP_API_KEY": "your-shared-secret"
  }
}
```

The MCP host must still send `_meta.talkback.apiKey` on each **tool call** (matching `TALKBACK_MCP_API_KEY`) unless your client injects it automatically.

For development without installing a binary:

```json
"talkback": {
  "command": "go",
  "args": ["run", "./cmd/talkback-mcp", "-version=dev"],
  "cwd": "/absolute/path/to/talkback",
  "env": {
    "TALKBACK_MCP_API_KEY": "your-shared-secret"
  }
}
```

## Claude Code

Use the same `command` / `args` pattern in your Claude Code MCP configuration, pointing at this repo and `cmd/talkback-mcp`.

## Hosted / containers

Run the same binary as the container entrypoint with stdio attached (no HTTP in this story). Ensure nothing else writes to stdout.
