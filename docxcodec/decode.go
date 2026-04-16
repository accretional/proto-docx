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
// tree under DocxPackage.Document.Body via recursive descent. It
// populates block-level content (paragraphs + tables), tracked-change
// wrappers, runs, text, breaks, and tabs. Summary fields
// (ParagraphCount, HasTrackedChanges) are populated as a side effect.
//
// Scope is the text-bearing subset consumers hit first:
// Body → (Paragraph | Table) →
//
//	Paragraph → (Run | TrackedChangeInsertion | TrackedChangeDeletion) →
//	            (TextContent | DeletedText | Break | Tab)
//	Table     → (TableRow → TableCell → BlockLevelElement …) (recursive)
//
// Hyperlinks, SDTs, bookmarks, field runs, and the remaining
// ParagraphChild / RunChild oneofs are not yet modeled here; they
// remain on the raw-bytes path and surface via the typed xmlcodec tree
// on DecodeWith.
func parseDocumentXML(data []byte, doc *pb.DocxDocumentWithMetadata) {
	body := &pb.Body{}
	doc.DocxPackage.Document = &pb.Document{Body: body}

	dec := xml.NewDecoder(bytes.NewReader(data))
	dec.Strict = false

	for {
		tok, err := dec.Token()
		if err != nil {
			return
		}
		if se, ok := tok.(xml.StartElement); ok && se.Name.Local == "body" {
			body.Content = parseBlockContainer(dec, se, doc)
			return
		}
	}
}

// parseBlockContainer reads child tokens until the end-tag matching
// `parent`, returning the block-level elements (paragraphs + tables)
// found at this level. Unknown children are skipped via skipElement.
// Used for both <w:body> and <w:tc> — table cells hold the same
// BlockLevelElement list.
func parseBlockContainer(dec *xml.Decoder, parent xml.StartElement, doc *pb.DocxDocumentWithMetadata) []*pb.BlockLevelElement {
	var out []*pb.BlockLevelElement
	for {
		tok, err := dec.Token()
		if err != nil {
			return out
		}
		switch t := tok.(type) {
		case xml.StartElement:
			switch t.Name.Local {
			case "p":
				p := parseParagraph(dec, t, doc)
				out = append(out, &pb.BlockLevelElement{
					Element: &pb.BlockLevelElement_Paragraph{Paragraph: p},
				})
			case "tbl":
				tbl := parseTable(dec, t, doc)
				out = append(out, &pb.BlockLevelElement{
					Element: &pb.BlockLevelElement_Table{Table: tbl},
				})
			default:
				skipElement(dec, t)
			}
		case xml.EndElement:
			if t.Name.Local == parent.Name.Local {
				return out
			}
		}
	}
}

// parseParagraph reads a <w:p> subtree and returns the populated
// Paragraph. The paragraph's direct children are runs and tracked-
// change wrappers; anything else (pPr, bookmarks, hyperlinks) is
// skipped.
func parseParagraph(dec *xml.Decoder, start xml.StartElement, doc *pb.DocxDocumentWithMetadata) *pb.Paragraph {
	doc.ParagraphCount++
	p := &pb.Paragraph{}
	for {
		tok, err := dec.Token()
		if err != nil {
			return p
		}
		switch t := tok.(type) {
		case xml.StartElement:
			switch t.Name.Local {
			case "r":
				r := parseRun(dec, t)
				p.Content = append(p.Content, &pb.ParagraphChild{
					Child: &pb.ParagraphChild_Run{Run: r},
				})
			case "ins":
				doc.HasTrackedChanges = true
				ins := parseTrackedInsertion(dec, t)
				p.Content = append(p.Content, &pb.ParagraphChild{
					Child: &pb.ParagraphChild_Ins{Ins: ins},
				})
			case "del":
				doc.HasTrackedChanges = true
				del := parseTrackedDeletion(dec, t)
				p.Content = append(p.Content, &pb.ParagraphChild{
					Child: &pb.ParagraphChild_Del{Del: del},
				})
			default:
				skipElement(dec, t)
			}
		case xml.EndElement:
			if t.Name.Local == start.Name.Local {
				return p
			}
		}
	}
}

// parseTrackedInsertion reads a <w:ins> subtree within a paragraph,
// collecting runs as ParagraphChild entries on the wrapper.
func parseTrackedInsertion(dec *xml.Decoder, start xml.StartElement) *pb.TrackedChangeInsertion {
	ins := &pb.TrackedChangeInsertion{Info: parseTrackedInfo(start)}
	for {
		tok, err := dec.Token()
		if err != nil {
			return ins
		}
		switch t := tok.(type) {
		case xml.StartElement:
			if t.Name.Local == "r" {
				r := parseRun(dec, t)
				ins.Content = append(ins.Content, &pb.ParagraphChild{
					Child: &pb.ParagraphChild_Run{Run: r},
				})
			} else {
				skipElement(dec, t)
			}
		case xml.EndElement:
			if t.Name.Local == start.Name.Local {
				return ins
			}
		}
	}
}

