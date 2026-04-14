#!/usr/bin/env bash
# Start Postgres (Docker), TalkBack API (PORT=8080), Vite (3000), then run Playwright E2E tests.
# Usage (from repo root): ./scripts/run-e2e-local.sh [playwright args]
# Or: cd web && npm run test:e2e:local -- [playwright args]

set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"

# Optional: `web/.env` (gitignored) — set TALKBACK_BOOTSTRAP_ADMIN_* and TALKBACK_ADMIN_* so API + Playwright use the same admin as your local API.
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
export TALKBACK_ADMIN_EMAIL="${TALKBACK_ADMIN_EMAIL:-$TALKBACK_BOOTSTRAP_ADMIN_EMAIL}"
export TALKBACK_ADMIN_PASSWORD="${TALKBACK_ADMIN_PASSWORD:-$TALKBACK_BOOTSTRAP_ADMIN_PASSWORD}"

cd web
# Playwright 1.50+ uses chromium headless shell by default; install both like CI --with-deps browsers.
npx playwright install chromium chromium-headless-shell >/dev/null 2>&1 || true
npx playwright test "$@"
