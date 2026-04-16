#!/usr/bin/env bash
# LET_IT_RIP.sh — full-ratchet check: tests + fuzz smoke + benchmarks.
# Runs test.sh first (idempotent).

set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$ROOT"

log() { printf '\033[1;34m[LET_IT_RIP]\033[0m %s\n' "$*"; }
warn() { printf '\033[1;33m[LET_IT_RIP]\033[0m %s\n' "$*" >&2; }

"$ROOT/test.sh"

FUZZ_TIME="${FUZZ_TIME:-10s}"

log "go test -run=none -fuzz=FuzzDecode -fuzztime=$FUZZ_TIME ./testing/fuzz/..."
go test -run=none -fuzz=FuzzDecode -fuzztime="$FUZZ_TIME" ./testing/fuzz/...

log "go test -bench=. -benchtime=1x -run=^$ ./testing/benchmarks/..."
go test -bench=. -benchtime=1x -run='^$' ./testing/benchmarks/...

# Regenerate demo screenshots under ./screenshots/. We need a running
# chromerpc gRPC server on :50051 to capture real PNGs; if nothing is
# listening we launch one from the sibling ../chromerpc checkout and
# stop it before exiting. Without a server, demo-screenshots falls
# through to placeholder PNGs so the files at least exist on disk.
CHROMERPC_ADDR="${CHROMERPC_ADDR:-localhost:50051}"
CHROMERPC_PORT="${CHROMERPC_ADDR##*:}"
CHROMERPC_DIR="$ROOT/../chromerpc"
CHROMERPC_BIN="$CHROMERPC_DIR/bin/chromerpc"
CHROMERPC_PID=""

stop_chromerpc() {
  if [ -n "$CHROMERPC_PID" ] && kill -0 "$CHROMERPC_PID" 2>/dev/null; then
    log "stopping chromerpc (pid $CHROMERPC_PID)"
    kill "$CHROMERPC_PID" 2>/dev/null || true
    wait "$CHROMERPC_PID" 2>/dev/null || true
  fi
}
trap stop_chromerpc EXIT

port_open() {
  # Portable: try lsof first, fall back to bash /dev/tcp.
  if command -v lsof >/dev/null 2>&1; then
    lsof -iTCP:"$1" -sTCP:LISTEN >/dev/null 2>&1
  else
    (exec 3<>/dev/tcp/127.0.0.1/"$1") 2>/dev/null && exec 3<&- 3>&-
  fi
}

if port_open "$CHROMERPC_PORT"; then
  log "chromerpc already running on :$CHROMERPC_PORT"
else
  if [ ! -x "$CHROMERPC_BIN" ]; then
    log "building chromerpc at $CHROMERPC_DIR"
    (cd "$CHROMERPC_DIR" && go build -o bin/chromerpc ./cmd/chromerpc)
  fi
  CHROME_APP="/Applications/Google Chrome.app/Contents/MacOS/Google Chrome"
  log "starting chromerpc on :$CHROMERPC_PORT (headless)"
  "$CHROMERPC_BIN" -headless -addr ":$CHROMERPC_PORT" -chrome "$CHROME_APP" \
    >/tmp/proto-docx-chromerpc.log 2>&1 &
  CHROMERPC_PID=$!
  # Poll for readiness, up to ~15s.
  for _ in $(seq 1 30); do
    if port_open "$CHROMERPC_PORT"; then break; fi
    sleep 0.5
  done
  if ! port_open "$CHROMERPC_PORT"; then
    warn "chromerpc didn't start within 15s — see /tmp/proto-docx-chromerpc.log"
  fi
fi

log "CHROMERPC_ADDR=$CHROMERPC_ADDR go run ./cmd/demo-screenshots -force"
CHROMERPC_ADDR="$CHROMERPC_ADDR" go run ./cmd/demo-screenshots -force

log "all systems go"
