#!/usr/bin/env bash
# Mission 1 Local Auth Checkpoint — 7-step verification for TalkBack auth.
# Usage: ./scripts/auth_check.sh   or   make auth-check
# Requires: server running at TB_BASE_URL, Postgres available for steps 1 and 6 (optional for 4).

set -euo pipefail

# --- Configurable (env) ---
TB_BASE_URL="${TB_BASE_URL:-http://localhost:8080}"
TB_COOKIE_NAME="${TB_COOKIE_NAME:-tb_login}"
TB_TEST_EMAIL="${TB_TEST_EMAIL:-paresh+local@test.com}"
TB_TEST_PASSWORD="${TB_TEST_PASSWORD:-Test1234!}"
TB_TEST_DISPLAY="${TB_TEST_DISPLAY:-Paresh Local}"
TB_ADMIN_EMAIL="${TB_ADMIN_EMAIL:-paresh+admin@test.com}"
TB_ADMIN_PASSWORD="${TB_ADMIN_PASSWORD:-Admin1234!}"
TB_ADMIN_DISPLAY="${TB_ADMIN_DISPLAY:-Paresh Admin}"
TB_PSQL_DSN="${TB_PSQL_DSN:-}"
TB_EXPECT_UNAUTH_STATUS="${TB_EXPECT_UNAUTH_STATUS:-401}"  # or 403

# Load DATABASE_URL from .env if not set (for psql steps)
if [ -z "${DATABASE_URL:-}" ] && [ -f .env ]; then
  DATABASE_URL=$(grep '^DATABASE_URL=' .env 2>/dev/null | sed 's/^DATABASE_URL=//' | tr -d '\r' | head -1)
  [ -n "$DATABASE_URL" ] && export DATABASE_URL
fi
if [ -n "$TB_PSQL_DSN" ]; then
  PSQL_DSN="$TB_PSQL_DSN"
else
  PSQL_DSN="${DATABASE_URL:-}"
fi

# Temp files (cleaned on exit)
COOKIES1=""
COOKIES2=""
COOKIES_ADMIN=""
RESP_JSON=""
HEADERS_TXT=""
cleanup() {
  rm -f "$COOKIES1" "$COOKIES2" "$COOKIES_ADMIN" "$RESP_JSON" "$HEADERS_TXT"
}
trap cleanup EXIT
COOKIES1=$(mktemp)
COOKIES2=$(mktemp)
COOKIES_ADMIN=$(mktemp)
RESP_JSON=$(mktemp)
HEADERS_TXT=$(mktemp)

# --- Helpers ---
pass() { echo "  PASS: $1"; }
fail() { echo "  FAIL: $1"; STEP_FAILED=1; }

assert_http_status() {
  local expected=$1
  local actual=$2
  if [ "$actual" -eq "$expected" ]; then
    return 0
  fi
  fail "Expected HTTP $expected, got $actual"
  return 1
}

# Assert JSON file contains a value (jq filter or grep fallback). Usage: assert_json_contains <file> <jq_filter> <expected>
# Or for simple key: assert_json_contains file ".email" '"paresh+local@test.com"'
assert_json_contains() {
  local file=$1
  local filter=$2
  local expected=$3
  if command -v jq &>/dev/null; then
    local actual
    actual=$(jq -r "$filter" "$file" 2>/dev/null || echo "")
    if [ "$actual" = "$expected" ]; then
      return 0
    fi
    fail "JSON $filter: expected '$expected', got '$actual'"
    return 1
  fi
  if echo "$expected" | grep -qF "$(cat "$file")"; then
    return 0
  fi
  if grep -qF "$expected" "$file"; then
    return 0
  fi
  fail "JSON (no jq): expected to find '$expected' in response"
  return 1
}

# curl and return status code; body in RESP_JSON, headers in HEADERS_TXT
curl_status() {
  local status
  status=$(curl -s -o "$RESP_JSON" -w "%{http_code}" -D "$HEADERS_TXT" "$@")
  echo "$status"
}

