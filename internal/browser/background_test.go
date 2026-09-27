package browser

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

func TestBackgroundLayerParsingAndShorthand(t *testing.T) {
	input := `url("a,b).png") center/4px 4px no-repeat, linear-gradient(red, blue), url('c.png') right bottom/2px 2px repeat-x #123456`
	layers := backgroundLayers(input)
	if len(layers) != 3 || backgroundURL(layers[0]) != "a,b).png" || backgroundURL(layers[1]) != "" || backgroundURL(layers[2]) != "c.png" {
		t.Fatalf("layers: %q", layers)
	}

	t.Run("linear gradient pixels and layers", func(t *testing.T) {
		tests := []struct {
			name, style string
			points      map[image.Point]color.RGBA
		}{
			{"default", `background:linear-gradient(red, blue)`,
				map[image.Point]color.RGBA{{0, 0}: {242, 0, 13, 255}, {0, 9}: {13, 0, 242, 255}}},
			{"direction", `background-image:linear-gradient(to right, #f00 0%, #00f 100%)`,
				map[image.Point]color.RGBA{{0, 0}: {242, 0, 13, 255}, {9, 0}: {13, 0, 242, 255}}},
			{"angle", `background:linear-gradient(90deg, red, blue)`,
				map[image.Point]color.RGBA{{0, 0}: {242, 0, 13, 255}, {9, 0}: {13, 0, 242, 255}}},
			{"hard-stop", `background:linear-gradient(red 50%, blue 50%)`,
				map[image.Point]color.RGBA{{0, 4}: {255, 0, 0, 255}, {0, 5}: {0, 0, 255, 255}}},
			{"interpolated-stops", `background:linear-gradient(red, green 50%, blue)`,
				map[image.Point]color.RGBA{{0, 5}: {0, 115, 26, 255}}},
			{"transparent-top", `background:linear-gradient(to bottom, transparent, rgba(255,0,0,1)), #00ff00`,
				map[image.Point]color.RGBA{{0, 0}: {13, 242, 0, 255}, {0, 9}: {242, 13, 0, 255}}},
			{"url-missing-top", `background-image:url(missing.png),linear-gradient(red,blue)`,
				map[image.Point]color.RGBA{{0, 0}: {242, 0, 13, 255}}},
			{"clipped-and-sized", `border:2px solid black;background:linear-gradient(to right, red, blue) no-repeat center/4px 4px;background-color:green`,
				map[image.Point]color.RGBA{{0, 0}: {0, 0, 0, 255}, {2, 2}: {0, 128, 0, 255},
					{5, 5}: {223, 0, 32, 255}, {8, 5}: {32, 0, 223, 255}}},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				img := painted(t, `<div style="margin:0;width:10px;height:10px;`+tt.style+`"></div>`, image.Rect(0, 0, 20, 20))
				for point, want := range tt.points {
					pixel(t, img, point.X, point.Y, want)
				}
			})
		}
	})
	resolved := resolveBackgroundURL(input, "https://example.org/css/site.css")
	if !strings.Contains(resolved, `url("https://example.org/css/a,b).png")`) ||
		!strings.Contains(resolved, `url("https://example.org/css/c.png")`) ||
		!strings.Contains(resolved, "linear-gradient(red, blue)") {
		t.Fatalf("resolved layers: %s", resolved)
	}
	props := map[string]string{}
	for _, d := range expandBackground(Declaration{Value: input}) {
		props[d.Property] = d.Value
	}
	for property, want := range map[string]string{
		"background-image":    `url("a,b).png"), linear-gradient(red, blue), url("c.png")`,
		"background-repeat":   "no-repeat, repeat, repeat-x",
		"background-position": "center, , right bottom",
		"background-size":     "4px 4px, , 2px 2px",
		"background-color":    "#123456",
	} {
		if props[property] != want {
			t.Errorf("%s = %q, want %q", property, props[property], want)
		}
		style := ComputedStyle{
			"background-size":     "2px 2px, 4px 4px",
			"background-repeat":   "no-repeat",
			"background-position": "",
		}
		third := backgroundLayerStyle(style, 2)
		if third["background-size"] != "2px 2px" || third["background-repeat"] != "no-repeat" ||
			third["background-position"] != "" {
			t.Fatalf("cyclic lists and defaults: %v", third)
		}
	}
}

func TestGradientStopNormalizationAndUnsupportedSyntax(t *testing.T) {
	g := parseGradient("linear-gradient(to right, red 80%, green, blue 20%, white)", 100, 20)
	if g == nil {
		t.Fatal("gradient rejected")
	}
	// Explicit backwards positions clamp before distributing unspecified stops.
	for i, want := range []float64{.8, .8, .8, 1} {
		if got := g.stops[i].at; got != want {
			t.Errorf("stop %d = %g, want %g", i, got, want)
		}
	}
	px := parseGradient("linear-gradient(to bottom, red 5px, blue 15px)", 20, 20)
	if px == nil || px.stops[0].at != .25 || px.stops[1].at != .75 {
		t.Errorf("pixel stop positions: %+v", px)
	}
	for _, invalid := range []string{
		"radial-gradient(red,blue)",
		"linear-gradient(red)",
		"linear-gradient(red, 20%, blue)", // color hint: #127
		"linear-gradient(red, blue 2em)",
		"linear-gradient(to sideways, red, blue)",
	} {
		if got := parseGradient(invalid, 20, 20); got != nil {
			t.Errorf("unexpected rendering for %q", invalid)
		}
	}
	if g := parseGradient("linear-gradient(red, blue)", 4097, 20); g != nil {
		t.Fatal("unbounded gradient allocation")
	}
}

