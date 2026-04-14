# TalkBack in GitHub Codespaces

Open this repo in a Codespace and you get a fully working development environment — Go, Node, Docker, and PostgreSQL — without installing anything locally.

The devcontainer (`/.devcontainer/devcontainer.json`) handles all of this automatically. The only manual step is setting secrets **once** in GitHub before opening your first Codespace.

---

## Required Codespace secrets

Set these in **GitHub → Your profile → Settings → Codespaces → Secrets** (user secrets apply to all repos), or in **Repo → Settings → Secrets and variables → Codespaces** (repo-scoped).

| Secret name | Required | Description |
|---|---|---|
| `TALKBACK_MCP_URL` | Yes | URL of the hosted TalkBack MCP server, e.g. `https://talkback-api.onrender.com/mcp` |
| `TALKBACK_MCP_API_KEY` | Yes | Shared secret for the MCP server (ask the team for this value) |
| `GH_PERSONAL_ACCESS_TOKEN` | Yes (for GitHub MCP) | Classic PAT with `repo` scope — used by the GitHub MCP server |
| `ATLASSIAN_DOMAIN` | Optional | Your Atlassian domain, e.g. `yourteam.atlassian.net` — enables Jira/Confluence MCP |
| `ATLASSIAN_EMAIL` | Optional | Your Atlassian account email |
| `ATLASSIAN_API_TOKEN` | Optional | Atlassian API token from [id.atlassian.com/manage-profile/security/api-tokens](https://id.atlassian.com/manage-profile/security/api-tokens) |

> **Important — `GITHUB_` prefix is reserved.** GitHub Codespaces silently drops secrets whose names start with `GITHUB_`. Always use `GH_PERSONAL_ACCESS_TOKEN`, not `GITHUB_PERSONAL_ACCESS_TOKEN`. The `setup-mcp-config.sh` script accepts both names.

---

## First-run checklist

1. **Set secrets** (one-time) — see table above.
2. **Open the Codespace** — from the GitHub repo, click **Code → Codespaces → New codespace**.
3. **Wait for `postCreateCommand` to finish** — the terminal shows progress. This installs Go and Node dependencies and starts the PostgreSQL container. Allow ~5 min on first open (image is ~10 GB; subsequent opens are fast).
4. **`postStartCommand` runs automatically** — `setup-mcp-config.sh` reads your Codespace secrets and writes `.mcp.json`. You should see a line like `Mode: REMOTE HTTP — talkback entry uses 'npx mcp-remote ...'`.
5. **Restart Claude Code / Cursor** — so the IDE picks up the new `.mcp.json`.
6. Done. The following all work immediately:

```bash
go run ./cmd/api          # API on :8080 — migrates DB on first run
cd web && npm run dev     # Web dev server on :3000
go test ./...             # Tests (DB-backed packages need DATABASE_URL — see below)
psql $DATABASE_URL        # Direct postgres access
```

Ports 3000 and 8080 are forwarded automatically and appear in the **Ports** panel.

---

## Environment variables provided automatically

The devcontainer sets `DATABASE_URL` via `remoteEnv` so you never need to export it manually:

```
postgres://talkback:talkback@localhost:5432/talkback?sslmode=disable
```

If you set a Codespace secret named `DATABASE_URL`, it overrides this value (useful if you want to point at a remote database).

---

## Long-running sessions (epic runs and extended CI polling)

Epic runs can take 30–60+ minutes of CI polling. GitHub Codespaces will suspend after the configured idle timeout if no active connection exists — the timeout tracks **connection activity**, not process activity. If your SSH session drops (e.g. laptop goes to sleep), the inactivity clock starts even though your processes are still running.

Use the following technique to keep a Codespace alive indefinitely.

### Setup (one time)

1. **Set your idle timeout to 240 minutes** (the maximum) — GitHub → Settings → Codespaces → Default idle timeout.

2. **Create a free UptimeRobot account** at [uptimerobot.com](https://uptimerobot.com). The free tier supports 5-minute ping intervals and is enough.

### Before starting each epic run

```bash
# 1. Start a trivial HTTP server on port 3000 (already forwarded by the devcontainer).
#    The web dev server (npm run dev) is not running during epic runs, so port 3000 is free.
python3 -m http.server 3000 &

# 2. In the Codespace Ports tab: right-click port 3000 → Port Visibility → Public.
#    This produces a stable URL like:
#    https://<codespace-name>-3000.app.github.dev

# 3. In UptimeRobot: Add Monitor → HTTP(s) → paste the URL above → 5-minute interval.
#    UptimeRobot pings every 5 min; GitHub sees an active connection; Codespace never suspends.

# 4. Start a tmux session so the epic run survives SSH disconnection.
tmux new -s epic

# 5. Inside tmux: start the epic run.
#    claude "run epic SCRUM-XX"
```

To reconnect after closing your laptop:

```bash
ssh <your-codespace>         # or reopen in browser
tmux attach -t epic          # pick up exactly where the run left off
```

### Why this works

- UptimeRobot pings the public URL every 5 minutes → GitHub sees an active HTTP connection → inactivity timer resets.
- `tmux` keeps the terminal session alive on the Codespace host regardless of whether your SSH/browser connection is open.
- The 240-minute idle timeout is a belt-and-suspenders fallback if UptimeRobot ever misses a ping.

### Cleanup

When the epic run finishes, kill the HTTP server and remove the UptimeRobot monitor:

```bash
# Kill the background HTTP server
pkill -f "python3 -m http.server 3000"

# In the Ports tab: set port 3000 back to Private (or leave it — it serves nothing sensitive).
```

---

## Troubleshooting

### 1. `setup-mcp-config.sh` reports a missing key or writes a local-mode config

**Symptom:** The script output says `TALKBACK_MCP_URL not set` or `Mode: LOCAL stdio`.

**Cause:** `TALKBACK_MCP_URL` or `TALKBACK_MCP_API_KEY` is missing from Codespace secrets, or the secret was added after the Codespace was created (secrets are injected at container start, not on the fly).

**Fix:**
1. Verify the secret exists under GitHub → Settings → Codespaces → Secrets.
2. **Stop and restart the Codespace** (not just the terminal — the container must restart to pick up new secrets).
3. Re-run `./scripts/setup-mcp-config.sh` in the terminal.

---

### 2. Docker not available / `docker: command not found`

**Symptom:** `docker info` fails or `postCreateCommand` errors on `docker run`.

**Cause:** The Docker-in-Docker feature initialisation can occasionally be slow.

**Fix:**
1. Run `sudo service docker start` in the terminal.
2. Verify with `docker info`.
3. If the error persists, rebuild the Codespace (**Command Palette → Codespaces: Rebuild Container**).

---

### 3. MCP not loading in Claude Code / Cursor

**Symptom:** The IDE shows no MCP tools, or `health_check` fails.

**Cause:** `.mcp.json` is missing, stale, or the IDE was not restarted after it was written.

**Fix:**
1. Confirm `.mcp.json` exists: `cat .mcp.json` — it should contain a `talkback` entry with `npx mcp-remote`.
2. If missing, run `./scripts/setup-mcp-config.sh` (check for secret errors in the output).
3. Fully quit and reopen the IDE — a window reload is not enough; the MCP server process must restart.
4. If using Cursor, check **Settings → MCP** to confirm the server appears and is not in an error state.

---

## Further reading

- `docs/mcp-server.md` — full MCP server documentation including remote deployment, acting user IDs, and key mapping
- `.devcontainer/devcontainer.json` — devcontainer configuration (image, features, lifecycle commands)
- `scripts/setup-mcp-config.sh` — MCP config generation script (supports remote HTTP and local stdio modes)