check_set_cookie() {
  grep -qi "set-cookie:.*${TB_COOKIE_NAME}=" "$HEADERS_TXT" || return 1
}

# --- Step 1: DB tables exist ---
step1_db_tables() {
  echo ""
  echo "Step 1: DB tables exist (users, password_credentials, login_sessions)"
  STEP_FAILED=0
  if [ -z "$PSQL_DSN" ]; then
    fail "TB_PSQL_DSN or DATABASE_URL not set; cannot check DB. Set DATABASE_URL (e.g. from .env) or TB_PSQL_DSN."
    return 1
  fi
  if ! command -v psql &>/dev/null; then
    fail "psql not found; install PostgreSQL client to run step 1."
    return 1
  fi
  local out
  out=$(psql "$PSQL_DSN" -t -A -c "SELECT to_regclass('public.users');" 2>/dev/null || echo "null")
  if [ -z "$out" ] || [ "$out" = "null" ]; then
    fail "Table public.users not found"
  fi
  out=$(psql "$PSQL_DSN" -t -A -c "SELECT to_regclass('public.password_credentials');" 2>/dev/null || echo "null")
  if [ -z "$out" ] || [ "$out" = "null" ]; then
    fail "Table public.password_credentials not found"
  fi
  out=$(psql "$PSQL_DSN" -t -A -c "SELECT to_regclass('public.login_sessions');" 2>/dev/null || echo "null")
  if [ -z "$out" ] || [ "$out" = "null" ]; then
    fail "Table public.login_sessions not found"
  fi
  out=$(psql "$PSQL_DSN" -t -A -c "SELECT count(*) FROM pg_indexes WHERE schemaname='public' AND tablename='users' AND indexdef LIKE '%email%';" 2>/dev/null || echo "0")
  if [ "${out:-0}" -eq 0 ]; then
    fail "users email index not found"
  fi
  out=$(psql "$PSQL_DSN" -t -A -c "SELECT count(*) FROM pg_constraint WHERE conrelid='public.password_credentials'::regclass AND contype='p';" 2>/dev/null || echo "0")
  if [ "${out:-0}" -eq 0 ]; then
    fail "password_credentials PK not found"
  fi
  out=$(psql "$PSQL_DSN" -t -A -c "SELECT count(*) FROM pg_constraint WHERE conrelid='public.login_sessions'::regclass AND contype='f';" 2>/dev/null || echo "0")
  if [ "${out:-0}" -eq 0 ]; then
    fail "login_sessions FK not found"
  fi
  [ "$STEP_FAILED" -eq 0 ] && pass "All auth tables and constraints exist"
  return "$STEP_FAILED"
}

# --- Step 2: Server health + routes ---
step2_server_routes() {
  echo ""
  echo "Step 2: Server starts cleanly (health + auth routes)"
  STEP_FAILED=0
  local status
  status=$(curl_status -s -o /dev/null "$TB_BASE_URL/healthz")
  if [ "$status" != "200" ]; then
    fail "GET /healthz returned $status (expected 200). Is the server running at $TB_BASE_URL?"
    return 1
  fi
  pass "GET /healthz returned 200"

  for path in "/api/auth/signup" "/api/auth/login" "/api/auth/logout"; do
    status=$(curl_status -X OPTIONS "$TB_BASE_URL$path" 2>/dev/null)
    if [ "$status" != "200" ] && [ "$status" != "204" ]; then
      fail "OPTIONS $path returned $status"
    fi
  done
  status=$(curl_status -s -o /dev/null -b "" "$TB_BASE_URL/api/me" 2>/dev/null)
  if [ "$status" != "401" ]; then
    fail "GET /api/me without cookie returned $status (expected 401)"
  fi
  pass "Auth routes reachable"
  return 0
}

