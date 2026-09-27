package browser

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

func TestExternalStylesheetQuotedBackgroundURLs(t *testing.T) {
	const root = "../../testdata/background-quoted-url"
	var paren, quote atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/assets/tile(1).png":
			paren.Add(1)
		case `/assets/tile'"2).png`:
			quote.Add(1)
		}
		http.FileServer(http.Dir(root)).ServeHTTP(w, r)
	}))
	defer server.Close()

	// Also exercise single and double quoted strings, escape parity, and
	// adjacent declarations: only the complete URL function is rewritten.
	for _, tc := range []struct{ css, want string }{
		{`url("tile(1).png") no-repeat`, `url("https://example.org/css/tile(1).png") no-repeat`},
		{`url('tile\'"2).png') no-repeat`, `url("https://example.org/css/tile'%222).png") no-repeat`},
		{`url("tile\"(1).png") center`, `url("https://example.org/css/tile%22(1).png") center`},
		{`url(tile\)1.png) no-repeat`, `url("https://example.org/css/tile)1.png") no-repeat`},
		{`linear-gradient(red,blue), url('tile(1).png') no-repeat`, `linear-gradient(red,blue), url("https://example.org/css/tile(1).png") no-repeat`},
	} {
		if got := resolveBackgroundURL(tc.css, "https://example.org/css/site.css"); got != tc.want {
			t.Errorf("resolve %s = %s, want %s", tc.css, got, tc.want)
		}
	}

	// The fixture's CSS is fetched as an external sheet, then backgrounds
	// resolve against its URL rather than the document URL.
	source, err := os.ReadFile(filepath.Join(root, "index.html"))
	if err != nil {
		t.Fatal(err)
	}
	doc, err := parse(Resource{URL: server.URL + "/index.html", Body: source})
	if err != nil {
		t.Fatal(err)
	}
	styled, err := style(doc, &Fetcher{})
	if err != nil {
		t.Fatal(err)
	}
	if paren.Load() != 1 || quote.Load() != 1 {
		t.Fatalf("PNG fetches: paren=%d quote=%d; want one each", paren.Load(), quote.Load())
	}
	layout, err := LayoutWithViewport(styled, image.Rect(0, 0, 80, 80))
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := paint(layout, &out, renderOptions{}); err != nil {
		t.Fatal(err)
	}
	img, err := png.Decode(&out)
	if err != nil {
		t.Fatal(err)
	}
	pixel(t, img.(*image.RGBA), 12, 12, color.RGBA{225, 49, 58, 255})
	pixel(t, img.(*image.RGBA), 12, 44, color.RGBA{35, 96, 210, 255})
}

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
			{"hint in stacked background", `background:linear-gradient(to right, red, 20%, blue),linear-gradient(green,green)`,
				map[image.Point]color.RGBA{{1, 0}: {142, 0, 113, 255}, {5, 0}: {58, 0, 197, 255}}},
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

func TestBackgroundPositionAxes(t *testing.T) {
	for _, tc := range []struct {
		value, x, y string
	}{
		{"", "left", "top"},
		{"right", "right", "center"},
		{"top", "center", "top"},
		{"bottom", "center", "bottom"},
		{"center", "center", "center"},
		{"25%", "25%", "center"},
		{"12px", "12px", "center"},
		{"top right", "right", "top"},
		{"bottom left", "left", "bottom"},
		{"center left", "left", "center"},
		{"right center", "right", "center"},
		{"left bottom", "left", "bottom"},
		{"top 25%", "25%", "top"},
	} {
		t.Run(tc.value, func(t *testing.T) {
			x, y := backgroundPositionAxes(strings.Fields(tc.value))
			if x != tc.x || y != tc.y {
				t.Errorf("axes = %q %q, want %q %q", x, y, tc.x, tc.y)
			}
			props := map[string]string{}
			for _, d := range expandBackground(Declaration{Value: "url(tile.png) no-repeat " + tc.value}) {
				props[d.Property] = d.Value
			}
			if got := props["background-position"]; got != tc.value {
				t.Errorf("shorthand position = %q, want %q", got, tc.value)
			}
		})
	}
}

