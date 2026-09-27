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
	f = math.Round(f*1000) / 1000
	if f == 0 {
		return "0" // ignore negative zero from trigonometric roundoff
	}
	return strconv.FormatFloat(f, 'f', -1, 64)
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
		// Absolute and compact relative arcs, followed by further commands.
		{"M0 0 L1 1 A1 1 0 0 1 2 2 L3 3", "M 0,0;L 1,1;C 1.552,1 2,1.448 2,2;L 3,3;"},
		{"M0 0a1 1 0 011 1l1 1", "M 0,0;C 0.552,0 1,0.448 1,1;L 2,2;"},
		{"M0 0A1 1 0 0 0 1 1", "M 0,0;C 0,0.552 0.448,1 1,1;"},
		{"M0 0a1 1 0 011 1 1 1 0 011 1", "M 0,0;C 0.552,0 1,0.448 1,1;C 1.552,1 2,1.448 2,2;"},
		{"M0 0A2 1 90 0 1 1 2", "M 0,0;C 0.552,0 1,0.895 1,2;"},
		// Zero radius is a line; identical endpoints omit even a large arc.
		{"M2 3a0 5 0 011 2a5 0 0 011 2l1 1", "M 2,3;L 3,5;L 4,7;L 5,8;"},
		{"M2 3A4 5 0 1 1 2 3l1 2", "M 2,3;L 3,5;"},
		{"M0 0A1 1 0 011 1z l2 2", "M 0,0;C 0.552,0 1,0.448 1,1;Z;L 2,2;"},
		// Generated cubics must not supply a reflected control point to S/T.
		{"M0 0A1 1 0 011 1S2 2 3 3", "M 0,0;C 0.552,0 1,0.448 1,1;C 1,1 2,2 3,3;"},
		{"M0 0Q1 2 2 2A1 1 0 012 2T4 4", "M 0,0;Q 1,2 2,2;Q 2,2 4,4;"},
		{"M0 0C1 2 2 3 3 3A1 1 0 013 3S4 4 5 5", "M 0,0;C 1,2 2,3 3,3;C 3,3 4,4 5,5;"},
		// Errors render the path up to the error.
		{"M0 0 L1 1 L2", "M 0,0;L 1,1;"},
		{"M0 0 L1 1 A1 1 0 0 2 2 2 L3 3", "M 0,0;L 1,1;"},
		{"M0 0A1 1 0 -1 0 2 2L3 3", "M 0,0;"},
		{"M0 0A1 1 0 0 +1 2 2L3 3", "M 0,0;"},
		{"M0 0A1 1 0 0.0 1 2 2L3 3", "M 0,0;"},
		{"M0 0A1 1 0 0 1 2", "M 0,0;"},
		{"M0 0R1 1L2 2", "M 0,0;"},
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

