package docxcodec

import (
	"archive/zip"
	"bytes"
	"fmt"
	"strings"

	xmlpb "openformat/gen/go/openformat/v1"
	"openformat/xmlcodec"

	pb "openformat-docx/gen/go/openformat/v1"
)

// DecodeOptions controls what DecodeWith populates beyond the minimum
// required for byte-faithful Encode round-trip.
type DecodeOptions struct {
	// IncludeTypedParts, when true, parses every text-bearing OPC part
	// through proto-xml's xmlcodec and attaches the typed trees to
	// Decoded (Document, Styles, Numbering, Settings, FontTable,
	// Comments, Footnotes, Endnotes, and the repeated Headers /
	// Footers). Media parts stay opaque on DocxPackage.MediaParts.
	IncludeTypedParts bool
}

// TypedPart pairs an OPC part name with its parsed XML tree. Used for
// Headers and Footers on Decoded, because DOCX packages can carry
// multiple header / footer parts (default / first / even variants).
type TypedPart struct {
	Name     string
	Document *xmlpb.XmlDocumentWithMetadata
}

// Decoded is the result of DecodeWith. It embeds the
// DocxDocumentWithMetadata that plain Decode returns, plus any optional
// typed OPC parts requested via DecodeOptions.
//
// Typed parts live on Go-side fields rather than on the embedded proto
// because docx.proto and xml.proto belong to different modules and
// docx.proto does not import xml.proto.
type Decoded struct {
	*pb.DocxDocumentWithMetadata

	Document  *xmlpb.XmlDocumentWithMetadata // word/document.xml
	Styles    *xmlpb.XmlDocumentWithMetadata // word/styles.xml
	Numbering *xmlpb.XmlDocumentWithMetadata // word/numbering.xml
	Settings  *xmlpb.XmlDocumentWithMetadata // word/settings.xml
	FontTable *xmlpb.XmlDocumentWithMetadata // word/fontTable.xml
	Comments  *xmlpb.XmlDocumentWithMetadata // word/comments.xml
	Footnotes *xmlpb.XmlDocumentWithMetadata // word/footnotes.xml
	Endnotes  *xmlpb.XmlDocumentWithMetadata // word/endnotes.xml

	Headers []TypedPart // word/header*.xml
	Footers []TypedPart // word/footer*.xml
}

// DecodeWith is the option-taking counterpart of Decode. With
// opts.IncludeTypedParts=true, every known OPC XML part is handed to
// proto-xml's xmlcodec.Decode so callers can walk each part as a
// typed XML tree without re-opening the ZIP.
//
// Individual malformed parts surface as errors — callers asking for
// typed parts want to know when they won't get one. Missing parts are
// not errors; their corresponding fields simply stay nil.
func DecodeWith(raw []byte, opts DecodeOptions) (*Decoded, error) {
	doc, err := Decode(raw)
	if err != nil {
		return nil, err
	}
	d := &Decoded{DocxDocumentWithMetadata: doc}

	if !opts.IncludeTypedParts {
		return d, nil
	}

	zr, err := zip.NewReader(bytes.NewReader(raw), int64(len(raw)))
	if err != nil {
		return nil, fmt.Errorf("docxcodec: reopen zip for typed parts: %w", err)
	}

	// Map filenames to destination field pointers for the singular parts.
	singulars := map[string]**xmlpb.XmlDocumentWithMetadata{
		"word/document.xml":  &d.Document,
		"word/styles.xml":    &d.Styles,
		"word/numbering.xml": &d.Numbering,
		"word/settings.xml":  &d.Settings,
		"word/fontTable.xml": &d.FontTable,
		"word/comments.xml":  &d.Comments,
		"word/footnotes.xml": &d.Footnotes,
		"word/endnotes.xml":  &d.Endnotes,
	}

	for _, f := range zr.File {
		name := f.Name

		if dest, ok := singulars[name]; ok {
			typed, err := decodeXMLPart(f, name)
			if err != nil {
				return nil, err
			}
			*dest = typed
			continue
		}

		if isHeaderPart(name) {
			typed, err := decodeXMLPart(f, name)
			if err != nil {
				return nil, err
			}
			d.Headers = append(d.Headers, TypedPart{Name: name, Document: typed})
			continue
		}
		if isFooterPart(name) {
			typed, err := decodeXMLPart(f, name)
			if err != nil {
				return nil, err
			}
			d.Footers = append(d.Footers, TypedPart{Name: name, Document: typed})
			continue
		}
	}
	return d, nil
}

func decodeXMLPart(f *zip.File, name string) (*xmlpb.XmlDocumentWithMetadata, error) {
	data, err := readZipFile(f)
	if err != nil {
		return nil, fmt.Errorf("docxcodec: read %s: %w", name, err)
	}
	typed, err := xmlcodec.Decode(data)
	if err != nil {
		return nil, fmt.Errorf("docxcodec: xmlcodec.Decode(%s): %w", name, err)
	}
	return typed, nil
}

func isHeaderPart(name string) bool {
	return strings.HasPrefix(name, "word/header") && strings.HasSuffix(name, ".xml")
}

func isFooterPart(name string) bool {
	return strings.HasPrefix(name, "word/footer") && strings.HasSuffix(name, ".xml")
}