func TestBackgroundLayersFetchAndComposite(t *testing.T) {
	opaque := image.NewRGBA(image.Rect(0, 0, 2, 2))
	top := image.NewRGBA(image.Rect(0, 0, 2, 2))
	for y := 0; y < 2; y++ {
		for x := 0; x < 2; x++ {
			opaque.SetRGBA(x, y, color.RGBA{0, 0, 255, 255})
			top.SetRGBA(x, y, color.RGBA{255, 0, 0, 255})
		}
	}
	top.SetRGBA(1, 1, color.RGBA{})
	var blue, red atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/css/site.css":
			_, _ = w.Write([]byte(`.tile {background-image:url("../assets/red,top).png"),url("../assets/blue.png"),url("../assets/blue.png");background-repeat:no-repeat;background-size:2px 2px,4px 4px;background-position:right bottom, left top;background-color:#00ff00}`))
		case "/assets/red,top).png":
			red.Add(1)
			_ = png.Encode(w, top)
		case "/assets/blue.png":
			blue.Add(1)
			_ = png.Encode(w, opaque)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	doc, err := parse(Resource{URL: server.URL + "/pages/page.html", Body: []byte(`<link rel="stylesheet" href="../css/site.css"><div class="tile" style="margin:0;width:6px;height:6px"></div><div class="tile" style="margin:0;width:6px;height:6px"></div>`)})
	if err != nil {
		t.Fatal(err)
	}
	styled, err := style(doc, &Fetcher{})
	if err != nil {
		t.Fatal(err)
	}
	if red.Load() != 1 || blue.Load() != 1 {
		t.Fatalf("fetch counts red=%d blue=%d; want one each", red.Load(), blue.Load())
	}
	l, err := LayoutWithViewport(styled, image.Rect(0, 0, 30, 30))
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := paint(l, &out, renderOptions{}); err != nil {
		t.Fatal(err)
	}
	decoded, err := png.Decode(&out)
	if err != nil {
		t.Fatal(err)
	}
	img := decoded.(*image.RGBA)
	pixel(t, img, 4, 4, color.RGBA{255, 0, 0, 255}) // top, positioned bottom right
	pixel(t, img, 5, 5, color.RGBA{0, 0, 255, 255}) // transparent top; third layer repeats size list
	pixel(t, img, 0, 0, color.RGBA{0, 0, 255, 255}) // bottom layer
	pixel(t, img, 4, 2, color.RGBA{0, 255, 0, 255}) // no-repeat default on third layer
	pixel(t, img, 5, 0, color.RGBA{0, 255, 0, 255}) // uncovered background color
}

// A stylesheet resolves its relative URL to an absolute local file before
// fetchImages resolves it again against the page. Exercise both passes with
// the pinned WPT fixture, rather than only testing ResolveCSSURL in isolation.
func TestWPTLocalBackgroundImageMatchesReference(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", "..", "testdata", "wpt", "backgrounds"))
	if err != nil {
		t.Fatal(err)
	}
	render := func(name string) image.Image {
		t.Helper()
		var out bytes.Buffer
		if err := RenderWithFetcher(filepath.Join(root, name), &out, &Fetcher{}); err != nil {
			t.Fatal(err)
		}
		img, err := png.Decode(&out)
		if err != nil {
			t.Fatal(err)
		}
		return img
	}
	want := render("background-001-ref.xht")
	for _, path := range []string{
		filepath.Join(root, "background-002.xht"),
		filepath.Join("..", "..", "testdata", "wpt", "backgrounds", "background-002.xht"),
	} {
		// Absolute and relative document inputs must both load the tile.
		var out bytes.Buffer
		if err := RenderWithFetcher(path, &out, &Fetcher{}); err != nil {
			t.Fatal(err)
		}
		got, err := png.Decode(&out)
		if err != nil {
			t.Fatal(err)
		}
		if got.Bounds() != want.Bounds() {
			t.Fatalf("%s image bounds: %v, want %v", path, got.Bounds(), want.Bounds())
		}
		for y := got.Bounds().Min.Y; y < got.Bounds().Max.Y; y++ {
			for x := got.Bounds().Min.X; x < got.Bounds().Max.X; x++ {
				if color.NRGBAModel.Convert(got.At(x, y)) != color.NRGBAModel.Convert(want.At(x, y)) {
					t.Fatalf("%s differs from WPT reference at (%d,%d)", path, x, y)
				}
			}
		}
	}
}