func TestSVGArcGeometry(t *testing.T) {
	// Roundoff on either side of a quarter turn must not change subdivision.
	for _, endX := range []float64{-1e-15, 0, 1e-15} {
		segs, ok := svgArc(1, 0, 1, 1, 0, false, true, endX, 1)
		if !ok || len(segs) != 1 || segs[0].pts[2] != [2]float64{endX, 1} {
			t.Errorf("near-quarter arc to (%g,1): %v, valid=%v", endX, segs, ok)
		}
	}
	// A unit circle's four possible arcs between these endpoints have known
	// centers, segment counts and tangent directions.
	for _, tc := range []struct {
		large, sweep bool
		center       [2]float64
		count        int
	}{
		{false, true, [2]float64{0, 0}, 1},
		{false, false, [2]float64{1, 1}, 1},
		{true, true, [2]float64{1, 1}, 3},
		{true, false, [2]float64{0, 0}, 3},
	} {
		segs, ok := svgArc(1, 0, 1, 1, 0, tc.large, tc.sweep, 0, 1)
		if !ok || len(segs) != tc.count {
			t.Fatalf("large=%v sweep=%v: %d segments, valid=%v", tc.large, tc.sweep, len(segs), ok)
		}
		start := [2]float64{1, 0}
		for _, seg := range segs {
			end := seg.pts[2]
			if math.Abs(math.Hypot(end[0]-tc.center[0], end[1]-tc.center[1])-1) > 1e-12 {
				t.Errorf("endpoint %v not on circle centered at %v", end, tc.center)
			}
			rx, ry := start[0]-tc.center[0], start[1]-tc.center[1]
			tx, ty := seg.pts[0][0]-start[0], seg.pts[0][1]-start[1]
			if math.Abs(rx*tx+ry*ty) > 1e-12 || (rx*ty-ry*tx > 0) != tc.sweep {
				t.Errorf("large=%v sweep=%v: incorrect start tangent %v", tc.large, tc.sweep, seg.pts[0])
			}
			start = end
		}
		if start != [2]float64{0, 1} {
			t.Errorf("endpoint drift: %v", start)
		}
	}
	// Arbitrary rotation must rotate the controls as well as the endpoints.
	s, c := math.Sincos(math.Pi / 4)
	segs, ok := svgArc(2*c, 2*s, 2, 1, 45, false, true, -s, c)
	want := [3][2]float64{
		{2*c - svgCircleConstant*s, 2*s + svgCircleConstant*c},
		{2*svgCircleConstant*c - s, 2*svgCircleConstant*s + c},
		{-s, c},
	}
	if !ok || len(segs) != 1 {
		t.Fatalf("rotated quarter ellipse: %v, valid=%v", segs, ok)
	}
	for i, p := range segs[0].pts {
		if math.Hypot(p[0]-want[i][0], p[1]-want[i][1]) > 1e-12 {
			t.Errorf("rotated control %d = %v, want %v", i, p, want[i])
		}
	}
	// Too-small radii scale to reach the endpoints; signs are discarded.
	wantPath := segmentPoints(parseSVGPath("M-10 0A10 20 0 0 1 10 0", 100))
	for _, d := range []string{"M-10 0A1 2 0 0 1 10 0", "M-10 0A-1 -2 0 0 1 10 0"} {
		if got := segmentPoints(parseSVGPath(d, 100)); got != wantPath {
			t.Errorf("radius correction for %q: %s, want %s", d, got, wantPath)
		}
	}
}

func TestSVGArcBounds(t *testing.T) {
	d := "M1 0" + strings.Repeat("A1 1 0 1 1 0 1A1 1 0 1 1 1 0", 100)
	for _, limit := range []int{0, 1, 2, 3, 4, 7, 100} {
		if got := len(parseSVGPath(d, limit)); got != limit {
			t.Errorf("arc segment budget %d: got %d", limit, got)
		}
	}
	// Decode's aggregate budget includes every expanded cubic, not just each A.
	d = "M1 0" + strings.Repeat("A1 1 0 1 1 0 1A1 1 0 1 1 1 0", maxSVGPathSegs/6+1)
	if len(d)+100 >= maxSVGBytes {
		t.Fatal("fixture must fit the byte budget to exercise the segment budget")
	}
	if _, err := decodeSVG([]byte(`<svg width="1" height="1"><path d="` + d + `"/></svg>`)); err == nil {
		t.Error("expanded arcs exceeded document segment budget")
	}
	// Finite input can overflow intermediate arithmetic; stop safely.
	for _, d := range []string{
		"M0 0A1e-300 1e-300 0 0 1 1e300 1e300",
		"M1e308 0a1 1 0 0 1 1e308 1",
	} {
		if got := parseSVGPath(d, 100); len(got) != 1 || got[0].op != 'M' {
			t.Errorf("non-finite arc should leave only moveto, got %v", got)
		}
	}
}

