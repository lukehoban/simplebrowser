package browser

import (
	"image"
	"image/color"
	"image/png"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
)

func segmentPoints(segs []svgSegment) string {
	var b strings.Builder
	for _, s := range segs {
		b.WriteByte(s.op)
		n := map[byte]int{'M': 1, 'L': 1, 'Q': 2, 'C': 3}[s.op]
		for i := 0; i < n; i++ {
			b.WriteString(" " + trimFloat(s.pts[i][0]) + "," + trimFloat(s.pts[i][1]))
		}
		b.WriteByte(';')
	}
	return b.String()
}

func trimFloat(f float64) string {
	return strconv.FormatFloat(math.Round(f*1000)/1000, 'f', -1, 64)
}

func TestParseSVGPath(t *testing.T) {
	for _, tc := range []struct{ d, want string }{
		// Absolute commands with H/V and close.
		{"M1 2 L3 4 H5 V6 Z", "M 1,2;L 3,4;L 5,4;L 5,6;Z;"},
		// Relative commands; implicit repeated coordinates after m become l.
		{"m4 4h188v188h-188z", "M 4,4;L 192,4;L 192,192;L 4,192;Z;"},
		{"m1 1 2 0 0 2", "M 1,1;L 3,1;L 3,3;"},
		{"M1 1 2 2 3 3", "M 1,1;L 2,2;L 3,3;"},
		// Repeated l arguments and compact number syntax (signs, .5.5, exponents).
		{"M0,0l1-1-1-1", "M 0,0;L 1,-1;L 0,-2;"},
		{"M.5.5L1e1 2E-1", "M 0.5,0.5;L 10,0.2;"},
		// Relative after Z starts from the subpath start.
		{"M10 10 l5 0 z m1 1 l1 0", "M 10,10;L 15,10;Z;M 11,11;L 12,11;"},
		// Curves, including the reflected S/T control points.
		{"M0 0 C1 1 2 1 3 0 S5 -1 6 0", "M 0,0;C 1,1 2,1 3,0;C 4,-1 5,-1 6,0;"},
		{"M0 0 q1 1 2 0 t2 0", "M 0,0;Q 1,1 2,0;Q 3,-1 4,0;"},
		// Errors render the path up to the error.
		{"M0 0 L1 1 L2", "M 0,0;L 1,1;"},
		{"M0 0 L1 1 A1 1 0 0 1 2 2 L3 3", "M 0,0;L 1,1;"},
		{"L1 1", ""},
		{"", ""},
	} {
		got := segmentPoints(parseSVGPath(tc.d, 1000))
		if got != tc.want {
			t.Errorf("parseSVGPath(%q) = %q, want %q", tc.d, got, tc.want)
		}
	}
	if got := len(parseSVGPath("M0 0 1 1 2 2 3 3", 2)); got != 2 {
		t.Errorf("segment limit not enforced: %d", got)
	}
}

func TestSVGViewBoxScaling(t *testing.T) {
	img, err := decodeSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" width="18" height="18" viewBox="4 4 188 188"/>`))
	if err != nil {
		t.Fatal(err)
	}
	if img.Bounds() != image.Rect(0, 0, 18, 18) {
		t.Fatalf("intrinsic bounds = %v", img.Bounds())
	}
	m := img.viewTransform(18, 18)
	if x, y := m.apply(4, 4); math.Abs(x) > 1e-9 || math.Abs(y) > 1e-9 {
		t.Errorf("viewBox origin maps to %v,%v", x, y)
	}
	if x, y := m.apply(192, 192); math.Abs(x-18) > 1e-9 || math.Abs(y-18) > 1e-9 {
		t.Errorf("viewBox corner maps to %v,%v", x, y)
	}
	// xMidYMid meet centers a 2:1 viewBox vertically in a square viewport.
	wide, err := decodeSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" width="32" height="32" viewBox="0 0 32 16"/>`))
	if err != nil {
		t.Fatal(err)
	}
	if x, y := wide.viewTransform(10, 10).apply(32, 0); math.Abs(x-10) > 1e-9 || math.Abs(y-2.5) > 1e-9 {
		t.Errorf("meet mapping = %v,%v, want 10,2.5", x, y)
	}
	stretched, err := decodeSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 32 16" preserveAspectRatio="none" width="10" height="10"/>`))
	if err != nil {
		t.Fatal(err)
	}
	if x, y := stretched.viewTransform(10, 10).apply(32, 16); x != 10 || y != 10 {
		t.Errorf("none mapping = %v,%v", x, y)
	}
	// Missing width/height derive from the viewBox ratio.
	ratio, err := decodeSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" width="40" viewBox="0 0 4 1"/>`))
	if err != nil {
		t.Fatal(err)
	}
	if ratio.Bounds().Size() != image.Pt(40, 10) {
		t.Errorf("ratio size = %v", ratio.Bounds().Size())
	}
}

