# gluon experiment

An experiment: can
[`github.com/accretional/gluon/v2`](https://github.com/accretional/gluon)'s
EBNF-driven parser (`metaparser.ParseCST`) parse DOCX
`word/document.xml` well enough to replace the hand-written recursive
descent parser in `docxcodec/decode.go`?

Short answer: **not without a custom lex layer.** The gluon parser
skips whitespace between terminals (matched by literal prefix after
`skipWSAndComments`). That's load-bearing for programming languages —
where whitespace is insignificant between tokens — but fatal for XML,
because whitespace inside element content is data.

## Layout

- `xml.ebnf` — a small WordprocessingML-flavored XML grammar written
  against gluon's ISO 14977 EBNF dialect. Scoped to what the
  hand-written parser already populates: `<w:document>`, `<w:body>`,
  `<w:p>`, `<w:r>`, `<w:t>`, `<w:ins>`, `<w:del>`, `<w:delText>`,
  `<w:br>`, `<w:tab>`, plus self-closing variants.
- `gluon_test.go` — drives the pipeline (`WrapString` → `ParseEBNF` →
  `ParseCST`) against a tokenised sample, then walks the resulting AST
  and compares the observed structure to the output of
  `docxcodec.Decode`.
- `tokenize.go` — a whitespace-separated preprocessor that turns real
  `word/document.xml` bytes into a form gluon can chew on. The whole
  point of the experiment is to measure how much work this preprocessor
  has to do.

## Findings

See `RESULTS.md` — full write-up of the experience, the grammar design,
the 14-fixture cross-check, and the real-world bug it surfaced in
`docxcodec.ParagraphCount`.
