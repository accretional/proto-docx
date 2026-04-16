// Command gen-fixtures writes the canonical set of DOCX test fixtures
// to data/generated/. Each file exercises a different aspect of the
// OOXML package shape so validation / fuzz / benchmark suites see
// diverse inputs.
//
// Idempotent: skips files that already exist unless -force is set.
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"openformat-docx/internal/docxbuild"
)

type fixture struct {
	name string
	spec docxbuild.Spec
}

// tinyPNG is a 1x1 PNG used for media-part fixtures.
var tinyPNG = []byte{
	0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A,
	0x00, 0x00, 0x00, 0x0D, 0x49, 0x48, 0x44, 0x52,
	0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x01,
	0x08, 0x02, 0x00, 0x00, 0x00, 0x90, 0x77, 0x53,
	0xDE, 0x00, 0x00, 0x00, 0x0C, 0x49, 0x44, 0x41,
	0x54, 0x08, 0xD7, 0x63, 0xF8, 0xCF, 0xC0, 0x00,
	0x00, 0x00, 0x03, 0x00, 0x01, 0x5B, 0x7A, 0x64,
	0xA5, 0x00, 0x00, 0x00, 0x00, 0x49, 0x45, 0x4E,
	0x44, 0xAE, 0x42, 0x60, 0x82,
}

// tinyJPEG is a minimal JPEG magic header with padding — enough to be
// picked up by mime-from-extension logic.
var tinyJPEG = []byte{0xFF, 0xD8, 0xFF, 0xE0, 0x00, 0x10, 'J', 'F', 'I', 'F', 0, 0, 0, 0, 0, 0, 0xFF, 0xD9}

func fixtures() []fixture {
	return []fixture{
		{
			name: "01_minimal.docx",
			spec: docxbuild.Spec{Paragraphs: []string{"Hello DOCX"}},
		},
		{
			name: "02_multiple_paragraphs.docx",
			spec: docxbuild.Spec{
				Paragraphs: []string{
					"The quick brown fox",
					"jumps over the lazy dog",
					"Pack my box with five dozen liquor jugs",
					"How vexingly quick daft zebras jump",
					"Sphinx of black quartz, judge my vow",
				},
				IncludeStyles: true,
			},
		},
		{
			name: "03_with_image_png.docx",
			spec: docxbuild.Spec{
				Paragraphs: []string{"Document with one PNG"},
				Images:     []docxbuild.Image{{Name: "image1.png", Data: tinyPNG}},
			},
		},
		{
			name: "04_with_image_jpeg.docx",
			spec: docxbuild.Spec{
				Paragraphs: []string{"Document with one JPEG"},
				Images:     []docxbuild.Image{{Name: "image1.jpg", Data: tinyJPEG}},
			},
		},
		{
			name: "05_mixed_media.docx",
			spec: docxbuild.Spec{
				Paragraphs: []string{"Mixed media"},
				Images: []docxbuild.Image{
					{Name: "image1.png", Data: tinyPNG},
					{Name: "image2.jpg", Data: tinyJPEG},
					{Name: "image3.png", Data: tinyPNG},
				},
			},
		},
		{
			name: "06_fonts.docx",
			spec: docxbuild.Spec{
				Paragraphs: []string{"Document with custom fonts"},
				Fonts:      []string{"Calibri", "Arial", "Times New Roman", "Consolas"},
			},
		},
		{
			name: "07_tracked_changes.docx",
			spec: docxbuild.Spec{
				Paragraphs:    []string{"This is the original text."},
				TrackedInsert: "freshly added sentence",
				TrackedDelete: "removed sentence",
			},
		},
		{
			name: "08_comments.docx",
			spec: docxbuild.Spec{
				Paragraphs: []string{"Text being annotated"},
				Comments:   []string{"First comment", "Second comment"},
			},
		},
		{
			name: "09_footnotes_endnotes.docx",
			spec: docxbuild.Spec{
				Paragraphs: []string{"Paragraph with footnote & endnote references"},
				Footnotes:  []string{"Footnote one.", "Footnote two."},
				Endnotes:   []string{"Endnote one."},
			},
		},
		{
			name: "10_headers_footers.docx",
			spec: docxbuild.Spec{
				Paragraphs: []string{"Body text"},
				Headers:    2,
				Footers:    2,
			},
		},
		{
			name: "12_tables.docx",
			spec: docxbuild.Spec{
				Paragraphs: []string{"Tables below"},
				Tables: []docxbuild.Table{
					{
						StyleID:  "TableGrid",
						GridCols: []int32{3000, 3000, 3000},
						Rows: [][]string{
							{"Name", "Role", "City"},
							{"Ada", "Mathematician", "London"},
							{"Grace", "Admiral", "Arlington"},
						},
					},
					{
						// No explicit grid — inferred uniform split.
						Rows: [][]string{
							{"single-row", "two-column"},
						},
					},
				},
			},
		},
		{
			name: "13_hyperlinks_bookmarks.docx",
			spec: docxbuild.Spec{
				Paragraphs: []string{"Links and bookmarks below"},
				Hyperlinks: []docxbuild.Hyperlink{
					{
						RelationshipID: "rIdLink1",
						Tooltip:        "Anthropic homepage",
						Text:           "Anthropic",
						History:        true,
					},
					{
						Anchor: "intro",
						Text:   "jump to intro",
					},
				},
				Bookmarks: []docxbuild.Bookmark{
					{ID: 1, Name: "intro", Text: "Introduction"},
					{ID: 2, Name: "_Toc_section_2", Text: "Section 2"},
				},
			},
		},
		{
			name: "11_kitchen_sink.docx",
			spec: docxbuild.Spec{
				Paragraphs: []string{
					"Chapter 1 — the beginning",
					"Everybody has a plan",
					"Until they get punched in the mouth",
					"— Mike Tyson",
				},
				TrackedInsert: "late addition",
				TrackedDelete: "obsolete sentence",
				Comments:      []string{"Review this paragraph"},
				Footnotes:     []string{"See chapter 3."},
				Endnotes:      []string{"Cited on p. 42."},
				Headers:       1,
				Footers:       1,
				Fonts:         []string{"Georgia", "Helvetica"},
				Images:        []docxbuild.Image{{Name: "image1.png", Data: tinyPNG}},
				IncludeStyles: true,
			},
		},
	}
}

func main() {
	outDir := flag.String("out", "data/generated", "output directory")
	force := flag.Bool("force", false, "overwrite existing files")
	flag.Parse()

	if err := os.MkdirAll(*outDir, 0o755); err != nil {
		fmt.Fprintf(os.Stderr, "mkdir %s: %v\n", *outDir, err)
		os.Exit(1)
	}

	for _, fx := range fixtures() {
		path := filepath.Join(*outDir, fx.name)
		if !*force {
			if _, err := os.Stat(path); err == nil {
				fmt.Printf("skip  %s\n", path)
				continue
			}
		}
		data, err := docxbuild.Build(fx.spec)
		if err != nil {
			fmt.Fprintf(os.Stderr, "build %s: %v\n", fx.name, err)
			os.Exit(1)
		}
		if err := os.WriteFile(path, data, 0o644); err != nil {
			fmt.Fprintf(os.Stderr, "write %s: %v\n", path, err)
			os.Exit(1)
		}
		fmt.Printf("write %s (%d bytes)\n", path, len(data))
	}
}
