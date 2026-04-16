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