# --- Step 3: Signup sets cookie + /api/me works ---
step3_signup_me() {
  echo ""
  echo "Step 3: Signup sets cookie + /api/me works"
  STEP_FAILED=0
  local status
  status=$(curl_status -s -c "$COOKIES1" -D "$HEADERS_TXT" -X POST "$TB_BASE_URL/api/auth/signup" \
    -H "Content-Type: application/json" \
    -d "{\"email\":\"$TB_TEST_EMAIL\",\"password\":\"$TB_TEST_PASSWORD\",\"display_name\":\"$TB_TEST_DISPLAY\"}")
  if [ "$status" = "409" ]; then
    # User already exists; login instead to get cookie
    status=$(curl_status -s -c "$COOKIES1" -D "$HEADERS_TXT" -X POST "$TB_BASE_URL/api/auth/login" \
      -H "Content-Type: application/json" \
      -d "{\"email\":\"$TB_TEST_EMAIL\",\"password\":\"$TB_TEST_PASSWORD\"}")
  fi
  if [ "$status" != "201" ] && [ "$status" != "200" ]; then
    fail "Signup/login returned $status (expected 201 or 200)"
    return 1
  fi
  if ! check_set_cookie; then
    fail "Set-Cookie with $TB_COOKIE_NAME not found in response"
  fi
  status=$(curl_status -s -b "$COOKIES1" -o "$RESP_JSON" "$TB_BASE_URL/api/me")
  assert_http_status 200 "$status"
  assert_json_contains "$RESP_JSON" ".email" "$TB_TEST_EMAIL"
  assert_json_contains "$RESP_JSON" ".display_name" "$TB_TEST_DISPLAY"
  [ "$STEP_FAILED" -eq 0 ] && pass "Signup/login and /api/me OK"
  return "$STEP_FAILED"
}

# --- Step 4: Logout revokes session + /api/me fails ---
step4_logout_me_fails() {
  echo ""
  echo "Step 4: Logout revokes session + /api/me fails"
  STEP_FAILED=0
  local status
  status=$(curl_status -s -b "$COOKIES1" -c "$COOKIES1" -X POST "$TB_BASE_URL/api/auth/logout")
  assert_http_status 200 "$status"
  status=$(curl_status -s -b "$COOKIES1" -o "$RESP_JSON" "$TB_BASE_URL/api/me")
  if [ "$status" != "401" ] && [ "$status" != "403" ]; then
    fail "GET /api/me after logout returned $status (expected $TB_EXPECT_UNAUTH_STATUS or 403)"
  fi
  [ "$STEP_FAILED" -eq 0 ] && pass "Logout and /api/me returns unauth"
  return "$STEP_FAILED"
}

# --- Step 5: Login works + /api/me works ---
step5_login_me() {
  echo ""
  echo "Step 5: Login works + /api/me works"
  STEP_FAILED=0
  local status
  status=$(curl_status -s -c "$COOKIES2" -D "$HEADERS_TXT" -X POST "$TB_BASE_URL/api/auth/login" \
    -H "Content-Type: application/json" \
    -d "{\"email\":\"$TB_TEST_EMAIL\",\"password\":\"$TB_TEST_PASSWORD\"}")
  assert_http_status 200 "$status"
  if ! check_set_cookie; then
    fail "Set-Cookie with $TB_COOKIE_NAME not found after login"
  fi
  status=$(curl_status -s -b "$COOKIES2" -o "$RESP_JSON" "$TB_BASE_URL/api/me")
  assert_http_status 200 "$status"
  [ "$STEP_FAILED" -eq 0 ] && pass "Login and /api/me OK"
  return "$STEP_FAILED"
}

