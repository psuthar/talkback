#!/usr/bin/env bash
# Writes .cursor/mcp.json and .mcp.json for Cursor + Claude Code (same schema).
# Both paths are gitignored — safe to run locally. Uses python3 for correct JSON escaping.
#
# Transport modes
# ---------------
# Local stdio (default):
#   Run without TALKBACK_MCP_URL. The generated config launches the binary via
#   "go run ./cmd/talkback-mcp" in the local repo. Requires Go to be on PATH.
#
# Remote HTTP (Render.com or any hosted instance):
#   Export TALKBACK_MCP_URL before running this script. The generated config
#   uses a "url" entry (StreamableHTTP) instead of "command"/"args", so no
#   local Go process is needed. Requires TALKBACK_MCP_API_KEY to be the API
#   key accepted by the remote server.
#
#   Example:
#     export TALKBACK_MCP_URL=https://talkback-mcp.onrender.com
#     export TALKBACK_MCP_API_KEY=sk-your-secret-key
#     ./scripts/setup-mcp-config.sh
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
KEY="${TALKBACK_MCP_API_KEY:-$(openssl rand -hex 16)}"
export SETUP_ROOT="$ROOT"
export SETUP_KEY="$KEY"
export SETUP_MCP_URL="${TALKBACK_MCP_URL:-}"

python3 <<'PY'
import json, os
from pathlib import Path

root = Path(os.environ["SETUP_ROOT"])
key = os.environ["SETUP_KEY"]
mcp_url = os.environ.get("SETUP_MCP_URL", "").strip()

if mcp_url:
    # Remote HTTP mode (StreamableHTTP): embed the key as a ?key= query parameter.
    # This avoids the "headers" field, which Claude Code's MCP schema does not accept.
    # The server's HTTPBearerAuthMiddleware accepts ?key= as a fallback to the
    # Authorization: Bearer header, so both Cursor and Claude Code work with this URL.
    sep = "&" if "?" in mcp_url else "?"
    talkback_entry = {
        "url": f"{mcp_url}{sep}key={key}",
    }
else:
    # Local stdio mode (default): launch the binary via go run.
    talkback_env = {
        "TALKBACK_MCP_API_KEY": key,
        "TALKBACK_MCP_REQUIRE_CLIENT_KEY": "false",
    }
    # Optional: if set in the shell when you run this script, copy into MCP env so get_session_metadata works locally.
    db_url = os.environ.get("DATABASE_URL", "").strip()
    if db_url:
        talkback_env["DATABASE_URL"] = db_url
    acting = os.environ.get("TALKBACK_MCP_ACTING_USER_ID", "").strip()
    if acting:
        talkback_env["TALKBACK_MCP_ACTING_USER_ID"] = acting
    # Optional Phase 4 (SCRUM-70): per-client-key → TalkBack user UUID map; requires strict key mode in practice.
    key_user_map = os.environ.get("TALKBACK_MCP_KEY_USER_MAP_JSON", "").strip()
    if key_user_map:
        talkback_env["TALKBACK_MCP_KEY_USER_MAP_JSON"] = key_user_map
    talkback_entry = {
        "command": "go",
        "args": ["run", str(root / "cmd/talkback-mcp"), "-version=dev"],
        "env": talkback_env,
    }

servers = {
    "talkback": talkback_entry,
}

# GitHub MCP server (github/github-mcp-server via Docker).
# Uses docker run so mergeable_state is available in get_pull_request responses.
# Requires Docker and a GitHub PAT (repo scope).
# Accepts GITHUB_PERSONAL_ACCESS_TOKEN or GH_PERSONAL_ACCESS_TOKEN (Codespaces
# disallows secrets prefixed with "GITHUB_", so the latter is the Codespace-safe name).
github_pat = (
    os.environ.get("GITHUB_PERSONAL_ACCESS_TOKEN", "").strip()
    or os.environ.get("GH_PERSONAL_ACCESS_TOKEN", "").strip()
)
if github_pat:
    servers["github"] = {
        "command": "docker",
        "args": ["run", "-i", "--rm", "-e", "GITHUB_PERSONAL_ACCESS_TOKEN",
                 "ghcr.io/github/github-mcp-server"],
        "env": {"GITHUB_PERSONAL_ACCESS_TOKEN": github_pat},
    }

