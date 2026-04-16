# CLAUDE.md

See `AGENTS.md` — same rules apply. This file is specifically for
Claude Code sessions.

## Quick reference

- Go toolchain: `go1.26` (box has `go1.26.2`).
- Build/test ONLY via `./setup.sh`, `./build.sh`, `./test.sh`,
  `./LET_IT_RIP.sh`. Each wraps the previous. All idempotent.
- Never run `go test ./...` or `go build ./...` directly for CI-style
  validation — use the scripts.
- Before committing or pushing: `./LET_IT_RIP.sh` must pass.

## Proto

- Source: vendored at `proto/openformat/v1/docx.proto`. The copy
  diverges from upstream (`accretional/mime-proto`) in exactly one
  line: the `go_package` option targets
  `openformat-docx/gen/go/openformat/v1;openformatdocxv1` so DOCX
  types land in *this* module.
- Generated Go: `gen/go/openformat/v1/docx.pb.go` (package
  `openformatdocxv1`).
- MimeType is shared with proto-xml via
  `--go_opt=Mopenformat/v1/mime.proto=openformat/gen/go/openformat/v1`.
  Don't vendor `mime.proto` locally — setup.sh points `protoc` at
  proto-xml's copy on the include path.

## proto-xml dependency

- Expected at `../proto-xml`. `setup.sh` exits with an error if the
  checkout is missing.
- Wired into Go via `replace openformat => ../proto-xml` in `go.mod`.
- Used for: `openformat/xmlcodec` (when introspecting individual XML
  parts) and `openformat/gen/go/openformat/v1` (for `MimeType`).

## Code layout

- `docxcodec/` — public `Decode(raw []byte)` and `Encode(doc)` entry
  points. `Encode` today is a raw-bytes round-trip, matching
  proto-xml's `xmlcodec` contract when `RawBytes` is set.
- `internal/docxbuild/` — spec-driven in-memory DOCX builder. Used by
  `cmd/gen-fixtures` and tests. Internal so consumers don't start
  depending on its surface.
- `data/generated/` — DOCX fixtures written by `cmd/gen-fixtures`.
  `setup.sh` regenerates when the directory is empty.
- `testing/validation/` — one parametrized test over every fixture.
- `testing/fuzz/` — Go native fuzz tests (`FuzzDecode`).
- `testing/benchmarks/` — `BenchmarkDecode` / `BenchmarkEncode` /
  `BenchmarkRoundTrip`.

## Documentation outputs

- `review.md` — findings and oddities in `docx.proto`.
- `testing/README.md` — overall test strategy + known semantic gaps.
- `docs/about.md` — narrative with a worked example (kitchen-sink
  fixture) and screenshots via `github.com/accretional/chromerpc`
  (gRPC client against a chromerpc server at `localhost:50051`).
- `README.md` `## NEXT STEPS` — append findings: format quirks,
  missing functionality, bugs in upstream proto.

## Gotchas

- `setup.sh` regenerates `data/generated/` only when empty. If you
  change a fixture's shape, delete the old file (or the dir) before
  re-running.
- `cmd/gen-fixtures` is run via `go run`, so changes to it are picked
  up automatically — no separate build step.
- `proto-xml` must be a sibling checkout at `../proto-xml`. Changing
  the layout breaks `setup.sh`.
- The two `--go_opt=M` flags in `setup.sh` are load-bearing — removing
  either will put docx types in the wrong Go package or make the
  `mime.proto` import unresolvable.
- Structural re-emit is *not* implemented — `Encode` returns
  `RawBytes`. Mutating a decoded proto and expecting byte-different
  output from `Encode` will silently return the original bytes.
