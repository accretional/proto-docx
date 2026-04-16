# AGENTS.md

`proto-docx` is the DOCX (OOXML WordprocessingML) codec for the
OpenFormat proto family. It is a sibling of `proto-xml` and depends
on it.

## Rules

- **Go toolchain:** `go1.26` (box has `go1.26.2`).
- **Build/test only via the scripts:** `./setup.sh`, `./build.sh`,
  `./test.sh`, `./LET_IT_RIP.sh`. Each wraps the previous and is
  idempotent. Do not run `go build ./...` / `go test ./...` directly
  for CI-style validation — use the scripts.
- **Before committing or pushing:** `./LET_IT_RIP.sh` must pass.
- **Commits:** match the style of existing commit messages (short
  summary, one-paragraph body explaining the *why*).
- **Never** commit vendored dependencies into `gen/` by hand — regen
  via `./setup.sh`.

## Layout

```
proto/openformat/v1/        vendored docx.proto (go_package modified for this module)
gen/go/openformat/v1/       generated Go (package openformatdocxv1)
docxcodec/                  public Decode/Encode — mirrors proto-xml's xmlcodec
internal/docxbuild/         in-memory DOCX package builder used by tests + gen-fixtures
cmd/gen-fixtures/           writes data/generated/*.docx
data/generated/             DOCX fixtures (programmatic)
testing/validation/         validation test across every fixture
testing/fuzz/               Go native fuzz tests
testing/benchmarks/         Decode/Encode/RoundTrip benchmarks
testing/README.md           test strategy + known discrepancies
docs/about.md               narrative with a worked example
review.md                   annotated review of docx.proto
```

## Dependency on proto-xml

`proto-docx` depends on `github.com/accretional/proto-xml` for:

1. **`openformat/xmlcodec`** — the XML byte↔proto codec used when a
   caller needs to introspect an individual OOXML part (e.g.
   `word/document.xml`) as a typed `XmlDocumentWithMetadata` rather
   than just counts.
2. **`openformat/gen/go/openformat/v1`** — the generated
   `openformatv1.MimeType` Go type, which `docx.proto` imports via
   `openformat/v1/mime.proto`. We redirect the generated import at
   `protoc` time via
   `--go_opt=Mopenformat/v1/mime.proto=openformat/gen/go/openformat/v1`.

The Go module system resolves the dependency through a `replace`
directive in `go.mod`:

```
replace openformat => ../proto-xml
```

This assumes `proto-xml` is checked out as a sibling of this repo.
`setup.sh` refuses to run if it isn't.

## Proto generation

`setup.sh` invokes `protoc` with two `--proto_path` entries (local
`proto/` + `../proto-xml/proto/`) and two `--go_opt=M…` remappings (one
for `mime.proto`, one for `docx.proto`). Re-read the script before
touching codegen — the exact flag combination is load-bearing.

## Scope

- Decode: full. Unzips the OOXML package, populates typed fields on
  `DocxDocumentWithMetadata`, preserves raw bytes.
- Encode: raw-bytes round-trip only. Structural re-emission of the
  ~4000-line OOXML schema is out of scope for this cut.
- Extraction (Text / Sections / Images / Fonts APIs from `mime-proto`)
  is not ported. See `## NEXT STEPS` in `README.md`.
