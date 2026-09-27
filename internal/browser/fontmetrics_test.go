package browser

import (
	"image"
	"math"
	"testing"
)

// The ratios must agree with the faces layout actually draws with, so that
// e.g. 10ch in Go Mono is exactly ten '0' advances.
func TestFontRatiosMatchLayoutFaces(t *testing.T) {
	faces := newFaceSet()
	defer faces.close()
	for _, style := range []ComputedStyle{
		{"font-family": "sans-serif", "font-size": "100px"},
		{"font-family": "monospace", "font-size": "100px"},
		{"font-family": "monospace", "font-size": "100px", "font-weight": "bold", "font-style": "italic"},
		{"font-family": "Verdana", "font-size": "100px"},
		{"font-family": "Verdana", "font-size": "100px", "font-weight": "bold", "font-style": "italic"},
	} {
		face := faces.metrics(style).face
		advance, ok := face.GlyphAdvance('0')
		if !ok {
			t.Fatalf("%v: no '0' glyph", style)
		}
		r := ratiosFor(style)
		if got, want := r.ch*100, float64(advance)/64; math.Abs(got-want) > 0.02 {
			t.Errorf("%v: ch = %v px, face '0' advance = %v px", style, got, want)
		}
		bounds, _, _ := face.GlyphBounds('x')
		if got, ink := r.ex*100, float64(-bounds.Min.Y)/64; math.Abs(got-ink) > 1 {
			t.Errorf("%v: ex = %v px, 'x' ink height = %v px", style, got, ink)
		}
		if r == fallbackFontRatios {
			t.Errorf("%v: used 0.5em fallback, want face metrics", style)
		}
	}
	if ratiosFor(ComputedStyle{"font-family": "monospace"}).ch <= ratiosFor(ComputedStyle{"font-family": "sans-serif"}).ch {
		t.Fatal("Go Mono '0' should be wider than Go Sans '0'")
	}
}

func TestExChGeometry(t *testing.T) {
	sans := ratiosFor(ComputedStyle{"font-family": "sans-serif"})
	mono := ratiosFor(ComputedStyle{"font-family": "monospace"})
	for _, tc := range []struct {
		name, family string
		r            fontRatios
	}{{"Go Sans", "sans-serif", sans}, {"Go Mono", "monospace", mono}} {
		t.Run(tc.name, func(t *testing.T) {
			source := `<body style="margin:0"><div style="font:20px ` + tc.family +
				`;width:10ch;margin-left:4ex;padding:0 1ch 0 2ex;height:1px"></div></body>`
			got, err := LayoutWithViewport(styledForLayout(t, source), image.Rect(0, 0, 400, 400))
			if err != nil {
				t.Fatal(err)
			}
			boxes := collectBoxes(got.Root, "div")
			if len(boxes) != 1 {
				t.Fatalf("got %d divs", len(boxes))
			}
			b := boxes[0]
			near := func(what string, got int, want float64) {
				t.Helper()
				if math.Abs(float64(got)-want) > 1 {
					t.Errorf("%s = %d, want ~%.3f", what, got, want)
				}
			}
			near("content width (10ch)", b.Content.Dx(), 200*tc.r.ch)
			near("margin-left (4ex)", b.Rect.Min.X, 80*tc.r.ex)
			near("padding-left (2ex)", b.Content.Min.X-b.Rect.Min.X, 40*tc.r.ex)
			near("padding-right (1ch)", b.Rect.Max.X-b.Content.Max.X, 20*tc.r.ch)
		})
	}
	if w := func(family string) int {
		got, _ := LayoutWithViewport(styledForLayout(t, `<div style="font:20px `+family+`;width:10ch"></div>`), image.Rect(0, 0, 400, 400))
		return collectBoxes(got.Root, "div")[0].Content.Dx()
	}; w("monospace") <= w("sans-serif") {
		t.Fatalf("10ch in Go Mono (%d) should be wider than in Go Sans (%d)", w("monospace"), w("sans-serif"))
	}
}

func TestExChFontSizeUsesParentFont(t *testing.T) {
	doc := styledForLayout(t, `<html><body style="font:20px monospace">
		<div id=ex style="font-size:2ex;font-family:sans-serif;width:1ex"></div>
		<div id=ch style="font-size:2ch;font-family:sans-serif"></div>
		<div id=mixed style="font-size:1em;font-family:sans-serif;width:10ch"></div>
	</body></html>`)
	mono := ratiosFor(ComputedStyle{"font-family": "monospace"})
	sans := ratiosFor(ComputedStyle{"font-family": "sans-serif"})
	check := func(id, property string, want float64) {
		t.Helper()
		got := px(styledElementByID(doc.StyleRoot, id).Style[property], 0, math.NaN())
		if math.Abs(got-want) > 1e-9 {
			t.Errorf("%s %s = %v, want %v", id, property, got, want)
		}
	}
	// font-size resolves ex/ch against the parent (Go Mono, 20px).
	exSize := 2 * 20 * mono.ex
	check("ex", "font-size", exSize)
	check("ch", "font-size", 2*20*mono.ch)
	// Other properties use the element's own font (Go Sans at its computed size).
	check("ex", "width", exSize*sans.ex)
	check("mixed", "width", 10*20*sans.ch)
}
