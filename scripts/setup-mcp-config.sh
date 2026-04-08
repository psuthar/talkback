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
obj = {
    "mcpServers": {
        "talkback": {
            "command": "go",
            "args": ["run", str(root / "cmd/talkback-mcp"), "-version=dev"],
            "env": {
                "TALKBACK_MCP_API_KEY": key,
                "TALKBACK_MCP_REQUIRE_CLIENT_KEY": "false",
            },
        }
    }
}
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
print("Next: fully quit and reopen Cursor / Claude Code so MCP reloads.")
PY
