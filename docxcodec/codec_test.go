package docxcodec

import (
	"archive/zip"
	"bytes"
	"testing"

	xmlpb "openformat/gen/go/openformat/v1"

	pb "openformat-docx/gen/go/openformat/v1"
)

// minimalDocx builds the smallest syntactically valid DOCX package in
// memory: [Content_Types].xml, _rels/.rels, and word/document.xml with
// one paragraph.
func minimalDocx(t *testing.T) []byte {
	t.Helper()

	var buf bytes.Buffer
	w := zip.NewWriter(&buf)

	add := func(name, content string) {
		f, err := w.Create(name)
		if err != nil {
			t.Fatalf("create %s: %v", name, err)
		}
		if _, err := f.Write([]byte(content)); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}

	add("[Content_Types].xml",
		`<?xml version="1.0" encoding="UTF-8"?>`+
			`<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types">`+
			`<Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/>`+
			`<Override PartName="/word/document.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.document.main+xml"/>`+
			`</Types>`)

	add("_rels/.rels",
		`<?xml version="1.0" encoding="UTF-8"?>`+
			`<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">`+
			`<Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="word/document.xml"/>`+
			`</Relationships>`)

	add("word/document.xml",
		`<?xml version="1.0" encoding="UTF-8"?>`+
			`<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main">`+
			`<w:body><w:p><w:r><w:t>Hello DOCX</w:t></w:r></w:p></w:body>`+
			`</w:document>`)

	if err := w.Close(); err != nil {
		t.Fatalf("close zip: %v", err)
	}
	return buf.Bytes()
}

func TestIsDocx(t *testing.T) {
	if !IsDocx(minimalDocx(t)) {
		t.Error("IsDocx(minimalDocx) = false, want true")
	}
	if IsDocx([]byte("not a docx")) {
		t.Error("IsDocx(text) = true, want false")
	}
	if IsDocx(nil) {
		t.Error("IsDocx(nil) = true, want false")
	}
}

func TestDecodeMinimal(t *testing.T) {
	raw := minimalDocx(t)
	doc, err := Decode(raw)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if got, want := doc.ParagraphCount, int32(1); got != want {
		t.Errorf("ParagraphCount = %d, want %d", got, want)
	}
	if doc.DocxPackage == nil {
		t.Fatal("DocxPackage nil")
	}
	if doc.DocxPackage.Document == nil || doc.DocxPackage.Document.Body == nil {
		t.Error("Document/Body not populated")
	}
	if doc.DocxPackage.ContentTypes == nil || len(doc.DocxPackage.ContentTypes.Overrides) != 1 {
		t.Errorf("ContentTypes.Overrides = %v, want 1 override", doc.DocxPackage.ContentTypes)
	}
	if len(doc.DocxPackage.PackageRelationships) != 1 {
		t.Errorf("PackageRelationships = %d, want 1", len(doc.DocxPackage.PackageRelationships))
	}
	if !bytes.Equal(doc.RawBytes, raw) {
		t.Error("RawBytes not preserved verbatim")
	}
}

func TestDecodeEmpty(t *testing.T) {
	if _, err := Decode(nil); err == nil {
		t.Error("Decode(nil) err = nil, want error")
	}
	if _, err := Decode([]byte("not a zip")); err == nil {
		t.Error("Decode(garbage) err = nil, want error")
	}
}

func TestEncodeRoundTrip(t *testing.T) {
	raw := minimalDocx(t)
	doc, err := Decode(raw)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	out, err := Encode(doc)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	if !bytes.Equal(out, raw) {
		t.Errorf("Encode round-trip differs: got %d bytes, want %d", len(out), len(raw))
	}
}

func TestEncodeRejectsMissingRawBytes(t *testing.T) {
	if _, err := Encode(nil); err == nil {
		t.Error("Encode(nil) err = nil, want error")
	}
	if _, err := Encode(&pb.DocxDocumentWithMetadata{}); err == nil {
		t.Error("Encode(empty) err = nil, want error")
	}
}

