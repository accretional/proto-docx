package main

import (
	"archive/zip"
	"bytes"
)

// zipNames returns the entry names of the ZIP archive in raw, in the
// order they appear in the central directory.
func zipNames(raw []byte) ([]string, error) {
	zr, err := zip.NewReader(bytes.NewReader(raw), int64(len(raw)))
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(zr.File))
	for _, f := range zr.File {
		out = append(out, f.Name)
	}
	return out, nil
}
