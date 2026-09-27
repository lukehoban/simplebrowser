package browser

import (
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"regexp"
	"testing"
)

var (
	geomRed   = color.RGBA{255, 0, 0, 255}
	geomBlue  = color.RGBA{0, 0, 255, 255}
	geomClear = color.RGBA{}
)

// Issue #143 repro: stylesheet geometry sizes and positions an attribute-less rect.
func TestSVGStylesheetGeometryRepro(t *testing.T) {
	img, err := decodeSVG([]byte(`<svg width="40" height="40"><style>rect { x: 8px; y: 8px; width: 24px; height: 24px; fill: red }</style><rect/></svg>`))
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		x, y int
		want color.RGBA
	}{{20, 20, geomRed}, {9, 9, geomRed}, {30, 30, geomRed}, {6, 20, geomClear}, {33, 20, geomClear}, {20, 6, geomClear}} {
		if got := img.RGBAAt(tc.x, tc.y); got != tc.want {
			t.Errorf("pixel (%d,%d) = %v, want %v", tc.x, tc.y, got, tc.want)
		}
	}
}

func TestSVGGeometryCascade(t *testing.T) {
	for _, tc := range []struct {
		name, style, shape string
		x, y               int
		want               color.RGBA
	}{
		{"stylesheet beats attribute", `rect { x: 20px }`, `<rect x="0" y="0" width="10" height="10"/>`, 25, 5, geomRed},
		{"attribute position vacated", `rect { x: 20px }`, `<rect x="0" y="0" width="10" height="10"/>`, 5, 5, geomClear},
		{"inline beats stylesheet", `rect { width: 5px }`, `<rect width="2" height="10" style="width: 30px"/>`, 25, 5, geomRed},
		{"important beats inline", `rect { width: 5px !important }`, `<rect height="10" style="width: 30px"/>`, 25, 5, geomClear},
		{"specificity", `#a { width: 30px } rect { width: 5px }`, `<rect id="a" height="10"/>`, 25, 5, geomRed},
		{"source order", `rect { height: 5px } rect { height: 30px }`, `<rect width="10"/>`, 5, 25, geomRed},
		{"percent of viewport", `rect { width: 50%; height: 50% }`, `<rect/>`, 19, 19, geomRed},
		{"percent upper bound", `rect { width: 50%; height: 50% }`, `<rect/>`, 21, 5, geomClear},
		{"em against font-size", `rect { font-size: 10px; width: 3em; height: 1em }`, `<rect/>`, 25, 5, geomRed},
		{"negative x", `rect { x: -10px }`, `<rect width="20" height="20"/>`, 9, 5, geomRed},
		{"negative x shifts", `rect { x: -10px }`, `<rect width="20" height="20"/>`, 11, 5, geomClear},
		{"unitless CSS invalid, attribute applies", `rect { width: 30 }`, `<rect width="10" height="10"/>`, 25, 5, geomClear},
		{"unitless inline invalid, attribute applies", ``, `<rect width="10" height="10" style="width: 30"/>`, 25, 5, geomClear},
		{"invalid falls back to lower rule", `rect { width: 30px } .c { width: bogus }`, `<rect class="c" height="10"/>`, 25, 5, geomRed},
		{"negative width invalid", `rect { width: -5px }`, `<rect width="10" height="10"/>`, 5, 5, geomRed},
		{"auto width hides rect", `rect { width: auto }`, `<rect width="10" height="10"/>`, 5, 5, geomClear},
		{"initial width hides rect", `rect { width: initial }`, `<rect width="10" height="10"/>`, 5, 5, geomClear},
		{"initial x resets to zero", `rect { x: initial }`, `<rect x="20" width="10" height="10"/>`, 5, 5, geomRed},
		{"inherit unsupported, attribute applies", `rect { width: inherit }`, `<rect width="10" height="10"/>`, 5, 5, geomRed},
		{"unitless zero valid", `rect { x: 0 }`, `<rect x="20" width="10" height="10"/>`, 5, 5, geomRed},
		{"not inherited from group", `g { width: 30px; height: 30px }`, `<g><rect/></g>`, 5, 5, geomClear},
		{"circle", `circle { cx: 20px; cy: 20px; r: 10px }`, `<circle r="1"/>`, 20, 20, geomRed},
		{"circle radius", `circle { cx: 20px; cy: 20px; r: 10px }`, `<circle r="1"/>`, 27, 20, geomRed},
		{"circle ignores rect width", `circle { width: 30px }`, `<circle cx="5" cy="5" r="4"/>`, 20, 5, geomClear},
		{"rect ignores circle r", `rect { r: 30px; cx: 30px }`, `<rect width="10" height="10"/>`, 5, 5, geomRed},
		{"ellipse auto ry", `ellipse { cx: 20px; cy: 20px; rx: 15px; ry: auto }`, `<ellipse ry="1"/>`, 20, 32, geomRed},
		{"ellipse rx", `ellipse { cx: 20px; cy: 20px; rx: 15px; ry: 5px }`, `<ellipse/>`, 33, 20, geomRed},
		{"rect rx rounds corner", `rect { rx: 10px }`, `<rect width="30" height="30"/>`, 1, 1, geomClear},
		{"use x stays attribute-only", `use { x: 20px }`, `<defs><rect id="r" width="10" height="10"/></defs><use href="#r"/>`, 5, 5, geomRed},
	} {
		t.Run(tc.name, func(t *testing.T) {
			src := `<svg width="40" height="40" fill="red"><style>` + tc.style + `</style>` + tc.shape + `</svg>`
			img, err := decodeSVG([]byte(src))
			if err != nil {
				t.Fatal(err)
			}
			if got := img.RGBAAt(tc.x, tc.y); got != tc.want {
				t.Errorf("pixel (%d,%d) = %v, want %v", tc.x, tc.y, got, tc.want)
			}
		})
	}
}