func TestDecodeMediaAndFonts(t *testing.T) {
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)

	mustCreate := func(name string, data []byte) {
		f, err := w.Create(name)
		if err != nil {
			t.Fatalf("create %s: %v", name, err)
		}
		if _, err := f.Write(data); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}

	mustCreate("word/document.xml", []byte(
		`<?xml version="1.0"?><w:document xmlns:w="http://w"><w:body>`+
			`<w:p/><w:p/><w:p/>`+
			`</w:body></w:document>`))
	mustCreate("word/media/image1.png", []byte{0x89, 'P', 'N', 'G', 0, 0, 0, 0})
	mustCreate("word/media/image2.jpeg", []byte{0xFF, 0xD8, 0xFF, 0xE0})
	mustCreate("word/fontTable.xml", []byte(
		`<?xml version="1.0"?><w:fonts xmlns:w="http://w">`+
			`<w:font w:name="Calibri"/><w:font w:name="Arial"/>`+
			`</w:fonts>`))
	if err := w.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	doc, err := Decode(buf.Bytes())
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if got := doc.ParagraphCount; got != 3 {
		t.Errorf("ParagraphCount = %d, want 3", got)
	}
	if got := doc.ImageCount; got != 2 {
		t.Errorf("ImageCount = %d, want 2", got)
	}
	if got := doc.FontCount; got != 2 {
		t.Errorf("FontCount = %d, want 2", got)
	}
	if len(doc.DocxPackage.MediaParts) != 2 {
		t.Errorf("MediaParts len = %d, want 2", len(doc.DocxPackage.MediaParts))
	}
	for _, mp := range doc.DocxPackage.MediaParts {
		if mp.Filename == "" || mp.ContentType == "" {
			t.Errorf("MediaPart missing fields: %+v", mp)
		}
	}
}

func TestDecodeWithTypedPartsOff(t *testing.T) {
	raw := minimalDocx(t)
	d, err := DecodeWith(raw, DecodeOptions{})
	if err != nil {
		t.Fatalf("DecodeWith: %v", err)
	}
	if d.DocxDocumentWithMetadata == nil {
		t.Fatal("embedded DocxDocumentWithMetadata nil")
	}
	if d.Document != nil {
		t.Error("Document populated despite IncludeTypedParts=false")
	}
	if !bytes.Equal(d.RawBytes, raw) {
		t.Error("RawBytes not preserved through DecodeWith")
	}
}

func TestDecodeWithTypedPartsOn(t *testing.T) {
	raw := minimalDocx(t)
	d, err := DecodeWith(raw, DecodeOptions{IncludeTypedParts: true})
	if err != nil {
		t.Fatalf("DecodeWith: %v", err)
	}
	if d.Document == nil {
		t.Fatal("Document nil with IncludeTypedParts=true")
	}
	if d.Document.Document == nil || d.Document.Document.DocumentElement == nil {
		t.Fatal("typed XML has no root element")
	}
	if got, want := d.Document.Document.DocumentElement.LocalName, "document"; got != want {
		t.Errorf("root element local name = %q, want %q", got, want)
	}
	if len(d.Document.RawBytes) == 0 {
		t.Error("typed XmlDocumentWithMetadata has no RawBytes")
	}
}

func TestDecodeWithAllTypedParts(t *testing.T) {
	// Build a package with every typed-parts-eligible file populated.
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)

	parts := map[string]string{
		"word/document.xml":  `<?xml version="1.0"?><w:document xmlns:w="http://w"><w:body><w:p><w:r><w:t>body</w:t></w:r></w:p></w:body></w:document>`,
		"word/styles.xml":    `<?xml version="1.0"?><w:styles xmlns:w="http://w"/>`,
		"word/numbering.xml": `<?xml version="1.0"?><w:numbering xmlns:w="http://w"/>`,
		"word/settings.xml":  `<?xml version="1.0"?><w:settings xmlns:w="http://w"/>`,
		"word/fontTable.xml": `<?xml version="1.0"?><w:fonts xmlns:w="http://w"><w:font w:name="X"/></w:fonts>`,
		"word/comments.xml":  `<?xml version="1.0"?><w:comments xmlns:w="http://w"><w:comment/></w:comments>`,
		"word/footnotes.xml": `<?xml version="1.0"?><w:footnotes xmlns:w="http://w"><w:footnote/></w:footnotes>`,
		"word/endnotes.xml":  `<?xml version="1.0"?><w:endnotes xmlns:w="http://w"><w:endnote/></w:endnotes>`,
		"word/header1.xml":   `<?xml version="1.0"?><w:hdr xmlns:w="http://w"><w:p/></w:hdr>`,
		"word/header2.xml":   `<?xml version="1.0"?><w:hdr xmlns:w="http://w"><w:p/></w:hdr>`,
		"word/footer1.xml":   `<?xml version="1.0"?><w:ftr xmlns:w="http://w"><w:p/></w:ftr>`,
	}
	for name, body := range parts {
		f, _ := w.Create(name)
		_, _ = f.Write([]byte(body))
	}
	_ = w.Close()

	d, err := DecodeWith(buf.Bytes(), DecodeOptions{IncludeTypedParts: true})
	if err != nil {
		t.Fatalf("DecodeWith: %v", err)
	}

	singulars := map[string]*xmlpb.XmlDocumentWithMetadata{
		"Document":  d.Document,
		"Styles":    d.Styles,
		"Numbering": d.Numbering,
		"Settings":  d.Settings,
		"FontTable": d.FontTable,
		"Comments":  d.Comments,
		"Footnotes": d.Footnotes,
		"Endnotes":  d.Endnotes,
	}
	for name, meta := range singulars {
		if meta == nil || meta.Document == nil || meta.Document.DocumentElement == nil {
			t.Errorf("%s not fully parsed", name)
		}
	}

	if len(d.Headers) != 2 {
		t.Errorf("Headers len = %d, want 2", len(d.Headers))
	}
	if len(d.Footers) != 1 {
		t.Errorf("Footers len = %d, want 1", len(d.Footers))
	}
	for _, h := range d.Headers {
		if h.Document == nil || h.Document.Document == nil || h.Document.Document.DocumentElement == nil {
			t.Errorf("header %q not fully parsed", h.Name)
		}
	}
}

