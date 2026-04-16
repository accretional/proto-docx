// Package docxbuild assembles in-memory DOCX packages for tests and
// fixture generation. It is intentionally small — just enough to drive
// docxcodec end-to-end without pulling in a full OOXML authoring stack.
package docxbuild

import (
	"archive/zip"
	"bytes"
	"fmt"
	"strings"
)

// Spec describes the contents of a synthetic DOCX package. All fields
// are optional; unset fields produce the minimum valid package.
type Spec struct {
	Paragraphs    []string    // one <w:p> per string
	TrackedInsert string      // wrapped in <w:ins>, appended after Paragraphs
	TrackedDelete string      // wrapped in <w:del>
	Comments      []string    // non-empty ⇒ comments.xml with <w:comment>s
	Footnotes     []string    // non-empty ⇒ footnotes.xml with <w:footnote>s
	Endnotes      []string    // non-empty ⇒ endnotes.xml with <w:endnote>s
	Headers       int         // number of header parts to emit
	Footers       int         // number of footer parts to emit
	Fonts         []string    // non-empty ⇒ fontTable.xml with <w:font w:name="...">
	Images        []Image     // media parts under word/media/
	Sections      int         // extra w:sectPr breaks inside the body
	IncludeStyles bool        // emit an empty word/styles.xml stub
	Tables        []Table     // each appended to the body after paragraphs
	Hyperlinks    []Hyperlink // each wrapped in its own <w:p>
	Bookmarks     []Bookmark  // each wrapped in its own <w:p>
	BlockSdts     []BlockSdt  // each emitted as a body-level <w:sdt>
	RunSdts       []RunSdt    // each emitted inside its own <w:p>
	Fields        []Field     // each emitted as a <w:p> with fldChar/instr sequence
}

// Image is a single media part embedded under word/media/.
type Image struct {
	Name string // filename under word/media/ (e.g. "image1.png")
	Data []byte // raw bytes
}

// Hyperlink is a <w:hyperlink> wrapper around a single run with the
// given Text. Either RelationshipID (for external links via r:id) or
// Anchor (for intra-document links) is typically set; both may be set.
// History defaults to true when a hyperlink is "visited"-tracked.
type Hyperlink struct {
	RelationshipID string // r:id attribute (optional)
	Anchor         string // w:anchor (optional)
	DocLocation    string // w:docLocation (optional)
	Tooltip        string // w:tooltip (optional)
	Text           string // run text inside the hyperlink
	History        bool   // w:history; emitted as "1" when true
}

// Bookmark renders a paragraph bracketed by <w:bookmarkStart>/
// <w:bookmarkEnd> markers wrapping one run with the given Text.
type Bookmark struct {
	ID   int32  // w:id on both start and end
	Name string // w:name on start
	Text string // run text inside the bookmark pair
}

// BlockSdt is a body-level <w:sdt> whose <w:sdtContent> holds one
// paragraph with the given Text. Alias / Tag / ID populate <w:sdtPr>.
type BlockSdt struct {
	Alias string // w:alias w:val
	Tag   string // w:tag w:val
	ID    int32  // w:id w:val
	Text  string // text of the single paragraph inside sdtContent
}

// RunSdt is a run-level <w:sdt> wrapped in its own <w:p>, whose
// <w:sdtContent> holds one run with the given Text.
type RunSdt struct {
	Alias string // w:alias w:val
	Tag   string // w:tag w:val
	ID    int32  // w:id w:val
	Text  string // run text inside sdtContent
}

// Field emits a paragraph carrying a classic OOXML field sequence:
//
//	<w:r><w:fldChar w:fldCharType="begin"/></w:r>
//	<w:r><w:instrText>Instr</w:instrText></w:r>
//	<w:r><w:fldChar w:fldCharType="separate"/></w:r>
//	<w:r><w:t>Cached</w:t></w:r>
//	<w:r><w:fldChar w:fldCharType="end"/></w:r>
//
// Instr is the field instruction (e.g. "PAGE \* MERGEFORMAT") and
// Cached is the rendered result (the value Word shows when the field
// isn't refreshed).
type Field struct {
	Instr  string // w:instrText content
	Cached string // displayed result between SEPARATE and END
	Dirty  bool   // w:dirty on the BEGIN fldChar
}

