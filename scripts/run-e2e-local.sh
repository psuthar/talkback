#!/usr/bin/env bash
# Start Postgres (Docker), TalkBack API (PORT=8080), Vite (3000), then run Playwright E2E tests.
# Usage (from repo root): ./scripts/run-e2e-local.sh [playwright args]
# Or: cd web && npm run test:e2e:local -- [playwright args]

set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"

# Source repo-root `.env` first (general API/Postgres config — matches what a
# locally-running `talkback-api` Docker container was bootstrapped from). When
# the script reuses an already-running API rather than starting a fresh one,
# this is what aligns the admin pre-flight + Playwright global-teardown with
# the running API's bootstrap admin (SCRUM-224).
if [ -f .env ]; then
  set -a
  # shellcheck disable=SC1091
  . ./.env
  set +a
fi

# Optional: `web/.env` (gitignored) — set TALKBACK_BOOTSTRAP_ADMIN_* and TALKBACK_ADMIN_* so API + Playwright use the same admin as your local API. Sourced after repo-root `.env` so explicit web/.env values win over repo-root defaults.
if [ -f web/.env ]; then
  set -a
  # shellcheck disable=SC1091
  . web/.env
  set +a
fi

API_PID=""
WEB_PID=""
STARTED_API=0
STARTED_WEB=0

cleanup() {
  set +e
  if [ "${STARTED_WEB:-0}" -eq 1 ] && [ -n "${WEB_PID:-}" ]; then
    kill "$WEB_PID" 2>/dev/null || true
    wait "$WEB_PID" 2>/dev/null || true
  fi
  if [ "${STARTED_API:-0}" -eq 1 ] && [ -n "${API_PID:-}" ]; then
    kill "$API_PID" 2>/dev/null || true
    wait "$API_PID" 2>/dev/null || true
  fi
}
trap cleanup EXIT INT TERM

if ! command -v docker >/dev/null 2>&1; then
  echo "run-e2e-local: docker not found. Install Docker or start Postgres and run the API yourself." >&2
  exit 1
fi

if ! docker info >/dev/null 2>&1; then
  echo "run-e2e-local: Docker daemon not reachable. Start Docker Desktop and retry." >&2
  exit 1
fi

docker compose -f deploy/docker-compose.yml up -d postgres

for i in $(seq 1 90); do
  if docker compose -f deploy/docker-compose.yml exec -T postgres pg_isready -U talkback >/dev/null 2>&1; then
    break
  fi
  if [ "$i" -eq 90 ]; then
    echo "run-e2e-local: Postgres did not become ready in time." >&2
    exit 1
  fi
  sleep 1
done

export DATABASE_URL="${DATABASE_URL:-postgres://talkback:talkback@127.0.0.1:5432/talkback?sslmode=disable}"
export PORT="${PORT:-8080}"
export RUN_MIGRATIONS="${RUN_MIGRATIONS:-true}"
export STORAGE_DRIVER="${STORAGE_DRIVER:-local}"
export ENCRYPTION_KEY="${ENCRYPTION_KEY:-ci-e2e-test-key-not-for-production}"
export ALLOW_DEV_RESET="${ALLOW_DEV_RESET:-false}"
export TALKBACK_BOOTSTRAP_ADMIN_EMAIL="${TALKBACK_BOOTSTRAP_ADMIN_EMAIL:-ci-admin@smoke.test}"
export TALKBACK_BOOTSTRAP_ADMIN_PASSWORD="${TALKBACK_BOOTSTRAP_ADMIN_PASSWORD:-SmokePass123!}"
export TALKBACK_BOOTSTRAP_ADMIN_DISPLAY_NAME="${TALKBACK_BOOTSTRAP_ADMIN_DISPLAY_NAME:-CI Admin}"

if curl -sf "http://127.0.0.1:${PORT}/health" >/dev/null 2>&1; then
  echo "run-e2e-local: reusing existing API at http://localhost:${PORT}"
  echo "run-e2e-local: note — Playwright global teardown logs in as admin; ensure TALKBACK_ADMIN_EMAIL/PASSWORD match that API's bootstrap admin (or set them in the environment)."
