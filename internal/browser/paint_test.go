package browser

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
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

func TestPaintTextInsideAbsoluteAndFixedBoxes(t *testing.T) {
	img := painted(t, `<div style="position:relative;margin:0;height:70px">
	<div style="position:absolute;left:10px;top:5px;color:red">hello</div>
	<div style="position:absolute;left:10px;top:30px;color:green">world</div>
	</div><div style="position:fixed;left:10px;top:70px;color:blue">fixed</div>`,
		image.Rect(0, 0, 120, 110))
	for _, tt := range []struct {
		name string
		area image.Rectangle
		want color.RGBA
	}{
		{"absolute", image.Rect(10, 5, 60, 25), color.RGBA{255, 0, 0, 255}},
		{"nested absolute", image.Rect(10, 30, 60, 50), color.RGBA{0, 128, 0, 255}},
		{"fixed", image.Rect(10, 70, 60, 90), color.RGBA{0, 0, 255, 255}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			found := false
			for y := tt.area.Min.Y; y < tt.area.Max.Y; y++ {
				for x := tt.area.Min.X; x < tt.area.Max.X; x++ {
					if img.RGBAAt(x, y) == tt.want {
						found = true
					}
				}
			}
			if !found {
				t.Errorf("no text-colored pixels in %v", tt.area)
			}
		})
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

func TestPaintFontShorthandMatchesLonghands(t *testing.T) {
	viewport := image.Rect(0, 0, 260, 100)
	shorthand := painted(t, `<p style="margin:0;font:italic bold 24px/1.5 monospace">Shorthand text</p>`, viewport)
	longhands := painted(t, `<p style="margin:0;font-style:italic;font-weight:bold;font-size:24px;line-height:1.5;font-family:monospace">Shorthand text</p>`, viewport)
	defaults := painted(t, `<p style="margin:0">Shorthand text</p>`, viewport)
	if !bytes.Equal(shorthand.Pix, longhands.Pix) {
		t.Fatal("font shorthand pixels differ from equivalent longhands")
	}
	if bytes.Equal(shorthand.Pix, defaults.Pix) {
		t.Fatal("font shorthand did not affect painted pixels")
	}
}

func TestPaintSystemFontsMatchEmbeddedFace(t *testing.T) {
	viewport := image.Rect(0, 0, 280, 80)
	for keyword, size := range systemFontSizes {
		t.Run(keyword, func(t *testing.T) {
			render := func(css string) *image.RGBA {
				return painted(t, `<p style="margin:0;`+css+`">System font sample</p>`, viewport)
			}
			system := render(`font: ` + keyword)
			embedded := render(`font: normal normal ` + size + ` sans-serif`)
			original := render(`font: italic bold 28px monospace`)
			if !bytes.Equal(system.Pix, embedded.Pix) {
				t.Fatal("system font pixels differ from matching embedded face")
			}
			if bytes.Equal(system.Pix, original.Pix) {
				t.Fatal("system font did not change painted pixels")
			}
		})
	}
}

func TestCanvasBackgroundUsesRootAndBodyPropagation(t *testing.T) {
	viewport := image.Rect(0, 0, 800, 600)
	tests := []struct {
		name   string
		markup string
		points []struct {
			x, y int
			want color.RGBA
		}
	}{
		{
			name:   "root background covers canvas",
			markup: `<html style="background:green"><body><div style="height:20px"></div></body></html>`,
			points: []struct {
				x, y int
				want color.RGBA
			}{{0, 0, color.RGBA{0, 128, 0, 255}}, {799, 599, color.RGBA{0, 128, 0, 255}}},
		},
		{
			name:   "body background propagates through transparent root",
			markup: `<html style="background:transparent"><body style="background:green"><div style="height:20px"></div></body></html>`,
			points: []struct {
				x, y int
				want color.RGBA
			}{{0, 0, color.RGBA{0, 128, 0, 255}}, {799, 599, color.RGBA{0, 128, 0, 255}}},
		},
		{
			name:   "default canvas remains white",
			markup: `<html><body><div style="height:20px"></div></body></html>`,
			points: []struct {
				x, y int
				want color.RGBA
			}{{0, 0, color.RGBA{255, 255, 255, 255}}, {799, 599, color.RGBA{255, 255, 255, 255}}},
		},
		{
			name:   "root color overrides body propagation",
			markup: `<html style="background:blue"><body style="background:green"><div style="height:20px"></div></body></html>`,
			points: []struct {
				x, y int
				want color.RGBA
			}{{0, 0, color.RGBA{0, 0, 255, 255}}, {10, 10, color.RGBA{0, 128, 0, 255}}, {799, 599, color.RGBA{0, 0, 255, 255}}},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			img := painted(t, tc.markup, viewport)
			for _, point := range tc.points {
				pixel(t, img, point.x, point.y, point.want)
			}
		})
	}
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

func imagePainted(t *testing.T, viewport image.Rectangle, pictures ...ImageBox) *image.RGBA {
	t.Helper()
	var buf bytes.Buffer
	layout := Layout{Viewport: viewport, Root: &Box{Children: []*Box{{Images: pictures}}}}
	if err := paint(layout, &buf, renderOptions{}); err != nil {
		t.Fatal(err)
	}
	img, err := png.Decode(&buf)
	if err != nil {
		t.Fatal(err)
	}
	return img.(*image.RGBA)
}

func TestPaintScaledImageAndDocumentOrder(t *testing.T) {
	src := image.NewRGBA(image.Rect(5, 7, 7, 9))
	src.SetRGBA(5, 7, color.RGBA{R: 255, A: 255})
	src.SetRGBA(6, 7, color.RGBA{G: 255, A: 255})
	src.SetRGBA(5, 8, color.RGBA{B: 255, A: 255})
	src.SetRGBA(6, 8, color.RGBA{R: 255, G: 255, A: 255})
	img := imagePainted(t, image.Rect(0, 0, 12, 12),
		ImageBox{Image: src, Rect: image.Rect(1, 1, 9, 9)},
		ImageBox{Image: image.NewUniform(color.RGBA{R: 42, A: 255}), Rect: image.Rect(6, 6, 10, 10)})
	pixel(t, img, 1, 1, color.RGBA{R: 255, A: 255})
	pixel(t, img, 8, 1, color.RGBA{G: 255, A: 255})
	pixel(t, img, 1, 8, color.RGBA{B: 255, A: 255})
	pixel(t, img, 8, 8, color.RGBA{R: 42, A: 255})
	pixel(t, img, 10, 10, color.RGBA{R: 255, G: 255, B: 255, A: 255})
}

func TestPaintBrokenImagePlaceholderAndClip(t *testing.T) {
	src := image.NewRGBA(image.Rect(0, 0, 2, 2))
	src.SetRGBA(0, 0, color.RGBA{R: 255, A: 255})
	src.SetRGBA(1, 0, color.RGBA{G: 255, A: 255})
	src.SetRGBA(0, 1, color.RGBA{B: 255, A: 255})
	src.SetRGBA(1, 1, color.RGBA{R: 255, G: 255, A: 255})
	boxes := []ImageBox{
		{Image: src, Rect: image.Rect(-2, -2, 6, 6)},
		{Rect: image.Rect(7, 1, 13, 7)},
	}
	full := imagePainted(t, image.Rect(0, 0, 16, 10), boxes...)
	clipped := imagePainted(t, image.Rect(0, 0, 10, 5), boxes...)
	for y := 0; y < 5; y++ {
		for x := 0; x < 10; x++ {
			pixel(t, clipped, x, y, full.RGBAAt(x, y))
		}
	}
	pixel(t, clipped, 7, 1, color.RGBA{R: 160, G: 160, B: 160, A: 255})
	pixel(t, clipped, 8, 2, color.RGBA{R: 245, G: 245, B: 245, A: 255})
	if clipped.RGBAAt(0, 0) == (color.RGBA{R: 255, G: 255, B: 255, A: 255}) {
		t.Fatal("partly clipped decoded image did not paint")
	}
}

func TestImageBoxDiagnosticStillOutlinesDecodedImages(t *testing.T) {
	picture := ImageBox{Image: image.NewUniform(color.RGBA{R: 255, A: 255}),
		Rect: image.Rect(2, 2, 8, 8)}
	var output bytes.Buffer
	layout := Layout{Viewport: image.Rect(0, 0, 10, 10),
		Root: &Box{Children: []*Box{{Images: []ImageBox{picture}}}}}
	if err := paint(layout, &output, renderOptions{debugImageBoxes: true}); err != nil {
		t.Fatal(err)
	}
	diagnostic, err := png.Decode(&output)
	if err != nil {
		t.Fatal(err)
	}
	pixel(t, diagnostic.(*image.RGBA), 2, 2, color.RGBA{R: 230, B: 200, A: 255})
	pixel(t, diagnostic.(*image.RGBA), 4, 4, color.RGBA{R: 255, G: 255, B: 255, A: 255})
}

func TestConcurrentImageRenders(t *testing.T) {
	dir := t.TempDir()
	source := image.NewRGBA(image.Rect(0, 0, 2, 2))
	source.SetRGBA(0, 0, color.RGBA{R: 255, A: 255})
	var asset bytes.Buffer
	if err := png.Encode(&asset, source); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "pixel.png"), asset.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}
	page := filepath.Join(dir, "index.html")
	if err := os.WriteFile(page, []byte(`<img src="pixel.png" width="8" height="8"><img src="missing.svg" width="6" height="6">`), 0600); err != nil {
		t.Fatal(err)
	}
	var expected bytes.Buffer
	if err := Render(page, &expected); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			var got bytes.Buffer
			if err := Render(page, &got); err != nil {
				t.Error(err)
			} else if !bytes.Equal(got.Bytes(), expected.Bytes()) {
				t.Error("concurrent image render differs")
			}
		}()
	}
	wg.Wait()
}