func TestSVGArcPixels(t *testing.T) {
	type pixel struct {
		x, y int
		want color.RGBA
	}
	for _, tc := range []struct {
		name, path string
		pixels     []pixel
	}{
		{"filled semicircle", `<path fill="red" d="M10 20A10 10 0 0 1 30 20Z"/>`, []pixel{
			{20, 15, color.RGBA{255, 0, 0, 255}},
			{20, 25, color.RGBA{}},
			{5, 15, color.RGBA{}},
		}},
		{"stroked semicircle and continuation", `<path fill="none" stroke="blue" stroke-width="4" d="M10 20a10 10 0 0120 0L30 30"/>`, []pixel{
			{20, 10, color.RGBA{0, 0, 255, 255}},
			{20, 20, color.RGBA{}},
			{30, 27, color.RGBA{0, 0, 255, 255}},
			{20, 30, color.RGBA{}},
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			img, err := decodeSVG([]byte(`<svg width="40" height="40">` + tc.path + `</svg>`))
			if err != nil {
				t.Fatal(err)
			}
			for _, p := range tc.pixels {
				if got := img.RGBAAt(p.x, p.y); got != p.want {
					t.Errorf("pixel (%d,%d) = %v, want %v", p.x, p.y, got, p.want)
				}
			}
		})
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
		<defs><rect width="40" height="20" fill="yellow"/></defs>
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
		{12, 2, color.RGBA{0, 0, 255, 255}}, // unsupported <defs> subtree skipped
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

// Set SVG_ARC_VISUAL_PATH to write this fixture's render. The before image was
// captured at main 52f706f, before adding arc support; the after uses this parser.
func TestSVGArcVisual(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "testdata", "svg", "arc-demo.svg"))
	if err != nil {
		t.Fatal(err)
	}
	img, err := decodeSVG(data)
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range []struct {
		x, y int
		want color.RGBA
	}{
		{45, 30, color.RGBA{17, 102, 170, 255}},   // blue arc fill
		{160, 60, color.RGBA{238, 85, 17, 255}},   // orange stroke after the arc
		{205, 55, color.RGBA{204, 221, 238, 255}}, // large-arc fill
	} {
		if got := img.RGBAAt(p.x, p.y); got != p.want {
			t.Errorf("arc fixture pixel (%d,%d) = %v, want %v", p.x, p.y, got, p.want)
		}
	}
	if path := os.Getenv("SVG_ARC_VISUAL_PATH"); path != "" {
		f, err := os.Create(path)
		if err != nil {
			t.Fatal(err)
		}
		err = png.Encode(f, img.RGBA)
		closeErr := f.Close()
		if err != nil || closeErr != nil {
			t.Fatalf("write arc render: %v, %v", err, closeErr)
		}
	}
}

