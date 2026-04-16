// Package docxcodec is the DOCX (OOXML WordprocessingML) codec for the
// OpenFormat proto family.
//
// Two entry points mirror proto-xml's xmlcodec pattern:
//
//	Decode(raw []byte) (*pb.DocxDocumentWithMetadata, error)
//	Encode(doc *pb.DocxDocumentWithMetadata) ([]byte, error)
//
// Decode unzips the DOCX package, populates the typed fields of
// DocxDocumentWithMetadata (paragraph/image/font counts, package
// relationships, media parts, presence of tracked changes / comments /
// notes), and always records the verbatim input bytes on
// DocxDocumentWithMetadata.RawBytes.
//
// Encode is a raw-bytes round-trip: given a DocxDocumentWithMetadata it
// returns DocxDocumentWithMetadata.RawBytes, which Decode populated on
// the way in. Structural re-emission of the full package is out of
// scope for this first cut — proto fidelity is preserved via the raw
// bytes escape hatch, matching the pattern used in proto-xml for
// attributes and CDATA.
//
// Individual XML parts inside the DOCX (word/document.xml, styles.xml,
// fontTable.xml, etc.) can be parsed with proto-xml's xmlcodec package
// when a caller needs a typed XmlDocumentWithMetadata rather than just
// counts.
package docxcodec