var (
	zRed    = color.RGBA{255, 0, 0, 255}
	zGreen  = color.RGBA{0, 128, 0, 255}
	zBlue   = color.RGBA{0, 0, 255, 255}
	zYellow = color.RGBA{255, 255, 0, 255}
)

func TestPaintNegativeZIndexBelowInFlowContent(t *testing.T) {
	img := painted(t, `<body style="margin:0"><div style="position:absolute;left:0;top:0;width:20px;height:20px;background:red;z-index:-1"></div><div style="width:20px;height:20px;background:green"></div></body>`, image.Rect(0, 0, 40, 40))
	pixel(t, img, 10, 10, zGreen)
}

func TestPaintPositiveZIndexOrdering(t *testing.T) {
	img := painted(t, `<body style="margin:0"><div style="position:absolute;left:0;top:0;width:20px;height:20px;background:green;z-index:3"></div><div style="position:absolute;left:10px;top:10px;width:20px;height:20px;background:red;z-index:1"></div><div style="width:30px;height:30px;background:blue"></div></body>`, image.Rect(0, 0, 40, 40))
	pixel(t, img, 15, 15, zGreen) // z-index:3 above later z-index:1
	pixel(t, img, 25, 25, zRed)   // z-index:1 above in-flow content
	pixel(t, img, 35, 35, color.RGBA{255, 255, 255, 255})
}