func TestSVGShapesVisual(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "testdata", "svg", "shapes-demo.svg"))
	if err != nil {
		t.Fatal(err)
	}
	img, err := decodeSVG(data)
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range []struct {
		x, y int
		want color.RGBA
	}{
		{35, 50, color.RGBA{17, 102, 170, 255}},   // circle fill
		{35, 26, color.RGBA{102, 68, 85, 255}},    // circle stroke
		{95, 50, color.RGBA{204, 221, 238, 255}},  // ellipse fill
		{140, 50, color.RGBA{238, 85, 17, 255}},   // line stroke
		{175, 70, color.RGBA{255, 221, 136, 255}}, // polyline fill
		{175, 81, color.RGBA{255, 255, 255, 255}}, // polyline stroke stays open
		{215, 50, color.RGBA{238, 85, 17, 255}},   // polygon fill
	} {
		if got := img.RGBAAt(p.x, p.y); got != p.want {
			t.Errorf("shapes fixture pixel (%d,%d) = %v, want %v", p.x, p.y, got, p.want)
		}
	}
	if path := os.Getenv("SVG_SHAPES_VISUAL_PATH"); path != "" {
		f, err := os.Create(path)
		if err != nil {
			t.Fatal(err)
		}
		err = png.Encode(f, img.RGBA)
		closeErr := f.Close()
		if err != nil || closeErr != nil {
			t.Fatalf("write shapes render: %v, %v", err, closeErr)
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

func TestSVGBasicShapeGeometry(t *testing.T) {
	attrs := func(kv ...string) map[string]string {
		m := map[string]string{}
		for i := 0; i+1 < len(kv); i += 2 {
			m[kv[i]] = kv[i+1]
		}
		return m
	}
	for _, tc := range []struct {
		name string
		got  []svgSegment
		want string
	}{
		{"circle", svgEllipse(attrs("cx", "5", "cy", "5"), 4, 4),
			"M 9,5;C 9,7.209 7.209,9 5,9;C 2.791,9 1,7.209 1,5;C 1,2.791 2.791,1 5,1;C 7.209,1 9,2.791 9,5;Z;"},
		{"ellipse", svgEllipseAttrs(attrs("cx", "-1", "rx", "2", "ry", "1")),
			"M 1,0;C 1,0.552 0.105,1 -1,1;C -2.105,1 -3,0.552 -3,0;C -3,-0.552 -2.105,-1 -1,-1;C 0.105,-1 1,-0.552 1,0;Z;"},
		// A missing or auto radius takes the other one.
		{"ellipse auto", svgEllipseAttrs(attrs("rx", "auto", "ry", "1")),
			"M 1,0;C 1,0.552 0.552,1 0,1;C -0.552,1 -1,0.552 -1,0;C -1,-0.552 -0.552,-1 0,-1;C 0.552,-1 1,-0.552 1,0;Z;"},
		{"ellipse missing", svgEllipseAttrs(attrs("rx", "1")),
			"M 1,0;C 1,0.552 0.552,1 0,1;C -0.552,1 -1,0.552 -1,0;C -1,-0.552 -0.552,-1 0,-1;C 0.552,-1 1,-0.552 1,0;Z;"},
		// Zero, negative, invalid and doubly-missing radii disable rendering.
		{"zero radius", svgEllipse(attrs(), 0, 4), ""},
		{"negative rx", svgEllipseAttrs(attrs("rx", "-1", "ry", "2")), ""},
		{"invalid ry", svgEllipseAttrs(attrs("rx", "1", "ry", "x")), ""},
		{"no radii", svgEllipseAttrs(attrs()), ""},
		{"line", svgLine(attrs("x1", "1", "y1", "-2", "x2", "3.5")), "M 1,-2;L 3.5,0;"},
		{"line invalid", svgLine(attrs("x1", "1", "x2", "a")), ""},
		{"polyline", svgPolyline("0,0 10,0 10,10", false, 100), "M 0,0;L 10,0;L 10,10;"},
		{"polygon", svgPolyline("0 0,10 0 10-10", true, 100), "M 0,0;L 10,0;L 10,-10;Z;"},
		// An odd trailing coordinate is dropped; points before an error render.
		{"odd points", svgPolyline("0,0 10,0 10", true, 100), "M 0,0;L 10,0;Z;"},
		{"points error", svgPolyline("0,0 5,5 x 10,10", false, 100), "M 0,0;L 5,5;"},
		{"single point", svgPolyline("3,4", true, 100), ""},
		{"empty points", svgPolyline("", true, 100), ""},
		// The segment budget truncates long lists, including the closing Z.
		{"points limit", svgPolyline("0 0 1 1 2 2 3 3", true, 3), "M 0,0;L 1,1;L 2,2;"},
	} {
		if got := segmentPoints(tc.got); got != tc.want {
			t.Errorf("%s: got %q, want %q", tc.name, got, tc.want)
		}
	}
}

func TestSVGBasicShapePixels(t *testing.T) {
	// The black-disc repro from #82.
	img, err := decodeSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" width="10" height="10"><circle cx="5" cy="5" r="4"/></svg>`))
	if err != nil {
		t.Fatal(err)
	}
	if got := img.RGBAAt(5, 5); got != (color.RGBA{0, 0, 0, 255}) {
		t.Errorf("circle center = %v, want opaque black", got)
	}
	if got := img.RGBAAt(0, 0); got != (color.RGBA{}) {
		t.Errorf("circle corner = %v, want transparent", got)
	}

	src := `<svg xmlns="http://www.w3.org/2000/svg" width="40" height="40" viewBox="0 0 20 20">
		<circle cx="5" cy="5" r="-3" fill="red"/>
		<ellipse cx="15" cy="5" rx="4" ry="2" fill="none" stroke="#00f" stroke-width="1"/>
		<line x1="0" y1="10" x2="20" y2="10" stroke="#0f0" stroke-width="2"/>
		<line x1="0" y1="12" x2="20" y2="12"/>
		<g transform="translate(0 10)" fill="#f00">
			<polygon points="0,4 10,4 10,10 0,10"/>
			<polyline points="12,4 20,4 20,10" fill="none" stroke="#000" stroke-width="1"/>
		</g>
	</svg>`
	img, err = decodeSVG([]byte(src))
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		x, y int
		want color.RGBA
	}{
		{10, 10, color.RGBA{}},               // negative radius disables rendering
		{30, 10, color.RGBA{}},               // stroked ellipse interior stays unfilled
		{37, 10, color.RGBA{0, 0, 255, 255}}, // ellipse stroke at (rx, 0), viewBox-scaled
		{20, 20, color.RGBA{0, 255, 0, 255}}, // line stroke
		{20, 24, color.RGBA{}},               // a line's fill has no area
		{10, 34, color.RGBA{255, 0, 0, 255}}, // transformed polygon fill
		{30, 28, color.RGBA{0, 0, 0, 255}},   // polyline stroke
		{24, 38, color.RGBA{}},               // open polyline stroke is not closed
		{34, 32, color.RGBA{}},               // fill="none" polyline
	} {
		if got := img.RGBAAt(tc.x, tc.y); got != tc.want {
			t.Errorf("pixel (%d,%d) = %v, want %v", tc.x, tc.y, got, tc.want)
		}
	}
}

func TestSVGBasicShapeResourceLimits(t *testing.T) {
	// Every emitted segment counts against the document budget.
	var b strings.Builder
	b.WriteString(`<svg xmlns="http://www.w3.org/2000/svg" width="10" height="10"><polygon points="`)
	for i := 0; i < maxSVGPathSegs; i++ {
		b.WriteString("1 1 ")
	}
	b.WriteString(`"/></svg>`)
	if _, err := decodeSVG([]byte(b.String())); err == nil {
		t.Error("oversized polygon accepted")
	}
	// A polygon just under the budget plus one circle exceeds it.
	b.Reset()
	b.WriteString(`<svg xmlns="http://www.w3.org/2000/svg" width="10" height="10"><polygon points="`)
	for i := 0; i < maxSVGPathSegs-4; i++ {
		b.WriteString("1 1 ")
	}
	b.WriteString(`"/>`)
	underBudget := b.String() + `</svg>`
	if _, err := decodeSVG([]byte(underBudget)); err != nil {
		t.Fatalf("polygon under the budget rejected: %v", err)
	}
	b.WriteString(`<circle r="1"/></svg>`)
	if _, err := decodeSVG([]byte(b.String())); err == nil {
		t.Error("circle segments not counted against the budget")
	}
}

func TestSVGFillRuleParsing(t *testing.T) {
	for _, tc := range []struct{ src, want string }{
		{`<svg xmlns="http://www.w3.org/2000/svg" width="10" height="10"><path d="M0 0L9 0L9 9Z"/></svg>`, "nonzero"},
		{`<svg xmlns="http://www.w3.org/2000/svg" width="10" height="10"><path fill-rule="evenodd" d="M0 0L9 0L9 9Z"/></svg>`, "evenodd"},
		{`<svg xmlns="http://www.w3.org/2000/svg" width="10" height="10"><path style="fill-rule: evenodd" d="M0 0L9 0L9 9Z"/></svg>`, "evenodd"},
		// Unknown values keep the inherited rule.
		{`<svg xmlns="http://www.w3.org/2000/svg" width="10" height="10"><g fill-rule="evenodd"><path fill-rule="bogus" d="M0 0L9 0L9 9Z"/></g></svg>`, "evenodd"},
		// Inherited from an ancestor group, and overridable by a child.
		{`<svg xmlns="http://www.w3.org/2000/svg" width="10" height="10"><g fill-rule="evenodd"><path d="M0 0L9 0L9 9Z"/></g></svg>`, "evenodd"},
		{`<svg xmlns="http://www.w3.org/2000/svg" width="10" height="10"><g fill-rule="evenodd"><path fill-rule="nonzero" d="M0 0L9 0L9 9Z"/></g></svg>`, "nonzero"},
	} {
		img, err := decodeSVG([]byte(tc.src))
		if err != nil {
			t.Fatalf("decode %q: %v", tc.src, err)
		}
		if len(img.shapes) != 1 {
			t.Fatalf("decode %q: got %d shapes, want 1", tc.src, len(img.shapes))
		}
		if got := img.shapes[0].fillRule; got != tc.want {
			t.Errorf("fill-rule for %q = %q, want %q", tc.src, got, tc.want)
		}
	}
}

// A ring drawn as two same-direction subpaths: non-zero fills it solid,
// even-odd leaves the inner square as a hole.
func TestSVGFillRuleRingHole(t *testing.T) {
	const d = "M0 0L40 0L40 40L0 40Z M10 10L30 10L30 30L10 30Z"
	for _, tc := range []struct {
		rule   string
		center color.RGBA
	}{
		{"nonzero", color.RGBA{255, 0, 0, 255}},
		{"evenodd", color.RGBA{0, 0, 0, 0}},
	} {
		src := `<svg xmlns="http://www.w3.org/2000/svg" width="40" height="40"><path fill="red" fill-rule="` + tc.rule + `" d="` + d + `"/></svg>`
		img, err := decodeSVG([]byte(src))
		if err != nil {
			t.Fatalf("%s: %v", tc.rule, err)
		}
		if got := img.RGBAAt(20, 20); got != tc.center {
			t.Errorf("%s center pixel = %v, want %v", tc.rule, got, tc.center)
		}
		// The ring band itself is filled under both rules.
		if got := img.RGBAAt(5, 20); got != (color.RGBA{255, 0, 0, 255}) {
			t.Errorf("%s band pixel = %v, want opaque red", tc.rule, got)
		}
		if got := img.RGBAAt(38, 5); got != (color.RGBA{255, 0, 0, 255}) {
			t.Errorf("%s corner pixel = %v, want opaque red", tc.rule, got)
		}
	}
}

// A self-intersecting star: even-odd hollows the pentagon in the middle.
func TestSVGFillRuleStar(t *testing.T) {
	const d = "M50 4L63 84L4 34L96 34L37 84Z"
	for _, tc := range []struct {
		rule   string
		center color.RGBA
	}{
		{"nonzero", color.RGBA{0, 0, 255, 255}},
		{"evenodd", color.RGBA{0, 0, 0, 0}},
	} {
		src := `<svg xmlns="http://www.w3.org/2000/svg" width="100" height="100"><path fill="blue" fill-rule="` + tc.rule + `" d="` + d + `"/></svg>`
		img, err := decodeSVG([]byte(src))
		if err != nil {
			t.Fatalf("%s: %v", tc.rule, err)
		}
		if got := img.RGBAAt(50, 45); got != tc.center {
			t.Errorf("%s star center = %v, want %v", tc.rule, got, tc.center)
		}
		// A point on the upper arm is inside under both rules.
		if got := img.RGBAAt(50, 20); got != (color.RGBA{0, 0, 255, 255}) {
			t.Errorf("%s star arm = %v, want opaque blue", tc.rule, got)
		}
		// Well outside the star stays transparent.
		if got := img.RGBAAt(2, 98); got != (color.RGBA{}) {
			t.Errorf("%s outside = %v, want transparent", tc.rule, got)
		}
	}
}

// Even-odd fills antialias their edges rather than snapping to whole pixels.
func TestSVGFillRuleAntialiasedEdge(t *testing.T) {
	src := `<svg xmlns="http://www.w3.org/2000/svg" width="20" height="20"><path fill="black" fill-rule="evenodd" d="M2 2L17.5 2L17.5 18L2 18Z"/></svg>`
	img, err := decodeSVG([]byte(src))
	if err != nil {
		t.Fatal(err)
	}
	if got := img.RGBAAt(17, 10).A; got < 100 || got > 160 {
		t.Errorf("half-covered edge pixel alpha = %d, want roughly 128", got)
	}
	if got := img.RGBAAt(10, 10).A; got != 255 {
		t.Errorf("interior pixel alpha = %d, want 255", got)
	}
}

// Even-odd fill respects fill-opacity and composites once, not twice, where
// subpaths meet.
func TestSVGFillRuleOpacity(t *testing.T) {
	src := `<svg xmlns="http://www.w3.org/2000/svg" width="20" height="20"><path fill="red" fill-opacity="0.5" fill-rule="evenodd" d="M0 0L20 0L20 20L0 20Z M5 5L15 5L15 15L5 15Z"/></svg>`
	img, err := decodeSVG([]byte(src))
	if err != nil {
		t.Fatal(err)
	}
	if got := img.RGBAAt(2, 10); got != (color.RGBA{128, 0, 0, 128}) {
		t.Errorf("band pixel = %v, want half-transparent red", got)
	}
	if got := img.RGBAAt(10, 10); got != (color.RGBA{}) {
		t.Errorf("hole pixel = %v, want transparent", got)
	}
}

// Set SVG_FILL_RULE_VISUAL_PATH to write this fixture's render. The before
// image was captured before fill-rule support, when every fill used non-zero.
func TestSVGFillRuleVisual(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "testdata", "svg", "fill-rule-demo.svg"))
	if err != nil {
		t.Fatal(err)
	}
	img, err := decodeSVG(data)
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range []struct {
		x, y int
		want color.RGBA
		note string
	}{
		{50, 50, color.RGBA{255, 255, 255, 255}, "ring hole"},
		{50, 20, color.RGBA{17, 102, 170, 255}, "ring band"},
		{120, 50, color.RGBA{255, 255, 255, 255}, "even-odd star center"},
		{120, 20, color.RGBA{238, 85, 17, 255}, "even-odd star arm"},
		{186, 50, color.RGBA{102, 68, 85, 255}, "non-zero star center"},
	} {
		if got := img.RGBAAt(p.x, p.y); got != p.want {
			t.Errorf("%s pixel (%d,%d) = %v, want %v", p.note, p.x, p.y, got, p.want)
		}
	}
	if path := os.Getenv("SVG_FILL_RULE_VISUAL_PATH"); path != "" {
		f, err := os.Create(path)
		if err != nil {
			t.Fatal(err)
		}
		err = png.Encode(f, img.RGBA)
		closeErr := f.Close()
		if err != nil || closeErr != nil {
			t.Fatalf("write fill-rule render: %v, %v", err, closeErr)
		}
	}
}

