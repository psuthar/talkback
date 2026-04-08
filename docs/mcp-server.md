# TalkBack MCP server (`talkback-mcp`)

Stdio [Model Context Protocol](https://modelcontextprotocol.io) server for agents (Cursor, Claude Code, etc.). **Stdout** carries the MCP JSON-RPC stream; **stderr** is used for operational logs (tool name and duration).

## Build

```bash
go build -o talkback-mcp ./cmd/talkback-mcp
```

## Run (local)

```bash
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
  "args": ["-version=dev"]
}
```

For development without installing a binary:

```json
"talkback": {
  "command": "go",
  "args": ["run", "./cmd/talkback-mcp", "-version=dev"],
  "cwd": "/absolute/path/to/talkback"
}
```

## Claude Code

Use the same `command` / `args` pattern in your Claude Code MCP configuration, pointing at this repo and `cmd/talkback-mcp`.

## Hosted / containers

Run the same binary as the container entrypoint with stdio attached (no HTTP in this story). Ensure nothing else writes to stdout.
