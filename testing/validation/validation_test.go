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
			// be set.
			if doc.DocxPackage.Document == nil || doc.DocxPackage.Document.Body == nil {
				t.Error("Document/Body not populated")
			}
		})
	}
}
