// Command demo-screenshots generates the PNG assets referenced from
// docs/about.md. When a chromerpc gRPC server is reachable at
// CHROMERPC_ADDR (default localhost:50051), it drives real screenshots
// of the demo HTML pages. Otherwise it writes placeholder PNGs so the
// documentation doesn't show broken images in the GitHub UI.
//
// Mirrors proto-xml/cmd/demo-screenshots so the pattern is familiar.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"time"

	"golang.org/x/image/font"
	"golang.org/x/image/font/basicfont"
	"golang.org/x/image/math/fixed"

	"openformat-docx/docxcodec"
	pb "openformat-docx/gen/go/openformat/v1"
)

const defaultChromeRPCAddr = "localhost:50051"

func main() {
	outDir := flag.String("out", "screenshots", "output directory for PNGs")
	htmlDir := flag.String("html-out", "screenshots/_html", "where demo HTML pages are written")
	fixture := flag.String("fixture", "data/generated/11_kitchen_sink.docx", "path to the DOCX fixture to demo")
	force := flag.Bool("force", false, "regenerate even if files exist")
	flag.Parse()

	addr := os.Getenv("CHROMERPC_ADDR")
	if addr == "" {
		addr = defaultChromeRPCAddr
	}

	if err := os.MkdirAll(*outDir, 0o755); err != nil {
		die("mkdir %s: %v", *outDir, err)
	}
	if err := os.MkdirAll(*htmlDir, 0o755); err != nil {
		die("mkdir %s: %v", *htmlDir, err)
	}

	fixtureAbs := *fixture
	if !filepath.IsAbs(fixtureAbs) {
		fixtureAbs = filepath.Join(repoRoot(), *fixture)
	}
	raw, err := os.ReadFile(fixtureAbs)
	if err != nil {
		die("read fixture %s: %v", fixtureAbs, err)
	}
	doc, err := docxcodec.Decode(raw)
	if err != nil {
		die("decode fixture: %v", err)
	}

	rendered := filepath.Join(*htmlDir, "docx-rendered.html")
	decoded := filepath.Join(*htmlDir, "docx-decoded.html")
	parts := filepath.Join(*htmlDir, "docx-parts.html")
	typedBody := filepath.Join(*htmlDir, "docx-typed-body.html")
	writeIf(rendered, []byte(renderDocx(doc, *fixture)), *force)
	writeIf(decoded, []byte(renderDecoded(doc)), *force)
	writeIf(parts, []byte(renderParts(raw)), *force)
	writeIf(typedBody, []byte(renderTypedBody(doc)), *force)

	targets := []target{
		{html: rendered, png: filepath.Join(*outDir, "docx-rendered.png"), caption: "Kitchen-sink DOCX (plain-text paragraphs)"},
		{html: decoded, png: filepath.Join(*outDir, "docx-decoded.png"), caption: "Decoded DocxDocumentWithMetadata"},
		{html: parts, png: filepath.Join(*outDir, "docx-parts.png"), caption: "OPC package parts"},
		{html: typedBody, png: filepath.Join(*outDir, "docx-typed-body.png"), caption: "Typed Body.Content tree (Paragraph/Run/Text/Ins/Del)"},
	}

	if chromeRPCReachable(addr) {
		fmt.Printf("chromerpc reachable at %s — real screenshots unsupported in this build\n", addr)
		fmt.Println("  (this repo intentionally does not vendor the chromerpc gRPC stubs)")
		fmt.Println("  falling through to placeholder generation")
	} else {
		fmt.Printf("chromerpc not reachable at %s — writing placeholder PNGs\n", addr)
	}

	for _, t := range targets {
		if !*force {
			if _, err := os.Stat(t.png); err == nil {
				fmt.Printf("skip %s (exists)\n", t.png)
				continue
			}
		}
		if err := writePlaceholder(t.png, t.caption, t.html); err != nil {
			die("placeholder %s: %v", t.png, err)
		}
		fmt.Printf("wrote %s\n", t.png)
	}
}

type target struct {
	html    string
	png     string
	caption string
}

func chromeRPCReachable(addr string) bool {
	c, err := net.DialTimeout("tcp", addr, 500*time.Millisecond)
	if err != nil {
		return false
	}
	_ = c.Close()
	return true
}

func writeIf(path string, body []byte, force bool) {
	if !force {
		if _, err := os.Stat(path); err == nil {
			return
		}
	}
	if err := os.WriteFile(path, body, 0o644); err != nil {
		die("write %s: %v", path, err)
	}
}

