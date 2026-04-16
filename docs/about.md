# proto-docx — DOCX as a typed protobuf

`proto-docx` is a Go codec that converts between raw DOCX bytes (an
OOXML WordprocessingML package — ZIP of XML parts) and the
`openformat.v1.DocxDocumentWithMetadata` protobuf message defined in
[`accretional/mime-proto`](https://github.com/accretional/mime-proto).

It's the DOCX sibling of
[`proto-xml`](https://github.com/accretional/proto-xml), and uses it
directly for the shared `MimeType` type and (optionally) for parsing
individual OOXML parts as typed XML via `openformat/xmlcodec`.

## Why a proto wrapper?

DOCX is an OPC package — a ZIP containing `word/document.xml`, styles,
numbering, fonts, comments, footnotes, headers, media, and a fistful
of relationships files. Any tool that wants to audit "does this
document have tracked changes?" or "how many embedded fonts?" either
unzips and parses each time, or keeps an ad-hoc cache. A typed
protobuf means:

- a single `DocxDocumentWithMetadata` message carries all the headline
  facts (`ParagraphCount`, `ImageCount`, `FontCount`,
  `HasTrackedChanges`, `HasComments`, `HasNotes`) and the full
  `Package` tree underneath;
- the message can cross service boundaries via gRPC without each hop
  re-unzipping;
- `RawBytes` rides along so forwarding the original is a one-liner.

## A worked example: the kitchen-sink fixture

The fixture used below is `data/generated/11_kitchen_sink.docx` — a
minimal but realistic `.docx` that exercises every feature the codec
knows about (paragraphs, tracked insert + delete, a comment, a
footnote + endnote, a header, a footer, one PNG image, one JPEG
image, and an embedded font table).

```go
package main

import (
    "fmt"
    "os"

    "openformat-docx/docxcodec"
)

func main() {
    raw, _ := os.ReadFile("data/generated/11_kitchen_sink.docx")

    doc, err := docxcodec.Decode(raw)
    if err != nil {
        panic(err)
    }

    fmt.Println("paragraphs          :", doc.ParagraphCount)
    fmt.Println("images              :", doc.ImageCount)
    fmt.Println("fonts               :", doc.FontCount)
    fmt.Println("has tracked changes :", doc.HasTrackedChanges)
    fmt.Println("has comments        :", doc.HasComments)
    fmt.Println("has notes           :", doc.HasNotes)

    // Walk media parts.
    for _, m := range doc.DocxPackage.MediaParts {
        fmt.Printf("  media: %s (%s, %d bytes)\n",
            m.Filename, m.ContentType, len(m.Data))
    }

    // Byte-identical re-emit (forwarding case):
    out, _ := docxcodec.Encode(doc)
    _ = out // == raw
}
```

### Mutating + re-emit — today's contract

`Encode` today returns `RawBytes` verbatim. Structural re-emit of the
~4000-line OOXML schema is intentionally out of scope for this cut —
the codec's one guarantee is `Encode(Decode(raw)) == raw`, which every
fixture in `data/` verifies on each test run.

If a consumer needs to edit content and re-emit, the intended pattern
today is:

1. `docxcodec.Decode(raw)` — populate the typed fields (counts, flags,
   media parts).
2. Open `raw` as a ZIP directly, read `word/document.xml` (or whatever
   part you want to mutate), hand those bytes to `proto-xml`'s
   `xmlcodec.Decode`, mutate the typed tree, then re-emit via
   `xmlcodec.Encode`.
3. Splice the mutated part back into the ZIP and call
   `docxcodec.Encode` on a freshly-constructed
   `DocxDocumentWithMetadata{RawBytes: newZip}` — or just write `newZip`
   to disk directly. Both paths are byte-faithful.

Wiring step 2 through the DOCX codec automatically (so callers get
`doc.Document` as a typed `XmlDocumentWithMetadata` without manual
unzipping) is a natural follow-up — see `README.md` `## NEXT STEPS`.

## Encoder / decoder contract

| Concern                                   | Decode preserves it? | Encode preserves it? |
| ----------------------------------------- | -------------------- | -------------------- |
| Every ZIP entry (parts + relationships)   | yes (via RawBytes)   | yes (byte-identical) |
| `[Content_Types].xml`                     | yes                  | yes                  |
| `word/document.xml` text                  | yes (paragraph text) | yes                  |
| Tracked changes `<w:ins>` / `<w:del>`     | flag only            | yes                  |
| Comments / footnotes / endnotes           | flag only            | yes                  |
| Headers / footers                         | yes (as parts)       | yes                  |
| Media parts (PNG, JPEG, …)                | yes (typed)          | yes                  |
| Font table                                | count + presence     | yes                  |
| DrawingML / VML / OLE / theme / charts    | opaque bytes         | yes                  |
| Digital signatures (`_xmlsignatures/`)    | opaque bytes         | yes                  |
| Structural mutation of decoded fields     | —                    | **not implemented**  |

## Demo screenshots

The three images below are produced by `cmd/demo-screenshots`, which
writes HTML viewer pages under `docs/screenshots/_html/` and hands them
to a [`chromerpc`](https://github.com/accretional/chromerpc) gRPC
server (by default `localhost:50051`) for capture. When no server is
reachable, the command falls back to placeholder PNGs so the
documentation doesn't show broken images.

Regenerate real captures against a running chromerpc server:

```sh
CHROMERPC_ADDR=localhost:50051 go run ./cmd/demo-screenshots -force
```

![Kitchen-sink DOCX rendered](screenshots/docx-rendered.png)

*Figure 1.* The kitchen-sink fixture's extracted paragraphs rendered
as HTML. Tracked-change markup survives the round-trip: the italic
sentence is `<w:ins>`-wrapped, the strikethrough is `<w:del>`.

![Decoded DocxDocumentWithMetadata](screenshots/docx-decoded.png)

*Figure 2.* The same fixture after `docxcodec.Decode`, summarised as
JSON. Paragraph / image / font counts and the `HasTrackedChanges` /
`HasComments` / `HasNotes` flags are populated directly on
`DocxDocumentWithMetadata`; `raw_bytes` carries the original 9 KB
package so `Encode` is byte-faithful.

![OPC package parts](screenshots/docx-parts.png)

*Figure 3.* The OPC container view — every ZIP entry in the fixture:
`[Content_Types].xml`, the top-level and per-directory `_rels`,
`word/document.xml` plus the auxiliary parts (`styles.xml`,
`fontTable.xml`, `comments.xml`, `footnotes.xml`, `endnotes.xml`,
`header1.xml`, `footer1.xml`), and the two media files under
`word/media/`.

## Where to go next

- `docxcodec/` — the codec itself (`Decode`, `Encode`, `IsDocx`).
- `internal/docxbuild/` — the in-memory DOCX builder that backs
  `cmd/gen-fixtures` and the codec tests.
- `proto/openformat/v1/docx.proto` — the vendored schema (312
  messages, 90 enums).
- `testing/README.md` — full test strategy, fixture table, and the
  list of known semantic gaps.
- `review.md` — annotated review of `docx.proto` issues and
  related-format follow-ons.
- `README.md` `## NEXT STEPS` — running findings about the format,
  the codec, and the OpenFormat proto family at large.
