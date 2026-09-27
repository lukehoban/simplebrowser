package browser

import (
	"image"
	"image/color"
	"image/png"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
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

func TestSVGShapeLengthsResolveAgainstViewportAndFont(t *testing.T) {
	for _, src := range []string{
		`<svg xmlns="http://www.w3.org/2000/svg" width="120" height="80"><circle cx="50%" cy="50%" r="30%" fill="red"/><rect x="10%" y="10%" width="25%" height="20%" fill="blue"/></svg>`,
		`<svg xmlns="http://www.w3.org/2000/svg" width="120" height="80" viewBox="0 0 60 40"><circle cx="50%" cy="50%" r="30%" fill="red"/><rect x="10%" y="10%" width="25%" height="20%" fill="blue"/></svg>`,
	} {
		img, err := decodeSVG([]byte(src))
		if err != nil {
			t.Fatal(err)
		}
		for _, tc := range []struct {
			x, y int
			want color.RGBA
		}{
			{60, 40, color.RGBA{255, 0, 0, 255}}, // center: r uses normalized diagonal
			{13, 9, color.RGBA{0, 0, 255, 255}},  // rect x/width and y/height use their axes
			{85, 40, color.RGBA{255, 0, 0, 255}}, // within 30% of normalized diagonal
			{92, 40, color.RGBA{}},               // outside that radius
		} {
			if got := img.RGBA.RGBAAt(tc.x, tc.y); got != tc.want {
				t.Errorf("decodeSVG shape lengths pixel (%d,%d) = %v, want %v", tc.x, tc.y, got, tc.want)
			}
		}
	}

	units, err := decodeSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" width="120" height="80"><rect x="1pt" y="1mm" width="2mm" height="2mm" fill="blue"/><g font-size="10"><circle cx="30" cy="30" r="1em" fill="red"/></g><circle cx="80" cy="30" r="1rem" fill="green"/></svg>`))
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		x, y int
		want color.RGBA
	}{
		{2, 4, color.RGBA{0, 0, 255, 255}},   // pt/mm conversions
		{10, 4, color.RGBA{}},                // 2mm extent
		{30, 30, color.RGBA{255, 0, 0, 255}}, // inherited em font size
		{40, 30, color.RGBA{}},               // em radius is 10 user units
		{80, 30, color.RGBA{0, 128, 0, 255}}, // rem uses the initial root size
	} {
		if got := units.RGBA.RGBAAt(tc.x, tc.y); got != tc.want {
			t.Errorf("absolute/font-relative pixel (%d,%d) = %v, want %v", tc.x, tc.y, got, tc.want)
		}
	}

	lengths := svgLengthBasis{horizontal: 120, vertical: 80, diagonal: math.Hypot(120/math.Sqrt2, 80/math.Sqrt2), fontSize: 10, rootFontSize: 16}
	for _, tc := range []struct {
		value string
		axis  svgAxis
		want  float64
	}{
		{"50%", svgHorizontal, 60},
		{"50%", svgVertical, 40},
		{"50%", svgDiagonal, lengths.diagonal / 2},
		{"1em", svgHorizontal, 10},
		{"1rem", svgHorizontal, 16},
	} {
		if got, ok := lengths.length(tc.value, tc.axis); !ok || math.Abs(got-tc.want) > 1e-9 {
			t.Errorf("resolve SVG length %q = %g, %v; want %g", tc.value, got, ok, tc.want)
		}
	}
	if got, ok := svgFontSize("2rem", 10, 16); !ok || got != 32 {
		t.Errorf("resolve SVG font-size rem = %g, %v; want 32, true", got, ok)
	}
	for _, tc := range []struct {
		value string
		want  float64
	}{
		{"1in", 96},
		{"2.54cm", 96},
		{"25.4mm", 96},
		{"101.6Q", 96},
		{"72pt", 96},
		{"6pc", 96},
	} {
		if got, ok := svgLength(tc.value); !ok || math.Abs(got-tc.want) > 1e-9 {
			t.Errorf("absolute SVG length %q = %g, %v; want %g", tc.value, got, ok, tc.want)
		}
	}
	if _, ok := svgLength("1000000in"); ok {
		t.Error("accepted an unbounded absolute SVG length")
	}

	rootRem, err := decodeSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" width="160" height="80" font-size="2rem"><circle cx="40" cy="40" r="1em" fill="red"/><circle cx="120" cy="40" r="1rem" fill="blue"/></svg>`))
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		x, y int
		want color.RGBA
	}{
		{40, 40, color.RGBA{255, 0, 0, 255}},
		{70, 40, color.RGBA{255, 0, 0, 255}}, // root font-size 2rem computes to 32px
		{73, 40, color.RGBA{}},
		{120, 40, color.RGBA{0, 0, 255, 255}}, // 1rem sees the computed root size
		{150, 40, color.RGBA{0, 0, 255, 255}},
		{153, 40, color.RGBA{}},
	} {
		if got := rootRem.RGBA.RGBAAt(tc.x, tc.y); got != tc.want {
			t.Errorf("root-relative SVG font size pixel (%d,%d) = %v, want %v", tc.x, tc.y, got, tc.want)
		}
	}
}

