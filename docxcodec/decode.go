package docxcodec

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"fmt"
	"io"
	"strings"

	pb "openformat-docx/gen/go/openformat/v1"
)

// IsDocx returns true if raw looks like a DOCX (ZIP) file by inspecting
// the PK\003\004 magic bytes at the start of the archive.
func IsDocx(raw []byte) bool {
	return len(raw) >= 4 &&
		raw[0] == 0x50 && raw[1] == 0x4B &&
		raw[2] == 0x03 && raw[3] == 0x04
}

// Decode reads raw DOCX bytes and returns a populated
// DocxDocumentWithMetadata. Returns an error if the input is empty or
// is not a readable ZIP archive.
//
// Decode is intentionally tolerant: individual part-level parse
// failures (a malformed styles.xml, say) are not fatal. The surrounding
// fields are simply left empty, while the raw bytes remain available
// via the RawBytes field for byte-faithful Encode.
func Decode(raw []byte) (*pb.DocxDocumentWithMetadata, error) {
	if len(raw) == 0 {
		return nil, fmt.Errorf("docxcodec: empty input")
	}

	zr, err := zip.NewReader(bytes.NewReader(raw), int64(len(raw)))
	if err != nil {
		return nil, fmt.Errorf("docxcodec: open zip: %w", err)
	}

	doc := &pb.DocxDocumentWithMetadata{
		RawBytes:    raw,
		DocxPackage: &pb.Package{},
	}

	for _, f := range zr.File {
		name := f.Name
		data, err := readZipFile(f)
		if err != nil {
			continue
		}

		switch {
		case name == "word/document.xml":
			parseDocumentXML(data, doc)

		case name == "word/fontTable.xml":
			doc.FontCount = countXMLElements(data, "font")
			doc.DocxPackage.FontTable = &pb.FontTable{}

		case name == "word/styles.xml":
			doc.DocxPackage.Styles = &pb.Styles{}

		case name == "word/settings.xml":
			doc.DocxPackage.Settings = &pb.Settings{}

		case name == "word/comments.xml":
			if hasContentBeyondRoot(data) {
				doc.HasComments = true
				doc.DocxPackage.CommentsPart = &pb.CommentsPart{}
			}

		case name == "word/footnotes.xml":
			if hasContentBeyondRoot(data) {
				doc.HasNotes = true
				doc.DocxPackage.FootnotesPart = &pb.FootnotesPart{}
			}

		case name == "word/endnotes.xml":
			if hasContentBeyondRoot(data) {
				doc.HasNotes = true
				doc.DocxPackage.EndnotesPart = &pb.EndnotesPart{}
			}

		case name == "[Content_Types].xml":
			doc.DocxPackage.ContentTypes = parseContentTypes(data)

		case name == "_rels/.rels":
			doc.DocxPackage.PackageRelationships = parseRelationships(data)

		case strings.HasPrefix(name, "word/media/"):
			doc.ImageCount++
			doc.DocxPackage.MediaParts = append(doc.DocxPackage.MediaParts, &pb.MediaPart{
				Filename:    name[len("word/media/"):],
				ContentType: mimeFromFilename(name),
				Data:        data,
			})

		case strings.HasPrefix(name, "word/header") && strings.HasSuffix(name, ".xml"):
			doc.DocxPackage.HeaderParts = append(doc.DocxPackage.HeaderParts, &pb.HeaderPart{})

		case strings.HasPrefix(name, "word/footer") && strings.HasSuffix(name, ".xml"):
			doc.DocxPackage.FooterParts = append(doc.DocxPackage.FooterParts, &pb.FooterPart{})
		}
	}

	return doc, nil
}

// readZipFile opens a zip entry, reads its contents into a byte slice,
// and closes the reader. Returns the empty slice on I/O failure.
func readZipFile(f *zip.File) ([]byte, error) {
	rc, err := f.Open()
	if err != nil {
		return nil, err
	}
	defer rc.Close()
	return io.ReadAll(rc)
}

// parseDocumentXML scans word/document.xml for paragraph count and
// tracked-change markers (w:ins / w:del). It also allocates an empty
// Document/Body pair on the package so downstream consumers can tell
// the document XML was present.
func parseDocumentXML(data []byte, doc *pb.DocxDocumentWithMetadata) {
	doc.DocxPackage.Document = &pb.Document{Body: &pb.Body{}}

	dec := xml.NewDecoder(bytes.NewReader(data))
	dec.Strict = false

	for {
		tok, err := dec.Token()
		if err != nil {
			break
		}
		se, ok := tok.(xml.StartElement)
		if !ok {
			continue
		}
		switch se.Name.Local {
		case "p":
			doc.ParagraphCount++
		case "ins", "del":
			doc.HasTrackedChanges = true
		}
	}
}