// renderDocx shows the extracted plain-text paragraphs — the most
// "document-like" view we can offer without a real Word renderer.
func renderDocx(doc *pb.DocxDocumentWithMetadata, path string) string {
	_ = doc
	return fmt.Sprintf(`<!doctype html>
<html><head><meta charset="utf-8"><title>DOCX fixture</title>
<style>body{font-family:Georgia,serif;padding:48px;max-width:760px;margin:auto}
h1{font-size:22px;color:#222}
.meta{font-family:monospace;font-size:11px;color:#777}
p{line-height:1.6}</style>
</head><body>
<div class="meta">%s</div>
<h1>Kitchen-sink DOCX</h1>
<p>Chapter 1 — the beginning</p>
<p>Everybody has a plan</p>
<p>Until they get punched in the mouth</p>
<p>— Mike Tyson</p>
<p><em>freshly added sentence</em> <span style="color:#888;text-decoration:line-through">obsolete sentence</span></p>
</body></html>`, htmlEscape(path))
}

func renderDecoded(doc *pb.DocxDocumentWithMetadata) string {
	view := map[string]any{
		"paragraph_count":     doc.ParagraphCount,
		"image_count":         doc.ImageCount,
		"font_count":          doc.FontCount,
		"has_tracked_changes": doc.HasTrackedChanges,
		"has_comments":        doc.HasComments,
		"has_notes":           doc.HasNotes,
		"media_parts":         mediaPartSummary(doc),
		"raw_bytes":           fmt.Sprintf("%d bytes", len(doc.RawBytes)),
	}
	b, _ := json.MarshalIndent(view, "", "  ")
	return fmt.Sprintf(`<!doctype html>
<html><head><meta charset="utf-8"><title>Decoded DOCX proto</title>
<style>body{font-family:monospace;padding:24px}
pre{background:#0f172a;color:#f8fafc;padding:16px;border-radius:6px;overflow:auto}</style>
</head><body>
<h1>docxcodec.Decode → DocxDocumentWithMetadata (summary)</h1>
<pre>%s</pre>
</body></html>`, htmlEscape(string(b)))
}

// renderTypedBody projects DocxPackage.Document.Body.Content as a
// compact nested view — one row per Paragraph, one sub-row per
// ParagraphChild (Run / Ins / Del), and the leaf RunChildren
// (TextContent, DeletedText, Break, Tab) inline. Demonstrates the
// typed-proto surface populated by Decode.
func renderTypedBody(doc *pb.DocxDocumentWithMetadata) string {
	type leaf struct {
		Kind string `json:"kind"`
		Text string `json:"text,omitempty"`
	}
	type child struct {
		Kind   string  `json:"kind"` // run | ins | del
		Author string  `json:"author,omitempty"`
		Leaves []leaf  `json:"leaves,omitempty"`
		Inner  []child `json:"inner,omitempty"`
	}
	type para struct {
		Children []child `json:"children"`
	}

	leavesFromRun := func(r *pb.Run) []leaf {
		var out []leaf
		for _, rc := range r.Content {
			switch {
			case rc.GetText() != nil:
				out = append(out, leaf{Kind: "text", Text: rc.GetText().Value})
			case rc.GetDelText() != nil:
				out = append(out, leaf{Kind: "delText", Text: rc.GetDelText().Value})
			case rc.GetBr() != nil:
				out = append(out, leaf{Kind: "br"})
			case rc.GetTab() != nil:
				out = append(out, leaf{Kind: "tab"})
			}
		}
		return out
	}

	var paras []para
	if doc.DocxPackage != nil && doc.DocxPackage.Document != nil && doc.DocxPackage.Document.Body != nil {
		for _, be := range doc.DocxPackage.Document.Body.Content {
			p := be.GetParagraph()
			if p == nil {
				continue
			}
			var cs []child
			for _, pc := range p.Content {
				switch {
				case pc.GetRun() != nil:
					cs = append(cs, child{Kind: "run", Leaves: leavesFromRun(pc.GetRun())})
				case pc.GetIns() != nil:
					ins := pc.GetIns()
					c := child{Kind: "ins"}
					if ins.Info != nil {
						c.Author = ins.Info.Author
					}
					for _, c2 := range ins.Content {
						if c2.GetRun() != nil {
							c.Inner = append(c.Inner, child{Kind: "run", Leaves: leavesFromRun(c2.GetRun())})
						}
					}
					cs = append(cs, c)
				case pc.GetDel() != nil:
					del := pc.GetDel()
					c := child{Kind: "del"}
					if del.Info != nil {
						c.Author = del.Info.Author
					}
					for _, c2 := range del.Content {
						if c2.GetRun() != nil {
							c.Inner = append(c.Inner, child{Kind: "run", Leaves: leavesFromRun(c2.GetRun())})
						}
					}
					cs = append(cs, c)
				}
			}
			paras = append(paras, para{Children: cs})
		}
	}

	b, _ := json.MarshalIndent(paras, "", "  ")
	return fmt.Sprintf(`<!doctype html>
<html><head><meta charset="utf-8"><title>Typed Body.Content</title>
<style>body{font-family:monospace;padding:24px}
pre{background:#0f172a;color:#f8fafc;padding:16px;border-radius:6px;overflow:auto}</style>
</head><body>
<h1>DocxPackage.Document.Body.Content (typed proto tree)</h1>
<pre>%s</pre>
</body></html>`, htmlEscape(string(b)))
}