// Table is a rectangular table spec consumed by buildTable. Each cell
// renders as a single paragraph containing one run with the given text.
// GridCols, when non-empty, becomes the <w:tblGrid> column widths in
// DXA (twips); otherwise a uniform split of a 9000-twip table is
// emitted. StyleID populates <w:tblStyle>.
type Table struct {
	StyleID  string     // <w:tblStyle w:val="..."/>
	GridCols []int32    // per-column width in DXA (twips)
	Rows     [][]string // row of cell texts
}

// Build returns the bytes of the DOCX package described by s, or an
// error if the archive cannot be assembled.
func Build(s Spec) ([]byte, error) {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)

	add := func(name, content string) error {
		f, err := zw.Create(name)
		if err != nil {
			return fmt.Errorf("create %s: %w", name, err)
		}
		if _, err := f.Write([]byte(content)); err != nil {
			return fmt.Errorf("write %s: %w", name, err)
		}
		return nil
	}
	addBytes := func(name string, data []byte) error {
		f, err := zw.Create(name)
		if err != nil {
			return fmt.Errorf("create %s: %w", name, err)
		}
		if _, err := f.Write(data); err != nil {
			return fmt.Errorf("write %s: %w", name, err)
		}
		return nil
	}

	if err := add("[Content_Types].xml", buildContentTypes(s)); err != nil {
		return nil, err
	}
	if err := add("_rels/.rels", defaultRels); err != nil {
		return nil, err
	}
	if err := add("word/document.xml", buildDocument(s)); err != nil {
		return nil, err
	}

	if s.IncludeStyles {
		if err := add("word/styles.xml", stylesStub); err != nil {
			return nil, err
		}
	}
	if len(s.Fonts) > 0 {
		if err := add("word/fontTable.xml", buildFontTable(s.Fonts)); err != nil {
			return nil, err
		}
	}
	if len(s.Comments) > 0 {
		if err := add("word/comments.xml", buildComments(s.Comments)); err != nil {
			return nil, err
		}
	}
	if len(s.Footnotes) > 0 {
		if err := add("word/footnotes.xml", buildFootnotes(s.Footnotes)); err != nil {
			return nil, err
		}
	}
	if len(s.Endnotes) > 0 {
		if err := add("word/endnotes.xml", buildEndnotes(s.Endnotes)); err != nil {
			return nil, err
		}
	}
	for i := 1; i <= s.Headers; i++ {
		if err := add(fmt.Sprintf("word/header%d.xml", i), headerStub); err != nil {
			return nil, err
		}
	}
	for i := 1; i <= s.Footers; i++ {
		if err := add(fmt.Sprintf("word/footer%d.xml", i), footerStub); err != nil {
			return nil, err
		}
	}
	for _, img := range s.Images {
		if err := addBytes("word/media/"+img.Name, img.Data); err != nil {
			return nil, err
		}
	}

	if err := zw.Close(); err != nil {
		return nil, fmt.Errorf("close zip: %w", err)
	}
	return buf.Bytes(), nil
}

// ------------------ body builders -----------------------------------------

const defaultRels = `<?xml version="1.0" encoding="UTF-8"?>` +
	`<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">` +
	`<Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="word/document.xml"/>` +
	`</Relationships>`

const stylesStub = `<?xml version="1.0" encoding="UTF-8"?>` +
	`<w:styles xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"/>`

const headerStub = `<?xml version="1.0" encoding="UTF-8"?>` +
	`<w:hdr xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main">` +
	`<w:p><w:r><w:t>Header</w:t></w:r></w:p></w:hdr>`

const footerStub = `<?xml version="1.0" encoding="UTF-8"?>` +
	`<w:ftr xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main">` +
	`<w:p><w:r><w:t>Footer</w:t></w:r></w:p></w:ftr>`

func buildContentTypes(s Spec) string {
	var sb strings.Builder
	sb.WriteString(`<?xml version="1.0" encoding="UTF-8"?>`)
	sb.WriteString(`<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types">`)
	sb.WriteString(`<Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/>`)
	sb.WriteString(`<Default Extension="xml" ContentType="application/xml"/>`)
	for _, ext := range imageExtensions(s.Images) {
		sb.WriteString(`<Default Extension="` + ext + `" ContentType="` + mimeForExt(ext) + `"/>`)
	}
	sb.WriteString(`<Override PartName="/word/document.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.document.main+xml"/>`)
	if s.IncludeStyles {
		sb.WriteString(`<Override PartName="/word/styles.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.styles+xml"/>`)
	}
	if len(s.Fonts) > 0 {
		sb.WriteString(`<Override PartName="/word/fontTable.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.fontTable+xml"/>`)
	}
	if len(s.Comments) > 0 {
		sb.WriteString(`<Override PartName="/word/comments.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.comments+xml"/>`)
	}
	if len(s.Footnotes) > 0 {
		sb.WriteString(`<Override PartName="/word/footnotes.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.footnotes+xml"/>`)
	}
	if len(s.Endnotes) > 0 {
		sb.WriteString(`<Override PartName="/word/endnotes.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.endnotes+xml"/>`)
	}
	sb.WriteString(`</Types>`)
	return sb.String()
}