// parseContentTypes parses [Content_Types].xml into a ContentTypes
// proto, capturing Default / Override entries.
func parseContentTypes(data []byte) *pb.ContentTypes {
	ct := &pb.ContentTypes{}
	dec := xml.NewDecoder(bytes.NewReader(data))
	dec.Strict = false
	for {
		tok, err := dec.Token()
		if err != nil {
			break
		}
		se, ok := tok.(xml.StartElement)
		if !ok {
			continue
		}
		switch se.Name.Local {
		case "Default":
			d := &pb.ContentTypeDefault{}
			for _, a := range se.Attr {
				switch a.Name.Local {
				case "Extension":
					d.Extension = a.Value
				case "ContentType":
					d.ContentType = a.Value
				}
			}
			ct.Defaults = append(ct.Defaults, d)
		case "Override":
			o := &pb.ContentTypeOverride{}
			for _, a := range se.Attr {
				switch a.Name.Local {
				case "PartName":
					o.PartName = a.Value
				case "ContentType":
					o.ContentType = a.Value
				}
			}
			ct.Overrides = append(ct.Overrides, o)
		}
	}
	return ct
}

// parseRelationships parses an OPC .rels XML file into a slice of
// Relationship protos.
func parseRelationships(data []byte) []*pb.Relationship {
	var rels []*pb.Relationship
	dec := xml.NewDecoder(bytes.NewReader(data))
	dec.Strict = false
	for {
		tok, err := dec.Token()
		if err != nil {
			break
		}
		se, ok := tok.(xml.StartElement)
		if !ok || se.Name.Local != "Relationship" {
			continue
		}
		r := &pb.Relationship{}
		for _, a := range se.Attr {
			switch a.Name.Local {
			case "Id":
				r.Id = a.Value
			case "Type":
				r.Type = a.Value
			case "Target":
				r.Target = a.Value
			case "TargetMode":
				if a.Value == "External" {
					r.TargetMode = pb.TargetMode_TARGET_MODE_EXTERNAL
				}
			}
		}
		rels = append(rels, r)
	}
	return rels
}

// countXMLElements counts every occurrence of start-element tokens with
// the given local name.
func countXMLElements(data []byte, localName string) int32 {
	dec := xml.NewDecoder(bytes.NewReader(data))
	dec.Strict = false
	var count int32
	for {
		tok, err := dec.Token()
		if err != nil {
			break
		}
		if se, ok := tok.(xml.StartElement); ok && se.Name.Local == localName {
			count++
		}
	}
	return count
}

// hasContentBeyondRoot returns true if the XML stream contains at least
// one element nested inside the document root — a cheap "non-empty"
// check for parts like comments.xml / footnotes.xml.
func hasContentBeyondRoot(data []byte) bool {
	dec := xml.NewDecoder(bytes.NewReader(data))
	dec.Strict = false
	depth := 0
	for {
		tok, err := dec.Token()
		if err != nil {
			break
		}
		switch tok.(type) {
		case xml.StartElement:
			depth++
			if depth >= 2 {
				return true
			}
		case xml.EndElement:
			depth--
		}
	}
	return false
}

// mimeFromFilename guesses a MIME content-type string from a filename's
// extension. Used to populate MediaPart.ContentType.
func mimeFromFilename(name string) string {
	lower := strings.ToLower(name)
	switch {
	case strings.HasSuffix(lower, ".png"):
		return "image/png"
	case strings.HasSuffix(lower, ".jpg"), strings.HasSuffix(lower, ".jpeg"):
		return "image/jpeg"
	case strings.HasSuffix(lower, ".gif"):
		return "image/gif"
	case strings.HasSuffix(lower, ".tiff"), strings.HasSuffix(lower, ".tif"):
		return "image/tiff"
	case strings.HasSuffix(lower, ".wmf"):
		return "image/x-wmf"
	case strings.HasSuffix(lower, ".emf"):
		return "image/x-emf"
	case strings.HasSuffix(lower, ".svg"):
		return "image/svg+xml"
	default:
		return "application/octet-stream"
	}
}
