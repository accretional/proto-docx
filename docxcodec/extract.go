package docxcodec

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"fmt"
	"strings"

	pb "openformat-docx/gen/go/openformat/v1"
)

// ExtractText reads raw DOCX bytes and returns the paragraph text of
// word/document.xml, one paragraph per line, in source order.
//
// Tracked-change insertions are included (their text sits in <w:t>
// like any other run). Tracked-change deletions are omitted (their
// text sits in <w:delText>, which this walker ignores).
//
// Returns the empty string with no error if the package has no
// word/document.xml.
func ExtractText(raw []byte) (string, error) {
	if len(raw) == 0 {
		return "", fmt.Errorf("docxcodec: empty input")
	}
	zr, err := zip.NewReader(bytes.NewReader(raw), int64(len(raw)))
	if err != nil {
		return "", fmt.Errorf("docxcodec: open zip: %w", err)
	}
	for _, f := range zr.File {
		if f.Name != "word/document.xml" {
			continue
		}
		data, err := readZipFile(f)
		if err != nil {
			return "", fmt.Errorf("docxcodec: read word/document.xml: %w", err)
		}
		return extractTextFromDocumentXML(data), nil
	}
	return "", nil
}

// Text is the method form of ExtractText. When Decoded was produced
// via DecodeWith with IncludeTypedParts=true, the typed XML tree's
// RawBytes are reused; otherwise the raw DOCX package is re-opened.
func (d *Decoded) Text() (string, error) {
	if d == nil {
		return "", nil
	}
	if d.Document != nil && len(d.Document.RawBytes) > 0 {
		return extractTextFromDocumentXML(d.Document.RawBytes), nil
	}
	if d.DocxDocumentWithMetadata != nil && len(d.RawBytes) > 0 {
		return ExtractText(d.RawBytes)
	}
	return "", nil
}

// ExtractFonts returns the declared font family names from
// word/fontTable.xml, in source order. Empty slice (nil) if no font
// table is present.
func ExtractFonts(raw []byte) ([]string, error) {
	if len(raw) == 0 {
		return nil, fmt.Errorf("docxcodec: empty input")
	}
	zr, err := zip.NewReader(bytes.NewReader(raw), int64(len(raw)))
	if err != nil {
		return nil, fmt.Errorf("docxcodec: open zip: %w", err)
	}
	for _, f := range zr.File {
		if f.Name != "word/fontTable.xml" {
			continue
		}
		data, err := readZipFile(f)
		if err != nil {
			return nil, fmt.Errorf("docxcodec: read word/fontTable.xml: %w", err)
		}
		return extractFontsFromFontTableXML(data), nil
	}
	return nil, nil
}

// Fonts is the method form of ExtractFonts.
func (d *Decoded) Fonts() ([]string, error) {
	if d == nil || d.DocxDocumentWithMetadata == nil || len(d.RawBytes) == 0 {
		return nil, nil
	}
	return ExtractFonts(d.RawBytes)
}

// Images returns a view of the MediaParts already populated by Decode.
// It exists as a method so callers can reach for all the extraction
// helpers uniformly: d.Text(), d.Fonts(), d.Images().
func (d *Decoded) Images() []*pb.MediaPart {
	if d == nil || d.DocxDocumentWithMetadata == nil || d.DocxPackage == nil {
		return nil
	}
	return d.DocxPackage.MediaParts
}

// extractTextFromDocumentXML walks word/document.xml and returns its
// <w:p> text, one paragraph per line. The walker is intentionally
// dumb (no namespace awareness beyond local-name matching) because
// the stdlib tokenizer already resolves qualified names for us.
func extractTextFromDocumentXML(data []byte) string {
	dec := xml.NewDecoder(bytes.NewReader(data))
	dec.Strict = false

	var (
		paragraphs []string
		cur        strings.Builder
		inP        bool
		inT        bool
	)
	for {
		tok, err := dec.Token()
		if err != nil {
			break
		}
		switch t := tok.(type) {
		case xml.StartElement:
			switch t.Name.Local {
			case "p":
				inP = true
				cur.Reset()
			case "t":
				inT = true
			case "br", "tab":
				if inP {
					if t.Name.Local == "br" {
						cur.WriteByte('\n')
					} else {
						cur.WriteByte('\t')
					}
				}
			}
		case xml.EndElement:
			switch t.Name.Local {
			case "p":
				if inP {
					paragraphs = append(paragraphs, cur.String())
					inP = false
				}
			case "t":
				inT = false
			}
		case xml.CharData:
			if inP && inT {
				cur.Write(t)
			}
		}
	}
	return strings.Join(paragraphs, "\n")
}

// extractFontsFromFontTableXML walks fontTable.xml and collects every
// <w:font>'s w:name attribute. Returns a nil slice on an empty table.
func extractFontsFromFontTableXML(data []byte) []string {
	dec := xml.NewDecoder(bytes.NewReader(data))
	dec.Strict = false
	var names []string
	for {
		tok, err := dec.Token()
		if err != nil {
			break
		}
		se, ok := tok.(xml.StartElement)
		if !ok || se.Name.Local != "font" {
			continue
		}
		for _, a := range se.Attr {
			if a.Name.Local == "name" {
				names = append(names, a.Value)
				break
			}
		}
	}
	return names
}
