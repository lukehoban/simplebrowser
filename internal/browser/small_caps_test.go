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
