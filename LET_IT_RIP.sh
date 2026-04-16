#!/usr/bin/env bash
# LET_IT_RIP.sh — full-ratchet check: tests + fuzz smoke + benchmarks.
# Runs test.sh first (idempotent).

set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$ROOT"

log() { printf '\033[1;34m[LET_IT_RIP]\033[0m %s\n' "$*"; }

"$ROOT/test.sh"

FUZZ_TIME="${FUZZ_TIME:-10s}"

log "go test -run=none -fuzz=FuzzDecode -fuzztime=$FUZZ_TIME ./testing/fuzz/..."
go test -run=none -fuzz=FuzzDecode -fuzztime="$FUZZ_TIME" ./testing/fuzz/...

log "go test -bench=. -benchtime=1x -run=^$ ./testing/benchmarks/..."
go test -bench=. -benchtime=1x -run='^$' ./testing/benchmarks/...

# Regenerate demo screenshots under ./screenshots/. With
# CHROMERPC_ADDR pointing at a running chromerpc server this renders
# real PNGs from the demo HTML; without one it falls through to
# placeholder PNGs so the files at least exist on disk. `-force`
# ensures every run picks up content changes.
log "go run ./cmd/demo-screenshots -force"
go run ./cmd/demo-screenshots -force

log "all systems go"
