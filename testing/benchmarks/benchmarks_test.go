// Package benchmarks runs Decode/Encode benchmarks across every
// fixture under data/. Use `go test -bench=. ./testing/benchmarks/...`
// (test scripts wrap this).
package benchmarks

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"openformat-docx/docxcodec"
)

type fixture struct {
	name string
	data []byte
}

func loadFixtures(tb testing.TB) []fixture {
	tb.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		tb.Fatal("runtime.Caller failed")
	}
	root := filepath.Clean(filepath.Join(filepath.Dir(thisFile), "..", "..", "data"))
	var out []fixture
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
		rel, _ := filepath.Rel(root, path)
		out = append(out, fixture{name: rel, data: data})
		return nil
	})
	if err != nil {
		tb.Fatalf("walk: %v", err)
	}
	return out
}

func BenchmarkDecode(b *testing.B) {
	for _, fx := range loadFixtures(b) {
		b.Run(fx.name, func(b *testing.B) {
			b.SetBytes(int64(len(fx.data)))
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if _, err := docxcodec.Decode(fx.data); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func BenchmarkEncode(b *testing.B) {
	for _, fx := range loadFixtures(b) {
		doc, err := docxcodec.Decode(fx.data)
		if err != nil {
			b.Fatalf("setup Decode: %v", err)
		}
		b.Run(fx.name, func(b *testing.B) {
			b.SetBytes(int64(len(fx.data)))
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if _, err := docxcodec.Encode(doc); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func BenchmarkRoundTrip(b *testing.B) {
	for _, fx := range loadFixtures(b) {
		b.Run(fx.name, func(b *testing.B) {
			b.SetBytes(int64(len(fx.data)))
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				doc, err := docxcodec.Decode(fx.data)
				if err != nil {
					b.Fatal(err)
				}
				if _, err := docxcodec.Encode(doc); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
