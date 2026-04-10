#!/usr/bin/env bash
# Writes .cursor/mcp.json and .mcp.json for Cursor + Claude Code (same schema).
# Both paths are gitignored — safe to run locally. Uses python3 for correct JSON escaping.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
KEY="${TALKBACK_MCP_API_KEY:-$(openssl rand -hex 16)}"
export SETUP_ROOT="$ROOT"
export SETUP_KEY="$KEY"

python3 <<'PY'
import json, os
from pathlib import Path

root = Path(os.environ["SETUP_ROOT"])
key = os.environ["SETUP_KEY"]
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

servers = {
    "talkback": {
        "command": "go",
        "args": ["run", str(root / "cmd/talkback-mcp"), "-version=dev"],
        "env": talkback_env,
    }
}

# GitHub MCP server (github/github-mcp-server via Docker).
# Uses docker run so mergeable_state is available in get_pull_request responses.
# Requires Docker and GITHUB_PERSONAL_ACCESS_TOKEN (repo scope).
github_pat = os.environ.get("GITHUB_PERSONAL_ACCESS_TOKEN", "").strip()
if github_pat:
    servers["github"] = {
        "command": "docker",
        "args": ["run", "-i", "--rm", "-e", "GITHUB_PERSONAL_ACCESS_TOKEN",
                 "ghcr.io/github/github-mcp-server"],
        "env": {"GITHUB_PERSONAL_ACCESS_TOKEN": github_pat},
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
if not github_pat:
    print("GitHub MCP server NOT written — export GITHUB_PERSONAL_ACCESS_TOKEN (repo scope) and re-run.")
    print("  Requires Docker and ghcr.io/github/github-mcp-server image (docker pull ghcr.io/github/github-mcp-server).")
    print()
print("Next: fully quit and reopen Cursor / Claude Code so MCP reloads.")
if not db_url and not acting and not key_user_map:
    print()
    print("Optional: export DATABASE_URL and TALKBACK_MCP_ACTING_USER_ID, then re-run this script to")
    print("         include them in the MCP env (enables get_session_metadata). See docs/mcp-server.md.")
elif key_user_map and not db_url:
    print()
    print("Note: TALKBACK_MCP_KEY_USER_MAP_JSON was copied into MCP env (SCRUM-70). For session tools you")
    print("      still need DATABASE_URL; strict per-key mode typically sets TALKBACK_MCP_REQUIRE_CLIENT_KEY=true.")
PY