func TestSVGFontMetricGeometry(t *testing.T) {
	for _, tc := range []struct {
		name, family, weight, style, unit string
		size                              float64
	}{
		{"sans ex", "Arial", "", "", "ex", 20},
		{"verdana ch", "Verdana", "", "", "ch", 30},
		{"mono italic bold ex", "Courier", "bold", "italic", "ex", 26},
		{"mono bold ch", "monospace", "700", "", "ch", 20},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ratios := ratiosFor(ComputedStyle{"font-family": tc.family, "font-weight": tc.weight, "font-style": tc.style})
			ratio := ratios.ex
			if tc.unit == "ch" {
				ratio = ratios.ch
			}
			src := `<svg width="100" height="100"><g font-size="` + trimFloat(tc.size) + `" font-family="` + tc.family +
				`" font-weight="` + tc.weight + `" font-style="` + tc.style + `"><circle cx="50" cy="50" r="1` + tc.unit + `"/></g></svg>`
			img, err := decodeSVG([]byte(src))
			if err != nil {
				t.Fatal(err)
			}
			if len(img.shapes) != 1 {
				t.Fatalf("shapes = %d, want one", len(img.shapes))
			}
			got := img.shapes[0].segments[0].pts[0][0] - 50
			if math.Abs(got-tc.size*ratio) > 1e-8 {
				t.Errorf("radius = %.6f, want selected-face metric %.6f", got, tc.size*ratio)
			}
			if got := img.RGBAAt(50, 50); got != (color.RGBA{0, 0, 0, 255}) {
				t.Errorf("center pixel = %v, want opaque circle", got)
			}
		})
	}
	if ratiosFor(ComputedStyle{"font-family": "Verdana"}).ch == ratiosFor(ComputedStyle{"font-family": "Courier"}).ch {
		t.Fatal("fixture font faces must have distinct 0 glyph advances")
	}
}

func TestSVGFontMetricLengthsAndInheritance(t *testing.T) {
	mono := ratiosFor(ComputedStyle{"font-family": "Courier", "font-weight": "bold", "font-style": "italic"})
	verdana := ratiosFor(ComputedStyle{"font-family": "Verdana"})
	src := `<svg width="100" height="100" font-size="20" font-family="Verdana">` +
		`<g font-size="2ex" font-family="Courier" font-style="italic" font-weight="bold">` +
		`<rect x="-1ch" y="1ex" width="2ch" height="1ex"/></g>` +
		`<circle cx="50" cy="50" r="1ch"/></svg>`
	img, err := decodeSVG([]byte(src))
	if err != nil {
		t.Fatal(err)
	}
	if len(img.shapes) != 2 {
		t.Fatalf("shapes = %d, want rect and circle", len(img.shapes))
	}
	childSize := 40 * verdana.ex // font-size uses parent's x-height, not new face
	rect := img.shapes[0].segments[0].pts[0]
	if math.Abs(rect[0]+childSize*mono.ch) > 1e-8 || math.Abs(rect[1]-childSize*mono.ex) > 1e-8 {
		t.Errorf("inherited rect origin = %v, want (%g,%g)", rect, -childSize*mono.ch, childSize*mono.ex)
	}
	if got, want := img.shapes[1].segments[0].pts[0][0], 50+20*verdana.ch; math.Abs(got-want) > 1e-8 {
		t.Errorf("sibling font leaked: radius endpoint %g, want %g", got, want)
	}
	for _, value := range []string{"1e999ex", "-1ex", "1000000000ch", "NaNex", "1exgarbage"} {
		if _, ok := (svgLengthBasis{fontSize: 20, ratios: mono}).length(value, svgHorizontal); ok {
			t.Errorf("unbounded/invalid shape length accepted: %q", value)
		}
	}
	// Root font-size: ch uses the initial face, not the root's new family.
	root, err := decodeSVG([]byte(`<svg width="100" height="100" font-family="Courier" font-size="2ch"><circle cx="50" cy="50" r="1ex"/></svg>`))
	if err != nil {
		t.Fatal(err)
	}
	want := 32 * ratiosFor(nil).ch * ratiosFor(ComputedStyle{"font-family": "Courier"}).ex
	if got := root.shapes[0].segments[0].pts[0][0] - 50; math.Abs(got-want) > 1e-8 {
		t.Errorf("root font-size/new face radius = %g, want %g", got, want)
	}
	// Inline style wins over presentation attributes and flows into local
	// <use> expansion without leaking to sibling shapes.
	reused, err := decodeSVG([]byte(`<svg width="100" height="100"><defs><rect id="bar" width="1ch" height="1ex"/></defs>` +
		`<use href="#bar" x="10" font-family="Verdana" style="font-family: Courier; font-size: 20px"/>` +
		`<rect y="50" width="1ch" height="1ex"/></svg>`))
	if err != nil {
		t.Fatal(err)
	}
	if len(reused.shapes) != 2 {
		t.Fatalf("reused shapes = %d", len(reused.shapes))
	}
	monoRegular := ratiosFor(ComputedStyle{"font-family": "Courier"})
	if got, want := reused.shapes[0].segments[1].pts[0][0], 20*monoRegular.ch; math.Abs(got-want) > 1e-8 {
		t.Errorf("reused width = %g, want %g", got, want)
	}
	if got, want := reused.shapes[1].segments[1].pts[0][0], 16*ratiosFor(nil).ch; math.Abs(got-want) > 1e-8 {
		t.Errorf("sibling width = %g, want %g", got, want)
	}
}