func TestDecodeWithMissingDocumentPart(t *testing.T) {
	// A valid ZIP with no word/document.xml — IncludeTypedParts should
	// succeed with Document == nil rather than erroring.
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	f, _ := w.Create("some-other-file.txt")
	_, _ = f.Write([]byte("hello"))
	_ = w.Close()

	d, err := DecodeWith(buf.Bytes(), DecodeOptions{IncludeTypedParts: true})
	if err != nil {
		t.Fatalf("DecodeWith: %v", err)
	}
	if d.Document != nil {
		t.Error("Document populated even though word/document.xml was absent")
	}
}

func TestExtractText(t *testing.T) {
	raw := minimalDocx(t)
	got, err := ExtractText(raw)
	if err != nil {
		t.Fatalf("ExtractText: %v", err)
	}
	if got != "Hello DOCX" {
		t.Errorf("ExtractText = %q, want %q", got, "Hello DOCX")
	}
}

func TestExtractTextSkipsTrackedDeletes(t *testing.T) {
	// Two paragraphs: one with a tracked insert (<w:ins><w:t>added</w:t></w:ins>),
	// one with a tracked delete (<w:del><w:delText>gone</w:delText></w:del>).
	// The walker captures only <w:t> content, so the insert is kept and
	// the delete is dropped.
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	f, _ := w.Create("word/document.xml")
	_, _ = f.Write([]byte(
		`<?xml version="1.0"?><w:document xmlns:w="http://w"><w:body>` +
			`<w:p><w:ins><w:r><w:t>added</w:t></w:r></w:ins></w:p>` +
			`<w:p><w:del><w:r><w:delText>gone</w:delText></w:r></w:del></w:p>` +
			`</w:body></w:document>`))
	_ = w.Close()

	got, err := ExtractText(buf.Bytes())
	if err != nil {
		t.Fatalf("ExtractText: %v", err)
	}
	if got != "added\n" {
		t.Errorf("ExtractText = %q, want %q", got, "added\n")
	}
}

func TestExtractFonts(t *testing.T) {
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	f, _ := w.Create("word/fontTable.xml")
	_, _ = f.Write([]byte(
		`<?xml version="1.0"?><w:fonts xmlns:w="http://w">` +
			`<w:font w:name="Georgia"/><w:font w:name="Helvetica"/>` +
			`</w:fonts>`))
	_ = w.Close()

	names, err := ExtractFonts(buf.Bytes())
	if err != nil {
		t.Fatalf("ExtractFonts: %v", err)
	}
	if len(names) != 2 || names[0] != "Georgia" || names[1] != "Helvetica" {
		t.Errorf("ExtractFonts = %v, want [Georgia Helvetica]", names)
	}
}

