module openformat-docx

go 1.26.2

require (
	golang.org/x/image v0.39.0
	google.golang.org/protobuf v1.36.11
	openformat v0.0.0-00010101000000-000000000000
)

// proto-docx depends on proto-xml for (a) the xmlcodec package that
// parses individual DOCX XML parts, and (b) the generated MimeType
// proto from openformat/v1/mime.proto, which both modules share.
// Until proto-xml publishes a module path, we resolve it from a sibling
// checkout.
replace openformat => ../proto-xml