// parseTrackedDeletion reads a <w:del> subtree within a paragraph.
func parseTrackedDeletion(dec *xml.Decoder, start xml.StartElement) *pb.TrackedChangeDeletion {
	del := &pb.TrackedChangeDeletion{Info: parseTrackedInfo(start)}
	for {
		tok, err := dec.Token()
		if err != nil {
			return del
		}
		switch t := tok.(type) {
		case xml.StartElement:
			if t.Name.Local == "r" {
				r := parseRun(dec, t)
				del.Content = append(del.Content, &pb.ParagraphChild{
					Child: &pb.ParagraphChild_Run{Run: r},
				})
			} else {
				skipElement(dec, t)
			}
		case xml.EndElement:
			if t.Name.Local == start.Name.Local {
				return del
			}
		}
	}
}

// parseRun reads a <w:r> subtree and returns the populated Run,
// capturing text, tracked-delete text, breaks, and tabs.
func parseRun(dec *xml.Decoder, start xml.StartElement) *pb.Run {
	r := &pb.Run{}
	for {
		tok, err := dec.Token()
		if err != nil {
			return r
		}
		switch t := tok.(type) {
		case xml.StartElement:
			switch t.Name.Local {
			case "t":
				text := parseTextContent(dec, t)
				r.Content = append(r.Content, &pb.RunChild{
					Child: &pb.RunChild_Text{Text: text},
				})
			case "delText":
				dt := parseDelText(dec, t)
				r.Content = append(r.Content, &pb.RunChild{
					Child: &pb.RunChild_DelText{DelText: dt},
				})
			case "br":
				r.Content = append(r.Content, &pb.RunChild{
					Child: &pb.RunChild_Br{Br: &pb.Break{Type: parseBreakType(t)}},
				})
				skipElement(dec, t)
			case "tab":
				r.Content = append(r.Content, &pb.RunChild{
					Child: &pb.RunChild_Tab{Tab: &pb.Tab{}},
				})
				skipElement(dec, t)
			default:
				skipElement(dec, t)
			}
		case xml.EndElement:
			if t.Name.Local == start.Name.Local {
				return r
			}
		}
	}
}

// parseTextContent reads the char-data under a <w:t> element into a
// TextContent, honoring xml:space="preserve".
func parseTextContent(dec *xml.Decoder, start xml.StartElement) *pb.TextContent {
	tc := &pb.TextContent{PreserveSpace: hasPreserveSpace(start)}
	for {
		tok, err := dec.Token()
		if err != nil {
			return tc
		}
		switch t := tok.(type) {
		case xml.CharData:
			tc.Value += string(t)
		case xml.StartElement:
			skipElement(dec, t)
		case xml.EndElement:
			if t.Name.Local == start.Name.Local {
				return tc
			}
		}
	}
}

// parseDelText reads <w:delText> into a DeletedText.
func parseDelText(dec *xml.Decoder, start xml.StartElement) *pb.DeletedText {
	dt := &pb.DeletedText{PreserveSpace: hasPreserveSpace(start)}
	for {
		tok, err := dec.Token()
		if err != nil {
			return dt
		}
		switch t := tok.(type) {
		case xml.CharData:
			dt.Value += string(t)
		case xml.StartElement:
			skipElement(dec, t)
		case xml.EndElement:
			if t.Name.Local == start.Name.Local {
				return dt
			}
		}
	}
}

// parseTable reads a <w:tbl> subtree into a Table, capturing table
// properties (style id, width), the table grid (column widths), and
// rows with their cells and block-level content.
func parseTable(dec *xml.Decoder, start xml.StartElement, doc *pb.DocxDocumentWithMetadata) *pb.Table {
	tbl := &pb.Table{}
	for {
		tok, err := dec.Token()
		if err != nil {
			return tbl
		}
		switch t := tok.(type) {
		case xml.StartElement:
			switch t.Name.Local {
			case "tblPr":
				tbl.Properties = parseTableProperties(dec, t)
			case "tblGrid":
				tbl.Grid = parseTableGrid(dec, t)
			case "tr":
				row := parseTableRow(dec, t, doc)
				tbl.Content = append(tbl.Content, &pb.TableChild{
					Child: &pb.TableChild_Row{Row: row},
				})
			default:
				skipElement(dec, t)
			}
		case xml.EndElement:
			if t.Name.Local == start.Name.Local {
				return tbl
			}
		}
	}
}

// parseTableProperties extracts the subset of <w:tblPr> we currently
// model: style id and logical width.
func parseTableProperties(dec *xml.Decoder, start xml.StartElement) *pb.TableProperties {
	props := &pb.TableProperties{}
	for {
		tok, err := dec.Token()
		if err != nil {
			return props
		}
		switch t := tok.(type) {
		case xml.StartElement:
			switch t.Name.Local {
			case "tblStyle":
				props.StyleId = attrVal(t, "val")
				skipElement(dec, t)
			case "tblW":
				props.Width = parseTableWidth(t)
				skipElement(dec, t)
			default:
				skipElement(dec, t)
			}
		case xml.EndElement:
			if t.Name.Local == start.Name.Local {
				return props
			}
		}
	}
}