func TestExtractSectionsSimple(t *testing.T) {
	// One sectPr at the end of body, with page size, margins, cols, and
	// one header and footer reference.
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	f, _ := w.Create("word/document.xml")
	_, _ = f.Write([]byte(
		`<?xml version="1.0"?><w:document xmlns:w="http://w" xmlns:r="http://r"><w:body>` +
			`<w:p><w:r><w:t>body</w:t></w:r></w:p>` +
			`<w:sectPr>` +
			`<w:headerReference w:type="default" r:id="rId10"/>` +
			`<w:footerReference w:type="default" r:id="rId11"/>` +
			`<w:pgSz w:w="12240" w:h="15840" w:orient="portrait"/>` +
			`<w:pgMar w:top="1440" w:right="1800" w:bottom="1440" w:left="1800"/>` +
			`<w:cols w:num="2"/>` +
			`</w:sectPr>` +
			`</w:body></w:document>`))
	_ = w.Close()

	secs, err := ExtractSections(buf.Bytes())
	if err != nil {
		t.Fatalf("ExtractSections: %v", err)
	}
	if len(secs) != 1 {
		t.Fatalf("len(secs) = %d, want 1", len(secs))
	}
	s := secs[0]
	if s.PageWidth != 12240 || s.PageHeight != 15840 {
		t.Errorf("page size = %dx%d, want 12240x15840", s.PageWidth, s.PageHeight)
	}
	if s.Orientation != "portrait" {
		t.Errorf("orientation = %q, want portrait", s.Orientation)
	}
	if s.MarginTop != 1440 || s.MarginBottom != 1440 || s.MarginLeft != 1800 || s.MarginRight != 1800 {
		t.Errorf("margins = %+v", s)
	}
	if s.Columns != 2 {
		t.Errorf("columns = %d, want 2", s.Columns)
	}
	if len(s.HeaderRefs) != 1 || s.HeaderRefs[0].RelId != "rId10" || s.HeaderRefs[0].Type != "default" {
		t.Errorf("HeaderRefs = %+v", s.HeaderRefs)
	}
	if len(s.FooterRefs) != 1 || s.FooterRefs[0].RelId != "rId11" || s.FooterRefs[0].Type != "default" {
		t.Errorf("FooterRefs = %+v", s.FooterRefs)
	}
}

func TestExtractSectionsMultipleBreaks(t *testing.T) {
	// Two sections: one break in a paragraph's <w:pPr>, and the trailing
	// body-level sectPr. Expect two entries in source order.
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	f, _ := w.Create("word/document.xml")
	_, _ = f.Write([]byte(
		`<?xml version="1.0"?><w:document xmlns:w="http://w"><w:body>` +
			`<w:p><w:pPr><w:sectPr><w:pgSz w:w="11906" w:h="16838"/></w:sectPr></w:pPr><w:r><w:t>part1</w:t></w:r></w:p>` +
			`<w:p><w:r><w:t>part2</w:t></w:r></w:p>` +
			`<w:sectPr><w:pgSz w:w="12240" w:h="15840"/></w:sectPr>` +
			`</w:body></w:document>`))
	_ = w.Close()

	secs, err := ExtractSections(buf.Bytes())
	if err != nil {
		t.Fatalf("ExtractSections: %v", err)
	}
	if len(secs) != 2 {
		t.Fatalf("len(secs) = %d, want 2", len(secs))
	}
	if secs[0].PageWidth != 11906 || secs[1].PageWidth != 12240 {
		t.Errorf("section widths = %d, %d — want 11906, 12240", secs[0].PageWidth, secs[1].PageWidth)
	}
}

func TestDecodedAccessors(t *testing.T) {
	raw := minimalDocx(t)
	d, err := DecodeWith(raw, DecodeOptions{IncludeTypedParts: true})
	if err != nil {
		t.Fatalf("DecodeWith: %v", err)
	}
	text, err := d.Text()
	if err != nil {
		t.Fatalf("d.Text: %v", err)
	}
	if text != "Hello DOCX" {
		t.Errorf("d.Text = %q, want %q", text, "Hello DOCX")
	}

	// minimalDocx has no fontTable.xml — Fonts should return nil,
	// not an error.
	fonts, err := d.Fonts()
	if err != nil {
		t.Fatalf("d.Fonts: %v", err)
	}
	if fonts != nil {
		t.Errorf("d.Fonts = %v, want nil", fonts)
	}

	// minimalDocx has no media parts either.
	if imgs := d.Images(); imgs != nil {
		t.Errorf("d.Images = %v, want nil", imgs)
	}

	// minimalDocx omits sectPr entirely, so Sections returns empty.
	secs, err := d.Sections()
	if err != nil {
		t.Fatalf("d.Sections: %v", err)
	}
	if len(secs) != 0 {
		t.Errorf("d.Sections = %v, want []", secs)
	}
}

