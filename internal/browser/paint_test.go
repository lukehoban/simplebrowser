package browser

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"sync"
	"testing"
)

func painted(t *testing.T, markup string, viewport image.Rectangle) *image.RGBA {
	t.Helper()
	doc := styledForLayout(t, markup)
	layout, err := LayoutWithViewport(doc, viewport)
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	if err := paint(layout, &buf, renderOptions{}); err != nil {
		t.Fatal(err)
	}
	img, err := png.Decode(&buf)
	if err != nil {
		t.Fatal(err)
	}
	return img.(*image.RGBA)
}

func pixel(t *testing.T, img *image.RGBA, x, y int, want color.RGBA) {
	t.Helper()
	if got := img.RGBAAt(x, y); got != want {
		t.Errorf("pixel (%d,%d) = %v, want %v", x, y, got, want)
	}
}

func TestPaintBackgroundsBordersAndOrder(t *testing.T) {
	img := painted(t, `<div style="margin:0;width:50px;height:30px;background-color:#ff0000;border:3px solid #0000ff"><div style="margin:0;width:10px;height:8px;background-color:green"></div></div>`, image.Rect(0, 0, 80, 50))
	pixel(t, img, 0, 0, color.RGBA{0, 0, 255, 255})
	pixel(t, img, 2, 10, color.RGBA{0, 0, 255, 255})
	pixel(t, img, 55, 10, color.RGBA{0, 0, 255, 255})
	pixel(t, img, 6, 15, color.RGBA{255, 0, 0, 255})
	pixel(t, img, 5, 5, color.RGBA{0, 128, 0, 255})
	pixel(t, img, 70, 40, color.RGBA{255, 255, 255, 255})
}

func TestPaintPerSideBordersAndTransparentBackground(t *testing.T) {
	img := painted(t, `<div style="margin:0;width:24px;height:12px;background-color:green"><div style="margin:0;width:12px;height:4px;background-color:transparent;border-width:1px 2px 3px 4px;border-color:red blue yellow black"></div></div>`, image.Rect(0, 0, 40, 30))
	pixel(t, img, 5, 0, color.RGBA{255, 0, 0, 255})
	pixel(t, img, 0, 2, color.RGBA{0, 0, 0, 255})
	pixel(t, img, 17, 2, color.RGBA{0, 0, 255, 255})
	pixel(t, img, 5, 7, color.RGBA{255, 255, 0, 255})
	pixel(t, img, 5, 3, color.RGBA{0, 128, 0, 255})
}

func TestPaintTextWeightColorSizeAndUnderline(t *testing.T) {
	viewport := image.Rect(0, 0, 200, 100)
	plain := painted(t, `<p style="margin:0;color:red;font-size:20px">Hello</p>`, viewport)
	bold := painted(t, `<p style="margin:0;color:red;font-size:20px;font-weight:bold">Hello</p>`, viewport)
	if bytes.Equal(plain.Pix, bold.Pix) {
		t.Fatal("bold glyphs should differ from regular glyphs")
	}
	colored := 0
	for _, img := range []*image.RGBA{plain, bold} {
		for y := 0; y < 24; y++ {
			for x := 0; x < 80; x++ {
				if p := img.RGBAAt(x, y); p != (color.RGBA{255, 255, 255, 255}) {
					if p.R != 255 || p.G != p.B || p.G == 255 {
						t.Fatalf("glyph has wrong color: %v", p)
					}
					colored++
				}
			}
		}
	}
	if colored < 20 {
		t.Fatalf("too few text pixels: %d", colored)
	}
	small := painted(t, `<p style="margin:0;font-size:10px">Hello</p>`, viewport)
	if bytes.Equal(plain.Pix, small.Pix) {
		t.Fatal("font sizes should produce different pixels")
	}
	linked := painted(t, `<p style="margin:0"><a style="color:blue"><b>Link</b></a></p>`, viewport)
	// The decoration propagates through the nested bold child.
	doc := styledForLayout(t, `<p style="margin:0"><a style="color:blue"><b>Link</b></a></p>`)
	l, err := LayoutWithViewport(doc, viewport)
	if err != nil {
		t.Fatal(err)
	}
	run := l.Root.Children[0].Children[0].Text[0]
	faces := newFaceSet()
	m := faces.metrics(run.Style)
	y := run.Rect.Min.Y + (run.Rect.Dy()-m.lineHeight())/2 + m.face.Metrics().Ascent.Ceil() + 1
	faces.close()
	pixel(t, linked, run.Rect.Min.X+2, y, color.RGBA{0, 0, 255, 255})
}

