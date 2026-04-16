module openformat-docx

go 1.26.2

require (
	github.com/accretional/chromerpc v0.0.0-00010101000000-000000000000
	github.com/accretional/gluon v0.0.0-00010101000000-000000000000
	golang.org/x/image v0.39.0
	google.golang.org/grpc v1.80.0
	google.golang.org/protobuf v1.36.11
	openformat v0.0.0-00010101000000-000000000000
)

require (
	github.com/accretional/proto-expr v0.0.0-20260416071217-9a69001c59bb // indirect
	golang.org/x/net v0.53.0 // indirect
	golang.org/x/sys v0.43.0 // indirect
	golang.org/x/text v0.36.0 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20260120221211-b8f7ae30c516 // indirect
)

// proto-docx depends on proto-xml for (a) the xmlcodec package that
// parses individual DOCX XML parts, and (b) the generated MimeType
// proto from openformat/v1/mime.proto, which both modules share.
// Until proto-xml publishes a module path, we resolve it from a sibling
// checkout.
replace openformat => ../proto-xml

// Demo screenshots use chromerpc's HeadlessBrowserService gRPC client.
// Resolved from a sibling checkout so the demo tool doesn't fetch over
// the network.
replace github.com/accretional/chromerpc => ../chromerpc

// gluon/v2 is used by the gluon/ experiment subpackage, which compares
// EBNF-grammar-driven parsing (via ParseCST) to the hand-written DOCX
// parser in docxcodec/. Resolved from a sibling checkout.
replace github.com/accretional/gluon => ../gluon
