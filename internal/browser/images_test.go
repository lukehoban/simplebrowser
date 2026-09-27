package browser

import (
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"image"
	"image/color"
	"image/gif"
	"image/jpeg"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

func encodedTestImage(t *testing.T, format string, bounds image.Rectangle) []byte {
	t.Helper()
	src := image.NewRGBA(bounds)
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			src.Set(x, y, color.RGBA{R: uint8(x * 40), G: uint8(y * 60), B: 180, A: 255})
		}
	}
	var out bytes.Buffer
	var err error
	switch format {
	case "png":
		err = png.Encode(&out, src)
	case "gif":
		paletted := image.NewPaletted(bounds, color.Palette{color.Black, color.White, color.RGBA{R: 255, A: 255}})
		for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
			for x := bounds.Min.X; x < bounds.Max.X; x++ {
				paletted.SetColorIndex(x, y, uint8((x+y)%3))
			}
		}
		err = gif.Encode(&out, paletted, nil)
	case "jpeg":
		err = jpeg.Encode(&out, src, nil)
	default:
		t.Fatalf("unknown format %q", format)
	}
	if err != nil {
		t.Fatal(err)
	}
	return out.Bytes()
}

func TestDecodeImageFormatsAndBounds(t *testing.T) {
	for _, format := range []string{"gif", "png", "jpeg"} {
		t.Run(format, func(t *testing.T) {
			decoded := decodeImage(encodedTestImage(t, format, image.Rect(0, 0, 4, 2)))
			if decoded == nil || decoded.Bounds().Dx() != 4 || decoded.Bounds().Dy() != 2 {
				t.Fatalf("decoded image = %v", decoded)
			}
		})
	}
	if got := decodeImage([]byte(`<svg xmlns="http://www.w3.org/2000/svg" width="3" height="2"/>`)); got == nil || got.Bounds().Size() != image.Pt(3, 2) {
		t.Fatalf("SVG subset should decode at its intrinsic size: %v", got)
	}
	if got := decodeImage([]byte(`<svg xmlns="http://www.w3.org/2000/svg"><path`)); got != nil {
		t.Fatalf("malformed SVG should not decode: %v", got)
	}
}

func TestDecodeDataImageURL(t *testing.T) {
	data := encodedTestImage(t, "png", image.Rect(0, 0, 2, 3))
	svg := `<svg xmlns="http://www.w3.org/2000/svg" width="3" height="2"/>`
	esc := strings.NewReplacer("<", "%3C", ">", "%3E", " ", "%20", `"`, `%22`)
	for _, tc := range []struct {
		name, url string
		size      image.Point
	}{{"png", "data:image/png;base64," + base64.StdEncoding.EncodeToString(data), image.Pt(2, 3)}, {"svg", "data:image/svg+xml;utf8," + esc.Replace(svg), image.Pt(3, 2)}} {
		t.Run(tc.name, func(t *testing.T) {
			d, ok := decodeDataImageURL(tc.url)
			if !ok || decodeImage(d) == nil || decodeImage(d).Bounds().Size() != tc.size {
				t.Fatalf("decode failed")
			}
		})
	}
	for _, s := range []string{"data:image/png;base64,not-valid", "data:text/plain,x", "data:image/png,%zz"} {
		if _, ok := decodeDataImageURL(s); ok {
			t.Errorf("accepted %q", s)
		}
	}
}
func TestFetchImageCacheAndInlineLayout(t *testing.T) {
	pngBytes := encodedTestImage(t, "png", image.Rect(0, 0, 4, 2))
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/page/assets/pixel.png" {
			http.NotFound(w, r)
			return
		}
		requests.Add(1)
		_, _ = w.Write(pngBytes)
	}))
	defer server.Close()

	source := `<p>a<img src="assets/pixel.png" width="8"> b` +
		`<img src="assets/pixel.png" height="6">` +
		`<img src="missing.svg" width="20" height="7"></p>`
	doc, err := parse(Resource{URL: server.URL + "/page/index.html", Body: []byte(source)})
	if err != nil {
		t.Fatal(err)
	}
	styled, err := style(doc, &Fetcher{})
	if err != nil {
		t.Fatal(err)
	}
	if requests.Load() != 1 {
		t.Fatalf("fetch count for duplicate image = %d, want 1", requests.Load())
	}
	layout, err := LayoutWithViewport(styled, image.Rect(0, 0, 300, 100))
	if err != nil {
		t.Fatal(err)
	}
	var boxes []ImageBox
	var visit func(*Box)
	visit = func(b *Box) {
		boxes = append(boxes, b.Images...)
		for _, child := range b.Children {
			visit(child)
		}
	}
	visit(layout.Root)
	if len(boxes) != 3 {
		t.Fatalf("laid-out image count = %d, boxes = %#v", len(boxes), boxes)
	}
	if boxes[0].Image == nil || boxes[0].Rect.Size() != image.Pt(8, 4) {
		t.Errorf("width-constrained image = (%v, %v), want 8x4", boxes[0].Image, boxes[0].Rect)
	}
	if boxes[1].Image == nil || boxes[1].Rect.Size() != image.Pt(12, 6) {
		t.Errorf("height-constrained image = (%v, %v), want 12x6", boxes[1].Image, boxes[1].Rect)
	}
	if boxes[2].Image != nil || boxes[2].Rect.Size() != image.Pt(20, 7) {
		t.Errorf("failed image placeholder = (%v, %v), want 20x7", boxes[2].Image, boxes[2].Rect)
	}
}

