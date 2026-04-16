package docxcodec

import (
	"archive/zip"
	"bytes"
	"testing"

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