else
  go run ./cmd/api &
  API_PID=$!
  STARTED_API=1
  for i in $(seq 1 90); do
    if curl -sf "http://127.0.0.1:${PORT}/health" >/dev/null 2>&1; then
      break
    fi
    if [ "$i" -eq 90 ]; then
      echo "run-e2e-local: API did not become healthy (http://localhost:${PORT}/health)." >&2
      exit 1
    fi
    sleep 1
  done
fi

if curl -sf http://localhost:3000 >/dev/null 2>&1; then
  echo "run-e2e-local: reusing existing dev server at http://localhost:3000"
else
  (
    cd web
    npm run dev -- --host localhost --port 3000 --strictPort --open false
  ) &
  WEB_PID=$!
  STARTED_WEB=1
  for i in $(seq 1 90); do
    if curl -sf http://localhost:3000 >/dev/null 2>&1; then
      break
    fi
    if [ "$i" -eq 90 ]; then
      echo "run-e2e-local: Vite did not respond on http://localhost:3000." >&2
      exit 1
    fi
    sleep 1
  done
fi

export TALKBACK_API_BASE="http://localhost:${PORT}"
# Node 17+ resolves `localhost` to IPv6 ::1 first; the API binds IPv4 only, so without ipv4first
# Playwright's fixtures and global teardown hit ECONNREFUSED on ::1:8080. Prepending preserves any
# caller-set NODE_OPTIONS.
export NODE_OPTIONS="--dns-result-order=ipv4first ${NODE_OPTIONS:-}"
export TALKBACK_ADMIN_EMAIL="${TALKBACK_ADMIN_EMAIL:-$TALKBACK_BOOTSTRAP_ADMIN_EMAIL}"
export TALKBACK_ADMIN_PASSWORD="${TALKBACK_ADMIN_PASSWORD:-$TALKBACK_BOOTSTRAP_ADMIN_PASSWORD}"

# Pre-flight: verify admin credentials match the running API. Without this, Playwright global
# teardown (and any admin-scoped fixtures) silently retry/stall, which looks like a hang.
ADMIN_STATUS=$(curl -s -o /dev/null -w '%{http_code}' -X POST "${TALKBACK_API_BASE}/api/auth/login" \
  -H 'content-type: application/json' \
  --data "{\"email\":\"${TALKBACK_ADMIN_EMAIL}\",\"password\":\"${TALKBACK_ADMIN_PASSWORD}\"}" || echo "000")
if [ "$ADMIN_STATUS" != "200" ]; then
  cat >&2 <<EOF
run-e2e-local: admin login pre-flight failed (HTTP ${ADMIN_STATUS}) at ${TALKBACK_API_BASE}
  Tried email: ${TALKBACK_ADMIN_EMAIL}
  Source precedence (highest first): caller env (TALKBACK_ADMIN_EMAIL) → web/.env → repo-root .env → built-in default (ci-admin@smoke.test)

  This usually means the running TalkBack API was bootstrapped from creds the
  script did not see. Fix one of:
    1. Restart the API: stop the running process so this script can boot a fresh
       one with the bootstrap admin it is configured to use, then re-run.
    2. Match the running API: export TALKBACK_ADMIN_EMAIL and TALKBACK_ADMIN_PASSWORD
       to the creds the running API was bootstrapped with, then re-run.
EOF
  exit 1
fi

cd web
# Playwright 1.50+ uses chromium headless shell by default; install both like CI --with-deps browsers.
npx playwright install chromium chromium-headless-shell >/dev/null 2>&1 || true
# Default to the `list` reporter for interactive runs (real-time per-test progress) when the
# caller hasn't specified one. Matches playwright.config.ts 'line' fallback but streams line-by-line.
case " $* " in
  *" --reporter="*|*" --reporter "*) npx playwright test "$@" ;;
  *) npx playwright test --reporter=list "$@" ;;
esac