func TestSVGRasterizesFills(t *testing.T) {
	src := `<svg xmlns="http://www.w3.org/2000/svg" width="20" height="10" viewBox="0 0 40 20">
		<title>ignored</title>
		<rect width="40" height="20" fill="#00f"/>
		<g fill="red"><path d="M0 0h20v20H0z"/><path d="M30 0h10v10h-10z" fill="none"/></g>
		<path d="M20 10h10v10h-10z"/>
		<g transform="translate(30 10)"><rect width="10" height="10" style="fill: #0f0"/></g>
		<circle cx="5" cy="5" r="5" fill="yellow"/>
	</svg>`
	img, err := decodeSVG([]byte(src))
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		x, y int
		want color.RGBA
	}{
		{2, 2, color.RGBA{255, 0, 0, 255}},  // group fill, scaled by viewBox
		{17, 2, color.RGBA{0, 0, 255, 255}}, // fill="none" leaves the rect beneath
		{12, 7, color.RGBA{0, 0, 0, 255}},   // implicit black fill
		{17, 7, color.RGBA{0, 255, 0, 255}}, // transform + style fill
		{12, 2, color.RGBA{0, 0, 255, 255}}, // unsupported <circle> skipped
	} {
		if got := img.RGBAAt(tc.x, tc.y); got != tc.want {
			t.Errorf("pixel (%d,%d) = %v, want %v", tc.x, tc.y, got, tc.want)
		}
	}
	// Rasterizing at a larger used size stays crisp and keeps proportions.
	big := img.rasterize(40, 20)
	if got := big.RGBAAt(19, 1); got != (color.RGBA{255, 0, 0, 255}) {
		t.Errorf("large raster pixel = %v", got)
	}
	if got := big.RGBAAt(20, 1); got != (color.RGBA{0, 0, 255, 255}) {
		t.Errorf("large raster edge = %v", got)
	}
	if img.rasterize(maxSVGRasterSide+1, 1) != nil || img.rasterize(0, 5) != nil {
		t.Error("raster limits not enforced")
	}
}

func TestSVGFillOpacityInheritanceAndOverride(t *testing.T) {
	for _, tc := range []struct {
		name, childOpacity string
		want               uint8
	}{
		{name: "explicit child overrides", childOpacity: ` fill-opacity=".75"`, want: 191},
		{name: "unspecified child inherits", childOpacity: "", want: 64},
	} {
		t.Run(tc.name, func(t *testing.T) {
			src := `<svg xmlns="http://www.w3.org/2000/svg" width="4" height="4">
				<g fill-opacity=".25">
					<rect width="4" height="4" fill="red"` + tc.childOpacity + `/>
				</g>
			</svg>`
			img, err := decodeSVG([]byte(src))
			if err != nil {
				t.Fatal(err)
			}
			got := img.RGBAAt(2, 2)
			// A partially transparent premultiplied red pixel has equal red
			// and alpha channels; checking both distinguishes the overridden
			// 75% paint from a multiplied 18.75% paint.
			if got != (color.RGBA{tc.want, 0, 0, tc.want}) {
				t.Errorf("pixel = %v, want premultiplied red with alpha %d", got, tc.want)
			}
		})
	}
}

