package browser

import (
	"bytes"
	"image"
	"image/color"
	"image/draw"
	"testing"

	"golang.org/x/image/font"
	"golang.org/x/image/font/gofont/goregular"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/math/fixed"

	"github.com/lukehoban/simplebrowser/internal/fonts/dejavu"
)

const modifierApostrophe = 'ʼ' // U+02BC

type facePart struct {
	face font.Face
	text string
}

func segmentsOf(m metrics, s string) []facePart {
	var parts []facePart
	m.eachTextSegment(s, func(face font.Face, text string) { parts = append(parts, facePart{face, text}) })
	return parts
}

func sumAdvance(parts []facePart) fixed.Int26_6 {
	var total fixed.Int26_6
	for _, p := range parts {
		total += font.MeasureString(p.face, p.text)
	}
	return total
}

// assertPaintMatches lays out and paints html, checks the run width against
// m, and compares pixels with a reference drawn part-by-part.
func assertPaintMatches(t *testing.T, html, text string, m metrics, parts []facePart) {
	t.Helper()
	viewport := image.Rect(0, 0, 400, 90)
	l, err := LayoutWithViewport(styledForLayout(t, html), viewport)
	if err != nil {
		t.Fatal(err)
	}
	run := l.Root.Children[0].Children[0].Text[0]
	if run.Text != text || run.Rect.Dx() != int((sumAdvance(parts)+63)/64) {
		t.Fatalf("run = %+v, want text %q width %d", run, text, int((sumAdvance(parts)+63)/64))
	}
	actual := painted(t, html, viewport)
	reference := image.NewRGBA(actual.Bounds())
	draw.Draw(reference, reference.Bounds(), image.NewUniform(color.White), image.Point{}, draw.Src)
	ascent, _ := m.lineMetrics()
	d := font.Drawer{Dst: reference, Src: image.NewUniform(color.Black),
		Dot: fixed.Point26_6{X: run.PenX, Y: fixed.I(run.Rect.Min.Y + ascent)}}
	for _, p := range parts {
		d.Face = p.face
		d.DrawString(p.text)
	}
	if !bytes.Equal(actual.Pix, reference.Pix) {
		t.Fatal("painted pixels differ from per-face reference")
	}
}

func TestGlyphFallbackPrecondition(t *testing.T) {
	for name, data := range map[string][]byte{"Go Sans": goregular.TTF, "DejaVu Sans": dejavu.Regular} {
		f, err := opentype.Parse(data)
		if err != nil {
			t.Fatal(err)
		}
		idx, _ := f.GlyphIndex(nil, modifierApostrophe)
		if has, want := idx != 0, name == "DejaVu Sans"; has != want {
			t.Fatalf("%s has U+02BC = %v, want %v", name, has, want)
		}
	}
}

func TestGlyphFallbackPlainText(t *testing.T) {
	const text = "Hawaiʼi"
	faces := newFaceSet()
	defer faces.close()
	m := faces.metrics(ComputedStyle{"font-size": "30px", "font-family": "sans-serif"})
	dv := faces.metrics(ComputedStyle{"font-size": "30px", "font-family": "verdana"})
	parts := segmentsOf(m, text)
	want := []facePart{{m.face, "Hawai"}, {dv.face, "ʼ"}, {m.face, "i"}}
	if len(parts) != len(want) {
		t.Fatalf("segments = %d, want %d", len(parts), len(want))
	}
	for i := range want {
		if parts[i] != want[i] {
			t.Fatalf("segment %d = %q (fallback=%v), want %q", i, parts[i].text, parts[i].face == dv.face, want[i].text)
		}
	}
	if m.advance(text) != sumAdvance(want) {
		t.Fatalf("advance = %v, want %v", m.advance(text), sumAdvance(want))
	}
	if m.advance(text) == font.MeasureString(m.face, text) {
		t.Fatal("advance still uses the Go Sans missing-glyph box")
	}
	assertPaintMatches(t, `<p style="margin:0;font:30px sans-serif">`+text+`</p>`, text, m, want)
}

func TestGlyphFallbackSmallCapsExpansion(t *testing.T) {
	const text = "ŉ" // small caps → ʼN
	faces := newFaceSet()
	defer faces.close()
	m := faces.metrics(ComputedStyle{"font-size": "30px", "font-family": "sans-serif", "font-variant": "small-caps"})
	dvSmall := faces.metrics(ComputedStyle{"font-size": "24px", "font-family": "verdana"})
	want := []facePart{{dvSmall.face, "ʼ"}, {m.smallCapsFace, "N"}}
	parts := segmentsOf(m, text)
	if len(parts) != 2 || parts[0] != want[0] || parts[1] != want[1] {
		t.Fatalf("segments = %+v, want 24px DejaVu ʼ then small-caps N", parts)
	}
	if m.advance(text) != sumAdvance(want) {
		t.Fatalf("advance = %v, want %v", m.advance(text), sumAdvance(want))
	}
	assertPaintMatches(t, `<p style="margin:0;font:small-caps 30px sans-serif">`+text+`</p>`, text, m, want)
}

func TestGlyphFallbackKeepsCombiningMarksWithBase(t *testing.T) {
	faces := newFaceSet()
	defer faces.close()
	m := faces.metrics(ComputedStyle{"font-size": "30px", "font-family": "sans-serif", "font-variant": "small-caps"})
	dvSmall := faces.metrics(ComputedStyle{"font-size": "24px", "font-family": "verdana"})
	// ΐ → Ι + U+0308 + U+0301; Go Sans lacks the combining marks, so the
	// whole cluster uses DejaVu rather than splitting the marks off.
	parts := segmentsOf(m, "\u0390")
	if len(parts) != 1 || parts[0] != (facePart{dvSmall.face, "\u0399\u0308\u0301"}) {
		t.Fatalf("segments = %+v, want one DejaVu cluster", parts)
	}
}

func TestGlyphFallbackLeavesCoveredTextAlone(t *testing.T) {
	faces := newFaceSet()
	defer faces.close()
	for _, style := range []ComputedStyle{
		{"font-size": "13px", "font-family": "sans-serif"},
		{"font-size": "13px", "font-family": "monospace", "font-weight": "bold"},
		{"font-size": "30px", "font-family": "sans-serif", "font-variant": "small-caps"},
	} {
		m := faces.metrics(style)
		const text = "Hacker News — “quotes” 123 é"
		if m.smallCapsFace == nil {
			if parts := segmentsOf(m, text); len(parts) != 1 || parts[0].face != m.face {
				t.Fatalf("%v: covered text split into %d segments", style, len(parts))
			}
			if m.advance(text) != font.MeasureString(m.face, text) {
				t.Fatalf("%v: covered text advance changed", style)
			}
		}
		for _, p := range segmentsOf(m, text) {
			if p.face != m.face && p.face != m.smallCapsFace {
				t.Fatalf("%v: covered text %q used a fallback face", style, p.text)
			}
		}
	}
	// DejaVu-selected text needs no fallback.
	if faces.metrics(ComputedStyle{"font-family": "verdana"}).fallback != nil {
		t.Fatal("DejaVu face should not fall back to itself")
	}
	// Runes neither face covers keep the primary face (its missing-glyph box).
	m := faces.metrics(ComputedStyle{"font-size": "20px"})
	if parts := segmentsOf(m, "a\U0010FFFDb"); len(parts) != 1 {
		t.Fatalf("uncovered rune split into %d segments", len(parts))
	}
}