func buildDocument(s Spec) string {
	var sb strings.Builder
	sb.WriteString(`<?xml version="1.0" encoding="UTF-8"?>`)
	sb.WriteString(`<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships"><w:body>`)
	paragraphs := s.Paragraphs
	if len(paragraphs) == 0 {
		paragraphs = []string{"Hello DOCX"}
	}
	for i, text := range paragraphs {
		sb.WriteString(paragraph(text))
		if s.Sections > 0 && (i+1)*(s.Sections+1) < len(paragraphs)*s.Sections {
			// spread sectPr breaks roughly evenly
			if (i+1)%max(1, len(paragraphs)/(s.Sections+1)) == 0 {
				sb.WriteString(`<w:p><w:pPr><w:sectPr/></w:pPr></w:p>`)
			}
		}
	}
	if s.TrackedInsert != "" {
		sb.WriteString(`<w:p><w:ins w:id="1" w:author="t" w:date="2026-04-16T00:00:00Z"><w:r><w:t>` + escape(s.TrackedInsert) + `</w:t></w:r></w:ins></w:p>`)
	}
	if s.TrackedDelete != "" {
		sb.WriteString(`<w:p><w:del w:id="2" w:author="t" w:date="2026-04-16T00:00:00Z"><w:r><w:delText>` + escape(s.TrackedDelete) + `</w:delText></w:r></w:del></w:p>`)
	}
	for _, tbl := range s.Tables {
		sb.WriteString(buildTable(tbl))
	}
	for _, h := range s.Hyperlinks {
		sb.WriteString(buildHyperlinkParagraph(h))
	}
	for _, b := range s.Bookmarks {
		sb.WriteString(buildBookmarkParagraph(b))
	}
	for _, bsdt := range s.BlockSdts {
		sb.WriteString(buildBlockSdt(bsdt))
	}
	for _, rsdt := range s.RunSdts {
		sb.WriteString(buildRunSdt(rsdt))
	}
	for _, fld := range s.Fields {
		sb.WriteString(buildFieldParagraph(fld))
	}
	sb.WriteString(buildSectPr(s))
	sb.WriteString(`</w:body></w:document>`)
	return sb.String()
}

// buildSectPr emits the trailing <w:sectPr>. When the spec has headers
// or footers, sectPr is enriched with page dimensions, one-inch
// margins, and w:headerReference / w:footerReference entries — enough
// to exercise ExtractSections end-to-end. With no headers / footers,
// sectPr stays empty.
func buildSectPr(s Spec) string {
	if s.Headers == 0 && s.Footers == 0 {
		return `<w:sectPr/>`
	}
	hfRefTypes := []string{"default", "first", "even"}
	var sb strings.Builder
	sb.WriteString(`<w:sectPr>`)
	for i := 1; i <= s.Headers; i++ {
		t := hfRefTypes[(i-1)%len(hfRefTypes)]
		sb.WriteString(fmt.Sprintf(`<w:headerReference w:type=%q r:id=%q/>`, t, fmt.Sprintf("rIdH%d", i)))
	}
	for i := 1; i <= s.Footers; i++ {
		t := hfRefTypes[(i-1)%len(hfRefTypes)]
		sb.WriteString(fmt.Sprintf(`<w:footerReference w:type=%q r:id=%q/>`, t, fmt.Sprintf("rIdF%d", i)))
	}
	sb.WriteString(`<w:pgSz w:w="12240" w:h="15840" w:orient="portrait"/>`)
	sb.WriteString(`<w:pgMar w:top="1440" w:right="1800" w:bottom="1440" w:left="1800"/>`)
	sb.WriteString(`</w:sectPr>`)
	return sb.String()
}

func paragraph(text string) string {
	return `<w:p><w:r><w:t>` + escape(text) + `</w:t></w:r></w:p>`
}