func TestSVGFontMetricVisual(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "testdata", "svg", "font-geometry-demo.svg"))
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
		{54, 38, color.RGBA{17, 102, 170, 255}},
		{160, 38, color.RGBA{238, 85, 17, 255}},
		{266, 38, color.RGBA{51, 153, 102, 255}},
		{40, 80, color.RGBA{17, 102, 170, 255}},
		{90, 4, color.RGBA{255, 255, 255, 255}},
	} {
		if got := img.RGBAAt(p.x, p.y); got != p.want {
			t.Errorf("pixel (%d,%d) = %v, want %v", p.x, p.y, got, p.want)
		}
	}
	if dir := os.Getenv("SVG_FONT_METRIC_VISUAL_DIR"); dir != "" {
		// Before reproduces the old parser's behavior: ex/ch were invalid.
		before := strings.NewReplacer("ex", "unsupported", "ch", "unsupported").Replace(string(data))
		old, err := decodeSVG([]byte(before))
		if err != nil {
			t.Fatal(err)
		}
		for name, render := range map[string]*svgImage{"svg-font-metrics-before.png": old, "svg-font-metrics-after.png": img} {
			f, err := os.Create(filepath.Join(dir, name))
			if err != nil {
				t.Fatal(err)
			}
			encodeErr := png.Encode(f, render.RGBA)
			closeErr := f.Close()
			if encodeErr != nil || closeErr != nil {
				t.Fatalf("%s: %v / %v", name, encodeErr, closeErr)
			}
		}
	}
}