// fill-rule also applies to the basic shapes, e.g. a self-intersecting
// <polygon>.
func TestSVGFillRulePolygon(t *testing.T) {
	const pts = "50,4 63,84 4,34 96,34 37,84"
	for _, tc := range []struct {
		rule   string
		center color.RGBA
	}{
		{"nonzero", color.RGBA{0, 128, 0, 255}},
		{"evenodd", color.RGBA{0, 0, 0, 0}},
	} {
		src := `<svg xmlns="http://www.w3.org/2000/svg" width="100" height="100"><polygon fill="green" fill-rule="` + tc.rule + `" points="` + pts + `"/></svg>`
		img, err := decodeSVG([]byte(src))
		if err != nil {
			t.Fatalf("%s: %v", tc.rule, err)
		}
		if got := img.RGBAAt(50, 45); got != tc.center {
			t.Errorf("%s polygon center = %v, want %v", tc.rule, got, tc.center)
		}
	}
}

func TestSVGUseForwardReferencePaintAndGeometry(t *testing.T) {
	src := `<svg xmlns="http://www.w3.org/2000/svg" xmlns:xlink="http://www.w3.org/1999/xlink" width="60" height="20">
				<use href="#tile" x="2" y="3" fill="red" stroke="blue" stroke-width="2" transform="translate(4 0)"/>
				<use xlink:href="#tile" x="30" fill="green"/>
				<defs><g id="tile" transform="translate(1 0)"><rect width="10" height="10" fill-opacity=".5"/></g></defs>
			</svg>`
	img, err := decodeSVG([]byte(src))
	if err != nil {
		t.Fatal(err)
	}
	if len(img.shapes) != 2 {
		t.Fatalf("got %d shapes, want 2", len(img.shapes))
	}
	if x, y := img.shapes[0].transform.apply(0, 0); x != 7 || y != 3 {
		t.Errorf("use x/y and parent/target transforms = %v,%v, want 7,3", x, y)
	}
	if x, y := img.shapes[1].transform.apply(0, 0); x != 31 || y != 0 {
		t.Errorf("xlink target transform = %v,%v, want 31,0", x, y)
	}
	if got := img.RGBAAt(10, 7); got != (color.RGBA{128, 0, 0, 128}) {
		t.Errorf("inherited fill pixel = %v", got)
	}
	if got := img.RGBAAt(35, 5); got != (color.RGBA{0, 64, 0, 128}) {
		t.Errorf("xlink inherited fill pixel = %v", got)
	}
}

