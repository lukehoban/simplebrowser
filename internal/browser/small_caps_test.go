package browser

import (
	"bytes"
	"image"
	"image/color"
	"image/draw"
	"testing"

	"golang.org/x/image/font"
	"golang.org/x/image/math/fixed"
)

func TestSmallCapsCascadeAndInheritance(t *testing.T) {
	doc := styledForLayout(t, `<style>
		#parent { font-variant: small-caps }
		#override { font: 20px sans-serif }
		#important { font-variant: normal !important; font: small-caps 20px sans-serif }
		#invalid { font-variant: nonsense }
	</style><div id=parent><span id=child>text</span>
		<span id=override>text</span><span id=important>text</span>
		<span id=invalid>text</span><span id=explicit style="font-variant:inherit">text</span>
	</div>`)
	for id, want := range map[string]string{
		"parent": "small-caps", "child": "small-caps", "override": "normal",
		"important": "normal", "invalid": "small-caps", "explicit": "small-caps",
	} {
		if got := styledElementByID(doc.StyleRoot, id).Style["font-variant"]; got != want {
			t.Errorf("%s variant = %q, want %q", id, got, want)
		}
	}
}

func TestSmallCapsMetricsAndPixels(t *testing.T) {
	const text = "aBcd 3éZ"
	const css = "font:small-caps 30px sans-serif"
	faces := newFaceSet()
	defer faces.close()
	m := faces.metrics(ComputedStyle{"font-size": "30px", "font-family": "sans-serif", "font-variant": "small-caps"})
	normal := faces.metrics(ComputedStyle{"font-size": "30px", "font-family": "sans-serif"})
	if m.smallCapsFace == nil || m.advance(text) == normal.advance(text) {
		t.Fatal("small-caps did not select a distinct face/width")
	}
	want := font.MeasureString(m.smallCapsFace, "A") +
		font.MeasureString(m.face, "B") +
		font.MeasureString(m.smallCapsFace, "CD") +
		font.MeasureString(m.face, " 3") +
		font.MeasureString(m.smallCapsFace, "É") +
		font.MeasureString(m.face, "Z")
	if m.advance(text) != want {
		t.Fatalf("advance = %v, want segment advance %v", m.advance(text), want)
	}
	doc := styledForLayout(t, `<p style="margin:0;`+css+`">`+text+`</p>`)
	l, err := LayoutWithViewport(doc, image.Rect(0, 0, 400, 90))
	if err != nil {
		t.Fatal(err)
	}
	run := l.Root.Children[0].Children[0].Text[0]
	if run.Text != text || run.Rect.Dx() != m.width(text) {
		t.Fatalf("run = %+v, want original text and width %d", run, m.width(text))
	}
	actual := painted(t, `<p style="margin:0;`+css+`">`+text+`</p>`, image.Rect(0, 0, 400, 90))
	reference := image.NewRGBA(actual.Bounds())
	draw.Draw(reference, reference.Bounds(), image.NewUniform(color.White), image.Point{}, draw.Src)
	ascent, _ := m.lineMetrics()
	d := font.Drawer{Dst: reference, Src: image.NewUniform(color.Black),
		Dot: fixed.Point26_6{X: run.PenX, Y: fixed.I(run.Rect.Min.Y + ascent)}}
	for _, part := range []struct {
		face font.Face
		text string
	}{{m.smallCapsFace, "A"}, {m.face, "B"}, {m.smallCapsFace, "CD"},
		{m.face, " 3"}, {m.smallCapsFace, "É"}, {m.face, "Z"}} {
		d.Face = part.face
		d.DrawString(part.text)
	}
	if !bytes.Equal(actual.Pix, reference.Pix) {
		t.Fatal("small-caps pixels differ from embedded-font reference")
	}
	shorthand := painted(t, `<p style="margin:0;`+css+`">`+text+`</p>`, image.Rect(0, 0, 400, 90))
	longhand := painted(t, `<p style="margin:0;font-variant:small-caps;font-size:30px;font-family:sans-serif">`+text+`</p>`, image.Rect(0, 0, 400, 90))
	if !bytes.Equal(shorthand.Pix, longhand.Pix) {
		t.Fatal("shorthand and longhand pixels differ")
	}
}

