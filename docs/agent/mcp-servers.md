# TalkBack MCP and Command Reference

Source of truth: This file owns repository command references and MCP server/tooling details.

## Repository Development Commands

- DB: `docker compose -f deploy/docker-compose.yml up -d` (migrations on API startup when `RUN_MIGRATIONS=true`)
- API: `go run ./cmd/api` (default port 8080; override via `PORT`)
- Web: `cd web && npm install && npm run dev`
- Tests: `go test ./...`
- Manual checks: `requests.http`, `make auth-check`, `scripts/auth_check.sh`

TalkBack MCP commands and schema docs:

- `./scripts/setup-mcp-config.sh` for Cursor + Claude local MCP config
- `TALKBACK_MCP_API_KEY=<secret> go run ./cmd/talkback-mcp`
- `docs/mcp-server.md` for local and remote setup
- Schemas/contracts:
  - `docs/mcp-session-decisions-schema.md`
  - `docs/schemas/mcp-session-decisions-v1.schema.json`
  - `docs/mcp-session-action-items-schema.md`
  - `docs/schemas/mcp-session-action-items-v1.schema.json`
  - `docs/mcp-structured-intelligence-fallbacks.md`

## MCP Servers

Three MCP servers are configured in `.cursor/mcp.json` and `.mcp.json`. Regenerate both with `./scripts/setup-mcp-config.sh`.

### `talkback` server

- Command: `go run /Users/psuthar/code/talkback/cmd/talkback-mcp -version=dev`
- Tools include `health_check`, session metadata/decision/action-item/search/raw/source/ask APIs.
- Session tools require DB; several retrieval/ask tools require `OPENAI_API_KEY`.

Important env vars:

- `TALKBACK_MCP_URL` (remote mode URL ending in `/mcp`)
- `TALKBACK_MCP_HTTP_ADDR`
- `TALKBACK_MCP_API_KEY`
- `TALKBACK_MCP_REQUIRE_CLIENT_KEY`
- `DATABASE_URL`
- `TALKBACK_MCP_ACTING_USER_ID`
- `TALKBACK_MCP_KEY_USER_MAP_JSON`
- `TALKBACK_MCP_MAX_EMBEDDING_CALLS_PER_SESSION_PER_MINUTE`
- `OPENAI_API_KEY`
- `STORAGE_DRIVER=r2` plus matching R2 env vars for parity indexing flows

Codespaces note: set `TALKBACK_MCP_URL` and `TALKBACK_MCP_API_KEY` secrets, then run setup script.

### `github` server

- Command: `docker run -i --rm -e GITHUB_PERSONAL_ACCESS_TOKEN ghcr.io/github/github-mcp-server`
- PAT requirement: classic token with `repo` scope.
- FULL_AUTO relies on `pull_request_read` with `method: get` and `mergeable_state`.
- If `mergeable_state` is absent, FULL_AUTO is unavailable and must hard-stop.

### `atlassian` server

- Package: `@xuandev/atlassian-mcp` via `npx -y`
- Tools: `jira_*` and `confluence_*`
- Required env vars: `ATLASSIAN_DOMAIN`, `ATLASSIAN_EMAIL`, `ATLASSIAN_API_TOKEN`
- `jira_add_comment` requires `body` (not `comment`)

