// Package gluon is a standalone experiment wiring
// github.com/accretional/gluon/v2 up against DOCX word/document.xml and
// comparing the result to docxcodec.Decode. See README.md.
package gluon

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"io"
	"strings"
)

// knownNames is the set of element names the grammar in xml.ebnf
// recognises. Everything else collapses to the "N" placeholder.
var knownNames = map[string]bool{
	"w:document": true,
	"w:body":     true,
	"w:p":        true,
	"w:r":        true,
	"w:t":        true,
	"w:ins":      true,
	"w:del":      true,
	"w:delText":  true,
	"w:br":       true,
	"w:tab":      true,
}

// Tokenize reads raw XML bytes and emits a space-separated token
// stream that matches the grammar in xml.ebnf:
//
//   - "<" / ">" / "/" punctuation is emitted verbatim
//   - known element names (see knownNames) are emitted as literal names
//   - other element names collapse to "N"
//   - each attribute on a StartElement becomes a single "A"
//   - each non-empty text run between elements becomes a single "T"
//   - self-closing "<elem/>" is normalised by encoding/xml to a
//     StartElement + EndElement pair, so the grammar always sees the
//     eight-token "< name A... > < / name >" shape
//   - XML declaration, comments, and processing instructions are
//     discarded before tokenisation
func Tokenize(src []byte) (string, error) {
	src = stripProlog(src)

	dec := xml.NewDecoder(bytes.NewReader(src))
	dec.Strict = false

	var out []string
	emit := func(tok string) { out = append(out, tok) }

	for {
		tok, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", fmt.Errorf("tokenize: %w", err)
		}
		switch t := tok.(type) {
		case xml.StartElement:
			emit("<")
			emit(nameToken(t.Name))
			for range t.Attr {
				emit("A")
			}
			emit(">")
		case xml.EndElement:
			emit("<")
			emit("/")
			emit(nameToken(t.Name))
			emit(">")
		case xml.CharData:
			if len(bytes.TrimSpace(t)) > 0 {
				emit("T")
			}
		case xml.Comment, xml.ProcInst, xml.Directive:
			// ignored
		}
	}
	return strings.Join(out, " "), nil
}

// stripProlog removes the XML declaration if present. encoding/xml
// handles it, but being explicit here keeps Tokenize's output stable.
func stripProlog(src []byte) []byte {
	src = bytes.TrimLeft(src, "\ufeff \r\n\t")
	if !bytes.HasPrefix(src, []byte("<?xml")) {
		return src
	}
	end := bytes.Index(src, []byte("?>"))
	if end < 0 {
		return src
	}
	return src[end+2:]
}

// nameToken maps a decoded XML name to the literal the grammar expects.
// encoding/xml resolves namespace URIs to n.Space. We rebuild the "w:"
// prefix for WordprocessingML names and fall back to "N" for everything
// else.
func nameToken(n xml.Name) string {
	full := n.Local
	if n.Space == "http://schemas.openxmlformats.org/wordprocessingml/2006/main" {
		full = "w:" + n.Local
	}
	if knownNames[full] {
		return full
	}
	return "N"
}