func TestImageIntrinsicWidthContributesToTable(t *testing.T) {
	data := encodedTestImage(t, "png", image.Rect(0, 0, 5, 3))
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/pixel.png" {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write(data)
	}))
	defer server.Close()
	doc, err := parse(Resource{URL: server.URL + "/page", Body: []byte(
		`<table><tr><td><img src="pixel.png" width="13"></td></tr></table>`)})
	if err != nil {
		t.Fatal(err)
	}
	styled, err := style(doc, &Fetcher{})
	if err != nil {
		t.Fatal(err)
	}
	var find func(*StyledNode) *StyledNode
	find = func(n *StyledNode) *StyledNode {
		if n.Node != nil && n.Node.Name == "img" {
			return n
		}
		for _, child := range n.Children {
			if found := find(child); found != nil {
				return found
			}
		}
		return nil
	}
	img := find(styled.StyleRoot)
	faces := newFaceSet()
	faces.images = styled.Images
	minWidth, maxWidth := intrinsicWidths(img, faces)
	faces.close()
	if minWidth != 13 || maxWidth != 13 {
		t.Fatalf("image intrinsic widths = %d..%d, want 13..13", minWidth, maxWidth)
	}
}

func layoutImageBoxes(t *testing.T, resource Resource, viewport image.Rectangle) []ImageBox {
	t.Helper()
	doc, err := parse(resource)
	if err != nil {
		t.Fatal(err)
	}
	styled, err := style(doc, &Fetcher{})
	if err != nil {
		t.Fatal(err)
	}
	layout, err := LayoutWithViewport(styled, viewport)
	if err != nil {
		t.Fatal(err)
	}
	var boxes []ImageBox
	var visit func(*Box)
	visit = func(b *Box) {
		boxes = append(boxes, b.Images...)
		for _, child := range b.Children {
			visit(child)
		}
	}
	visit(layout.Root)
	return boxes
}

// Images referenced from a local page resolve relative to the page's directory.
func TestFetchImagesResolvesRelativeLocalPaths(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "assets"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "assets", "pixel.gif"),
		encodedTestImage(t, "gif", image.Rect(0, 0, 6, 3)), 0o644); err != nil {
		t.Fatal(err)
	}
	page := filepath.Join(dir, "index.html")
	source := []byte(`<p><img src="assets/pixel.gif"><img src="assets/absent.png"></p>`)
	if err := os.WriteFile(page, source, 0o644); err != nil {
		t.Fatal(err)
	}
	boxes := layoutImageBoxes(t, Resource{Source: page, URL: page, Body: source}, image.Rect(0, 0, 300, 100))
	if len(boxes) != 2 {
		t.Fatalf("image box count = %d, want 2", len(boxes))
	}
	if boxes[0].Image == nil || boxes[0].Rect.Size() != image.Pt(6, 3) {
		t.Errorf("relative local image = (%v, %v), want intrinsic 6x3", boxes[0].Image, boxes[0].Rect)
	}
	// A missing resource must not fail the render; it keeps a default box.
	if boxes[1].Image != nil || boxes[1].Rect.Size() != image.Pt(16, 16) {
		t.Errorf("missing local image = (%v, %v), want nil image and default box", boxes[1].Image, boxes[1].Rect)
	}
}

// Malformed SVG and other unsupported or corrupt payloads keep a sized
// placeholder box.
func TestUnsupportedAndCorruptImagesKeepPlaceholders(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/logo.svg":
			w.Header().Set("Content-Type", "image/svg+xml")
			_, _ = w.Write([]byte(`<svg xmlns="http://www.w3.org/2000/svg" width="30" height="10"><path`))
		case "/broken.png":
			_, _ = w.Write([]byte("\x89PNG\r\n\x1a\nnot really a png"))
		default:
			http.Error(w, "boom", http.StatusInternalServerError)
		}
	}))
	defer server.Close()
	source := []byte(`<p><img src="logo.svg" width="30" height="10">` +
		`<img src="broken.png" width="9" height="4">` +
		`<img src="error.png" height="5"></p>`)
	boxes := layoutImageBoxes(t, Resource{URL: server.URL + "/index.html", Body: source},
		image.Rect(0, 0, 300, 100))
	if len(boxes) != 3 {
		t.Fatalf("image box count = %d, want 3", len(boxes))
	}
	want := []image.Point{{X: 30, Y: 10}, {X: 9, Y: 4}, {X: 5, Y: 5}}
	for i, box := range boxes {
		if box.Image != nil {
			t.Errorf("box %d decoded unexpectedly", i)
		}
		if box.Rect.Size() != want[i] {
			t.Errorf("box %d size = %v, want %v", i, box.Rect.Size(), want[i])
		}
	}
}