func TestPaintEqualZIndexUsesTreeOrder(t *testing.T) {
	img := painted(t, `<body style="margin:0"><div style="position:absolute;left:0;top:0;width:20px;height:20px;background:red;z-index:2"></div><div style="position:absolute;left:0;top:0;width:20px;height:20px;background:green;z-index:2"></div></body>`, image.Rect(0, 0, 40, 40))
	pixel(t, img, 10, 10, zGreen)
}

func TestPaintZIndexAutoAndZeroUseTreeOrder(t *testing.T) {
	img := painted(t, `<body style="margin:0"><div style="position:absolute;left:0;top:0;width:20px;height:20px;background:red;z-index:0"></div><div style="position:absolute;left:0;top:0;width:20px;height:20px;background:green"></div></body>`, image.Rect(0, 0, 40, 40))
	pixel(t, img, 10, 10, zGreen)
}

func TestPaintLaterAutoWidthPositionedSiblingCoversReference(t *testing.T) {
	img := painted(t, `<body style="margin:0"><div style="position:relative"><div style="position:absolute;left:0;top:0;width:60px;height:60px;border:5px solid red"></div><div style="position:absolute;left:0;top:0;border:5px solid green"><div style="width:30px;height:30px;margin:10px;border:5px solid green"></div></div></div></body>`, image.Rect(0, 0, 100, 100))
	for _, point := range []image.Point{{2, 2}, {67, 2}, {2, 67}, {67, 67}} {
		pixel(t, img, point.X, point.Y, zGreen)
	}
}

