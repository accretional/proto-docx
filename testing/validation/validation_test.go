// Package validation runs a single parametrized test across every DOCX
// fixture under data/. One test — TestValidate — asserts the same
// invariants for every file: Decode succeeds, RawBytes round-trips via
// Encode, and a few summary fields are populated when the fixture
// declares them.
package validation

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"openformat-docx/docxcodec"
)

// dataDir locates the repo-level data/ directory relative to this
// file so tests are runnable via `go test ./...` from any cwd.
func dataDir(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(thisFile), "..", "..", "data"))
}

func collectDocx(t *testing.T, root string) []string {
	t.Helper()
	var paths []string
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		if strings.HasSuffix(strings.ToLower(path), ".docx") {
			paths = append(paths, path)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", root, err)
	}
	return paths
}

func TestValidate(t *testing.T) {
	root := dataDir(t)
	paths := collectDocx(t, root)
	if len(paths) == 0 {
		t.Fatalf("no .docx fixtures found under %s — run ./setup.sh first", root)
	}

	for _, p := range paths {
		rel, _ := filepath.Rel(root, p)
		t.Run(rel, func(t *testing.T) {
			raw, err := os.ReadFile(p)
			if err != nil {
				t.Fatalf("read: %v", err)
			}
			if !docxcodec.IsDocx(raw) {
				t.Fatalf("IsDocx = false")
			}
			doc, err := docxcodec.Decode(raw)
			if err != nil {
				t.Fatalf("Decode: %v", err)
			}
			if len(doc.RawBytes) != len(raw) {
				t.Errorf("RawBytes len = %d, want %d", len(doc.RawBytes), len(raw))
			}
			if !bytes.Equal(doc.RawBytes, raw) {
				t.Error("RawBytes not preserved verbatim")
			}
			out, err := docxcodec.Encode(doc)
			if err != nil {
				t.Fatalf("Encode: %v", err)
			}
			if !bytes.Equal(out, raw) {
				t.Errorf("raw-bytes round-trip differs: %d vs %d", len(out), len(raw))
			}
			if doc.DocxPackage == nil {
				t.Fatal("DocxPackage nil")
			}
			// Every fixture has word/document.xml, so Document/Body must
			// be set, and the typed body tree must have at least one
			// Paragraph child (every fixture has ≥1 <w:p>).
			if doc.DocxPackage.Document == nil || doc.DocxPackage.Document.Body == nil {
				t.Fatal("Document/Body not populated")
			}
			body := doc.DocxPackage.Document.Body
			if len(body.Content) == 0 {
				t.Error("Body.Content is empty — typed tree not populated")
			} else {
				anyPara := false
				for _, e := range body.Content {
					if e.GetParagraph() != nil {
						anyPara = true
						break
					}
				}
				if !anyPara {
					t.Error("Body.Content has no Paragraph entries")
				}
			}

			// Exercise the typed-parts path too: word/document.xml
			// handed to proto-xml's xmlcodec must parse cleanly for
			// every fixture, and the resulting tree must have a
			// document element.
			d, err := docxcodec.DecodeWith(raw, docxcodec.DecodeOptions{IncludeTypedParts: true})
			if err != nil {
				t.Fatalf("DecodeWith(typed): %v", err)
			}
			if d.Document == nil {
				t.Fatal("DecodeWith: Document nil")
			}
			if d.Document.Document == nil || d.Document.Document.DocumentElement == nil {
				t.Error("typed XML has no root element")
			}

			// Extraction helpers must never error on a valid fixture.
			if _, err := d.Text(); err != nil {
				t.Errorf("d.Text: %v", err)
			}
			if _, err := d.Fonts(); err != nil {
				t.Errorf("d.Fonts: %v", err)
			}
			secs, err := d.Sections()
			if err != nil {
				t.Errorf("d.Sections: %v", err)
			}
			// Tables fixture must surface at least one Table in the
			// typed body tree.
			if strings.Contains(rel, "12_tables") {
				var tables []string
				for _, e := range body.Content {
					if tbl := e.GetTable(); tbl != nil {
						tables = append(tables, "tbl")
						if len(tbl.Content) == 0 {
							t.Error("table has no rows")
						}
						for _, tc := range tbl.Content {
							row := tc.GetRow()
							if row == nil || len(row.Content) == 0 {
								t.Error("table row has no cells")
							}
						}
					}
				}
				if len(tables) == 0 {
					t.Error("12_tables fixture surfaced no Table entries in body")
				}
			}

			// Hyperlinks/bookmarks fixture must surface at least one
			// hyperlink and one bookmarkStart/end pair in the typed body.
			if strings.Contains(rel, "13_hyperlinks_bookmarks") {
				var hyperlinks, bmStart, bmEnd int
				for _, e := range body.Content {
					p := e.GetParagraph()
					if p == nil {
						continue
					}
					for _, c := range p.Content {
						if c.GetHyperlink() != nil {
							hyperlinks++
						}
						if c.GetBookmarkStart() != nil {
							bmStart++
						}
						if c.GetBookmarkEnd() != nil {
							bmEnd++
						}
					}
				}
				if hyperlinks < 2 {
					t.Errorf("expected ≥2 hyperlinks, got %d", hyperlinks)
				}
				if bmStart < 2 || bmEnd < 2 {
					t.Errorf("expected ≥2 bookmarkStart/End pairs, got start=%d end=%d", bmStart, bmEnd)
				}
			}

			// SDT fixture must surface at least one block-level SDT
			// (direct child of Body) and one run-level SDT (child of a
			// paragraph) in the typed tree.
			if strings.Contains(rel, "14_sdt") {
				var blockSdts, runSdts int
				for _, e := range body.Content {
					if e.GetSdt() != nil {
						blockSdts++
					}
					if p := e.GetParagraph(); p != nil {
						for _, c := range p.Content {
							if c.GetSdt() != nil {
								runSdts++
							}
						}
					}
				}
				if blockSdts == 0 {
					t.Error("expected ≥1 block-level Sdt, got none")
				}
				if runSdts == 0 {
					t.Error("expected ≥1 run-level Sdt, got none")
				}
			}

			// Fixtures whose filenames imply headers/footers must
			// surface at least one section with matching references.
			if strings.Contains(rel, "10_headers_footers") || strings.Contains(rel, "11_kitchen_sink") {
				if len(secs) == 0 {
					t.Errorf("expected ≥1 section, got none")
				} else {
					s := secs[0]
					if len(s.HeaderRefs) == 0 || len(s.FooterRefs) == 0 {
						t.Errorf("expected header+footer refs, got headers=%d footers=%d", len(s.HeaderRefs), len(s.FooterRefs))
					}
					if s.PageWidth == 0 || s.PageHeight == 0 {
						t.Errorf("expected page size populated, got %dx%d", s.PageWidth, s.PageHeight)
					}
				}
			}
		})
	}
}
