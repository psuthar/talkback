#!/usr/bin/env bash
# Read the release-readiness-core pin from ops/release-readiness/version.txt.
# Single source of truth referenced from CI workflows and local scripts so the
# package version, git SHA, and tag ref do not drift across the migration
# (see SCRUM-261 / SCRUM-262).
#
# Usage:
#   scripts/release_readiness_core_pin.sh [field]
#
# Fields:
#   version  PyPI version, e.g. 0.3.4 (default)
#   sha      immutable git SHA
#   ref      tag ref, e.g. v0.3.4 (use as @-suffix on composite actions)
#   spec     pip spec, e.g. release-readiness-core==0.3.4
#
# Override the pin file via PIN_FILE env var (e.g. for tests).
set -euo pipefail

PIN_FILE="${PIN_FILE:-ops/release-readiness/version.txt}"

if [[ ! -f "$PIN_FILE" ]]; then
  echo "release-readiness-core pin file not found: $PIN_FILE" >&2
  exit 2
fi

read_field() {
  awk -F= -v key="$1" '
    $0 ~ /^[[:space:]]*#/ { next }
    $0 ~ /^[[:space:]]*$/ { next }
    $1 == key { print $2; exit }
  ' "$PIN_FILE"
}

field="${1:-version}"
case "$field" in
  version) val=$(read_field version) ;;
  sha)     val=$(read_field sha) ;;
  ref)     val=$(read_field ref) ;;
  spec)
    v=$(read_field version)
    if [[ -z "$v" ]]; then
      echo "field 'version' missing in $PIN_FILE" >&2
      exit 1
    fi
    val="release-readiness-core==${v}"
    ;;
  -h|--help)
    grep -E '^#( |$)' "$0" | sed 's/^# \{0,1\}//'
    exit 0
    ;;
  *)
    echo "unknown field: $field (expected: version|sha|ref|spec)" >&2
    exit 2
    ;;
esac

if [[ -z "${val:-}" ]]; then
  echo "field '$field' missing in $PIN_FILE" >&2
  exit 1
fi

printf '%s\n' "$val"