func TestSVGStrokePixels(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		x, y       int
		want       color.RGBA
	}{
		{"stroke only", `<path fill="none" stroke="red" stroke-width="2" d="M1 5H9"/>`, 5, 5, color.RGBA{255, 0, 0, 255}},
		{"butt cap", `<path fill="none" stroke="red" stroke-width="2" d="M2 5H8"/>`, 1, 5, color.RGBA{}},
		{"square cap", `<path fill="none" stroke="red" stroke-width="2" stroke-linecap="square" d="M2 5H8"/>`, 1, 5, color.RGBA{255, 0, 0, 255}},
		{"round cap", `<path fill="none" stroke="red" stroke-width="4" stroke-linecap="round" d="M3 5H7"/>`, 2, 5, color.RGBA{255, 0, 0, 255}},
		{"inherited style override", `<g stroke="blue" stroke-width="4" stroke-opacity=".25"><path fill="none" style="stroke: red; stroke-opacity: .75; stroke-width: 2" d="M1 5H9"/></g>`, 5, 5, color.RGBA{191, 0, 0, 191}},
		{"inherited opacity", `<g stroke="red" stroke-opacity=".25"><path fill="none" stroke-width="2" d="M1 5H9"/></g>`, 5, 5, color.RGBA{64, 0, 0, 64}},
		{"no stroke", `<path fill="none" stroke="none" d="M1 5H9"/>`, 5, 5, color.RGBA{}},
		{"zero width", `<path fill="none" stroke="red" stroke-width="0" d="M1 5H9"/>`, 5, 5, color.RGBA{}},
		{"fill remains", `<rect x="1" y="1" width="8" height="8" fill="blue" stroke="red" stroke-width="2"/>`, 5, 5, color.RGBA{0, 0, 255, 255}},
		{"rect outline", `<rect x="1" y="1" width="8" height="8" fill="none" stroke="red" stroke-width="2"/>`, 1, 5, color.RGBA{255, 0, 0, 255}},
		{"transform scales stroke", `<g transform="scale(2)"><path fill="none" stroke="red" stroke-width="1" d="M1 2.5H4"/></g>`, 5, 5, color.RGBA{255, 0, 0, 255}},
		{"curve stroke", `<path fill="none" stroke="red" stroke-width="2" d="M1 5Q5 5 9 5"/>`, 5, 5, color.RGBA{255, 0, 0, 255}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			src := `<svg xmlns="http://www.w3.org/2000/svg" width="10" height="10">` + tc.body + `</svg>`
			img, err := decodeSVG([]byte(src))
			if err != nil {
				t.Fatal(err)
			}
			if got := img.RGBAAt(tc.x, tc.y); got != tc.want {
				t.Errorf("pixel (%d,%d) = %v; want %v", tc.x, tc.y, got, tc.want)
			}
		})
	}
}

func TestSVGStrokeViewBox(t *testing.T) {
	img, err := decodeSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" width="10" height="10" viewBox="0 0 5 5"><path fill="none" stroke="red" stroke-width="1" d="M1 2.5H4"/></svg>`))
	if err != nil {
		t.Fatal(err)
	}
	if got := img.RGBAAt(5, 5); got != (color.RGBA{255, 0, 0, 255}) {
		t.Errorf("scaled stroke pixel = %v", got)
	}
}

// The fixture is intentionally simple enough to review visually, and checks
// that the before image remains fill-only while the after image adds strokes.
func TestSVGStrokeVisual(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "testdata", "svg", "stroke-demo.svg"))
	if err != nil {
		t.Fatal(err)
	}
	after, err := decodeSVG(data)
	if err != nil {
		t.Fatal(err)
	}
	if got := after.RGBAAt(30, 20); got.A == 0 {
		t.Errorf("stroke-only line invisible: %v", got)
	}
	if dir := os.Getenv("SVG_VISUAL_DIR"); dir != "" {
		before, err := decodeSVG([]byte(strings.ReplaceAll(string(data), "stroke=", "data-stroke=")))
		if err != nil {
			t.Fatal(err)
		}
		for name, img := range map[string]*svgImage{"svg-strokes-before.png": before, "svg-strokes-after.png": after} {
			f, err := os.Create(filepath.Join(dir, name))
			if err != nil {
				t.Fatal(err)
			}
			err = png.Encode(f, img.RGBA)
			closeErr := f.Close()
			if err != nil || closeErr != nil {
				t.Fatalf("write %s: %v, %v", name, err, closeErr)
			}
		}
	}
}