// Presentation properties keep their cascade alongside geometry.
func TestSVGGeometryWithPaintCascade(t *testing.T) {
	img, err := decodeSVG([]byte(`<svg width="40" height="20"><style>.b { fill: blue; x: 20px }</style><rect class="b" width="10" height="10" fill="red"/></svg>`))
	if err != nil {
		t.Fatal(err)
	}
	if got := img.RGBAAt(25, 5); got != geomBlue {
		t.Errorf("pixel = %v, want blue", got)
	}
}

func TestSVGGeometryDeclarationValidation(t *testing.T) {
	for _, tc := range []struct {
		property, value, want string
		ok                    bool
	}{
		{"x", "8px", "8px", true},
		{"x", "-8px", "-8px", true},
		{"x", "10%", "10%", true},
		{"x", "2em", "2em", true},
		{"x", "1ch", "1ch", true},
		{"x", "1in", "1in", true},
		{"x", "8", "8", false},
		{"x", "0", "0", true},
		{"x", "auto", "auto", false},
		{"x", "unset", "0", true},
		{"width", "-1px", "-1px", false},
		{"width", "AUTO", "auto", true},
		{"width", "initial", "auto", true},
		{"r", "auto", "auto", false},
		{"r", "-1px", "-1px", false},
		{"rx", "auto", "auto", true},
		{"height", "calc(1px + 2px)", "calc(1px + 2px)", false},
		{"height", "var(--h)", "var(--h)", false},
		{"height", "inherit", "inherit", false},
		{"height", "1e99px", "1e99px", false},
	} {
		got, ok := svgGeometryDeclaration(tc.property, tc.value)
		if ok != tc.ok || ok && got != tc.want {
			t.Errorf("svgGeometryDeclaration(%q, %q) = %q, %v; want %q, %v", tc.property, tc.value, got, ok, tc.want, tc.ok)
		}
	}
}

func TestSVGStylesheetGeometryVisual(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "testdata", "svg", "style-geometry.svg"))
	if err != nil {
		t.Fatal(err)
	}
	img, err := decodeSVG(data)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		x, y int
		want color.RGBA
	}{
		{28, 30, color.RGBA{211, 38, 74, 255}},
		{9, 11, color.RGBA{255, 255, 255, 0}},
		{80, 30, color.RGBA{34, 153, 85, 255}},
		{132, 30, color.RGBA{51, 102, 204, 255}},
		{132, 44, color.RGBA{}},
	} {
		got := img.RGBAAt(tc.x, tc.y)
		if tc.want.A == 0 {
			if got.A != 0 {
				t.Errorf("pixel (%d,%d) = %v, want transparent", tc.x, tc.y, got)
			}
			continue
		}
		if got != tc.want {
			t.Errorf("pixel (%d,%d) = %v, want %v", tc.x, tc.y, got, tc.want)
		}
	}
	write := func(env string, src []byte) {
		path := os.Getenv(env)
		if path == "" {
			return
		}
		render, err := decodeSVG(src)
		if err != nil {
			t.Fatal(err)
		}
		f, err := os.Create(path)
		if err != nil {
			t.Fatal(err)
		}
		encodeErr := png.Encode(f, render.RGBA)
		closeErr := f.Close()
		if encodeErr != nil || closeErr != nil {
			t.Fatalf("write %s: %v, %v", env, encodeErr, closeErr)
		}
	}
	// "Before" strips geometry declarations, matching the old renderer that
	// ignored them: no shapes are painted.
	before := regexp.MustCompile(`\b(x|y|width|height|rx|ry|cx|cy|r): [^;}]+;?`).ReplaceAll(data, nil)
	write("SVG_GEOMETRY_BEFORE_VISUAL_PATH", before)
	write("SVG_GEOMETRY_VISUAL_PATH", data)
}