func TestSVGUseOverridesAndCycles(t *testing.T) {
	src := `<svg xmlns="http://www.w3.org/2000/svg" width="24" height="8">
				<defs>
				  <g id="a"><use href="#b"/><rect x="8" width="4" height="8" fill="blue"/></g>
				  <g id="b"><use href="#a"/><rect width="4" height="8" fill="red"/></g>
				</defs>
				<use href="#a"/>
				<use href="#a" x="12" fill="green"/>
			</svg>`
	img, err := decodeSVG([]byte(src))
	if err != nil {
		t.Fatal(err)
	}
	if len(img.shapes) != 4 {
		t.Errorf("cycle should skip cyclic edge, got %d shapes", len(img.shapes))
	}
	for _, tc := range []struct {
		x    int
		want color.RGBA
	}{{1, color.RGBA{255, 0, 0, 255}}, {9, color.RGBA{0, 0, 255, 255}}, {13, color.RGBA{255, 0, 0, 255}}, {21, color.RGBA{0, 0, 255, 255}}} {
		if got := img.RGBAAt(tc.x, 4); got != tc.want {
			t.Errorf("pixel %d = %v, want %v", tc.x, got, tc.want)
		}
	}
}

func TestSVGUseInvalidAndExternalReferences(t *testing.T) {
	src := `<svg xmlns="http://www.w3.org/2000/svg" width="12" height="12">
				<defs><rect id="box" width="12" height="12" fill="red"/></defs>
				<use href="https://example.com/box.svg#box"/>
				<use href="other.svg#box"/>
				<use href="#missing"/>
				<use href="#box" x="invalid"/>
				<use href="#box" transform="unknown(2)"/>
			</svg>`
	img, err := decodeSVG([]byte(src))
	if err != nil {
		t.Fatal(err)
	}
	if len(img.shapes) != 0 || img.RGBAAt(5, 5).A != 0 {
		t.Errorf("malformed/external use produced paint: %v", img.shapes)
	}
}

