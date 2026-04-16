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
	Paragraphs    []string // one <w:p> per string
	TrackedInsert string   // wrapped in <w:ins>, appended after Paragraphs
	TrackedDelete string   // wrapped in <w:del>
	Comments      []string // non-empty ⇒ comments.xml with <w:comment>s
	Footnotes     []string // non-empty ⇒ footnotes.xml with <w:footnote>s
	Endnotes      []string // non-empty ⇒ endnotes.xml with <w:endnote>s
	Headers       int      // number of header parts to emit
	Footers       int      // number of footer parts to emit
	Fonts         []string // non-empty ⇒ fontTable.xml with <w:font w:name="...">
	Images        []Image  // media parts under word/media/
	Sections      int      // extra w:sectPr breaks inside the body
	IncludeStyles bool     // emit an empty word/styles.xml stub
}

// Image is a single media part embedded under word/media/.
type Image struct {
	Name string // filename under word/media/ (e.g. "image1.png")
	Data []byte // raw bytes
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
	sb.WriteString(`<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:body>`)
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
	sb.WriteString(`<w:sectPr/></w:body></w:document>`)
	return sb.String()
}

func paragraph(text string) string {
	return `<w:p><w:r><w:t>` + escape(text) + `</w:t></w:r></w:p>`
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