func TestSmallCapsFullCaseMapping(t *testing.T) {
	faces := newFaceSet()
	defer faces.close()
	m := faces.metrics(ComputedStyle{"font-size": "30px", "font-family": "sans-serif", "font-variant": "small-caps"})
	// Case mapping only; per-glyph fallback splits are covered in
	// glyph_fallback_test.go.
	m.fallback = nil
	type part struct {
		small bool
		text  string
	}
	for _, tc := range []struct {
		in   string
		want []part
	}{
		{"straße", []part{{true, "STRASSE"}}},
		{"Maße", []part{{false, "M"}, {true, "ASSE"}}},
		{"ﬁle", []part{{true, "FILE"}}},
		{"ŉ", []part{{true, "ʼN"}}},
		{"\u0390", []part{{true, "\u0399\u0308\u0301"}}},
		{"Größe 3", []part{{false, "G"}, {true, "RÖSSE"}, {false, " 3"}}},
		{"ABC", []part{{false, "ABC"}}},
	} {
		var got []part
		m.eachTextSegment(tc.in, func(face font.Face, text string) {
			got = append(got, part{face == m.smallCapsFace, text})
		})
		if len(got) != len(tc.want) {
			t.Errorf("%q segments = %+v, want %+v", tc.in, got, tc.want)
			continue
		}
		for i := range got {
			if got[i] != tc.want[i] {
				t.Errorf("%q segments = %+v, want %+v", tc.in, got, tc.want)
				break
			}
		}
	}
}

func TestSmallCapsSharpSWidthAndPixels(t *testing.T) {
	const text = "straße"
	const css = "margin:0;font:small-caps 30px sans-serif"
	faces := newFaceSet()
	defer faces.close()
	m := faces.metrics(ComputedStyle{"font-size": "30px", "font-family": "sans-serif", "font-variant": "small-caps"})
	want := font.MeasureString(m.smallCapsFace, "STRASSE")
	if got := m.advance(text); got != want {
		t.Fatalf("advance = %v, want expanded STRASSE advance %v", got, want)
	}
	if m.advance(text) == font.MeasureString(m.smallCapsFace, "STRAßE") {
		t.Fatal("ß was not expanded")
	}
	html := `<p style="` + css + `">` + text + `</p>`
	doc := styledForLayout(t, html)
	l, err := LayoutWithViewport(doc, image.Rect(0, 0, 400, 90))
	if err != nil {
		t.Fatal(err)
	}
	run := l.Root.Children[0].Children[0].Text[0]
	if run.Text != text || run.Rect.Dx() != m.width(text) {
		t.Fatalf("run = %+v, want original text %q and width %d", run, text, m.width(text))
	}
	actual := painted(t, html, image.Rect(0, 0, 400, 90))
	reference := image.NewRGBA(actual.Bounds())
	draw.Draw(reference, reference.Bounds(), image.NewUniform(color.White), image.Point{}, draw.Src)
	ascent, _ := m.lineMetrics()
	d := font.Drawer{Dst: reference, Src: image.NewUniform(color.Black), Face: m.smallCapsFace,
		Dot: fixed.Point26_6{X: run.PenX, Y: fixed.I(run.Rect.Min.Y + ascent)}}
	d.DrawString("STRASSE")
	if !bytes.Equal(actual.Pix, reference.Pix) {
		t.Fatal("straße small-caps pixels differ from STRASSE reference")
	}
	// Normal text keeps ß as-is.
	normal := faces.metrics(ComputedStyle{"font-size": "30px", "font-family": "sans-serif"})
	var seen string
	normal.eachTextSegment(text, func(_ font.Face, s string) { seen += s })
	if seen != text {
		t.Fatalf("normal text segments = %q, want %q", seen, text)
	}
}

func TestSmallCapsConcurrentRenders(t *testing.T) {
	html := `<p style="margin:0;font:small-caps 20px sans-serif">straße ﬁle Größe</p>`
	first := painted(t, html, image.Rect(0, 0, 300, 40))
	done := make(chan []byte, 8)
	for range 8 {
		go func() { done <- painted(t, html, image.Rect(0, 0, 300, 40)).Pix }()
	}
	for range 8 {
		if !bytes.Equal(<-done, first.Pix) {
			t.Fatal("concurrent small-caps render differs")
		}
	}
}
