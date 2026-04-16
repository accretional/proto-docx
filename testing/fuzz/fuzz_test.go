// Package fuzz runs fuzz tests against docxcodec.Decode.
//
// Goals:
//  1. Decode must never panic on arbitrary input.
//  2. For inputs that Decode accepts, Encode followed by another Decode
//     must also not panic.
//
// Seeded with every .docx file under data/ so the fuzzer starts from
// real-world shapes rather than random bytes.
package fuzz

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"openformat-docx/docxcodec"
)

func seedCorpus(f *testing.F) {
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		f.Fatal("runtime.Caller failed")
	}
	root := filepath.Clean(filepath.Join(filepath.Dir(thisFile), "..", "..", "data"))
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() || !strings.HasSuffix(strings.ToLower(path), ".docx") {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		f.Add(data)
		return nil
	})
	if err != nil {
		f.Fatalf("walk %s: %v", root, err)
	}
	// Bare seeds so the fuzzer can explore non-DOCX inputs without
	// relying on the corpus.
	f.Add([]byte{})
	f.Add([]byte("not a docx"))
	f.Add([]byte{0x50, 0x4B, 0x03, 0x04})
}

func FuzzDecode(f *testing.F) {
	seedCorpus(f)
	f.Fuzz(func(t *testing.T, data []byte) {
		doc, err := docxcodec.Decode(data)
		if err != nil {
			return
		}
		out, err := docxcodec.Encode(doc)
		if err != nil {
			return
		}
		// Accepted → Encode → Decode must also not panic.
		if _, err := docxcodec.Decode(out); err != nil {
			t.Fatalf("re-decode of encoded output failed: %v", err)
		}
	})
}