// The paired fixture visual shows percentages, absolute units and inherited
// font-relative units before and after shape-length resolution.
func TestSVGShapeLengthsVisual(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "testdata", "svg", "shape-lengths-demo.svg"))
	if err != nil {
		t.Fatal(err)
	}
	after, err := decodeSVG(data)
	if err != nil {
		t.Fatal(err)
	}
	if path := os.Getenv("SVG_SHAPE_LENGTHS_VISUAL_DIR"); path != "" {
		before := image.NewRGBA(after.Bounds())
		for name, img := range map[string]image.Image{"svg-shape-lengths-before.png": before, "svg-shape-lengths-after.png": after.RGBA} {
			f, err := os.Create(filepath.Join(path, name))
			if err != nil {
				t.Fatal(err)
			}
			err = png.Encode(f, img)
			closeErr := f.Close()
			if err != nil || closeErr != nil {
				t.Fatalf("write %s: %v, %v", name, err, closeErr)
			}
		}
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

func TestSVGStrokeDashArray(t *testing.T) {
	render := func(body string) *svgImage {
		t.Helper()
		img, err := decodeSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" width="40" height="12">` + body + `</svg>`))
		if err != nil {
			t.Fatal(err)
		}
		return img
	}
	red := color.RGBA{A: 255}
	clear := color.RGBA{}
	for _, tc := range []struct {
		name, attrs string
		x           int
		want        color.RGBA
	}{
		{"issue repro 4/4 dash", `stroke-dasharray="4 4"`, 6, clear},
		{"dash start", `stroke-dasharray="4 4"`, 2, red},
		{"positive offset starts in gap", `stroke-dasharray="4 4" stroke-dashoffset="4"`, 1, clear},
		{"positive offset advances to next dash", `stroke-dasharray="4 4" stroke-dashoffset="4"`, 5, red},
		{"negative offset starts later in dash", `stroke-dasharray="4 4" stroke-dashoffset="-2"`, 1, red},
		{"negative offset moves gap earlier", `stroke-dasharray="4 4" stroke-dashoffset="-2"`, 3, clear},
		{"style overrides dash array attribute", `stroke-dasharray="4 4" style="stroke-dasharray: 8 2"`, 6, red},
		{"style overrides dash offset attribute", `stroke-dasharray="4 4" stroke-dashoffset="0" style="stroke-dashoffset: 4"`, 5, red},
		{"odd array repeats", `stroke-dasharray="3"`, 4, clear},
		{"zero total means solid", `stroke-dasharray="0 0"`, 6, red},
		{"zero gap is skipped", `stroke-dasharray="4 0 4 4"`, 6, red},
		{"none means solid", `stroke-dasharray="none"`, 6, red},
		{"negative value is ignored", `stroke-dasharray="-4 4"`, 6, red},
		{"percentage dash lengths", `stroke-dasharray="10% 10%"`, 1, red},
		{"percentage gap", `stroke-dasharray="10% 10%"`, 4, clear},
		{"percentage repeats", `stroke-dasharray="10% 10%"`, 7, red},
		{"absolute CSS dash lengths", `stroke-dasharray="0.1in 0.1in"`, 2, red},
		{"absolute CSS gap", `stroke-dasharray="0.1in 0.1in"`, 11, clear},
		{"percentage offset", `stroke-dasharray="20% 20%" stroke-dashoffset="10%"`, 1, clear},
		{"percentage offset advances to dash", `stroke-dasharray="20% 20%" stroke-dashoffset="10%"`, 4, red},
		{"inherited pattern", ``, 6, clear},
		{"invalid child preserves inherited pattern", `stroke-dasharray="-4 4"`, 6, clear},
		{"invalid unit preserves inherited pattern", `stroke-dasharray="4em 4px"`, 6, clear},
		{"none overrides inherited pattern", `stroke-dasharray="none"`, 6, red},
	} {
		t.Run(tc.name, func(t *testing.T) {
			prefix, suffix := "", ""
			if strings.Contains(tc.name, "inherited") || strings.Contains(tc.name, "overrides inherited") {
				prefix, suffix = `<g stroke-dasharray="4px 4px">`, `</g>`
			}
			img := render(prefix + `<path fill="none" stroke="red" ` + tc.attrs + ` d="M0 6H40"/>` + suffix)
			got := img.RGBAAt(tc.x, 6)
			if (got.A == 0) != (tc.want.A == 0) {
				t.Errorf("pixel (%d,6) = %v, want painted=%v", tc.x, got, tc.want.A != 0)
			}
		})
	}

	joined := render(`<path fill="none" stroke="red" stroke-dasharray="7 3" d="M1 2H5V8"/>`)
	if got := joined.RGBAAt(5, 4); got.A == 0 {
		t.Errorf("dash continuing around joined corner = %v, want painted", got)
	}
	if got := joined.RGBAAt(5, 7); got != clear {
		t.Errorf("gap after joined dash = %v, want transparent", got)
	}

	closed := render(`<path fill="none" stroke="red" stroke-dasharray="4 4" stroke-dashoffset="2" d="M2 2H10V10H2Z"/>`)
	if got := closed.RGBAAt(2, 2); got != clear {
		t.Errorf("closed path offset gap at seam = %v, want transparent", got)
	}
	if got := closed.RGBAAt(5, 2); got.A == 0 {
		t.Errorf("closed path dash after seam = %v, want painted", got)
	}
}

func TestSVGStrokeDashPercentUsesViewBoxDiagonal(t *testing.T) {
	img, err := decodeSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" width="40" height="10" viewBox="0 0 100 50"><path fill="none" stroke="red" stroke-width="2" stroke-dasharray="10% 10%" d="M0 25H100"/></svg>`))
	if err != nil {
		t.Fatal(err)
	}
	if got := img.RGBAAt(11, 5); got.A == 0 {
		t.Errorf("percentage dash = %v, want painted", got)
	}
	if got := img.RGBAAt(12, 5); got.A != 0 {
		t.Errorf("percentage gap = %v, want transparent", got)
	}
	if got := img.RGBAAt(14, 5); got.A == 0 {
		t.Errorf("percentage dash after gap = %v, want painted", got)
	}
}

func TestSVGStrokeDashParsingBounds(t *testing.T) {
	for _, tc := range []struct {
		value string
		basis float64
		want  []float64
	}{
		{"1,2,3", 10, []float64{1, 2, 3, 1, 2, 3}},
		{"0 4", 10, []float64{0, 4}},
		{"1e2 3.5", 10, []float64{100, 3.5}},
		{"1in 2.54cm 25.4mm 101.6Q 72pt 6pc", 10, []float64{96, 96, 96, 96, 96, 96}},
		{"10% 5px", 20, []float64{2, 5}},
	} {
		got, ok := parseSVGStrokeDashArray(tc.value, tc.basis)
		if !ok || !reflect.DeepEqual(got, tc.want) {
			t.Errorf("parseSVGStrokeDashArray(%q, %g) = %v, %v; want %v", tc.value, tc.basis, got, ok, tc.want)
		}
	}
	for _, value := range []string{
		"-1 2", "NaN 1", "Inf 1", "1e8 1", "1em 2", ",1 2", "1,,2", "1 2,",
		strings.TrimSpace(strings.Repeat("1 ", maxSVGStrokeDashEntries+1)),
	} {
		if _, ok := parseSVGStrokeDashArray(value, 10); ok {
			t.Errorf("parseSVGStrokeDashArray(%q) accepted an invalid pattern", value)
		}
	}
	if got, ok := parseSVGStrokeDashOffset("-2.5px", 10); !ok || got != -2.5 {
		t.Errorf("parse negative dash offset = %v, %v; want -2.5, true", got, ok)
	}
	if got, ok := parseSVGStrokeDashOffset("-10%", 20); !ok || got != -2 {
		t.Errorf("parse percentage dash offset = %v, %v; want -2, true", got, ok)
	}
	if _, ok := parseSVGStrokeDashOffset("1e8", 10); ok {
		t.Error("accepted an unbounded dash offset")
	}
}

// The paired fixture is a visual regression for alternating dash runs, offsets
// and dash continuity through path corners.
func TestSVGStrokeDashVisual(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "testdata", "svg", "dash-demo.svg"))
	if err != nil {
		t.Fatal(err)
	}
	after, err := decodeSVG(data)
	if err != nil {
		t.Fatal(err)
	}
	if path := os.Getenv("SVG_DASH_VISUAL_DIR"); path != "" {
		withoutDash := regexp.MustCompile(`\s+stroke-dash(?:array|offset)="[^"]*"`).ReplaceAll(data, nil)
		before, err := decodeSVG(withoutDash)
		if err != nil {
			t.Fatal(err)
		}
		for name, img := range map[string]*svgImage{"svg-dashes-before.png": before, "svg-dashes-after.png": after} {
			f, err := os.Create(filepath.Join(path, name))
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

func TestSVGStrokeDashLengthsVisual(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "testdata", "svg", "dash-lengths-demo.svg"))
	if err != nil {
		t.Fatal(err)
	}
	after, err := decodeSVG(data)
	if err != nil {
		t.Fatal(err)
	}
	if path := os.Getenv("SVG_DASH_LENGTHS_VISUAL_DIR"); path != "" {
		withoutDash := regexp.MustCompile(`\s+stroke-dash(?:array|offset)="[^"]*"`).ReplaceAll(data, nil)
		before, err := decodeSVG(withoutDash)
		if err != nil {
			t.Fatal(err)
		}
		for name, img := range map[string]*svgImage{
			"svg-dash-lengths-before.png": before,
			"svg-dash-lengths-after.png":  after,
		} {
			f, err := os.Create(filepath.Join(path, name))
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

// Equivalent simple geometry must take the same antialiasing path regardless
// of fill-rule. This skewed edge differed by up to 19 alpha levels when
// even-odd used the lower-resolution scanline filler.
func TestSVGEquivalentFillRulesHaveSameAntialiasing(t *testing.T) {
	const d = "M2 2.13L18 3.01L18 17L2 17Z"
	nonzero, evenodd := renderSVGFillRulePair(t, d)
	for y := 0; y < 20; y++ {
		for x := 0; x < 20; x++ {
			a, b := nonzero.RGBAAt(x, y).A, evenodd.RGBAAt(x, y).A
			if a != b {
				t.Fatalf("alpha at (%d,%d): nonzero=%d evenodd=%d", x, y, a, b)
			}
		}
	}

	if path := os.Getenv("SVG_FILL_AA_VISUAL_PATH"); path != "" {
		writeSVGFillAACloseup(t, path, nonzero, evenodd)
	}
}

// Complex paths still require parity rasterization. Its denser vertical
// sampling keeps near-horizontal edge coverage within one alpha level of the
// vector rasterizer. The off-canvas second subpath conservatively selects the
// parity path without changing the visible geometry.
func TestSVGFillRuleComplexPathAntialiasing(t *testing.T) {
	const d = "M2 2.13L18 3.01L18 17L2 17Z M-10 -10L-9 -10L-9 -9Z"
	nonzero, evenodd := renderSVGFillRulePair(t, d)
	maxDelta := 0
	for y := 0; y < 20; y++ {
		for x := 0; x < 20; x++ {
			delta := int(nonzero.RGBAAt(x, y).A) - int(evenodd.RGBAAt(x, y).A)
			if delta < 0 {
				delta = -delta
			}
			if delta > maxDelta {
				maxDelta = delta
			}
		}
	}
	if maxDelta > 1 {
		t.Errorf("maximum alpha difference = %d, want <= 1", maxDelta)
	}
}

func renderSVGFillRulePair(t *testing.T, d string) (*svgImage, *svgImage) {
	t.Helper()
	var images [2]*svgImage
	for i, rule := range []string{"nonzero", "evenodd"} {
		src := `<svg xmlns="http://www.w3.org/2000/svg" width="20" height="20"><path fill="black" fill-rule="` + rule + `" d="` + d + `"/></svg>`
		img, err := decodeSVG([]byte(src))
		if err != nil {
			t.Fatalf("%s: %v", rule, err)
		}
		images[i] = img
	}
	return images[0], images[1]
}

// writeSVGFillAACloseup enlarges source pixels without smoothing. The panels
// are non-zero, even-odd, and an amplified red alpha-difference map.
func writeSVGFillAACloseup(t *testing.T, path string, nonzero, evenodd *svgImage) {
	t.Helper()
	const scale, gap = 12, 8
	panel := 20 * scale
	out := image.NewRGBA(image.Rect(0, 0, panel*3+gap*2, panel))
	drawPanel := func(offset int, pixel func(x, y int) color.RGBA) {
		for y := 0; y < 20; y++ {
			for x := 0; x < 20; x++ {
				c := pixel(x, y)
				for yy := 0; yy < scale; yy++ {
					for xx := 0; xx < scale; xx++ {
						out.SetRGBA(offset+x*scale+xx, y*scale+yy, c)
					}
				}
			}
		}
	}
	onWhite := func(img *svgImage) func(int, int) color.RGBA {
		return func(x, y int) color.RGBA {
			alpha := img.RGBAAt(x, y).A
			return color.RGBA{255 - alpha, 255 - alpha, 255 - alpha, 255}
		}
	}
	drawPanel(0, onWhite(nonzero))
	drawPanel(panel+gap, onWhite(evenodd))
	drawPanel((panel+gap)*2, func(x, y int) color.RGBA {
		delta := int(nonzero.RGBAAt(x, y).A) - int(evenodd.RGBAAt(x, y).A)
		if delta < 0 {
			delta = -delta
		}
		amplified := min(255, delta*12)
		return color.RGBA{255, uint8(255 - amplified), uint8(255 - amplified), 255}
	})
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	err = png.Encode(f, out)
	closeErr := f.Close()
	if err != nil || closeErr != nil {
		t.Fatalf("write fill antialiasing close-up: %v, %v", err, closeErr)
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

func TestSVGStylesheetRingVisual(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "testdata", "svg", "style-demo.svg"))
	if err != nil {
		t.Fatal(err)
	}
	// Without <style>, the output is identical to the old renderer, which
	// skipped the element. Keep both renders for a reproducible comparison.
	if path := os.Getenv("SVG_STYLE_BEFORE_VISUAL_PATH"); path != "" {
		before := regexp.MustCompile(`(?s)<style>.*?</style>`).ReplaceAll(data, nil)
		old, err := decodeSVG(before)
		if err != nil {
			t.Fatal(err)
		}
		f, err := os.Create(path)
		if err != nil {
			t.Fatal(err)
		}
		encodeErr := png.Encode(f, old.RGBA)
		closeErr := f.Close()
		if encodeErr != nil || closeErr != nil {
			t.Fatalf("write SVG stylesheet before visual: %v, %v", encodeErr, closeErr)
		}
	}
	img, err := decodeSVG(data)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		x, y int
		want color.RGBA
	}{
		{15, 15, color.RGBA{211, 38, 74, 255}},
		{35, 30, color.RGBA{255, 255, 255, 255}},
		{95, 15, color.RGBA{34, 153, 85, 255}},
		{115, 30, color.RGBA{255, 255, 255, 255}},
	} {
		if got := img.RGBAAt(tc.x, tc.y); got != tc.want {
			t.Errorf("pixel (%d,%d) = %v, want %v", tc.x, tc.y, got, tc.want)
		}
	}
	if path := os.Getenv("SVG_STYLE_VISUAL_PATH"); path != "" {
		f, err := os.Create(path)
		if err != nil {
			t.Fatal(err)
		}
		encodeErr := png.Encode(f, img.RGBA)
		closeErr := f.Close()
		if encodeErr != nil || closeErr != nil {
			t.Fatalf("write SVG stylesheet visual: %v, %v", encodeErr, closeErr)
		}
	}
}

func TestSVGStylesheetCascade(t *testing.T) {
	const svg = `<svg width="100" height="20">
	<style>
	rect { fill: red; stroke: blue; stroke-width: 2 }
	.group > rect.box { fill: green }
	#specific { fill: blue }
	.group rect.box { fill: #008000 !important }
	@media print { rect { fill: black } }
	rect:hover { fill: black }
	</style>
	<g class="group" fill="red"><rect class="box" id="specific" x="2" y="2" width="16" height="16" fill="yellow" style="fill: red; stroke: none"/></g>
	<rect x="22" y="2" width="16" height="16" style="fill: yellow"/>
	<rect x="42" y="2" width="16" height="16" fill="yellow"/>
	<g fill="blue"><rect x="62" y="2" width="16" height="16" style="fill: inherit"/></g>
	<rect x="82" y="2" width="16" height="16" class="box" fill="yellow"/>
	</svg>`
	img, err := decodeSVG([]byte(svg))
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		x    int
		want color.RGBA
	}{
		{10, color.RGBA{0, 128, 0, 255}},   // important beats inline and id
		{30, color.RGBA{255, 255, 0, 255}}, // normal inline beats type
		{50, color.RGBA{255, 0, 0, 255}},   // stylesheet beats presentation
		{70, color.RGBA{0, 0, 255, 255}},   // explicit inherited value
		{90, color.RGBA{255, 0, 0, 255}},   // descendant selector does not leak
	} {
		if got := img.RGBAAt(tc.x, 10); got != tc.want {
			t.Errorf("pixel (%d,10) = %v, want %v", tc.x, got, tc.want)
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

func TestSVGStylesheetFontFaceDrivesFontMetricLengths(t *testing.T) {
	mono := ratiosFor(ComputedStyle{"font-family": "Courier", "font-weight": "bold"})
	src := `<svg width="100" height="100"><style>.m { font-family: Courier; font-weight: bold; font-size: 20px }</style>` +
		`<g class="m" font-family="Verdana"><circle cx="50" cy="50" r="1ch"/></g></svg>`
	img, err := decodeSVG([]byte(src))
	if err != nil {
		t.Fatal(err)
	}
	if len(img.shapes) != 1 {
		t.Fatalf("shapes = %d, want one", len(img.shapes))
	}
	if got, want := img.shapes[0].segments[0].pts[0][0]-50, 20*mono.ch; math.Abs(got-want) > 1e-8 {
		t.Errorf("stylesheet font face radius = %g, want %g", got, want)
	}
}

func TestSVGStrokeWidthFontRelativeUnits(t *testing.T) {
	verdana := ratiosFor(ComputedStyle{"font-family": "Verdana"})
	mono := ratiosFor(ComputedStyle{"font-family": "Courier", "font-weight": "bold", "font-style": "italic"})
	src := `<svg xmlns="http://www.w3.org/2000/svg" width="100" height="100">` +
		`<g font-family="Verdana" font-size="20" stroke="red">` +
		`<line x1="2" y1="15" x2="28" y2="15" stroke-width="1ex"/>` +
		`<line x1="2" y1="15" x2="28" y2="15" stroke-width="1ch"/>` +
		`<g stroke-width="1ex" font-family="Courier" font-weight="bold" font-style="italic"><line x1="2" y1="15" x2="28" y2="15"/></g>` +
		`<g stroke-width="1ch"><line x1="2" y1="15" x2="28" y2="15" font-size="40"/></g>` +
		`<line x1="2" y1="15" x2="28" y2="15" style="stroke-width: 2ex"/>` +
		`<line x1="2" y1="15" x2="28" y2="15" stroke-width="1em"/>` +
		`<line x1="2" y1="15" x2="28" y2="15" stroke-width="10%"/>` +
		`<g stroke-width="3"><line x1="2" y1="15" x2="28" y2="15" stroke-width="1e9ex"/>` +
		`<line x1="2" y1="15" x2="28" y2="15" stroke-width="-1ch"/>` +
		`<line x1="2" y1="15" x2="28" y2="15" stroke-width="1exgarbage"/>` +
		`<line x1="2" y1="15" x2="28" y2="15" stroke-width="inherit"/></g>` +
		`</g><line x1="2" y1="15" x2="28" y2="15" stroke="red"/></svg>`
	img, err := decodeSVG([]byte(src))
	if err != nil {
		t.Fatal(err)
	}
	want := []struct {
		name  string
		width float64
	}{
		{"verdana ex", 20 * verdana.ex},
		{"verdana ch", 20 * verdana.ch},
		// Descendants inherit the computed length, resolved against the
		// face and size of the element that specified it.
		{"inherited ex resolved on group face", 20 * mono.ex},
		{"inherited ch resolved on group size", 20 * verdana.ch},
		{"style ex", 40 * verdana.ex},
		{"em", 20},
		{"percent of normalized diagonal", 10},
		{"out of bounds keeps inherited", 3},
		{"negative keeps inherited", 3},
		{"garbage keeps inherited", 3},
		{"inherit keyword", 3},
		{"default", 1},
	}
	if len(img.shapes) != len(want) {
		t.Fatalf("shapes = %d, want %d", len(img.shapes), len(want))
	}
	for i, w := range want {
		if got := img.shapes[i].width; math.Abs(got-w.width) > 1e-8 {
			t.Errorf("%s: stroke width = %g, want %g", w.name, got, w.width)
		}
	}
}

func TestSVGStrokeFontUnitsVisual(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "testdata", "svg", "stroke-font-units-demo.svg"))
	if err != nil {
		t.Fatal(err)
	}
	img, err := decodeSVG(data)
	if err != nil {
		t.Fatal(err)
	}
	blue, orange, green, white := color.RGBA{17, 102, 170, 255}, color.RGBA{238, 85, 17, 255}, color.RGBA{51, 153, 102, 255}, color.RGBA{255, 255, 255, 255}
	verdanaEx := 20 * ratiosFor(ComputedStyle{"font-family": "Verdana"}).ex
	for _, p := range []struct {
		x, y int
		want color.RGBA
	}{
		{50, 36, blue},
		{50, 36 + int(verdanaEx/2) - 1, blue},
		{50, 36 + int(verdanaEx/2) + 2, white},
		{50, 80, blue},
		{160, 36, orange},
		{160, 80, orange},
		{266, 36, green},
		{266, 80, green},
		{266, 36 - 5, green}, // 30px Arial x-height is well above 10px
	} {
		if got := img.RGBAAt(p.x, p.y); got != p.want {
			t.Errorf("pixel (%d,%d) = %v, want %v", p.x, p.y, got, p.want)
		}
	}
	if dir := os.Getenv("SVG_STROKE_FONT_UNITS_VISUAL_DIR"); dir != "" {
		// Before reproduces the old parser: font-relative stroke widths were
		// invalid, so the default 1px stroke was painted.
		before := strings.NewReplacer("1ex", "1unsupported", "1ch", "1unsupported").Replace(string(data))
		old, err := decodeSVG([]byte(before))
		if err != nil {
			t.Fatal(err)
		}
		for name, render := range map[string]*svgImage{"svg-stroke-font-units-before.png": old, "svg-stroke-font-units-after.png": img} {
			f, err := os.Create(filepath.Join(dir, name))
			if err != nil {
				t.Fatal(err)
			}
			encodeErr := png.Encode(f, render.RGBA)
			closeErr := f.Close()
			if encodeErr != nil || closeErr != nil {
				t.Fatalf("%s: %v / %v", name, encodeErr, closeErr)
			}
		}
	}
}