func TestBackgroundRasterPaintAndStylesheetBase(t *testing.T) {
	data := encodedTestImage(t, "png", image.Rect(0, 0, 2, 2))
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/css/site.css":
			_, _ = w.Write([]byte(`.tile { background: url("../assets/pixel.png") no-repeat; background-size: 4px auto; background-position: right bottom; background-color: #0000ff }`))
		case "/assets/pixel.png":
			requests.Add(1)
			_, _ = w.Write(data)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	source := `<link rel="stylesheet" href="../css/site.css"><div class="tile" style="margin:0;width:8px;height:8px;padding:2px;border:2px solid red"></div><div class="tile" style="margin:0;width:8px;height:8px"></div><img src="../assets/pixel.png">`
	doc, err := parse(Resource{URL: server.URL + "/pages/page.html", Body: []byte(source)})
	if err != nil {
		t.Fatal(err)
	}
	styled, err := style(doc, &Fetcher{})
	if err != nil {
		t.Fatal(err)
	}
	if requests.Load() != 1 {
		t.Fatalf("image fetched %d times, want once shared with img", requests.Load())
	}
	layout, err := LayoutWithViewport(styled, image.Rect(0, 0, 80, 60))
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := paint(layout, &out, renderOptions{}); err != nil {
		t.Fatal(err)
	}
	decoded, err := png.Decode(&out)
	if err != nil {
		t.Fatal(err)
	}
	img := decoded.(*image.RGBA)
	pixel(t, img, 0, 0, color.RGBA{255, 0, 0, 255})     // border
	pixel(t, img, 2, 2, color.RGBA{0, 0, 255, 255})     // color underneath
	pixel(t, img, 13, 13, color.RGBA{40, 60, 180, 255}) // scaled bottom right
	pixel(t, img, 10, 2, color.RGBA{0, 0, 255, 255})    // no repeat
}

func TestBackgroundShorthandCompactSlashPaintsScaledTile(t *testing.T) {
	data := encodedTestImage(t, "png", image.Rect(0, 0, 2, 2))
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/tile.png" {
			_, _ = w.Write(data)
		} else {
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	source := `<div style="margin:0;width:8px;height:8px;background:url(tile.png) center/4px 4px no-repeat #00ff00"></div>`
	doc, err := parse(Resource{URL: server.URL + "/page", Body: []byte(source)})
	if err != nil {
		t.Fatal(err)
	}
	styled, err := style(doc, &Fetcher{})
	if err != nil {
		t.Fatal(err)
	}
	layout, err := LayoutWithViewport(styled, image.Rect(0, 0, 12, 12))
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := paint(layout, &out, renderOptions{}); err != nil {
		t.Fatal(err)
	}
	decoded, err := png.Decode(&out)
	if err != nil {
		t.Fatal(err)
	}
	img := decoded.(*image.RGBA)
	pixel(t, img, 1, 1, color.RGBA{0, 255, 0, 255})
	pixel(t, img, 2, 2, color.RGBA{0, 0, 180, 255})
	pixel(t, img, 6, 6, color.RGBA{0, 255, 0, 255})
}

func TestBackgroundRepeatAndFailuresHaveNoPlaceholder(t *testing.T) {
	data := encodedTestImage(t, "png", image.Rect(0, 0, 2, 2))
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/tile.png" {
			_, _ = w.Write(data)
		} else if r.URL.Path == "/arrow.svg" {
			_, _ = w.Write([]byte(`<svg xmlns="http://www.w3.org/2000/svg"/>`))
		} else {
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	for _, tc := range []struct {
		css  string
		x, y int
		want color.RGBA
	}{
		{`background-image:url(tile.png)`, 4, 4, color.RGBA{0, 0, 180, 255}},
		{`background:url(tile.png) no-repeat`, 4, 4, color.RGBA{255, 255, 255, 255}},
		{`background:url(arrow.svg) no-repeat`, 0, 0, color.RGBA{255, 255, 255, 255}},
		{`background-image:url(absent.png)`, 0, 0, color.RGBA{255, 255, 255, 255}},
	} {
		doc, err := parse(Resource{URL: server.URL + "/page", Body: []byte(`<div style="margin:0;width:8px;height:8px;` + tc.css + `"></div>`)})
		if err != nil {
			t.Fatal(err)
		}
		styled, err := style(doc, &Fetcher{})
		if err != nil {
			t.Fatal(err)
		}
		l, err := LayoutWithViewport(styled, image.Rect(0, 0, 20, 20))
		if err != nil {
			t.Fatal(err)
		}
		var out bytes.Buffer
		if err := paint(l, &out, renderOptions{}); err != nil {
			t.Fatal(err)
		}
		img, err := png.Decode(&out)
		if err != nil {
			t.Fatal(err)
		}
		pixel(t, img.(*image.RGBA), tc.x, tc.y, tc.want)
	}
}
