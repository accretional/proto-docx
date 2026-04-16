package docxcodec

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"fmt"
	"strconv"
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

// Section summarises one <w:sectPr> block from word/document.xml:
// page dimensions, margins, column count, and references to header /
// footer parts.
//
// All numeric fields are in OOXML twips (1 inch = 1440 twips). Zero
// means the attribute was absent from the source — callers should
// treat zero as "use the Word default" (US Letter, one-inch margins,
// single column) rather than a literal zero.
type Section struct {
	PageWidth    int64  // <w:pgSz w:w="...">
	PageHeight   int64  // <w:pgSz w:h="...">
	Orientation  string // <w:pgSz w:orient="portrait|landscape">
	MarginTop    int64  // <w:pgMar w:top="...">
	MarginRight  int64  // <w:pgMar w:right="...">
	MarginBottom int64  // <w:pgMar w:bottom="...">
	MarginLeft   int64  // <w:pgMar w:left="...">
	Columns      int32  // <w:cols w:num="..."> (zero = unset, Word defaults to 1)
	HeaderRefs   []HeaderFooterRef
	FooterRefs   []HeaderFooterRef
}

// HeaderFooterRef points at a header / footer part via an OPC
// relationship id (<w:headerReference r:id="..." w:type="default|first|even"/>).
type HeaderFooterRef struct {
	Type  string // "default", "first", "even"
	RelId string
}

// ExtractSections returns the section-property blocks from
// word/document.xml in source order. A sectPr appearing in <w:pPr>
// marks a section break at that paragraph; a final sectPr as a direct
// child of <w:body> applies to the last section.
//
// A valid .docx always has at least one section; an empty result
// slice means word/document.xml is absent or has no sectPr element.
func ExtractSections(raw []byte) ([]Section, error) {
	if len(raw) == 0 {
		return nil, fmt.Errorf("docxcodec: empty input")
	}
	zr, err := zip.NewReader(bytes.NewReader(raw), int64(len(raw)))
	if err != nil {
		return nil, fmt.Errorf("docxcodec: open zip: %w", err)
	}
	for _, f := range zr.File {
		if f.Name != "word/document.xml" {
			continue
		}
		data, err := readZipFile(f)
		if err != nil {
			return nil, fmt.Errorf("docxcodec: read word/document.xml: %w", err)
		}
		return extractSectionsFromDocumentXML(data), nil
	}
	return nil, nil
}

// Sections is the method form of ExtractSections.
func (d *Decoded) Sections() ([]Section, error) {
	if d == nil || d.DocxDocumentWithMetadata == nil || len(d.RawBytes) == 0 {
		return nil, nil
	}
	return ExtractSections(d.RawBytes)
}

func extractSectionsFromDocumentXML(data []byte) []Section {
	dec := xml.NewDecoder(bytes.NewReader(data))
	dec.Strict = false

	var (
		sections []Section
		cur      *Section
	)
	for {
		tok, err := dec.Token()
		if err != nil {
			break
		}
		switch t := tok.(type) {
		case xml.StartElement:
			switch t.Name.Local {
			case "sectPr":
				sections = append(sections, Section{})
				cur = &sections[len(sections)-1]

			case "pgSz":
				if cur == nil {
					continue
				}
				for _, a := range t.Attr {
					switch a.Name.Local {
					case "w":
						cur.PageWidth = parseTwip(a.Value)
					case "h":
						cur.PageHeight = parseTwip(a.Value)
					case "orient":
						cur.Orientation = a.Value
					}
				}

			case "pgMar":
				if cur == nil {
					continue
				}
				for _, a := range t.Attr {
					switch a.Name.Local {
					case "top":
						cur.MarginTop = parseTwip(a.Value)
					case "right":
						cur.MarginRight = parseTwip(a.Value)
					case "bottom":
						cur.MarginBottom = parseTwip(a.Value)
					case "left":
						cur.MarginLeft = parseTwip(a.Value)
					}
				}

			case "cols":
				if cur == nil {
					continue
				}
				for _, a := range t.Attr {
					if a.Name.Local == "num" {
						cur.Columns = int32(parseTwip(a.Value))
					}
				}

			case "headerReference", "footerReference":
				if cur == nil {
					continue
				}
				ref := HeaderFooterRef{}
				for _, a := range t.Attr {
					switch a.Name.Local {
					case "type":
						ref.Type = a.Value
					case "id":
						ref.RelId = a.Value
					}
				}
				if t.Name.Local == "headerReference" {
					cur.HeaderRefs = append(cur.HeaderRefs, ref)
				} else {
					cur.FooterRefs = append(cur.FooterRefs, ref)
				}
			}

		case xml.EndElement:
			if t.Name.Local == "sectPr" {
				cur = nil
			}
		}
	}
	return sections
}

// parseTwip parses an OOXML integer attribute value (twips, column
// count, etc.). Returns 0 on parse failure — OOXML uses zero to mean
// "use default" for most of these fields, so silent-zero matches the
// schema's own convention.
func parseTwip(s string) int64 {
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return 0
	}
	return n
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
