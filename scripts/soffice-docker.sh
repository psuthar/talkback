#!/usr/bin/env bash
# Run soffice via TalkBack Docker image (macOS/Linux). Pass-through all arguments.
# Requires: TALKBACK_UPLOAD_ROOT set to the same path as the API (repo root or project dir).
# Requires: docker image built: docker build -t talkback-api .
# Set in .env: TALKBACK_SOFFICE_CMD=<repo>/scripts/soffice-docker.sh
set -euo pipefail
root="${TALKBACK_UPLOAD_ROOT:-}"
if [[ -z "$root" ]]; then
  echo "TALKBACK_UPLOAD_ROOT must be set when using soffice-docker" >&2
  exit 1
fi
root="${root%/}"
args=()
for arg in "$@"; do
  a="${arg//$root//data}"
  a="${a//\\//}"
  args+=("$a")
done
exec docker run --rm --entrypoint soffice -v "${root}:/data" talkback-api "${args[@]}"