// TestDecodeBodyTypedTreeMinimal verifies that the minimal fixture's
// single paragraph flows into DocxPackage.Document.Body.Content as a
// Paragraph > Run > TextContent triple.
func TestDecodeBodyTypedTreeMinimal(t *testing.T) {
	doc, err := Decode(minimalDocx(t))
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	body := doc.DocxPackage.Document.Body
	if len(body.Content) != 1 {
		t.Fatalf("body.Content len = %d, want 1", len(body.Content))
	}
	para := body.Content[0].GetParagraph()
	if para == nil {
		t.Fatal("body.Content[0] is not a Paragraph")
	}
	if len(para.Content) != 1 {
		t.Fatalf("para.Content len = %d, want 1", len(para.Content))
	}
	run := para.Content[0].GetRun()
	if run == nil {
		t.Fatal("para.Content[0] is not a Run")
	}
	if len(run.Content) != 1 {
		t.Fatalf("run.Content len = %d, want 1", len(run.Content))
	}
	text := run.Content[0].GetText()
	if text == nil {
		t.Fatal("run.Content[0] is not a TextContent")
	}
	if text.Value != "Hello DOCX" {
		t.Errorf("text.Value = %q, want %q", text.Value, "Hello DOCX")
	}
}

// TestDecodeBodyTypedTreeTracked exercises both tracked-change
// paragraph children: one <w:ins> wrapping a <w:r><w:t>, and one
// <w:del> wrapping a <w:r><w:delText>. Author / date / id attributes
// must also flow onto TrackedChangeInfo.
func TestDecodeBodyTypedTreeTracked(t *testing.T) {
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	f, _ := w.Create("word/document.xml")
	_, _ = f.Write([]byte(
		`<?xml version="1.0"?><w:document xmlns:w="http://w"><w:body>` +
			`<w:p><w:ins w:id="3" w:author="alice" w:date="2026-04-16T00:00:00Z"><w:r><w:t>added</w:t></w:r></w:ins></w:p>` +
			`<w:p><w:del w:id="4" w:author="bob" w:date="2026-04-17T00:00:00Z"><w:r><w:delText>gone</w:delText></w:r></w:del></w:p>` +
			`</w:body></w:document>`))
	_ = w.Close()

	doc, err := Decode(buf.Bytes())
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	body := doc.DocxPackage.Document.Body
	if len(body.Content) != 2 {
		t.Fatalf("body.Content len = %d, want 2", len(body.Content))
	}

	ins := body.Content[0].GetParagraph().Content[0].GetIns()
	if ins == nil {
		t.Fatal("expected ParagraphChild_Ins")
	}
	if ins.Info == nil || ins.Info.Id != 3 || ins.Info.Author != "alice" || ins.Info.Date != "2026-04-16T00:00:00Z" {
		t.Errorf("ins.Info = %+v", ins.Info)
	}
	if len(ins.Content) != 1 {
		t.Fatalf("ins.Content len = %d, want 1", len(ins.Content))
	}
	if got := ins.Content[0].GetRun().Content[0].GetText().Value; got != "added" {
		t.Errorf("inserted text = %q, want %q", got, "added")
	}

	del := body.Content[1].GetParagraph().Content[0].GetDel()
	if del == nil {
		t.Fatal("expected ParagraphChild_Del")
	}
	if del.Info == nil || del.Info.Id != 4 || del.Info.Author != "bob" {
		t.Errorf("del.Info = %+v", del.Info)
	}
	if got := del.Content[0].GetRun().Content[0].GetDelText().Value; got != "gone" {
		t.Errorf("deleted text = %q, want %q", got, "gone")
	}
}

// TestDecodeBodyTypedTreeBreakAndTab verifies that <w:br> and <w:tab>
// inside a run land on RunChild_Br / RunChild_Tab, and that the break's
// type attribute maps to the Break.Type enum.
func TestDecodeBodyTypedTreeBreakAndTab(t *testing.T) {
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	f, _ := w.Create("word/document.xml")
	_, _ = f.Write([]byte(
		`<?xml version="1.0"?><w:document xmlns:w="http://w"><w:body>` +
			`<w:p><w:r><w:t>before</w:t><w:br w:type="page"/><w:tab/><w:t>after</w:t></w:r></w:p>` +
			`</w:body></w:document>`))
	_ = w.Close()

	doc, err := Decode(buf.Bytes())
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	run := doc.DocxPackage.Document.Body.Content[0].GetParagraph().Content[0].GetRun()
	if len(run.Content) != 4 {
		t.Fatalf("run.Content len = %d, want 4", len(run.Content))
	}
	if v := run.Content[0].GetText().Value; v != "before" {
		t.Errorf("run[0].Text = %q, want before", v)
	}
	br := run.Content[1].GetBr()
	if br == nil {
		t.Fatal("run[1] is not a Break")
	}
	if br.Type != pb.BreakType_BREAK_PAGE {
		t.Errorf("br.Type = %v, want BREAK_PAGE", br.Type)
	}
	if run.Content[2].GetTab() == nil {
		t.Error("run[2] is not a Tab")
	}
	if v := run.Content[3].GetText().Value; v != "after" {
		t.Errorf("run[3].Text = %q, want after", v)
	}
}

