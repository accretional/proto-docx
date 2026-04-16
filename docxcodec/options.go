package docxcodec

import (
	"archive/zip"
	"bytes"
	"fmt"

	xmlpb "openformat/gen/go/openformat/v1"
	"openformat/xmlcodec"

	pb "openformat-docx/gen/go/openformat/v1"
)

// DecodeOptions controls what DecodeWith populates beyond the minimum
// required for byte-faithful Encode round-trip.
type DecodeOptions struct {
	// IncludeTypedParts, when true, parses word/document.xml into a
	// typed *XmlDocumentWithMetadata via proto-xml's xmlcodec. The
	// result is available on Decoded.Document. Other OPC parts remain
	// available as raw bytes via the ZIP carried in RawBytes.
	IncludeTypedParts bool
}

// Decoded is the result of DecodeWith. It embeds the
// DocxDocumentWithMetadata that plain Decode returns, plus any optional
// typed OPC parts requested via DecodeOptions.
type Decoded struct {
	*pb.DocxDocumentWithMetadata

	// Document carries word/document.xml parsed as a typed XML tree
	// when DecodeOptions.IncludeTypedParts is true. Nil otherwise.
	//
	// Held as a Go-side field rather than on the embedded protobuf
	// because docx.proto and xml.proto live in separate modules and
	// docx.proto does not import xml.proto.
	Document *xmlpb.XmlDocumentWithMetadata
}

// DecodeWith is the option-taking counterpart of Decode. With
// opts.IncludeTypedParts=true, word/document.xml is additionally handed
// to proto-xml's xmlcodec.Decode so callers can walk the document as a
// typed XML tree without re-opening the ZIP themselves.
//
// Absence of word/document.xml is not an error — d.Document stays nil.
// A malformed document part *is* an error, because callers asking for
// typed parts want to know when they won't get one.
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
	for _, f := range zr.File {
		if f.Name != "word/document.xml" {
			continue
		}
		data, err := readZipFile(f)
		if err != nil {
			return nil, fmt.Errorf("docxcodec: read word/document.xml: %w", err)
		}
		typed, err := xmlcodec.Decode(data)
		if err != nil {
			return nil, fmt.Errorf("docxcodec: xmlcodec.Decode(word/document.xml): %w", err)
		}
		d.Document = typed
		return d, nil
	}
	return d, nil
}
