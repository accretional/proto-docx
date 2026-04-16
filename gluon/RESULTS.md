# gluon-vs-hand-written experiment: results

Goal: can `github.com/accretional/gluon/v2`'s EBNF-driven parser
(`metaparser.ParseCST`) parse DOCX `word/document.xml` well enough to
replace the hand-written recursive-descent parser in
`docxcodec/decode.go`?

Short answer: **partially, yes — but only after a custom tokeniser
preprocesses the XML**, because gluon's source lexer skips whitespace
between terminals, and XML text content is whitespace-sensitive.

The experiment was still valuable: it found a **latent bug in
`docxcodec.ParagraphCount`** (undercount on real-world DOCX) by driving
both parsers across the same fixture set and reporting disagreements.

## Approach

1. **Add `github.com/accretional/gluon` as a sibling-checkout
   dependency** — `go.mod` gains a `replace github.com/accretional/gluon
   => ../gluon` line. The v2 subtree is reachable as
   `github.com/accretional/gluon/v2/metaparser` without a separate
   module.
2. **Write a whitespace-separated token stream** from raw
   `word/document.xml` bytes. `tokenize.go` uses `encoding/xml` to emit
   a canonical form where:
     - `<`, `>`, `/` are standalone tokens
     - element names collapse to one of ten known literals (`w:p`,
       `w:r`, `w:t`, `w:ins`, `w:del`, `w:delText`, `w:br`, `w:tab`,
       `w:body`, `w:document`) or the single placeholder `N`
     - each attribute becomes one `A` (value discarded)
     - each non-empty text run becomes one `T` (characters discarded)
3. **Write an EBNF grammar (`xml.ebnf`) over that token vocabulary.**
   The modelled subset matches what `docxcodec/decode.go` already
   populates: paragraphs, runs, tracked-change wrappers, text leaves.
   Everything else collapses into a permissive `misc_pair` rule that
   recurses on its own contents (so tables, SDT, hyperlinks, drawings
   parse as opaque nests).
4. **Drive `ParseCST` against every DOCX under `data/`** and compare
   paragraph counts to `docxcodec.Decode`.

## Results

| Fixture                                   | ParseCST | Paragraphs (gluon / docxcodec) |
| ----------------------------------------- | -------- | ------------------------------ |
| `generated/01_minimal.docx`               | ok       | 1 / 1                          |
| `generated/02_multiple_paragraphs.docx`   | ok       | 5 / 5                          |
| `generated/03_with_image_png.docx`        | ok       | 1 / 1                          |
| `generated/04_with_image_jpeg.docx`       | ok       | 1 / 1                          |
| `generated/05_mixed_media.docx`           | ok       | 3 / 3                          |
| `generated/06_fonts.docx`                 | ok       | 1 / 1                          |
| `generated/07_tracked_changes.docx`       | ok       | 1 / 1                          |
| `generated/08_comments.docx`              | ok       | 1 / 1                          |
| `generated/09_footnotes_endnotes.docx`    | ok       | 1 / 1                          |
| `generated/10_headers_footers.docx`       | ok       | 1 / 1                          |
| `generated/11_kitchen_sink.docx`          | ok       | 2 / 2                          |
| `generated/12_tables.docx`                | ok       | 10 / 10                        |
| `DOCX_TestPage.docx` (real Word)          | ok       | **12 / 8 ← disagreement**      |
| `sample-word-document.docx` (real Word)   | ok       | 1 / 1                          |

Every fixture — including two real-world Word-authored DOCX files —
parses under the grammar. The paragraph count agrees on every
synthetic fixture. The only disagreement is on `DOCX_TestPage.docx`:
raw `<w:p` occurrences = 12, gluon AST paragraph nodes = 12,
`docxcodec.ParagraphCount` = 8. That's a docxcodec bug (four
paragraphs inside an unmodelled wrapper element are being silently
skipped during the count pass). Tracked as a follow-up; see
`../README.md` `## NEXT STEPS` once recorded.

## What worked

- **ISO-14977 EBNF is expressive enough for XML *structure*.** Open
  tag, close tag, attribute list, balanced open/close wrappers, and
  opaque recursion all encode cleanly.
- **gluon's prefix-match with backtracking handles terminal ambiguity.**
  `w:t` is a strict prefix of both `w:tab` and `w:delText`. When the
  parser tries `text` first on a `w:delText` opening, the `w:t`
  terminal prefix-matches, then the follow-up `attrs`/`>` terminals
  fail, and the parser backtracks cleanly into the `del_text`
  alternative. No hand-authored lookahead was needed.
- **The `misc_pair` catch-all is surprisingly powerful.** Making its
  body permissive (accepting `paragraph | run | tracked_ins |
  tracked_del | text | misc | "T"`) means unmodelled wrappers — table
  cells, hyperlinks, SDT, field chars — don't need dedicated grammar
  rules. Content under them still parses. This is where real-world
  Word documents fit that the hand-written parser would otherwise need
  cases for.
- **Build integration was cheap.** One `replace` directive, no
  codegen step. `go vet` + `go build` + `go test` all pass.

## What didn't (and why it matters)

