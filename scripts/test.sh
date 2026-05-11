#!/usr/bin/env bash
# Unix/macOS test runner: ensure Postgres is up, set DATABASE_URL, run Go tests.
# Usage: ./scripts/test.sh [go test args...]
# Default package is ./...
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"

echo "Ensuring Postgres is up (docker compose; postgres service only — avoids binding :8080)..."
docker compose -f deploy/docker-compose.yml up -d postgres
sleep 2

export DATABASE_URL="${DATABASE_URL:-postgres://talkback:talkback@localhost:5432/talkback?sslmode=disable}"

# Default to full suite; pass -short to skip opt-in tests (when unset).
if [[ "$#" -eq 0 ]]; then
  set -- -p 1 ./...
fi

go test "$@"
