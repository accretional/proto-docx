#!/usr/bin/env bash
# setup.sh — install toolchain deps and generate Go code from .proto files.
# Idempotent: safe to run multiple times. Re-runs are skipped when outputs are up to date.

set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$ROOT"

log() { printf '\033[1;34m[setup]\033[0m %s\n' "$*"; }
warn() { printf '\033[1;33m[setup]\033[0m %s\n' "$*" >&2; }
die() { printf '\033[1;31m[setup]\033[0m %s\n' "$*" >&2; exit 1; }

# --- tool checks --------------------------------------------------------------

command -v go >/dev/null 2>&1 || die "go not found in PATH; install Go 1.26"
GO_VERSION="$(go env GOVERSION)"
case "$GO_VERSION" in
  go1.26*) : ;;
  *) die "Go 1.26 required, got $GO_VERSION" ;;
esac
log "go: $GO_VERSION"

command -v protoc >/dev/null 2>&1 || die "protoc not found in PATH; install protoc"
log "protoc: $(protoc --version)"

GOBIN="$(go env GOBIN)"
if [ -z "$GOBIN" ]; then
  GOBIN="$(go env GOPATH)/bin"
fi
export PATH="$GOBIN:$PATH"

if ! command -v protoc-gen-go >/dev/null 2>&1; then
  log "installing protoc-gen-go"
  go install google.golang.org/protobuf/cmd/protoc-gen-go@latest
fi
log "protoc-gen-go: $(command -v protoc-gen-go)"

# --- proto-xml sibling checkout ----------------------------------------------
# proto-docx depends on proto-xml for mime.proto (so the generated DOCX code
# can reuse the MimeType proto) and for the xmlcodec Go package.  Expect it
# at ../proto-xml — the replace directive in go.mod points there.

PROTO_XML_ROOT="$ROOT/../proto-xml"
if [ ! -d "$PROTO_XML_ROOT" ]; then
  die "proto-xml checkout not found at $PROTO_XML_ROOT — clone accretional/proto-xml as a sibling of this repo"
fi
log "proto-xml: $PROTO_XML_ROOT"

PROTO_XML_PROTO_DIR="$PROTO_XML_ROOT/proto"
if [ ! -f "$PROTO_XML_PROTO_DIR/openformat/v1/mime.proto" ]; then
  die "proto-xml does not contain mime.proto at $PROTO_XML_PROTO_DIR/openformat/v1/mime.proto"
fi

# --- proto codegen ------------------------------------------------------------

PROTO_SRC_DIR="$ROOT/proto"
DOCX_PROTO="openformat/v1/docx.proto"
GEN_DIR="$ROOT/gen/go/openformat/v1"
OUT_FILE="$GEN_DIR/docx.pb.go"

needs_regen=0
if [ ! -f "$OUT_FILE" ]; then
  needs_regen=1
elif [ "$PROTO_SRC_DIR/$DOCX_PROTO" -nt "$OUT_FILE" ]; then
  needs_regen=1
fi

if [ "$needs_regen" -eq 1 ]; then
  log "regenerating protobuf Go sources"
  mkdir -p "$GEN_DIR"
  rm -f "$GEN_DIR"/*.pb.go
  # Two --proto_path entries: our own proto/ for docx.proto, and proto-xml's
  # proto/ for the imported mime.proto.
  # --go_opt=M remaps the mime.proto import to proto-xml's generated Go package.
  protoc \
    --proto_path="$PROTO_SRC_DIR" \
    --proto_path="$PROTO_XML_PROTO_DIR" \
    --go_out="$ROOT" \
    --go_opt=module=openformat-docx \
    --go_opt=Mopenformat/v1/mime.proto=openformat/gen/go/openformat/v1 \
    --go_opt=Mopenformat/v1/docx.proto=openformat-docx/gen/go/openformat/v1 \
    "$PROTO_SRC_DIR/$DOCX_PROTO"
else
  log "proto outputs up to date"
fi

# --- go deps ------------------------------------------------------------------

log "resolving go dependencies"
go mod tidy

# --- generated DOCX fixtures --------------------------------------------------

FIXTURES_DIR="$ROOT/data/generated"
mkdir -p "$FIXTURES_DIR"
if [ -z "$(ls -A "$FIXTURES_DIR" 2>/dev/null)" ]; then
  log "generating DOCX fixtures into $FIXTURES_DIR"
  go run ./cmd/gen-fixtures -out "$FIXTURES_DIR"
else
  log "DOCX fixtures present"
fi

log "setup complete"