// parseTableGrid reads <w:tblGrid> and collects each <w:gridCol>'s
// width attribute.
func parseTableGrid(dec *xml.Decoder, start xml.StartElement) *pb.TableGrid {
	grid := &pb.TableGrid{}
	for {
		tok, err := dec.Token()
		if err != nil {
			return grid
		}
		switch t := tok.(type) {
		case xml.StartElement:
			if t.Name.Local == "gridCol" {
				col := &pb.TableGridColumn{}
				if w, err := parseInt32(attrVal(t, "w")); err == nil {
					col.W = w
				}
				grid.Columns = append(grid.Columns, col)
			}
			skipElement(dec, t)
		case xml.EndElement:
			if t.Name.Local == start.Name.Local {
				return grid
			}
		}
	}
}

// parseTableRow reads <w:tr>, capturing optional row properties and
// the sequence of cells.
func parseTableRow(dec *xml.Decoder, start xml.StartElement, doc *pb.DocxDocumentWithMetadata) *pb.TableRow {
	row := &pb.TableRow{}
	for {
		tok, err := dec.Token()
		if err != nil {
			return row
		}
		switch t := tok.(type) {
		case xml.StartElement:
			switch t.Name.Local {
			case "tc":
				cell := parseTableCell(dec, t, doc)
				row.Content = append(row.Content, &pb.TableRowChild{
					Child: &pb.TableRowChild_Cell{Cell: cell},
				})
			default:
				skipElement(dec, t)
			}
		case xml.EndElement:
			if t.Name.Local == start.Name.Local {
				return row
			}
		}
	}
}

// parseTableCell reads <w:tc>, capturing optional cell properties and
// recursing into parseBlockContainer for nested block-level content
// (paragraphs + tables).
func parseTableCell(dec *xml.Decoder, start xml.StartElement, doc *pb.DocxDocumentWithMetadata) *pb.TableCell {
	cell := &pb.TableCell{}
	for {
		tok, err := dec.Token()
		if err != nil {
			return cell
		}
		switch t := tok.(type) {
		case xml.StartElement:
			switch t.Name.Local {
			case "tcPr":
				cell.Properties = parseTableCellProperties(dec, t)
			case "p":
				p := parseParagraph(dec, t, doc)
				cell.Content = append(cell.Content, &pb.BlockLevelElement{
					Element: &pb.BlockLevelElement_Paragraph{Paragraph: p},
				})
			case "tbl":
				tbl := parseTable(dec, t, doc)
				cell.Content = append(cell.Content, &pb.BlockLevelElement{
					Element: &pb.BlockLevelElement_Table{Table: tbl},
				})
			default:
				skipElement(dec, t)
			}
		case xml.EndElement:
			if t.Name.Local == start.Name.Local {
				return cell
			}
		}
	}
}

// parseTableCellProperties extracts the subset of <w:tcPr> we model:
// cell width and horizontal grid span.
func parseTableCellProperties(dec *xml.Decoder, start xml.StartElement) *pb.TableCellProperties {
	props := &pb.TableCellProperties{}
	for {
		tok, err := dec.Token()
		if err != nil {
			return props
		}
		switch t := tok.(type) {
		case xml.StartElement:
			switch t.Name.Local {
			case "tcW":
				props.Width = parseTableWidth(t)
			case "gridSpan":
				if n, err := parseInt32(attrVal(t, "val")); err == nil {
					props.GridSpan = n
				}
			}
			skipElement(dec, t)
		case xml.EndElement:
			if t.Name.Local == start.Name.Local {
				return props
			}
		}
	}
}

// parseTableWidth captures the <w:tblW> / <w:tcW> form: w:w (numeric
// amount) + w:type (enum).
func parseTableWidth(se xml.StartElement) *pb.TableWidth {
	tw := &pb.TableWidth{}
	for _, a := range se.Attr {
		switch a.Name.Local {
		case "w":
			if n, err := parseInt32(a.Value); err == nil {
				tw.W = n
			}
		case "type":
			switch a.Value {
			case "nil":
				tw.Type = pb.TableWidthType_TABLE_WIDTH_NIL
			case "auto":
				tw.Type = pb.TableWidthType_TABLE_WIDTH_AUTO
			case "dxa":
				tw.Type = pb.TableWidthType_TABLE_WIDTH_DXA
			case "pct":
				tw.Type = pb.TableWidthType_TABLE_WIDTH_PCT
			}
		}
	}
	return tw
}

// attrVal returns the value of the named attribute (local-name match)
// on the start element, or the empty string if absent.
func attrVal(se xml.StartElement, local string) string {
	for _, a := range se.Attr {
		if a.Name.Local == local {
			return a.Value
		}
	}
	return ""
}

// skipElement consumes tokens until the matching end-element for `se`
// is seen, discarding content. Used to ignore elements we don't yet
// model without disrupting the recursive descent.
func skipElement(dec *xml.Decoder, se xml.StartElement) {
	depth := 1
	for depth > 0 {
		tok, err := dec.Token()
		if err != nil {
			return
		}
		switch tok.(type) {
		case xml.StartElement:
			depth++
		case xml.EndElement:
			depth--
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