func TestSVGStrokeJoinsAndNoDoubleAlpha(t *testing.T) {
	for _, join := range []string{"miter", "round", "bevel"} {
		src := `<svg xmlns="http://www.w3.org/2000/svg" width="20" height="20"><path fill="none" stroke="red" stroke-opacity=".5" stroke-width="4" stroke-linejoin="` + join + `" d="M3 15L10 5L17 15"/></svg>`
		img, err := decodeSVG([]byte(src))
		if err != nil {
			t.Fatal(err)
		}
		if got := img.RGBAAt(10, 6); got != (color.RGBA{128, 0, 0, 128}) {
			t.Errorf("%s join overlapping strokes = %v, want one 50%% alpha paint", join, got)
		}
	}
	// The miter has a longer apex than the bevel for an acute join.
	render := func(join string) *svgImage {
		img, err := decodeSVG([]byte(`<svg width="20" height="20"><path fill="none" stroke="red" stroke-width="4" stroke-linejoin="` + join + `" d="M3 15L10 5L17 15"/></svg>`))
		if err != nil {
			t.Fatal(err)
		}
		return img
	}
	miter, bevel := render("miter"), render("bevel")
	if miter.RGBAAt(10, 2).A == 0 || bevel.RGBAAt(10, 2).A != 0 {
		t.Errorf("miter/bevel apex pixels = %v/%v", miter.RGBAAt(10, 2), bevel.RGBAAt(10, 2))
	}
	limited, err := decodeSVG([]byte(`<svg width="20" height="20" stroke-miterlimit="1"><path fill="none" stroke="red" stroke-width="4" d="M3 15L10 5L17 15"/></svg>`))
	if err != nil {
		t.Fatal(err)
	}
	if got := limited.RGBAAt(10, 2).A; got != 0 {
		t.Errorf("miter limit should bevel the apex, got alpha %d", got)
	}
}

func TestSVGHackerNewsAssets(t *testing.T) {
	read := func(name string) *svgImage {
		data, err := os.ReadFile(filepath.Join("..", "..", "testdata", "hn", name))
		if err != nil {
			t.Fatal(err)
		}
		decoded, ok := decodeImage(data).(*svgImage)
		if !ok {
			t.Fatalf("%s did not decode as SVG", name)
		}
		return decoded
	}
	logo := read("y18.svg")
	if logo.Bounds().Size() != image.Pt(18, 18) {
		t.Fatalf("logo size = %v", logo.Bounds().Size())
	}
	if got := logo.RGBAAt(1, 1); got != (color.RGBA{255, 102, 0, 255}) {
		t.Errorf("logo background = %v, want #f60", got)
	}
	// The stem covers ~1.5px, so allow anti-aliasing on its edge column.
	if got := logo.RGBAAt(8, 12); got.G < 240 || got.B < 240 {
		t.Errorf("logo Y stem = %v, want (near) white", got)
	}
	arrow := read("triangle.svg").rasterize(10, 10)
	if got := arrow.RGBAAt(5, 7); got != (color.RGBA{153, 153, 153, 255}) {
		t.Errorf("arrow body = %v, want #999", got)
	}
	if got := arrow.RGBAAt(0, 2); got.A != 0 {
		t.Errorf("arrow corner = %v, want transparent", got)
	}
}

func TestSVGFailuresFallBack(t *testing.T) {
	for _, src := range []string{
		`<svg xmlns="http://www.w3.org/2000/svg"><path d="M0 0`,                                  // truncated XML
		`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 0 10"/>`,                           // degenerate viewBox
		`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 a b"/>`,                            // malformed viewBox
		`<svg xmlns="http://www.w3.org/2000/svg" width="100000" height="10"/>`,                   // raster limit
		`<html><svg xmlns="http://www.w3.org/2000/svg"/></html>`,                                 // not an SVG root
		`<svg xmlns="http://www.w3.org/2000/svg">` + strings.Repeat(" ", maxSVGBytes) + `</svg>`, // input limit
	} {
		if got := decodeImage([]byte(src)); got != nil {
			t.Errorf("decodeImage(%.60q) = %T, want nil", src, got)
		}
	}
	many := `<svg xmlns="http://www.w3.org/2000/svg">` + strings.Repeat("<g/>", maxSVGElements+1) + `</svg>`
	if _, err := decodeSVG([]byte(many)); err == nil {
		t.Error("element limit not enforced")
	}
	if !reflect.DeepEqual(parseSVGPath("M0 0 X", 10), []svgSegment{{op: 'M'}}) {
		t.Error("unknown command should stop path parsing")
	}
}

func TestSVGTransformParsing(t *testing.T) {
	m, ok := parseSVGTransform("translate(10,5) scale(2)")
	if !ok {
		t.Fatal("transform rejected")
	}
	if x, y := m.apply(1, 1); x != 12 || y != 7 {
		t.Errorf("translate/scale = %v,%v", x, y)
	}
	m, ok = parseSVGTransform("rotate(90 1 1)")
	if !ok {
		t.Fatal("rotate rejected")
	}
	if x, y := m.apply(2, 1); math.Abs(x-1) > 1e-9 || math.Abs(y-2) > 1e-9 {
		t.Errorf("rotate about center = %v,%v", x, y)
	}
	if _, ok := parseSVGTransform("wobble(3)"); ok {
		t.Error("unknown transform accepted")
	}
}