// buildHyperlinkParagraph wraps a single run in <w:hyperlink>, emitting
// only the attributes set on h. The result is a complete <w:p>.
func buildHyperlinkParagraph(h Hyperlink) string {
	var attrs strings.Builder
	if h.RelationshipID != "" {
		attrs.WriteString(` r:id="` + escape(h.RelationshipID) + `"`)
	}
	if h.Anchor != "" {
		attrs.WriteString(` w:anchor="` + escape(h.Anchor) + `"`)
	}
	if h.DocLocation != "" {
		attrs.WriteString(` w:docLocation="` + escape(h.DocLocation) + `"`)
	}
	if h.Tooltip != "" {
		attrs.WriteString(` w:tooltip="` + escape(h.Tooltip) + `"`)
	}
	if h.History {
		attrs.WriteString(` w:history="1"`)
	}
	return `<w:p><w:hyperlink` + attrs.String() + `><w:r><w:t>` + escape(h.Text) + `</w:t></w:r></w:hyperlink></w:p>`
}

// buildBookmarkParagraph renders a paragraph with <w:bookmarkStart>,
// one run, and <w:bookmarkEnd> carrying the same w:id.
func buildBookmarkParagraph(b Bookmark) string {
	return fmt.Sprintf(
		`<w:p><w:bookmarkStart w:id="%d" w:name="%s"/><w:r><w:t>%s</w:t></w:r><w:bookmarkEnd w:id="%d"/></w:p>`,
		b.ID, escape(b.Name), escape(b.Text), b.ID,
	)
}

// sdtPrBlock emits <w:sdtPr> with the alias/tag/id attributes set.
func sdtPrBlock(alias, tag string, id int32) string {
	var sb strings.Builder
	sb.WriteString(`<w:sdtPr>`)
	if alias != "" {
		sb.WriteString(`<w:alias w:val="` + escape(alias) + `"/>`)
	}
	if tag != "" {
		sb.WriteString(`<w:tag w:val="` + escape(tag) + `"/>`)
	}
	if id != 0 {
		sb.WriteString(fmt.Sprintf(`<w:id w:val="%d"/>`, id))
	}
	sb.WriteString(`</w:sdtPr>`)
	return sb.String()
}

// buildBlockSdt emits a body-level <w:sdt> with one paragraph inside
// <w:sdtContent>.
func buildBlockSdt(b BlockSdt) string {
	return `<w:sdt>` + sdtPrBlock(b.Alias, b.Tag, b.ID) +
		`<w:sdtContent>` + paragraph(b.Text) + `</w:sdtContent></w:sdt>`
}

// buildRunSdt emits a paragraph containing a run-level <w:sdt> whose
// <w:sdtContent> wraps one run.
func buildRunSdt(r RunSdt) string {
	return `<w:p><w:sdt>` + sdtPrBlock(r.Alias, r.Tag, r.ID) +
		`<w:sdtContent><w:r><w:t>` + escape(r.Text) + `</w:t></w:r></w:sdtContent>` +
		`</w:sdt></w:p>`
}

// buildFieldParagraph emits a paragraph carrying a BEGIN/instr/
// SEPARATE/result/END field sequence. `f.Dirty` sets w:dirty="1" on
// the BEGIN fldChar.
func buildFieldParagraph(f Field) string {
	beginAttrs := ` w:fldCharType="begin"`
	if f.Dirty {
		beginAttrs += ` w:dirty="1"`
	}
	return `<w:p>` +
		`<w:r><w:fldChar` + beginAttrs + `/></w:r>` +
		`<w:r><w:instrText xml:space="preserve">` + escape(f.Instr) + `</w:instrText></w:r>` +
		`<w:r><w:fldChar w:fldCharType="separate"/></w:r>` +
		`<w:r><w:t>` + escape(f.Cached) + `</w:t></w:r>` +
		`<w:r><w:fldChar w:fldCharType="end"/></w:r>` +
		`</w:p>`
}

