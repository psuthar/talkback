# TalkBack MCP server (`talkback-mcp`)

Stdio [Model Context Protocol](https://modelcontextprotocol.io) server for agents (Cursor, Claude Code, Claude Desktop). **Stdout** carries the MCP JSON-RPC stream; **stderr** is used for operational logs (structured lines; see [Structured logging](#structured-logging-scrum-40) below).

**Protocol wiring:** The binary uses `internal/mcpserver.NewTalkbackMCPServer`, which constructs the official Go SDK server, attaches receiving middleware so **only** `tools/call` is API-key gated (`initialize` / `tools/list` stay open), registers tools, then runs [`mcp.StdioTransport`](https://pkg.go.dev/github.com/modelcontextprotocol/go-sdk/mcp#StdioTransport) (newline-delimited JSON-RPC).

## Quick start from a clone (Cursor + Claude Code)

Goal: run `talkback-mcp` from a **local clone** with minimal friction—`health_check` first; optional Postgres-backed `get_session_metadata` when you need it.

1. **Install Go** using a toolchain that matches the **`go` version** in the repo root [`go.mod`](../go.mod) (the project tracks a current Go release).
2. **Build** (from the repository root):

   ```bash
   go build -o talkback-mcp ./cmd/talkback-mcp
   ```

   Or use `go run ./cmd/talkback-mcp` via the generated config (below); no separate build step is required.
3. **Generate IDE config** (writes **gitignored** `.cursor/mcp.json` and `.mcp.json`):

   ```bash
   ./scripts/setup-mcp-config.sh
   ```

   The script uses **`go run`** with an **absolute** path to `cmd/talkback-mcp`, so the MCP subprocess does not depend on shell `cwd`. It sets `TALKBACK_MCP_REQUIRE_CLIENT_KEY=false` and a random `TALKBACK_MCP_API_KEY` unless you preset **`TALKBACK_MCP_API_KEY`** in the environment before running the script.
4. **Optional — session metadata tool:** If you already have a TalkBack database (e.g. local Docker Postgres from the main app), export **`DATABASE_URL`** and **`TALKBACK_MCP_ACTING_USER_ID`** (a TalkBack `users.id` UUID) in your shell and **run the setup script again**—those variables are copied into the MCP `env` block. Otherwise omit them; only `health_check` is registered when `DATABASE_URL` is unset at process start.
5. **Restart** Cursor and/or Claude Code fully so MCP reloads (config is not hot-reloaded).

**PATH and platforms**

- **macOS + GUI IDEs:** If `go` is installed via Homebrew (`/opt/homebrew/bin/go`) but the IDE cannot find `go`, ensure that directory is on `PATH` for GUI-launched apps (e.g. Cursor/VS Code `terminal.integrated.env.osx` or launch Cursor from a terminal). The repo includes `.vscode/settings.json` that prepends `/opt/homebrew/bin` for the **integrated terminal** only.
- **Linux:** Install Go from your distro or [go.dev](https://go.dev/dl/); confirm `go version` in the environment that starts the IDE.
- **Windows:** Prefer **WSL** or **Git Bash** with Go installed; use **forward slashes or escaped backslashes** in JSON `args` paths if you hand-edit configs.

**Committed examples (placeholders only, no real secrets)**

- Minimal: [`mcp-config.example.json`](mcp-config.example.json)
- With optional DB + acting user (placeholders): [`mcp-config.example-with-db.json`](mcp-config.example-with-db.json)

## Build

```bash
go build -o talkback-mcp ./cmd/talkback-mcp
```

## Authentication

The server always requires **`TALKBACK_MCP_API_KEY`** (non-empty) at startup.

| Variable | Required | Purpose |
|----------|----------|---------|
| `TALKBACK_MCP_API_KEY` | Yes | One or more comma-separated shared secrets the server knows (rotation). |
| `TALKBACK_MCP_REQUIRE_CLIENT_KEY` | No | Default **true** (strict): each `tools/call` must include a key matching `TALKBACK_MCP_API_KEY` (see below). Set to **`false`** so Cursor / Claude Code can use tools **without** per-call metadata (typical local dev). |
| `TALKBACK_MCP_ACTING_USER_ID` | No | TalkBack **users.id** UUID for the acting user. Required for **`get_session_metadata`** (access control); otherwise that tool returns 403. |
| `DATABASE_URL` | No | When set at process start, the server registers **`get_session_metadata`** (Postgres). When unset, only `health_check` is available. |

**Local dev presets**

| Goal | Set these (minimum) |
|------|---------------------|
| `health_check` only | `TALKBACK_MCP_API_KEY` (non-empty), `TALKBACK_MCP_REQUIRE_CLIENT_KEY=false` for typical Cursor/Claude Code |
| `get_session_metadata` too | Above plus `DATABASE_URL` and `TALKBACK_MCP_ACTING_USER_ID` (must be a valid user UUID in your DB) |

### API key middleware

Implementation: `internal/mcpserver/middleware.go` — `Auth.RequireToolAuthMiddleware`. Only the JSON-RPC method **`tools/call`** is gated. **`initialize`**, **`tools/list`**, **`ping`**, and other non-tool methods pass through without a client key so the MCP handshake and tool discovery work. Keys are compared in constant time (per equal-length candidate); auth failures emit a structured stderr line (`event=auth_failed`, sanitized tool name, `reason=missing_or_invalid_key`) and **never** log key material or `_meta`.

### Strict mode (`TALKBACK_MCP_REQUIRE_CLIENT_KEY` unset or true)

Pass the key on every **`tools/call`** via `_meta` (or HTTP headers when the transport fills `RequestExtra`):

```json
{
  "name": "health_check",
  "arguments": {},
  "_meta": {
    "talkback": { "apiKey": "same-secret-as-TALKBACK_MCP_API_KEY" }
  }
}
```

Alternatives: `_meta.talkbackApiKey`, `_meta.authorization` as `Bearer <token>`, or `Authorization` / `X-API-Key` headers on HTTP-based transports.

### IDE mode (`TALKBACK_MCP_REQUIRE_CLIENT_KEY=false`)

Many IDE hosts do not attach `_meta` on tool calls. Set **`TALKBACK_MCP_REQUIRE_CLIENT_KEY=false`** in the MCP server `env` block (see [One-shot setup](#one-shot-setup-cursor--claude-code)). The process still loads `TALKBACK_MCP_API_KEY` so only your configured subprocess can run; treat this as **local-trust** (weaker than per-call keys).

Invalid keys in strict mode produce an **unauthorized** error for `tools/call` (no secrets in logs).

## Run (local)

```bash
export TALKBACK_MCP_API_KEY=dev-shared-secret
export TALKBACK_MCP_REQUIRE_CLIENT_KEY=false
./talkback-mcp
# optional:
./talkback-mcp -version=1.0.0
```

## One-shot setup (Cursor + Claude Code)

From the **repository root**:

```bash
./scripts/setup-mcp-config.sh
```

This writes **both** (gitignored, not committed):

- **`.cursor/mcp.json`** — Cursor (project or picked up when you open this repo)
- **`.mcp.json`** — Claude Code **project**-scoped MCP

It sets `TALKBACK_MCP_REQUIRE_CLIENT_KEY=false` and generates a random API key unless you preset **`TALKBACK_MCP_API_KEY`**.

If **`DATABASE_URL`** and/or **`TALKBACK_MCP_ACTING_USER_ID`** are set in the shell when you run the script, they are copied into the generated `env` object so `get_session_metadata` can run against your local DB.

Then **fully quit and reopen** Cursor and/or Claude Code so MCP reloads.

Committed references (edit paths if you copy manually): [`docs/mcp-config.example.json`](mcp-config.example.json), [`docs/mcp-config.example-with-db.json`](mcp-config.example-with-db.json).

## Cursor

**Option A — script:** run `./scripts/setup-mcp-config.sh` (see above).

**Option B — manual:** merge this shape into **`~/.cursor/mcp.json`** (global) or **`.cursor/mcp.json`** (project). Same JSON as Claude Code’s `mcpServers` block:

```json
{
  "mcpServers": {
    "talkback": {
      "command": "go",
      "args": ["run", "/absolute/path/to/talkback/cmd/talkback-mcp", "-version=dev"],
      "env": {
        "TALKBACK_MCP_API_KEY": "your-shared-secret",
        "TALKBACK_MCP_REQUIRE_CLIENT_KEY": "false"
      }
    }
  }
}
```

Use a **built binary** by setting `"command"` to the absolute path of `talkback-mcp` and `"args": ["-version=dev"]`, or keep the `go run` form above so you do not need `cwd`.

## Claude Code

**Option A — script:** `./scripts/setup-mcp-config.sh` creates **`.mcp.json`** at the repo root (project scope). Approve the server when prompted (`claude mcp reset-project-choices` if you need to re-approve).

**Option B — user scope:** add the same `talkback` entry under `mcpServers` in **`~/.claude.json`** (see [Claude Code MCP docs](https://code.claude.com/docs/en/mcp)).

**Secrets in git:** prefer `${TALKBACK_MCP_API_KEY}` style expansion in committed templates if your Claude Code version supports it, or keep `.mcp.json` gitignored and generate it with the script.

## Claude Desktop

Merge the same `mcpServers.talkback` object into:

- **macOS:** `~/Library/Application Support/Claude/claude_desktop_config.json`
- **Windows:** `%APPDATA%\Claude\claude_desktop_config.json`

Restart Claude Desktop after saving.

## Structured logging (SCRUM-40)

The server writes **single-line** messages to stderr via Go’s standard `log` package. Each line starts with `mcp` and uses space-separated `key=value` fields for grep-friendly parsing:

| Field | Meaning |
|-------|--------|
| `event` | `tool_complete` after a tool handler returns, or `auth_failed` when strict API-key validation fails on `tools/call`. |
| `tool` | Sanitized tool name (`a-z`, `0-9`, `_` only; max 64 chars); invalid names appear as `unknown_tool`. |
| `duration_ms` | Wall time for the tool handler (always present; `0` for `auth_failed`). |
| `session_id` | Present on `get_session_metadata` **only** after the input UUID parses successfully (canonical string). Invalid `session_id` arguments are **not** echoed. |
| `reason` | On `auth_failed`, fixed `missing_or_invalid_key`. |

**Not logged:** API keys, bearer tokens, full `_meta`, request bodies, or arbitrary client-supplied fields (only the allowlisted keys above are emitted).

Implementation: `internal/mcpserver/mcp_log.go` (`logMCPToolComplete`, `logMCPAuthFailed`).

## Tools (SCRUM-32+)

| Tool | Description |
|------|-------------|
| `health_check` | Returns JSON object: `status` (`ok`; `degraded` reserved), `service` (always `talkback-mcp`), `version` (process version, default `dev`). No secrets or session data. Implemented in `internal/mcpserver/health.go`. |
| `get_session_metadata` | Input: `session_id` (UUID). Output: `title`, `created_at`, `owner` (`created_by`, optional `display_name`). Requires `DATABASE_URL` and `TALKBACK_MCP_ACTING_USER_ID`. Errors mirror HTTP semantics in JSON (`http_status` 400/403/404). Implemented in `internal/mcpserver/session_metadata.go` (SCRUM-39). |

### Session metadata / DB (SCRUM-39)

When **`DATABASE_URL`** is set at process start, the server opens Postgres through [`internal/database`](../internal/database) and registers **`get_session_metadata`**. The acting user is the TalkBack user UUID from **`TALKBACK_MCP_ACTING_USER_ID`** (wired into the request context after API-key middleware on `tools/call`). Access is allowed for **global admins** or users who pass **`UserCanAccessSession`** for that session — same rules as the web app. The tool does **not** return transcript or material bodies.

## Hosted deployment (containers & ops) — SCRUM-42

This section describes a **single trusted deployment** (one environment, one MCP process or small replica count). Multi-tenant SaaS MCP is out of scope.

### Process model

- **Binary:** `talkback-mcp` from [`cmd/talkback-mcp`](../cmd/talkback-mcp). Same build as local dev; no separate “enterprise” binary.
- **Stdio:** MCP JSON-RPC is on **stdin/stdout**; operational logs go to **stderr** (see [Structured logging](#structured-logging-scrum-40)). Nothing else may write to stdout.
- **Supervisor / probes:** Stdio MCP still closes when **stdin** closes (e.g. a bare Pod with no attached client). For **Kubernetes**, set **`TALKBACK_MCP_HEALTH_ADDR`** (e.g. `:8080`) so the process also serves **GET `/healthz`** and **GET `/ready`** for liveness/readiness without a stdio bridge (SCRUM-69). You can still add a bridge later for remote MCP clients over stdio.

### Container image

The repo includes a **minimal image** (MCP only, not the full TalkBack API stack):

```bash
docker build -f deploy/Dockerfile.mcp -t talkback-mcp:latest .
```

- **Layout:** single static binary at `/usr/local/bin/talkback-mcp`, non-root user `65532`, CA certificates for Postgres TLS.
- **Main app image:** The root [`Dockerfile`](../Dockerfile) builds the HTTP API (`cmd/api`); it does **not** include `talkback-mcp`. Use `deploy/Dockerfile.mcp` when you only need the MCP server.

### Environment (hosted)

| Variable | Typical hosted value | Notes |
|----------|----------------------|--------|
| `TALKBACK_MCP_API_KEY` | **Required** | Inject via secrets manager / K8s Secret / parameter store — **never commit** real values. |
| `TALKBACK_MCP_REQUIRE_CLIENT_KEY` | **`true`** | Clients must send a matching key on each `tools/call` (see **Strict mode** under [Authentication](#authentication)). |
| `DATABASE_URL` | If using `get_session_metadata` | Same Postgres as TalkBack when that tool is required; omit to register only `health_check`. |
| `TALKBACK_MCP_ACTING_USER_ID` | If using `get_session_metadata` | TalkBack `users.id` UUID; inject alongside DB URL. |
| `TALKBACK_MCP_HEALTH_ADDR` | e.g. `:8080` | When set, starts an **HTTP** listener (in addition to stdio MCP) with **GET `/healthz`** (liveness) and **GET `/ready`** (readiness; pings Postgres when `DATABASE_URL` is in use). Omit for IDE-only local use. |

### Secrets injection

- **Kubernetes:** mount with `envFrom.secretRef` or the [External Secrets Operator](https://external-secrets.io/) / cloud-specific sync to Secrets. Example manifest (placeholders only): [`deploy/k8s/mcp-hosted.example.yaml`](../deploy/k8s/mcp-hosted.example.yaml).
- **AWS / GCP / Azure:** resolve secrets at deploy time into env or files; do not bake credentials into images.
- **Rotation:** `TALKBACK_MCP_API_KEY` supports comma-separated keys; roll by adding the new key before retiring the old one.

### Health and observability

| Signal | Use |
|--------|-----|
| **Process** | Exit code non-zero on fatal config (`TALKBACK_MCP_API_KEY` missing, invalid `TALKBACK_MCP_ACTING_USER_ID`, DB connect failure at startup when `DATABASE_URL` is set). Restart policies catch crashes. |
| **Stderr** | Structured lines (`event=tool_complete`, `event=auth_failed`, …) for auth and tool outcomes — see [Structured logging](#structured-logging-scrum-40). |
| **HTTP `/healthz` and `/ready`** | When **`TALKBACK_MCP_HEALTH_ADDR`** is set, **GET `/healthz`** returns JSON matching the `health_check` tool shape (`status`, `service`, `version`). **GET `/ready`** returns `200` when Postgres is not required, or when the pool can **ping** the DB (otherwise `503`). **Unauthenticated** — intended for in-cluster probes; protect with NetworkPolicy or similar if needed. |
| **`health_check` MCP tool** | Still requires a **client** that speaks MCP JSON-RPC over **stdio** (IDE, bridge, or test harness). |

### Failure modes (observable behavior)

| Condition | What happens |
|-----------|----------------|
| Missing / empty `TALKBACK_MCP_API_KEY` | Process **exits** at startup (`LoadAuthFromEnv`). |
| Invalid `TALKBACK_MCP_ACTING_USER_ID` | Process **exits** at startup. |
| `DATABASE_URL` set but DB unreachable at startup | Process **exits** during `database.New()` in [`cmd/talkback-mcp`](../cmd/talkback-mcp). |
| Wrong client key in strict mode | `tools/call` returns unauthorized; stderr logs `event=auth_failed` (no secrets). |
| DB up at start but fails later | `get_session_metadata` may return errors to the client; check stderr and app DB health. |
| **`TALKBACK_MCP_HEALTH_ADDR` set but listen fails** (bad address or port in use) | Process **exits** from the health HTTP goroutine (`log.Fatalf`). |
| **`/ready` with DB** | Returns **503** if Postgres ping fails within ~2s (readiness probe should fail). |

### Kubernetes and Docker references

- **Example manifest (Secret + ConfigMap + Deployment):** [`deploy/k8s/mcp-hosted.example.yaml`](../deploy/k8s/mcp-hosted.example.yaml) — edit image name, resources, and secret wiring before apply. Sets **`TALKBACK_MCP_HEALTH_ADDR=:8080`** and HTTP **liveness/readiness** probes on `/healthz` and `/ready`.
- **Docker Compose:** The main stack is [`deploy/docker-compose.yml`](../deploy/docker-compose.yml) (Postgres + API). There is no default `talkback-mcp` service there; run the MCP image beside your stack when you have a stdio bridge, or use the image in CI/smoke with `docker run -i` and a test client.