func TestBackgroundPositionPixels(t *testing.T) {
	tile := image.NewRGBA(image.Rect(0, 0, 10, 10))
	for y := 0; y < 10; y++ {
		for x := 0; x < 10; x++ {
			tile.SetRGBA(x, y, color.RGBA{255, 0, 0, 255})
		}
	}
	for _, tc := range []struct {
		position string
		x, y     int
	}{
		{"right", 90, 45},
		{"left", 0, 45},
		{"top", 45, 0},
		{"bottom", 45, 90},
		{"center", 45, 45},
		{"top right", 90, 0},
		{"right top", 90, 0},
		{"bottom left", 0, 90},
		{"left bottom", 0, 90},
		{"center right", 90, 45},
		{"25% 75%", 23, 68},
	} {
		t.Run(tc.position, func(t *testing.T) {
			dst := image.NewRGBA(image.Rect(0, 0, 100, 100))
			drawBackgroundImage(dst, &Box{Rect: dst.Bounds()}, tile, ComputedStyle{
				"background-position": tc.position, "background-repeat": "no-repeat",
			})
			pixel(t, dst, tc.x, tc.y, color.RGBA{255, 0, 0, 255})
			for _, p := range []image.Point{{0, 0}, {99, 99}, {tc.x - 1, tc.y}, {tc.x, tc.y - 1}} {
				if p.In(dst.Bounds()) && (p.X < tc.x || p.Y < tc.y) {
					pixel(t, dst, p.X, p.Y, color.RGBA{})
				}
			}
		})
	}

	oversized := image.NewRGBA(image.Rect(0, 0, 120, 120))
	for y := 0; y < 120; y++ {
		for x := 0; x < 120; x++ {
			oversized.SetRGBA(x, y, color.RGBA{uint8(x), uint8(y), 0, 255})
		}
	}
	for _, tc := range []struct {
		position string
		want     color.RGBA
	}{
		{"0% 0%", color.RGBA{0, 0, 0, 255}},
		{"25% 75%", color.RGBA{5, 15, 0, 255}},
		{"50% 50%", color.RGBA{10, 10, 0, 255}},
		{"100% 100%", color.RGBA{20, 20, 0, 255}},
		{"right", color.RGBA{20, 10, 0, 255}},
	} {
		t.Run("oversized "+tc.position, func(t *testing.T) {
			dst := image.NewRGBA(image.Rect(0, 0, 100, 100))
			drawBackgroundImage(dst, &Box{Rect: dst.Bounds()}, oversized, ComputedStyle{
				"background-position": tc.position, "background-repeat": "no-repeat",
			})
			pixel(t, dst, 0, 0, tc.want)
			pixel(t, dst, 99, 99, color.RGBA{tc.want.R + 99, tc.want.G + 99, 0, 255})
		})
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

func TestGradientPercentageHint(t *testing.T) {
	g := parseGradient("linear-gradient(to right, red, 20%, blue)", 100, 20)
	if g == nil || len(g.stops) != 2 || !g.stops[1].hasHint || g.stops[1].hint != .2 {
		t.Fatalf("percentage hint rejected or misplaced: %+v", g)
	}
	// Pixel centers are at (x+.5)%; near the 20% hint the mixture is
	// approximately half red and half blue, instead of the usual 80/20.
	for _, tc := range []struct {
		x      int
		redMin int
		redMax int
	}{
		{9, 160, 164},  // first half of the gradient is compressed
		{19, 126, 131}, // the shifted midpoint
		{49, 65, 69},   // second half is stretched
	} {
		c := color.NRGBAModel.Convert(g.At(tc.x, 10)).(color.NRGBA)
		if int(c.R) < tc.redMin || int(c.R) > tc.redMax || int(c.B) != 255-int(c.R) {
			t.Errorf("hint pixel %d = %v", tc.x, c)
		}
	}
	ordinary := parseGradient("linear-gradient(to right, red, blue)", 100, 20)
	if c := color.NRGBAModel.Convert(ordinary.At(19, 10)).(color.NRGBA); c.R != 205 || c.B != 50 {
		t.Errorf("ordinary midpoint shifted: %v", c)
	}
	// Direction, adjacent intervals and premultiplied alpha must remain intact.
	vertical := parseGradient("linear-gradient(to bottom, transparent, 20%, blue 50%, red)", 20, 100)
	if vertical == nil || vertical.stops[2].hasHint || !vertical.stops[1].hasHint {
		t.Fatalf("hint interval: %+v", vertical)
	}
	c := color.NRGBAModel.Convert(vertical.At(10, 19)).(color.NRGBA)
	if c.A < 120 || c.A > 130 || c.B != 255 {
		t.Errorf("premultiplied transparent-blue hint: %v", c)
	}
	if c := color.NRGBAModel.Convert(vertical.At(10, 75)).(color.NRGBA); c.R < 125 || c.R > 131 || c.B < 124 || c.B > 130 {
		t.Errorf("ordinary interval following hint: %v", c)
	}
	for _, invalid := range []string{
		"linear-gradient(20%, red, blue)",
		"linear-gradient(red, blue, 20%)",
		"linear-gradient(red, 20%, 30%, blue)",
		"linear-gradient(red, 20px, blue)",
		"linear-gradient(red, NaN%, blue)",
		"linear-gradient(red, 1e309%, blue)",
		"linear-gradient(red 30%, 20%, blue)",
		"linear-gradient(red, 100%, blue)",
		"linear-gradient(red, 20%, blue 10%)",
		"linear-gradient(red, calc(20%), blue)",
	} {
		if got := parseGradient(invalid, 100, 20); got != nil {
			t.Errorf("accepted malformed hint %q", invalid)
		}
	}
	if got := parseGradient("linear-gradient(red, 20%, blue)", 4097, 20); got != nil {
		t.Fatal("hint bypassed resource bounds")
	}
}

func TestGradientMultiplePercentageHintsStayWithinTheirIntervals(t *testing.T) {
	g := parseGradient("linear-gradient(to right, red 0%, 10%, blue 25%, green 75%, 90%, white 100%)", 100, 20)
	if g == nil || len(g.stops) != 4 {
		t.Fatalf("multiple-hint gradient rejected: %+v", g)
	}
	for i, want := range []struct {
		at      float64
		hasHint bool
	}{{0, false}, {.25, true}, {.75, false}, {1, true}} {
		if got := g.stops[i]; got.at != want.at || got.hasHint != want.hasHint {
			t.Errorf("stop %d = %+v, want at %g with hint=%v", i, got, want.at, want.hasHint)
		}
	}
	for i, want := range []float64{0, .1, 0, .9} {
		if got := g.stops[i].hint; g.stops[i].hasHint && got != want {
			t.Errorf("stop %d hint = %g, want %g", i, got, want)
		}
	}
	for _, tc := range []struct {
		x    int
		want color.NRGBA
		tol  uint8
	}{
		{9, color.NRGBA{132, 0, 123, 255}, 3},    // first hint remaps red -> blue
		{20, color.NRGBA{38, 0, 217, 255}, 3},    // still the first interval
		{49, color.NRGBA{0, 64, 128, 255}, 2},    // middle interval remains linear
		{89, color.NRGBA{122, 191, 122, 255}, 3}, // second hint remaps green -> white
	} {
		got := color.NRGBAModel.Convert(g.At(tc.x, 10)).(color.NRGBA)
		if absInt(int(got.R)-int(tc.want.R)) > int(tc.tol) ||
			absInt(int(got.G)-int(tc.want.G)) > int(tc.tol) ||
			absInt(int(got.B)-int(tc.want.B)) > int(tc.tol) ||
			got.A != tc.want.A {
			t.Errorf("hint pixel %d = %v, want about %v", tc.x, got, tc.want)
		}
	}
	for _, invalid := range []string{
		"linear-gradient(red, 20%, 30%, blue)",
		"linear-gradient(red, 20%, blue, 60%, 70%, green)",
		"linear-gradient(red 20%, 20%, blue)",
		"linear-gradient(red, 20%, blue 10%, 15%, green)",
		"linear-gradient(red, 20%, blue, 100%, green)",
	} {
		if got := parseGradient(invalid, 100, 20); got != nil {
			t.Errorf("accepted malformed multiple hint gradient %q", invalid)
		}
	}
}

func absInt(v int) int {
	if v < 0 {
		return -v
	}
	return v
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