func TestSVGUseNestedReferencesAndOverrides(t *testing.T) {
	src := `<svg xmlns="http://www.w3.org/2000/svg" xmlns:xlink="http://www.w3.org/1999/xlink" width="18" height="8">
		<defs>
		  <rect id="leaf" width="4" height="8"/>
		  <g id="pair"><use href="#leaf" x="4"/><rect x="12" width="4" height="8" fill="blue"/></g>
		</defs>
		<g transform="translate(2 0)">
		  <use href="#pair" x="-2" y="0" transform="translate(2)" style="fill: red"/>
		</g>
		<use href="" xlink:href="#leaf" fill="green"/>
	</svg>`
	img, err := decodeSVG([]byte(src))
	if err != nil {
		t.Fatal(err)
	}
	if len(img.shapes) != 2 {
		t.Fatalf("nested refs/empty href produced %d shapes, want 2", len(img.shapes))
	}
	if x, y := img.shapes[0].transform.apply(0, 0); x != 6 || y != 0 {
		t.Errorf("nested use translation = %v,%v, want 6,0", x, y)
	}
	if got := img.RGBAAt(7, 4); got != (color.RGBA{255, 0, 0, 255}) {
		t.Errorf("use style inherited by referenced leaf = %v", got)
	}
	if got := img.RGBAAt(15, 4); got != (color.RGBA{0, 0, 255, 255}) {
		t.Errorf("target explicit fill overrides use = %v", got)
	}
}