- **Text content is lossy.** Collapsing arbitrary text runs to a single
  `T` token throws away the characters, the whitespace between them,
  and the distinction between `<w:t> </w:t>` (a space) and
  `<w:t>xml:space="preserve"</w:t>`. A real typed decoder needs to
  surface the text, so gluon-as-written can't replace
  `docxcodec/decode.go` — only *validate* its structural decisions.
  Recovering the bytes would require either (a) a custom
  `TokenMatcher` registered via `ASTParseOptions` (lexkit's
  escape-hatch for per-production tokenisation), or (b) post-processing
  the AST and re-reading source ranges from `ASTNode.Location.Offset` —
  both of which moot the "pure EBNF" framing.
- **Attribute values are lossy in the same way.** `w:val="..."` on
  `<w:rPr>` drives bold/italic/font rendering; the grammar sees only
  `A`. Same mitigations apply.
- **gluon's lexer skips whitespace before every terminal match.**
  (`lexkit/parse_ast.go:387` — `matchTerminal` starts with
  `ap.skipWSAndComments()`.) For XML content where whitespace is
  significant, that's a structural mismatch. Programming-language
  grammars get this free; markup grammars don't.
- **Prefix-matching is brittle under grammar growth.** Today the ten
  known element names compose cleanly because the grammar enumerates
  them; adding a new literal like `w:footnoteReference` would
  prefix-collide with `w:footnote`. Order-sensitive alternation
  catches the failure via backtracking, but the rule author has to
  keep that in mind. Real-keyword boundary checks only fire for
  all-alpha terminals (`lexkit/parse_ast.go:393`), and element names
  with colons/dots aren't all-alpha.
- **Start-rule-is-first-rule is load-bearing** — `ParseCST` always
  starts at `grammar.rules[0]`. Reordering `xml.ebnf` would silently
  change the entry point. Flagged rather than worked around.

## Comparison to `docxcodec/decode.go`

The hand-written parser is ~300 lines of recursive descent over
`encoding/xml` tokens. It:

- Preserves text content and attribute values directly into proto
  fields (`TextContent`, `DeletedText`, `TableProperties`, …).
- Counts paragraphs, images, fonts in one pass.
- Populates typed `Body.Content` nodes (Paragraph, Run, Table, etc.)
  with real values.
- Covers exactly the subset matching our proto schema — extension
  requires touching Go code, not grammar text.

The gluon approach as framed here:

- Gets structural recognition in ~80 lines of grammar + ~80 lines of
  Go tokeniser.
- Does **not** produce typed-proto output. It produces a generic
  `ASTNode` tree. Lowering it into proto messages is the same amount
  of work the hand-written parser already does inline.
- Extensions are cheap *if* they're structural (add a rule), expensive
  *if* they're semantic (add a token matcher / a lowerer / a new proto
  field). Most extensions we'd want are semantic.

**Verdict**: gluon is a solid *grammar sanity-check* for what
`docxcodec` ought to parse. It's not a drop-in replacement because
text and attribute values are load-bearing in OOXML and the current
gluon lexer model erases them. The right pattern is probably the one
this experiment stumbled into by accident: run both parsers in a
cross-check, treat disagreements as evidence of a bug somewhere.

## Surprising finding: `docxcodec.ParagraphCount` undercounts

`DOCX_TestPage.docx` contains 12 `<w:p>` elements (verified with
`grep -o '<w:p[ >/]'` on the raw XML). Gluon's AST finds 12
`paragraph` nodes. `docxcodec.Decode(...).ParagraphCount` returns 8.

Root cause (not fixed in this experiment, tracked as follow-up):
`parseDocumentXML` in `docxcodec/decode.go` walks the `<w:body>` tree
but doesn't descend into all unmodelled wrappers. Some real-world
DOCX files have paragraphs inside `w:sdtContent`, alternate-content
branches, or section-broken containers that the hand parser's walker
doesn't enter. Gluon's permissive `misc_child` catches them because
the grammar lets `paragraph` appear inside any `misc_pair`.

This is the experiment's most concrete output: a reproducible
discrepancy between two parsers on a real-world input, driven by a
three-line test that diffs `kinds["paragraph"]` against
`ParagraphCount`.

## Reproducing

```sh
cd /path/to/proto-docx
./build.sh        # runs setup.sh, fetches gluon via replace directive
go test -v ./gluon/
```

Test outputs:

- `TestTokenizeMinimal` — asserts the canonical token stream for
  `01_minimal.docx`.
- `TestParseCSTMinimal` — drives `ParseCST` against the minimal
  fixture and cross-checks against `docxcodec.Decode`.
- `TestParseCSTAcrossFixtures` — runs the full sweep; logs (does not
  fail) on paragraph count disagreement.

## Pipeline reference

```
word/document.xml bytes
        │
        ▼ Tokenize()                    (encoding/xml → space-separated tokens)
space-separated token stream
        │
        ▼ metaparser.WrapString()       (→ DocumentDescriptor)
DocumentDescriptor (source)
        │
xml.ebnf
        │
        ▼ metaparser.WrapString() + ParseEBNF()
GrammarDescriptor
        │
        ▼ metaparser.ParseCST()         (grammar + source → AST)
ASTDescriptor
        │
        ▼ countKinds(root)              (experiment-only: summary map)
```

Each arrow above corresponds to one line of driver code. The full
driver fits in `gluon_test.go`.