# --- Step 6: Disabled user is blocked ---
step6_disabled_blocked() {
  echo ""
  echo "Step 6: Disabled user is blocked"
  STEP_FAILED=0
  if [ -n "$PSQL_DSN" ] && command -v psql &>/dev/null; then
    psql "$PSQL_DSN" -t -c "UPDATE users SET status='disabled' WHERE lower(email)=lower('$TB_TEST_EMAIL');" &>/dev/null || true
  fi
  local status
  status=$(curl_status -s -b "$COOKIES2" -o "$RESP_JSON" "$TB_BASE_URL/api/me")
  if [ "$status" != "401" ] && [ "$status" != "403" ]; then
    fail "GET /api/me for disabled user returned $status (expected 401 or 403)"
  fi
  status=$(curl_status -s -X POST "$TB_BASE_URL/api/auth/login" -H "Content-Type: application/json" \
    -d "{\"email\":\"$TB_TEST_EMAIL\",\"password\":\"$TB_TEST_PASSWORD\"}")
  if [ "$status" != "401" ] && [ "$status" != "403" ]; then
    fail "POST /api/auth/login for disabled user returned $status (expected 401 or 403)"
  fi
  # Re-enable so future runs work
  if [ -n "$PSQL_DSN" ] && command -v psql &>/dev/null; then
    psql "$PSQL_DSN" -t -c "UPDATE users SET status='active' WHERE lower(email)=lower('$TB_TEST_EMAIL');" &>/dev/null || true
  fi
  [ "$STEP_FAILED" -eq 0 ] && pass "Disabled user blocked; re-enabled for idempotency"
  return "$STEP_FAILED"
}

# --- Step 7: Bootstrap admin works ---
step7_bootstrap_admin() {
  echo ""
  echo "Step 7: Bootstrap admin works"
  STEP_FAILED=0
  local status
  status=$(curl_status -s -c "$COOKIES_ADMIN" -D "$HEADERS_TXT" -X POST "$TB_BASE_URL/api/auth/signup" \
    -H "Content-Type: application/json" \
    -d "{\"email\":\"$TB_ADMIN_EMAIL\",\"password\":\"$TB_ADMIN_PASSWORD\",\"display_name\":\"$TB_ADMIN_DISPLAY\"}")
  if [ "$status" = "409" ]; then
    status=$(curl_status -s -c "$COOKIES_ADMIN" -D "$HEADERS_TXT" -X POST "$TB_BASE_URL/api/auth/login" \
      -H "Content-Type: application/json" \
      -d "{\"email\":\"$TB_ADMIN_EMAIL\",\"password\":\"$TB_ADMIN_PASSWORD\"}")
  fi
  if [ "$status" != "201" ] && [ "$status" != "200" ]; then
    fail "Admin signup/login returned $status"
    return 1
  fi
  status=$(curl_status -s -b "$COOKIES_ADMIN" -o "$RESP_JSON" "$TB_BASE_URL/api/me")
  assert_http_status 200 "$status"
  local role
  if command -v jq &>/dev/null; then
    role=$(jq -r '.global_role' "$RESP_JSON" 2>/dev/null || echo "")
  else
    role=$(grep -o '"global_role":"[^"]*"' "$RESP_JSON" 2>/dev/null | sed 's/"global_role":"\([^"]*\)"/\1/' || echo "")
  fi
  if [ "$role" != "admin" ]; then
    fail "Bootstrap admin: global_role is '$role', expected 'admin'. Restart server with TALKBACK_BOOTSTRAP_ADMIN_EMAIL=$TB_ADMIN_EMAIL and rerun."
    return 1
  fi
  pass "Bootstrap admin: global_role is admin"
  return 0
}

# --- Main ---
main() {
  echo "TalkBack Mission 1 — Local Auth Checkpoint"
  echo "Base URL: $TB_BASE_URL | Cookie: $TB_COOKIE_NAME"
  TOTAL_FAILED=0
  step1_db_tables || TOTAL_FAILED=$((TOTAL_FAILED+1))
  step2_server_routes || TOTAL_FAILED=$((TOTAL_FAILED+1))
  step3_signup_me || TOTAL_FAILED=$((TOTAL_FAILED+1))
  step4_logout_me_fails || TOTAL_FAILED=$((TOTAL_FAILED+1))
  step5_login_me || TOTAL_FAILED=$((TOTAL_FAILED+1))
  step6_disabled_blocked || TOTAL_FAILED=$((TOTAL_FAILED+1))
  step7_bootstrap_admin || TOTAL_FAILED=$((TOTAL_FAILED+1))

  echo ""
  if [ "$TOTAL_FAILED" -eq 0 ]; then
    echo "ALL CHECKS PASSED"
    exit 0
  else
    echo "CHECKS FAILED ($TOTAL_FAILED step(s))"
    exit 1
  fi
}
main