// TestDecodeBodyTypedTreePreserveSpace covers xml:space="preserve" on
// <w:t> — common for runs containing leading/trailing whitespace.
func TestDecodeBodyTypedTreePreserveSpace(t *testing.T) {
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	f, _ := w.Create("word/document.xml")
	_, _ = f.Write([]byte(
		`<?xml version="1.0"?><w:document xmlns:w="http://w" xmlns:xml="http://www.w3.org/XML/1998/namespace"><w:body>` +
			`<w:p><w:r><w:t xml:space="preserve"> leading</w:t></w:r></w:p>` +
			`</w:body></w:document>`))
	_ = w.Close()

	doc, err := Decode(buf.Bytes())
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	text := doc.DocxPackage.Document.Body.Content[0].GetParagraph().Content[0].GetRun().Content[0].GetText()
	if !text.PreserveSpace {
		t.Error("PreserveSpace = false, want true")
	}
	if text.Value != " leading" {
		t.Errorf("Value = %q, want %q", text.Value, " leading")
	}
}

// TestDecodeBodyTypedTreeTable covers the <w:tbl> branch of
// parseBlockContainer: a 2×2 table with properties, grid, rows, cells,
// and paragraph content inside each cell flows into the typed tree.
func TestDecodeBodyTypedTreeTable(t *testing.T) {
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	f, _ := w.Create("word/document.xml")
	_, _ = f.Write([]byte(
		`<?xml version="1.0"?><w:document xmlns:w="http://w"><w:body>` +
			`<w:p><w:r><w:t>before</w:t></w:r></w:p>` +
			`<w:tbl>` +
			`<w:tblPr><w:tblStyle w:val="TableGrid"/><w:tblW w:w="6000" w:type="dxa"/></w:tblPr>` +
			`<w:tblGrid><w:gridCol w:w="3000"/><w:gridCol w:w="3000"/></w:tblGrid>` +
			`<w:tr>` +
			`<w:tc><w:tcPr><w:tcW w:w="3000" w:type="dxa"/><w:gridSpan w:val="1"/></w:tcPr>` +
			`<w:p><w:r><w:t>A1</w:t></w:r></w:p></w:tc>` +
			`<w:tc><w:tcPr><w:tcW w:w="3000" w:type="dxa"/></w:tcPr>` +
			`<w:p><w:r><w:t>B1</w:t></w:r></w:p></w:tc>` +
			`</w:tr>` +
			`<w:tr>` +
			`<w:tc><w:tcPr><w:tcW w:w="6000" w:type="dxa"/><w:gridSpan w:val="2"/></w:tcPr>` +
			`<w:p><w:r><w:t>merged</w:t></w:r></w:p></w:tc>` +
			`</w:tr>` +
			`</w:tbl>` +
			`<w:p><w:r><w:t>after</w:t></w:r></w:p>` +
			`</w:body></w:document>`))
	_ = w.Close()

	doc, err := Decode(buf.Bytes())
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	body := doc.DocxPackage.Document.Body
	if len(body.Content) != 3 {
		t.Fatalf("body.Content len = %d, want 3 (p, tbl, p)", len(body.Content))
	}
	if body.Content[0].GetParagraph() == nil {
		t.Error("body.Content[0] is not a Paragraph")
	}
	tbl := body.Content[1].GetTable()
	if tbl == nil {
		t.Fatal("body.Content[1] is not a Table")
	}
	if got := tbl.Properties.GetStyleId(); got != "TableGrid" {
		t.Errorf("tbl.Properties.StyleId = %q, want TableGrid", got)
	}
	if tbl.Properties.GetWidth().GetW() != 6000 {
		t.Errorf("tbl width w = %d, want 6000", tbl.Properties.GetWidth().GetW())
	}
	if tbl.Properties.GetWidth().GetType() != pb.TableWidthType_TABLE_WIDTH_DXA {
		t.Errorf("tbl width type = %v, want DXA", tbl.Properties.GetWidth().GetType())
	}
	if got := len(tbl.Grid.GetColumns()); got != 2 {
		t.Errorf("grid columns = %d, want 2", got)
	} else if tbl.Grid.Columns[0].GetW() != 3000 {
		t.Errorf("grid col[0].w = %d, want 3000", tbl.Grid.Columns[0].GetW())
	}
	if len(tbl.Content) != 2 {
		t.Fatalf("tbl.Content len = %d, want 2 rows", len(tbl.Content))
	}
	row0 := tbl.Content[0].GetRow()
	if row0 == nil || len(row0.Content) != 2 {
		t.Fatalf("row0 = %+v (want 2 cells)", row0)
	}
	cellA := row0.Content[0].GetCell()
	if cellA == nil {
		t.Fatal("row0[0] is not a Cell")
	}
	if cellA.Properties.GetWidth().GetW() != 3000 {
		t.Errorf("cellA width = %d, want 3000", cellA.Properties.GetWidth().GetW())
	}
	if cellA.Properties.GetGridSpan() != 1 {
		t.Errorf("cellA GridSpan = %d, want 1", cellA.Properties.GetGridSpan())
	}
	if len(cellA.Content) != 1 || cellA.Content[0].GetParagraph() == nil {
		t.Fatalf("cellA.Content = %+v", cellA.Content)
	}
	if got := cellA.Content[0].GetParagraph().Content[0].GetRun().Content[0].GetText().Value; got != "A1" {
		t.Errorf("cellA text = %q, want A1", got)
	}
	row1 := tbl.Content[1].GetRow()
	merged := row1.Content[0].GetCell()
	if merged.Properties.GetGridSpan() != 2 {
		t.Errorf("merged GridSpan = %d, want 2", merged.Properties.GetGridSpan())
	}
	if got := merged.Content[0].GetParagraph().Content[0].GetRun().Content[0].GetText().Value; got != "merged" {
		t.Errorf("merged text = %q, want merged", got)
	}
	if body.Content[2].GetParagraph() == nil {
		t.Error("body.Content[2] is not a Paragraph")
	}
}

