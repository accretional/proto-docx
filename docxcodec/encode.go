package docxcodec

import (
	"fmt"

	pb "openformat-docx/gen/go/openformat/v1"
)

// Encode returns the raw DOCX bytes for doc. Because a structural
// re-emission of the full OOXML package would need every schema element
// covered (~4000 lines of docx.proto), this first cut uses the
// raw-bytes round-trip path: DocxDocumentWithMetadata.RawBytes is
// populated by Decode on the way in and returned verbatim on the way
// out. Callers that need to mutate the package should treat the output
// of Decode as read-only.
//
// Returns an error if doc or doc.RawBytes is nil/empty — the contract
// is byte-faithful round-trip.
func Encode(doc *pb.DocxDocumentWithMetadata) ([]byte, error) {
	if doc == nil {
		return nil, fmt.Errorf("docxcodec: nil document")
	}
	if len(doc.RawBytes) == 0 {
		return nil, fmt.Errorf("docxcodec: document has no RawBytes (structural encode not implemented)")
	}
	out := make([]byte, len(doc.RawBytes))
	copy(out, doc.RawBytes)
	return out, nil
}