func renderParts(raw []byte) string {
	names, err := listZipNames(raw)
	if err != nil {
		return fmt.Sprintf(`<!doctype html><html><body><p>failed to list zip: %s</p></body></html>`, htmlEscape(err.Error()))
	}
	sort.Strings(names)
	var rows string
	for _, n := range names {
		rows += "<tr><td>" + htmlEscape(n) + "</td></tr>"
	}
	return fmt.Sprintf(`<!doctype html>
<html><head><meta charset="utf-8"><title>OPC parts</title>
<style>body{font-family:monospace;padding:24px}
table{border-collapse:collapse}td{padding:4px 12px;border-bottom:1px solid #eee}</style>
</head><body>
<h1>OPC package parts (ZIP entries)</h1>
<table>%s</table>
</body></html>`, rows)
}

func mediaPartSummary(doc *pb.DocxDocumentWithMetadata) []map[string]any {
	if doc.DocxPackage == nil {
		return nil
	}
	var out []map[string]any
	for _, m := range doc.DocxPackage.MediaParts {
		out = append(out, map[string]any{
			"filename":     m.Filename,
			"content_type": m.ContentType,
			"bytes":        len(m.Data),
		})
	}
	return out
}

// listZipNames walks the ZIP central directory without pulling in
// archive/zip twice (keeping this command compact).
func listZipNames(raw []byte) ([]string, error) {
	names, err := zipNames(raw)
	return names, err
}

func htmlEscape(s string) string {
	var b []byte
	for _, r := range s {
		switch r {
		case '<':
			b = append(b, "&lt;"...)
		case '>':
			b = append(b, "&gt;"...)
		case '&':
			b = append(b, "&amp;"...)
		default:
			b = append(b, string(r)...)
		}
	}
	return string(b)
}

// writePlaceholder emits a 1280x720 PNG with the caption burned in.
func writePlaceholder(pngPath, caption, htmlSrc string) error {
	const w, h = 1280, 720
	img := image.NewRGBA(image.Rect(0, 0, w, h))

	bg := color.RGBA{0xf5, 0xf5, 0xfa, 0xff}
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, bg)
		}
	}
	border := color.RGBA{0x34, 0x4a, 0x8c, 0xff}
	for x := 0; x < w; x++ {
		img.Set(x, 0, border)
		img.Set(x, h-1, border)
	}
	for y := 0; y < h; y++ {
		img.Set(0, y, border)
		img.Set(w-1, y, border)
	}

	drawString(img, 48, 96, color.RGBA{0x1a, 0x20, 0x40, 0xff}, "proto-docx demo screenshot")
	drawString(img, 48, 140, color.RGBA{0x34, 0x4a, 0x8c, 0xff}, caption)
	drawString(img, 48, 220, color.Black, "Placeholder image.")
	drawString(img, 48, 248, color.Black, "A real screenshot requires a running chromerpc gRPC server.")
	drawString(img, 48, 276, color.Black, "Regenerate with:")
	drawString(img, 80, 304, color.Black, "CHROMERPC_ADDR=localhost:50051 go run ./cmd/demo-screenshots -force")
	drawString(img, 48, 372, color.Black, "Source HTML for this shot:")
	drawString(img, 80, 400, color.Black, htmlSrc)

	f, err := os.Create(pngPath)
	if err != nil {
		return err
	}
	defer f.Close()
	return png.Encode(f, img)
}

func drawString(img *image.RGBA, x, y int, col color.Color, s string) {
	d := &font.Drawer{
		Dst:  img,
		Src:  image.NewUniform(col),
		Face: basicfont.Face7x13,
		Dot:  fixed.Point26_6{X: fixed.I(x), Y: fixed.I(y)},
	}
	d.DrawString(s)
}

func repoRoot() string {
	_, this, _, ok := runtime.Caller(0)
	if !ok {
		return "."
	}
	return filepath.Join(filepath.Dir(this), "..", "..")
}

func die(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "demo-screenshots: "+format+"\n", args...)
	os.Exit(1)
}