func TestPaintZIndexIgnoredOnNonPositionedBoxes(t *testing.T) {
	// The static block's z-index is ignored, so the positioned box (z-index
	// auto) still paints above it; a negative z-index on a static box does
	// not push it under the canvas either.
	img := painted(t, `<body style="margin:0"><div style="position:absolute;left:0;top:0;width:20px;height:20px;background:green"></div><div style="z-index:10;width:30px;height:30px;background:red"></div><div style="z-index:-5;width:30px;height:10px;background:blue"></div></body>`, image.Rect(0, 0, 40, 50))
	pixel(t, img, 10, 10, zGreen)
	pixel(t, img, 25, 25, zRed)
	pixel(t, img, 10, 35, zBlue)
}

func TestPaintNestedStackingContextsAreAtomic(t *testing.T) {
	// A z-index:100 child cannot escape its z-index:1 parent context to rise
	// above a z-index:2 sibling context.
	img := painted(t, `<body style="margin:0"><div style="position:absolute;left:0;top:0;width:30px;height:30px;z-index:1"><div style="position:absolute;left:0;top:0;width:20px;height:20px;background:red;z-index:100"></div></div><div style="position:absolute;left:0;top:0;width:20px;height:20px;background:green;z-index:2"></div></body>`, image.Rect(0, 0, 40, 40))
	pixel(t, img, 10, 10, zGreen)
}

func TestPaintZIndexAutoDescendantsJoinParentContext(t *testing.T) {
	// A z-index:auto positioned box does not form a context, so its
	// z-index:5 child is ordered against the z-index:2 sibling directly.
	img := painted(t, `<body style="margin:0"><div style="position:absolute;left:0;top:0;width:30px;height:30px;background:yellow"><div style="position:absolute;left:0;top:0;width:20px;height:20px;background:green;z-index:5"></div></div><div style="position:absolute;left:0;top:0;width:20px;height:20px;background:red;z-index:2"></div></body>`, image.Rect(0, 0, 40, 40))
	pixel(t, img, 10, 10, zGreen)
	pixel(t, img, 25, 25, zYellow)
}

func TestPaintNegativeZIndexAboveContextBackground(t *testing.T) {
	// Negative layers paint above their own stacking context's background.
	img := painted(t, `<body style="margin:0"><div style="position:relative;z-index:0;width:30px;height:30px;background:red"><div style="position:absolute;left:0;top:0;width:20px;height:20px;background:green;z-index:-1"></div></div></body>`, image.Rect(0, 0, 40, 40))
	pixel(t, img, 10, 10, zGreen)
	pixel(t, img, 25, 25, zRed)
}

func TestPaintOverflowingTextAfterLaterBlockBackground(t *testing.T) {
	// Block backgrounds are painted before inline content, so text that
	// overflows its block remains visible over a following block background.
	img := painted(t, `<body style="margin:0"><div style="height:10px;font:20px monospace;color:black;white-space:nowrap">X</div><div style="height:10px;margin-top:-10px;background:lime"></div></body>`, image.Rect(0, 0, 30, 20))
	ink := 0
	for y := 0; y < 10; y++ {
		for x := 0; x < 16; x++ {
			p := img.RGBAAt(x, y)
			if p.R < 80 && p.G < 80 && p.B < 80 {
				ink++
			}
		}
	}
	if ink == 0 {
		t.Fatal("overflowing text was covered by the later block background")
	}
}

func TestPaintFloatAfterLaterBlockBackground(t *testing.T) {
	img := painted(t, `<body style="margin:0"><div style="float:left;width:20px;height:20px;background:red;margin-bottom:-20px"></div><div style="height:20px;background:blue"></div></body>`, image.Rect(0, 0, 30, 20))
	pixel(t, img, 10, 10, zRed)
}