func TestSVGUseExpansionLimits(t *testing.T) {
	// Ordinary source-tree nesting is not reference recursion and must not
	// consume the use-depth budget.
	deep := `<svg xmlns="http://www.w3.org/2000/svg" width="1" height="1">` +
		strings.Repeat(`<g>`, maxSVGUseDepth+10) + `<rect width="1" height="1"/>` +
		strings.Repeat(`</g>`, maxSVGUseDepth+10) + `</svg>`
	img, err := decodeSVG([]byte(deep))
	if err != nil {
		t.Fatalf("deep non-reference tree: %v", err)
	}
	if got := img.RGBAAt(0, 0); got.A == 0 {
		t.Error("ordinary nesting incorrectly consumed use-depth budget")
	}

	// A short, exponentially expanding graph must not evade the source
	// element cap. It should fail before rasterizing the image.
	var b strings.Builder
	b.WriteString(`<svg xmlns="http://www.w3.org/2000/svg" width="1" height="1"><defs><g id="a0"><rect width="1" height="1"/></g>`)
	for i := 1; i < 15; i++ {
		b.WriteString(`<g id="a` + strconv.Itoa(i) + `"><use href="#a` + strconv.Itoa(i-1) + `"/><use href="#a` + strconv.Itoa(i-1) + `"/></g>`)
	}
	b.WriteString(`</defs><use href="#a14"/></svg>`)
	if _, err := decodeSVG([]byte(b.String())); err == nil {
		t.Error("unbounded fan-out accepted")
	}
	// The budget is for expanded path segments, not just source segments.
	path := "M0 0" + strings.Repeat("L0 0", maxSVGPathSegs/4)
	src := `<svg xmlns="http://www.w3.org/2000/svg" width="1" height="1"><defs><path id="p" d="` + path + `"/></defs>` +
		strings.Repeat(`<use href="#p"/>`, 5) + `</svg>`
	if _, err := decodeSVG([]byte(src)); err == nil {
		t.Error("unbounded expanded segments accepted")
	}
}