// TestDecodeBodyTypedTreeHyperlink covers the <w:hyperlink> branch of
// parseParagraphChildren. An external-link hyperlink (r:id + tooltip +
// history) and an internal anchor hyperlink both appear on
// ParagraphChild_Hyperlink, with nested runs recursing through the
// same ParagraphChild list.
func TestDecodeBodyTypedTreeHyperlink(t *testing.T) {
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	f, _ := w.Create("word/document.xml")
	_, _ = f.Write([]byte(
		`<?xml version="1.0"?>` +
			`<w:document xmlns:w="http://w" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships">` +
			`<w:body>` +
			`<w:p><w:hyperlink r:id="rIdLink1" w:tooltip="homepage" w:history="1">` +
			`<w:r><w:t>anchor text</w:t></w:r></w:hyperlink></w:p>` +
			`<w:p><w:hyperlink w:anchor="intro" w:history="0">` +
			`<w:r><w:t>jump</w:t></w:r></w:hyperlink></w:p>` +
			`</w:body></w:document>`))
	_ = w.Close()

	doc, err := Decode(buf.Bytes())
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	body := doc.DocxPackage.Document.Body
	if len(body.Content) != 2 {
		t.Fatalf("body.Content len = %d, want 2", len(body.Content))
	}

	ext := body.Content[0].GetParagraph().Content[0].GetHyperlink()
	if ext == nil {
		t.Fatal("expected ParagraphChild_Hyperlink")
	}
	if ext.RelationshipId != "rIdLink1" {
		t.Errorf("RelationshipId = %q, want rIdLink1", ext.RelationshipId)
	}
	if ext.Tooltip != "homepage" {
		t.Errorf("Tooltip = %q, want homepage", ext.Tooltip)
	}
	if !ext.History {
		t.Error("History = false, want true")
	}
	if got := ext.Content[0].GetRun().Content[0].GetText().Value; got != "anchor text" {
		t.Errorf("hyperlink text = %q, want %q", got, "anchor text")
	}

	anchor := body.Content[1].GetParagraph().Content[0].GetHyperlink()
	if anchor == nil {
		t.Fatal("expected anchor ParagraphChild_Hyperlink")
	}
	if anchor.Anchor != "intro" {
		t.Errorf("Anchor = %q, want intro", anchor.Anchor)
	}
	if anchor.History {
		t.Error(`History = true on w:history="0", want false`)
	}
}

