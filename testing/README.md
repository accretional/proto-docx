# testing

Three suites, layered on a shared fixture pool under `data/`.

```
validation/   one parametrized TestValidate over every .docx in data/
fuzz/         FuzzDecode — arbitrary inputs must never panic
benchmarks/   Decode / Encode / RoundTrip benchmarks
```

The build scripts wrap them:

- `./test.sh` runs `validation/` + any unit tests under `docxcodec/`.
- `./LET_IT_RIP.sh` runs `test.sh`, then a 10s fuzz (or whatever
  `FUZZ_TIME` is set to), then one iteration of every benchmark.

## Strategy

- **RawBytes round-trip is the correctness baseline.** Every fixture
  must satisfy `Encode(Decode(raw)) == raw`. Anything less breaks the
  contract.
- **Typed fields are spot-checked, not exhaustively asserted.** The
  validation test requires `Document`/`Body` to be populated (so we
  know `word/document.xml` was parsed) and verifies the raw-bytes
  invariant; per-fixture deeper assertions live in
  `docxcodec/codec_test.go`.
- **Fuzz exercises the ZIP + XML tokenizer layers.** Seeds are every
  fixture plus a handful of bare inputs (`nil`, "not a docx", stray PK
  magic). The property: `Decode` never panics, and if `Decode` accepts
  an input, `Encode(decode(out))` doesn't panic either.
- **Benchmarks are informational.** No per-op budget is enforced; the
  `LET_IT_RIP.sh` run is purely to catch "it regressed 100x"
  pathologies.

## Fixtures

All fixtures under `data/generated/` are built by
`cmd/gen-fixtures`. Each targets a specific OOXML feature:

| File | Exercises |
| --- | --- |
| `01_minimal.docx` | Single paragraph, bare package. |
| `02_multiple_paragraphs.docx` | Five paragraphs, styles stub. |
| `03_with_image_png.docx` | One PNG media part. |
| `04_with_image_jpeg.docx` | One JPEG media part. |
| `05_mixed_media.docx` | Three images (png / jpeg / png). |
| `06_fonts.docx` | `fontTable.xml` with four fonts. |
| `07_tracked_changes.docx` | `<w:ins>` + `<w:del>` (HasTrackedChanges). |
| `08_comments.docx` | `comments.xml` with two comments. |
| `09_footnotes_endnotes.docx` | `footnotes.xml` + `endnotes.xml`. |
| `10_headers_footers.docx` | `header1.xml` / `footer1.xml` etc. |
| `11_kitchen_sink.docx` | Combines every feature above. |
| `12_tables.docx` | Two `<w:tbl>` tables — one with `<w:tblStyle>` and an explicit `<w:tblGrid>`, one with inferred grid. |
| `13_hyperlinks_bookmarks.docx` | External `<w:hyperlink r:id>` with tooltip + `w:history`, internal `<w:hyperlink w:anchor>`, two `<w:bookmarkStart>`/`<w:bookmarkEnd>` pairs. |

Real-world DOCX files live at the top level of `data/`
(e.g. `data/DOCX_TestPage.docx`, `data/sample-word-document.docx`) and
are picked up by the walker automatically.

### Regenerating

```bash
rm -rf data/generated
./setup.sh
```

`setup.sh` only regenerates when the directory is empty, so deleting
is the trigger.

### Adding real-world DOCX files

`data/` is ready to accept any ZIP-packaged `.docx` — Word, LibreOffice,
Google Docs exports all work. Drop them into a new subdirectory (for
example `data/real/`) and the validation + fuzz + benchmark suites
pick them up automatically via `filepath.Walk`.

We don't ship real Word-generated files because:
1. Bloat — a blank Word doc is ~11 KB with ~18 XML parts and theme
   bytes.
2. Licensing opacity — shipped fonts, theme resources, and custom XML
   from arbitrary sources.

If you add any, note the provenance in a `data/real/README.md`.

## Known discrepancies and gaps

- **Structural encode is not implemented.** `Encode` returns
  `RawBytes` verbatim. Mutating a decoded proto and calling `Encode`
  silently returns the original bytes. A real structural encoder
  would need every OOXML schema element covered — see `review.md`.
- **Extraction APIs are not ported.** The mime-proto sibling exposes
  `Text`, `Sections`, `Images`, `Fonts` extraction; those depend on
  types from `openformat.proto` and `pdf_document.proto` which are
  not vendored here. If a user needs them, the port is mechanical —
  vendor the extra protos and copy `mime-proto/pb/internal/extract/docx/`.
- **Drawing / VML / OLE / chart / diagram content is opaque.** The
  proto models these as `bytes`. Round-trip-safe via `RawBytes` but
  not introspectable.
- **Individual XML parts are parsed with stdlib `encoding/xml` for
  counts only.** When a caller wants the full typed
  `XmlDocumentWithMetadata` for (say) `word/document.xml`, they can
  read it out of the ZIP manually and hand it to `proto-xml`'s
  `xmlcodec.Decode` — that wiring is *not* automatic yet.
- **DOCX version / conformance class detection.** `PdfVersion`-style
  fields are not populated; consumers interested in distinguishing
  transitional from strict OOXML have to inspect
  `Package.ContentTypes` themselves.

These are tracked in `../README.md` `## NEXT STEPS` and in `review.md`.