# Atlassian MCP server (Jira + Confluence via npx).
# Requires ATLASSIAN_DOMAIN, ATLASSIAN_EMAIL, ATLASSIAN_API_TOKEN.
atlassian_domain = os.environ.get("ATLASSIAN_DOMAIN", "").strip()
atlassian_email = os.environ.get("ATLASSIAN_EMAIL", "").strip()
atlassian_token = os.environ.get("ATLASSIAN_API_TOKEN", "").strip()
if atlassian_domain and atlassian_email and atlassian_token:
    servers["atlassian"] = {
        "command": "npx",
        "args": ["-y", "@xuandev/atlassian-mcp"],
        "env": {
            "ATLASSIAN_DOMAIN": atlassian_domain,
            "ATLASSIAN_EMAIL": atlassian_email,
            "ATLASSIAN_API_TOKEN": atlassian_token,
        },
    }

obj = {"mcpServers": servers}
text = json.dumps(obj, indent=2) + "\n"
(root / ".cursor").mkdir(parents=True, exist_ok=True)
(root / ".cursor" / "mcp.json").write_text(text, encoding="utf-8")
(root / ".mcp.json").write_text(text, encoding="utf-8")
print("Wrote:")
print(f"  {root / '.cursor' / 'mcp.json'}   (Cursor)")
print(f"  {root / '.mcp.json'}          (Claude Code project scope)")
print()
print(f"TALKBACK_MCP_API_KEY used: {key}")
print("(set TALKBACK_MCP_API_KEY before running this script to pin your own secret)")
print()
if mcp_url:
    print(f"Mode: REMOTE HTTP — talkback entry uses url: {mcp_url}?key=<key>")
    print("      Key embedded as ?key= query param (compatible with Claude Code + Cursor).")
    print("      No local Go process is required. Ensure the remote server is running.")
else:
    print("Mode: LOCAL stdio — talkback entry uses 'go run ./cmd/talkback-mcp'.")
    print("      Set TALKBACK_MCP_URL before running this script to switch to remote mode.")
    if not db_url and not acting and not key_user_map:
        print()
        print("Optional: export DATABASE_URL and TALKBACK_MCP_ACTING_USER_ID, then re-run this script to")
        print("         include them in the MCP env (enables get_session_metadata). See docs/mcp-server.md.")
    elif key_user_map and not db_url:
        print()
        print("Note: TALKBACK_MCP_KEY_USER_MAP_JSON was copied into MCP env (SCRUM-70). For session tools you")
        print("      still need DATABASE_URL; strict per-key mode typically sets TALKBACK_MCP_REQUIRE_CLIENT_KEY=true.")
print()
if not github_pat:
    print("GitHub MCP server NOT written — export GITHUB_PERSONAL_ACCESS_TOKEN or GH_PERSONAL_ACCESS_TOKEN (repo scope) and re-run.")
    print("  Requires Docker and ghcr.io/github/github-mcp-server image (docker pull ghcr.io/github/github-mcp-server).")
    print("  Note: Codespaces disallows secrets prefixed with GITHUB_ — use GH_PERSONAL_ACCESS_TOKEN there.")
    print()
if not (atlassian_domain and atlassian_email and atlassian_token):
    print("Atlassian MCP server NOT written — export ATLASSIAN_DOMAIN, ATLASSIAN_EMAIL, ATLASSIAN_API_TOKEN and re-run.")
    print()
print("Next: fully quit and reopen Cursor / Claude Code so MCP reloads.")
PY