// Images wider than the line box wrap onto their own line.
func TestImagesWrapWithinInlineFlow(t *testing.T) {
	data := encodedTestImage(t, "png", image.Rect(0, 0, 40, 10))
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(data)
	}))
	defer server.Close()
	source := []byte(`<p><img src="a.png"><img src="a.png"></p>`)
	boxes := layoutImageBoxes(t, Resource{URL: server.URL + "/index.html", Body: source},
		image.Rect(0, 0, 50, 100))
	if len(boxes) != 2 {
		t.Fatalf("image box count = %d, want 2", len(boxes))
	}
	if boxes[0].Rect.Min.Y == boxes[1].Rect.Min.Y {
		t.Fatalf("images did not wrap: %v and %v", boxes[0].Rect, boxes[1].Rect)
	}
	if boxes[1].Rect.Min.X != boxes[0].Rect.Min.X {
		t.Errorf("wrapped image x = %d, want %d", boxes[1].Rect.Min.X, boxes[0].Rect.Min.X)
	}
}

// display:none images are neither fetched nor laid out.
func TestDisplayNoneImagesAreSkipped(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		_, _ = w.Write(encodedTestImage(t, "png", image.Rect(0, 0, 3, 3)))
	}))
	defer server.Close()
	source := []byte(`<p><img src="a.png" style="display:none"></p>`)
	boxes := layoutImageBoxes(t, Resource{URL: server.URL + "/index.html", Body: source},
		image.Rect(0, 0, 300, 100))
	if len(boxes) != 0 {
		t.Errorf("image boxes = %d, want 0", len(boxes))
	}
	if requests.Load() != 0 {
		t.Errorf("fetches = %d, want 0", requests.Load())
	}
}

func TestDecodeImageRejectsOversizedConfig(t *testing.T) {
	// A PNG header claiming a huge canvas is rejected before decoding.
	var out bytes.Buffer
	if err := png.Encode(&out, image.NewRGBA(image.Rect(0, 0, 2, 2))); err != nil {
		t.Fatal(err)
	}
	data := out.Bytes()
	binary.BigEndian.PutUint32(data[16:20], 1<<16)
	binary.BigEndian.PutUint32(data[20:24], 1<<16)
	if got := decodeImage(data); got != nil {
		t.Fatalf("oversized image decoded: %v", got)
	}
	if got := decodeImage(nil); got != nil {
		t.Fatalf("empty payload decoded: %v", got)
	}
}

// A block-level img is laid out as a replaced block, not dropped.
func TestBlockLevelImageProducesImageBox(t *testing.T) {
	data := encodedTestImage(t, "png", image.Rect(0, 0, 8, 4))
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(data)
	}))
	defer server.Close()
	source := []byte(`<div><img src="a.png" style="display:block"><p>after</p></div>`)
	boxes := layoutImageBoxes(t, Resource{URL: server.URL + "/index.html", Body: source},
		image.Rect(0, 0, 300, 100))
	if len(boxes) != 1 {
		t.Fatalf("image box count = %d, want 1", len(boxes))
	}
	if boxes[0].Image == nil || boxes[0].Rect.Size() != image.Pt(8, 4) {
		t.Fatalf("block image = (%v, %v), want 8x4", boxes[0].Image, boxes[0].Rect)
	}
}

// The offline Hacker News fixture exercises the SVG logo and spacer GIFs.
func TestHackerNewsFixtureImageBoxes(t *testing.T) {
	page := filepath.Join("..", "..", "testdata", "hn", "news.html")
	body, err := os.ReadFile(page)
	if err != nil {
		t.Fatal(err)
	}
	boxes := layoutImageBoxes(t, Resource{Source: page, URL: page, Body: body}, image.Rect(0, 0, 800, 600))
	if len(boxes) == 0 {
		t.Fatal("fixture produced no image boxes")
	}
	logos := 0
	for _, box := range boxes {
		if box.Rect.Size() == image.Pt(18, 18) {
			if _, ok := box.Image.(*svgImage); !ok {
				t.Errorf("18x18 logo box did not decode as SVG: %T", box.Image)
			}
			logos++
		}
		if box.Rect.Dx() < 0 || box.Rect.Dy() < 0 {
			t.Fatalf("negative image rectangle %v", box.Rect)
		}
	}
	if logos == 0 {
		t.Errorf("expected an 18x18 SVG logo, got %d boxes", len(boxes))
	}
}
