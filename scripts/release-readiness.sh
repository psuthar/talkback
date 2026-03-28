#!/usr/bin/env bash
# Run release readiness from repo root. Forwards all args to scripts/release_readiness.py.
# If the evaluator crashes before writing report.json, sets READINESS_FAILED=true in GITHUB_ENV
# (BLOCK outcomes still produce report.json — not marked as execution failure).
set -u
REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$REPO_ROOT" || exit 1
PYTHON="${PYTHON:-python3}"
"$PYTHON" scripts/release_readiness.py "$@"
EC=$?
if [[ "$EC" -ne 0 ]]; then
  if [[ ! -f artifacts/release-readiness/report.json ]]; then
    if [[ -n "${GITHUB_ENV:-}" ]]; then
      echo "READINESS_FAILED=true" >> "$GITHUB_ENV"
    fi
  fi
fi
exit "$EC"
