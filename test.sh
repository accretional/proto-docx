#!/usr/bin/env bash
# test.sh — run unit tests + validation suite. Runs build.sh first (idempotent).

set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$ROOT"

log() { printf '\033[1;34m[test]\033[0m %s\n' "$*"; }

"$ROOT/build.sh"

log "go test ./..."
go test ./...

log "tests complete"
