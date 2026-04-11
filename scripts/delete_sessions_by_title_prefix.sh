#!/usr/bin/env bash
# Delete all sessions whose title starts with a given prefix (admin API).
# Usage:
#   ./scripts/delete_sessions_by_title_prefix.sh [PREFIX]
# Env (required for login):
#   TALKBACK_API_BASE   default http://localhost:8080
#   TALKBACK_ADMIN_EMAIL
#   TALKBACK_ADMIN_PASSWORD
#
# Example:
#   set -a && source .env && set +a
#   ./scripts/delete_sessions_by_title_prefix.sh E2E

set -euo pipefail

PREFIX="${1:-E2E}"
API="${TALKBACK_API_BASE:-http://localhost:8080}"
EMAIL="${TALKBACK_ADMIN_EMAIL:-${TALKBACK_BOOTSTRAP_ADMIN_EMAIL:?Set TALKBACK_ADMIN_EMAIL or TALKBACK_BOOTSTRAP_ADMIN_EMAIL}}"
PASS="${TALKBACK_ADMIN_PASSWORD:-${TALKBACK_BOOTSTRAP_ADMIN_PASSWORD:?Set TALKBACK_ADMIN_PASSWORD or TALKBACK_BOOTSTRAP_ADMIN_PASSWORD}}"

COOKIEJAR="$(mktemp)"
trap 'rm -f "$COOKIEJAR"' EXIT

LOGIN_CODE="$(curl -s -o /dev/null -w "%{http_code}" -c "$COOKIEJAR" -X POST "$API/api/auth/login" \
  -H "Content-Type: application/json" \
  -d "{\"email\":\"$EMAIL\",\"password\":\"$PASS\"}")"

if [[ "$LOGIN_CODE" != "200" ]]; then
  echo "Admin login failed (HTTP $LOGIN_CODE). Check TALKBACK_API_BASE and credentials." >&2
  exit 1
fi

JSON="$(curl -s -b "$COOKIEJAR" "$API/api/sessions")"
if ! echo "$JSON" | jq -e . >/dev/null 2>&1; then
  echo "Failed to parse session list as JSON." >&2
  exit 1
fi

IDS="$(echo "$JSON" | jq -r --arg p "$PREFIX" '.[] | select(.session.title | startswith($p)) | .session.id')"

if [[ -z "${IDS//[$'\n']/}" ]]; then
  echo "No sessions with title starting with \"$PREFIX\"."
  exit 0
fi

N=0
while IFS= read -r sid; do
  [[ -z "$sid" ]] && continue
  CODE="$(curl -s -o /dev/null -w "%{http_code}" -b "$COOKIEJAR" -X DELETE "$API/api/sessions/$sid")"
  if [[ "$CODE" == "204" ]] || [[ "$CODE" == "404" ]]; then
    echo "Deleted session $sid (HTTP $CODE)"
    N=$((N + 1))
  else
    echo "Failed to delete $sid (HTTP $CODE)" >&2
  fi
done <<< "$IDS"

echo "Done. Removed $N session(s) matching title prefix \"$PREFIX\"."