func TestPaintUnderlineExcludesTrailingWhitespace(t *testing.T) {
	viewport := image.Rect(0, 0, 240, 60)
	withSpace := painted(t, `<p style="margin:0"><a style="text-decoration:underline">first word   </a></p>`, viewport)
	withoutSpace := painted(t, `<p style="margin:0"><a style="text-decoration:underline">first word</a></p>`, viewport)
	if !bytes.Equal(withSpace.Pix, withoutSpace.Pix) {
		t.Fatal("trailing collapsible whitespace changed the underlined render")
	}

	doc := styledForLayout(t, `<p style="margin:0"><a style="text-decoration:underline">first word</a></p>`)
	layout, err := LayoutWithViewport(doc, viewport)
	if err != nil {
		t.Fatal(err)
	}
	run := layout.Root.Children[0].Children[0].Text[0]
	faces := newFaceSet()
	m := faces.metrics(run.Style)
	y := run.Rect.Min.Y + (run.Rect.Dy()-m.lineHeight())/2 + m.face.Metrics().Ascent.Ceil() + 1
	spaceStart := run.Rect.Min.X + m.width("first")
	spaceWidth := m.width(" ")
	faces.close()
	if spaceWidth == 0 {
		t.Fatal("font reports zero-width space")
	}
	pixel(t, withSpace, spaceStart+spaceWidth/2, y, color.RGBA{0, 0, 255, 255})
}

func TestPaintUnderlineEndsAtMultiWordRunGlyphs(t *testing.T) {
	const text = "Does Georgism work? Five years later"
	viewport := image.Rect(0, 0, 400, 40)
	markup := `<p style="margin:0"><a style="text-decoration:underline">` + text + `</a><span> (site)</span></p>`
	doc := styledForLayout(t, markup)
	layout, err := LayoutWithViewport(doc, viewport)
	if err != nil {
		t.Fatal(err)
	}
	run := layout.Root.Children[0].Children[0].Text[0]
	faces := newFaceSet()
	m := faces.metrics(run.Style)
	want := m.width(text)
	y := run.Rect.Min.Y + (run.Rect.Dy()-m.lineHeight())/2 + m.face.Metrics().Ascent.Ceil() + 1
	faces.close()
	if run.Text != text || run.Rect.Dx() != want {
		t.Fatalf("run %q width = %d, want %d (width of the whole string)", run.Text, run.Rect.Dx(), want)
	}
	img := painted(t, markup, viewport)
	pixel(t, img, run.Rect.Min.X+want-1, y, color.RGBA{0, 0, 255, 255})
	pixel(t, img, run.Rect.Min.X+want+1, y, color.RGBA{255, 255, 255, 255})
}

func TestPaintClipsToViewportAndTextRun(t *testing.T) {
	img := painted(t, `<div style="margin:0;width:200px;height:200px;background:blue;border:5px solid red">abcdefghijklmnopqrstuvwxyz</div>`, image.Rect(0, 0, 24, 15))
	if img.Bounds() != image.Rect(0, 0, 24, 15) {
		t.Fatalf("bounds = %v", img.Bounds())
	}
	pixel(t, img, 0, 0, color.RGBA{255, 0, 0, 255})
	if p := img.RGBAAt(23, 14); p.R != 0 || p.G != 0 || p.B < 180 || p.A != 255 {
		t.Fatalf("clipped edge pixel = %v, want blue background or glyph", p)
	}
}

func TestPaintConcurrentRenders(t *testing.T) {
	const markup = `<p style="margin:0;background:#eef"><a style="font-size:19px"><b>Concurrent text</b></a></p>`
	want := painted(t, markup, image.Rect(0, 0, 240, 80))
	var wg sync.WaitGroup
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			got := painted(t, markup, image.Rect(0, 0, 240, 80))
			if !bytes.Equal(got.Pix, want.Pix) {
				t.Error("concurrent paint differs")
			}
		}()
	}
	wg.Wait()
}
