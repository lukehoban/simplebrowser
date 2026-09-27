package browser

import (
	"image"
	"testing"

	"github.com/lukehoban/simplebrowser/internal/fonts/dejavu"

	"golang.org/x/image/font"
	"golang.org/x/image/font/opentype"
)

// The Hacker News family stack must render with the bundled DejaVu Sans
// faces (Verdana-like metrics), one face per weight/style.
func TestVerdanaStackUsesDejaVuSansMetrics(t *testing.T) {
	faces := newFaceSet()
	defer faces.close()
	const sample = "Hacker News story metadata"
	for _, tc := range []struct {
		name   string
		style  ComputedStyle
		source []byte
	}{
		{"regular", ComputedStyle{}, dejavu.Regular},
		{"bold", ComputedStyle{"font-weight": "bold"}, dejavu.Bold},
		{"oblique", ComputedStyle{"font-style": "italic"}, dejavu.Oblique},
		{"bold oblique", ComputedStyle{"font-weight": "700", "font-style": "oblique"}, dejavu.BoldOblique},
	} {
		t.Run(tc.name, func(t *testing.T) {
			style := ComputedStyle{"font-family": "Verdana, Geneva, sans-serif", "font-size": "10pt"}
			for k, v := range tc.style {
				style[k] = v
			}
			parsed, err := opentype.Parse(tc.source)
			if err != nil {
				t.Fatal(err)
			}
			ref, err := opentype.NewFace(parsed, &opentype.FaceOptions{Size: fontSize("10pt"), DPI: 72, Hinting: font.HintingNone})
			if err != nil {
				t.Fatal(err)
			}
			defer ref.Close()
			if got, want := faces.metrics(style).advance(sample), font.MeasureString(ref, sample); got != want {
				t.Fatalf("Verdana advance = %v, DejaVu Sans %s advance = %v", got, tc.name, want)
			}
			if got, want := faces.metrics(style).face.Metrics(), ref.Metrics(); got != want {
				t.Fatalf("Verdana metrics = %+v, want DejaVu %+v", got, want)
			}
		})
	}

	// Unrelated families keep Go Sans.
	verdana := faces.metrics(ComputedStyle{"font-family": "Verdana", "font-size": "10pt"}).width(sample)
	arial := faces.metrics(ComputedStyle{"font-family": "Arial", "font-size": "10pt"}).width(sample)
	if verdana <= arial {
		t.Fatalf("Verdana/DejaVu width %d should exceed Arial/Go Sans width %d", verdana, arial)
	}
}

// A 180px-wide HN-style line fits in Go Sans but wraps in the wider
// Verdana substitute, as it would in a browser with real Verdana.
func TestVerdanaStackWrapsWiderThanGoSans(t *testing.T) {
	for _, tc := range []struct {
		family    string
		wantLines int
	}{
		{"Verdana, Geneva, sans-serif", 2},
		{"Arial, sans-serif", 1},
	} {
		t.Run(tc.family, func(t *testing.T) {
			doc := styledForLayout(t, `<p style="margin:0;width:180px;font-size:10pt;font-family:`+tc.family+`">Hacker News story metadata</p>`)
			got, err := LayoutWithViewport(doc, image.Rect(0, 0, 400, 100))
			if err != nil {
				t.Fatal(err)
			}
			lines := map[int]bool{}
			var walk func(*Box)
			walk = func(b *Box) {
				for _, run := range b.Text {
					lines[run.Rect.Min.Y] = true
				}
				for _, child := range b.Children {
					walk(child)
				}
			}
			walk(got.Root)
			if len(lines) != tc.wantLines {
				t.Fatalf("lines = %d, want %d", len(lines), tc.wantLines)
			}
		})
	}
}
