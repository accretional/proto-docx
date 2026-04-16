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

// parseDocumentXML walks word/document.xml and builds the typed-proto
// tree under DocxPackage.Document.Body: paragraphs, runs, text,
// tracked-change wrappers, breaks, and tabs. Summary fields
// (ParagraphCount, HasTrackedChanges) are populated as a side effect.
//
// Scope is deliberately narrow — the text-bearing subset consumers
// hit first: Paragraph → (Run | TrackedChangeInsertion |
// TrackedChangeDeletion) → (TextContent | DeletedText | Break | Tab).
// Tables, hyperlinks, SDTs, bookmarks, field runs, and the rest of the
// ParagraphChild / RunChild oneofs are not yet modeled here; they are
// still present in the raw-bytes path and surface via the typed
// xmlcodec tree on DecodeWith.
func parseDocumentXML(data []byte, doc *pb.DocxDocumentWithMetadata) {
	body := &pb.Body{}
	doc.DocxPackage.Document = &pb.Document{Body: body}

	dec := xml.NewDecoder(bytes.NewReader(data))
	dec.Strict = false

	var (
		curPara    *pb.Paragraph
		curRun     *pb.Run
		curIns     *pb.TrackedChangeInsertion
		curDel     *pb.TrackedChangeDeletion
		curText    *pb.TextContent
		curDelText *pb.DeletedText
	)

	// attachRunChild appends a finished Run to whichever container is
	// currently open — a tracked-change wrapper if one is active, else
	// the paragraph directly.
	attachRun := func(r *pb.Run) {
		child := &pb.ParagraphChild{Child: &pb.ParagraphChild_Run{Run: r}}
		switch {
		case curIns != nil:
			curIns.Content = append(curIns.Content, child)
		case curDel != nil:
			curDel.Content = append(curDel.Content, child)
		case curPara != nil:
			curPara.Content = append(curPara.Content, child)
		}
	}

	for {
		tok, err := dec.Token()
		if err != nil {
			break
		}
		switch t := tok.(type) {
		case xml.StartElement:
			switch t.Name.Local {
			case "p":
				doc.ParagraphCount++
				curPara = &pb.Paragraph{}
			case "ins":
				doc.HasTrackedChanges = true
				curIns = &pb.TrackedChangeInsertion{Info: parseTrackedInfo(t)}
			case "del":
				doc.HasTrackedChanges = true
				curDel = &pb.TrackedChangeDeletion{Info: parseTrackedInfo(t)}
			case "r":
				curRun = &pb.Run{}
			case "t":
				if curRun != nil {
					curText = &pb.TextContent{PreserveSpace: hasPreserveSpace(t)}
				}
			case "delText":
				if curRun != nil {
					curDelText = &pb.DeletedText{PreserveSpace: hasPreserveSpace(t)}
				}
			case "br":
				if curRun != nil {
					curRun.Content = append(curRun.Content, &pb.RunChild{
						Child: &pb.RunChild_Br{Br: &pb.Break{Type: parseBreakType(t)}},
					})
				}
			case "tab":
				if curRun != nil {
					curRun.Content = append(curRun.Content, &pb.RunChild{
						Child: &pb.RunChild_Tab{Tab: &pb.Tab{}},
					})
				}
			}
		case xml.CharData:
			if curText != nil {
				curText.Value += string(t)
			} else if curDelText != nil {
				curDelText.Value += string(t)
			}
		case xml.EndElement:
			switch t.Name.Local {
			case "t":
				if curText != nil && curRun != nil {
					curRun.Content = append(curRun.Content, &pb.RunChild{
						Child: &pb.RunChild_Text{Text: curText},
					})
				}
				curText = nil
			case "delText":
				if curDelText != nil && curRun != nil {
					curRun.Content = append(curRun.Content, &pb.RunChild{
						Child: &pb.RunChild_DelText{DelText: curDelText},
					})
				}
				curDelText = nil
			case "r":
				if curRun != nil {
					attachRun(curRun)
				}
				curRun = nil
			case "ins":
				if curIns != nil && curPara != nil {
					curPara.Content = append(curPara.Content, &pb.ParagraphChild{
						Child: &pb.ParagraphChild_Ins{Ins: curIns},
					})
				}
				curIns = nil
			case "del":
				if curDel != nil && curPara != nil {
					curPara.Content = append(curPara.Content, &pb.ParagraphChild{
						Child: &pb.ParagraphChild_Del{Del: curDel},
					})
				}
				curDel = nil
			case "p":
				if curPara != nil {
					body.Content = append(body.Content, &pb.BlockLevelElement{
						Element: &pb.BlockLevelElement_Paragraph{Paragraph: curPara},
					})
				}
				curPara = nil
			}
		}
	}
}

// parseTrackedInfo extracts the w:id / w:author / w:date attributes
// from a w:ins or w:del start element.
func parseTrackedInfo(se xml.StartElement) *pb.TrackedChangeInfo {
	info := &pb.TrackedChangeInfo{}
	for _, a := range se.Attr {
		switch a.Name.Local {
		case "id":
			if n, err := parseInt32(a.Value); err == nil {
				info.Id = n
			}
		case "author":
			info.Author = a.Value
		case "date":
			info.Date = a.Value
		}
	}
	return info
}

// hasPreserveSpace returns true when the element carries
// xml:space="preserve".
func hasPreserveSpace(se xml.StartElement) bool {
	for _, a := range se.Attr {
		if a.Name.Space == "xml" && a.Name.Local == "space" && a.Value == "preserve" {
			return true
		}
		if a.Name.Local == "space" && a.Value == "preserve" {
			return true
		}
	}
	return false
}

// parseBreakType maps w:type="page|column|textWrapping" to the
// corresponding enum value. Missing type is treated as the default
// (page, which is proto value 0).
func parseBreakType(se xml.StartElement) pb.BreakType {
	for _, a := range se.Attr {
		if a.Name.Local != "type" {
			continue
		}
		switch a.Value {
		case "page":
			return pb.BreakType_BREAK_PAGE
		case "column":
			return pb.BreakType_BREAK_COLUMN
		case "textWrapping":
			return pb.BreakType_BREAK_TEXT_WRAPPING
		}
	}
	return pb.BreakType_BREAK_PAGE
}

func parseInt32(s string) (int32, error) {
	var n int32
	_, err := fmt.Sscanf(s, "%d", &n)
	return n, err
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