// buildTable emits a <w:tbl> for the given Table spec. If t.GridCols is
// empty the grid is inferred from the first row by uniformly splitting
// 9000 twips; if Rows is empty, nothing is emitted.
func buildTable(t Table) string {
	if len(t.Rows) == 0 {
		return ""
	}
	cols := t.GridCols
	if len(cols) == 0 {
		n := int32(len(t.Rows[0]))
		if n == 0 {
			return ""
		}
		w := int32(9000) / n
		cols = make([]int32, n)
		for i := range cols {
			cols[i] = w
		}
	}
	var sb strings.Builder
	sb.WriteString(`<w:tbl>`)
	sb.WriteString(`<w:tblPr>`)
	if t.StyleID != "" {
		sb.WriteString(`<w:tblStyle w:val="` + escape(t.StyleID) + `"/>`)
	}
	total := int32(0)
	for _, c := range cols {
		total += c
	}
	sb.WriteString(fmt.Sprintf(`<w:tblW w:w="%d" w:type="dxa"/>`, total))
	sb.WriteString(`</w:tblPr>`)
	sb.WriteString(`<w:tblGrid>`)
	for _, c := range cols {
		sb.WriteString(fmt.Sprintf(`<w:gridCol w:w="%d"/>`, c))
	}
	sb.WriteString(`</w:tblGrid>`)
	for _, row := range t.Rows {
		sb.WriteString(`<w:tr>`)
		for i, cell := range row {
			w := int32(0)
			if i < len(cols) {
				w = cols[i]
			}
			sb.WriteString(`<w:tc><w:tcPr>`)
			sb.WriteString(fmt.Sprintf(`<w:tcW w:w="%d" w:type="dxa"/>`, w))
			sb.WriteString(`</w:tcPr>`)
			sb.WriteString(paragraph(cell))
			sb.WriteString(`</w:tc>`)
		}
		sb.WriteString(`</w:tr>`)
	}
	sb.WriteString(`</w:tbl>`)
	return sb.String()
}

func buildFontTable(fonts []string) string {
	var sb strings.Builder
	sb.WriteString(`<?xml version="1.0" encoding="UTF-8"?>`)
	sb.WriteString(`<w:fonts xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main">`)
	for _, f := range fonts {
		sb.WriteString(`<w:font w:name="` + escape(f) + `"/>`)
	}
	sb.WriteString(`</w:fonts>`)
	return sb.String()
}

func buildComments(comments []string) string {
	var sb strings.Builder
	sb.WriteString(`<?xml version="1.0" encoding="UTF-8"?>`)
	sb.WriteString(`<w:comments xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main">`)
	for i, c := range comments {
		sb.WriteString(fmt.Sprintf(`<w:comment w:id="%d" w:author="t" w:date="2026-04-16T00:00:00Z"><w:p><w:r><w:t>%s</w:t></w:r></w:p></w:comment>`, i, escape(c)))
	}
	sb.WriteString(`</w:comments>`)
	return sb.String()
}

func buildFootnotes(notes []string) string {
	var sb strings.Builder
	sb.WriteString(`<?xml version="1.0" encoding="UTF-8"?>`)
	sb.WriteString(`<w:footnotes xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main">`)
	for i, n := range notes {
		sb.WriteString(fmt.Sprintf(`<w:footnote w:id="%d"><w:p><w:r><w:t>%s</w:t></w:r></w:p></w:footnote>`, i, escape(n)))
	}
	sb.WriteString(`</w:footnotes>`)
	return sb.String()
}

func buildEndnotes(notes []string) string {
	var sb strings.Builder
	sb.WriteString(`<?xml version="1.0" encoding="UTF-8"?>`)
	sb.WriteString(`<w:endnotes xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main">`)
	for i, n := range notes {
		sb.WriteString(fmt.Sprintf(`<w:endnote w:id="%d"><w:p><w:r><w:t>%s</w:t></w:r></w:p></w:endnote>`, i, escape(n)))
	}
	sb.WriteString(`</w:endnotes>`)
	return sb.String()
}

func imageExtensions(imgs []Image) []string {
	seen := map[string]bool{}
	var out []string
	for _, img := range imgs {
		i := strings.LastIndex(img.Name, ".")
		if i < 0 {
			continue
		}
		ext := strings.ToLower(img.Name[i+1:])
		if !seen[ext] {
			seen[ext] = true
			out = append(out, ext)
		}
	}
	return out
}

func mimeForExt(ext string) string {
	switch ext {
	case "png":
		return "image/png"
	case "jpg", "jpeg":
		return "image/jpeg"
	case "gif":
		return "image/gif"
	case "tiff", "tif":
		return "image/tiff"
	default:
		return "application/octet-stream"
	}
}

// escape does minimal XML escaping: & < > " '.
func escape(s string) string {
	r := strings.NewReplacer(
		"&", "&amp;",
		"<", "&lt;",
		">", "&gt;",
		`"`, "&quot;",
		"'", "&apos;",
	)
	return r.Replace(s)
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