// TestDecodeBodyTypedTreeBookmarks covers the <w:bookmarkStart> and
// <w:bookmarkEnd> branches. Attributes (id, name, colFirst/colLast)
// must flow onto the corresponding proto fields.
func TestDecodeBodyTypedTreeBookmarks(t *testing.T) {
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	f, _ := w.Create("word/document.xml")
	_, _ = f.Write([]byte(
		`<?xml version="1.0"?><w:document xmlns:w="http://w"><w:body>` +
			`<w:p>` +
			`<w:bookmarkStart w:id="7" w:name="intro" w:colFirst="1" w:colLast="3"/>` +
			`<w:r><w:t>body</w:t></w:r>` +
			`<w:bookmarkEnd w:id="7"/>` +
			`</w:p>` +
			`</w:body></w:document>`))
	_ = w.Close()

	doc, err := Decode(buf.Bytes())
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	children := doc.DocxPackage.Document.Body.Content[0].GetParagraph().Content
	if len(children) != 3 {
		t.Fatalf("paragraph children = %d, want 3 (bookmarkStart, run, bookmarkEnd)", len(children))
	}
	start := children[0].GetBookmarkStart()
	if start == nil {
		t.Fatal("children[0] is not a BookmarkStart")
	}
	if start.Id != 7 || start.Name != "intro" || start.ColFirst != 1 || start.ColLast != 3 {
		t.Errorf("BookmarkStart = %+v", start)
	}
	end := children[2].GetBookmarkEnd()
	if end == nil {
		t.Fatal("children[2] is not a BookmarkEnd")
	}
	if end.Id != 7 {
		t.Errorf("BookmarkEnd.Id = %d, want 7", end.Id)
	}
}

// TestDecodeParagraphCountCountsAlternateContent guards against the
// undercount regression surfaced by the gluon experiment on
// DOCX_TestPage.docx: paragraphs inside <mc:AlternateContent> and
// <w:txbxContent> (drawing text boxes) must be included in
// ParagraphCount even when the typed walker doesn't descend into their
// wrappers.
func TestDecodeParagraphCountCountsAlternateContent(t *testing.T) {
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	f, _ := w.Create("word/document.xml")
	_, _ = f.Write([]byte(
		`<?xml version="1.0"?>` +
			`<w:document xmlns:w="http://w" xmlns:mc="http://mc"><w:body>` +
			// One plain body paragraph.
			`<w:p><w:r><w:t>top</w:t></w:r></w:p>` +
			// A drawing with two paragraphs inside its text box, wrapped
			// in mc:AlternateContent / mc:Choice — the typed walker
			// skipElements the whole thing, but the count must still
			// include both nested <w:p>.
			`<w:p><w:r><mc:AlternateContent>` +
			`<mc:Choice Requires="wps">` +
			`<w:drawing><wps:txbx xmlns:wps="http://wps"><w:txbxContent>` +
			`<w:p><w:r><w:t>tbx-a</w:t></w:r></w:p>` +
			`<w:p><w:r><w:t>tbx-b</w:t></w:r></w:p>` +
			`</w:txbxContent></wps:txbx></w:drawing>` +
			`</mc:Choice>` +
			`<mc:Fallback><w:pict/></mc:Fallback>` +
			`</mc:AlternateContent></w:r></w:p>` +
			`</w:body></w:document>`))
	_ = w.Close()

	doc, err := Decode(buf.Bytes())
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	// 2 body-level + 2 inside txbxContent = 4 <w:p> in total.
	if got, want := doc.ParagraphCount, int32(4); got != want {
		t.Errorf("ParagraphCount = %d, want %d", got, want)
	}
}

func TestDecodeTrackedChanges(t *testing.T) {
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	f, _ := w.Create("word/document.xml")
	_, _ = f.Write([]byte(
		`<?xml version="1.0"?><w:document xmlns:w="http://w"><w:body>` +
			`<w:p><w:ins><w:r><w:t>added</w:t></w:r></w:ins></w:p>` +
			`</w:body></w:document>`))
	_ = w.Close()

	doc, err := Decode(buf.Bytes())
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if !doc.HasTrackedChanges {
		t.Error("HasTrackedChanges = false, want true")
	}
}
