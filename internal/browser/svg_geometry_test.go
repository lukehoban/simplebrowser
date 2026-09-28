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

// Issue #161: CSS math and custom properties resolve against the current SVG
// viewport and font; inherit retains the parent's computed geometry.
func TestSVGComputedGeometry(t *testing.T) {
	for _, tc := range []struct {
		name, source string
		x, y         int
		want         color.RGBA
	}{
		{"mixed calc", `<style>rect { width: calc(50% + 4px); height: calc(1cm - 17.795px) }</style><rect/>`, 23, 5, geomRed},
		{"mixed calc boundary", `<style>rect { width: calc(50% + 4px); height: 20px }</style><rect/>`, 25, 5, geomClear},
		{"axis and font", `<style>circle { cx: calc(10% + 1em); cy: calc(50% - 1ex); r: calc(5px + 5%) }</style><circle font-size="10" />`, 14, 20, geomRed},
		{"group variable", `<style>g { --w: 10px } rect { width: calc(var(--w) + 5px); height: 10px }</style><g><rect/></g>`, 14, 5, geomRed},
		{"local variable overrides group", `<style>g { --w: 10px } rect { --w: 25px; width: var(--w); height: 10px }</style><g><rect/></g>`, 24, 5, geomRed},
		{"var fallback", `<style>rect { width: var(--missing, 15px); height: 10px }</style><rect/>`, 14, 5, geomRed},
		{"unresolved var unsets not attribute", `<style>rect { width: var(--missing); height: 10px }</style><rect width="20"/>`, 5, 5, geomClear},
		{"invalid substituted value unsets", `<style>rect { --bad: 12; width: var(--bad); height: 10px }</style><rect width="20"/>`, 5, 5, geomClear},
		{"malformed calc falls through", `<style>rect { width: 15px } .a { width: calc(5px +) }</style><rect class="a" height="10"/>`, 14, 5, geomRed},
		{"negative calc unsets", `<style>rect { width: calc(5px - 10px); height: 10px }</style><rect width="20"/>`, 5, 5, geomClear},
		{"inherit parent computed", `<style>svg { width: 40px } rect { width: inherit; height: 10px }</style><rect/>`, 35, 5, geomRed},
		{"inherit not inherited by default", `<style>svg { width: 40px }</style><rect height="10"/>`, 5, 5, geomClear},
		{"inherit initial on group", `<style>rect { width: inherit; height: 10px }</style><g><rect width="20"/></g>`, 5, 5, geomClear},
	} {
		t.Run(tc.name, func(t *testing.T) {
			img, err := decodeSVG([]byte(`<svg width="40" height="40" fill="red">` + tc.source + `</svg>`))
			if err != nil {
				t.Fatal(err)
			}
			if got := img.RGBAAt(tc.x, tc.y); got != tc.want {
				t.Errorf("pixel (%d,%d) = %v, want %v", tc.x, tc.y, got, tc.want)
			}
		})
	}
}

func TestSVGComputedGeometryVisual(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "testdata", "svg", "computed-geometry.svg"))
	if err != nil {
		t.Fatal(err)
	}
	img, err := decodeSVG(data)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		x    int
		want color.RGBA
	}{{32, color.RGBA{211, 38, 74, 255}}, {108, color.RGBA{34, 153, 89, 255}}, {184, color.RGBA{51, 102, 204, 255}}} {
		if got := img.RGBAAt(tc.x, 32); got != tc.want {
			t.Errorf("visual pixel (%d,32) = %v, want %v", tc.x, got, tc.want)
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
		{"inherit root computed width", `rect { width: inherit }`, `<rect width="10" height="10"/>`, 5, 5, geomRed},
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

// Issue #160 repro: the SVG 2 d property shapes an attribute-less path.
func TestSVGStylesheetPathRepro(t *testing.T) {
	img, err := decodeSVG([]byte(`<svg width="40" height="40"><style>path { d: path("M8 8H32V32H8Z"); fill: red }</style><path/></svg>`))
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		x, y int
		want color.RGBA
	}{{20, 20, geomRed}, {9, 9, geomRed}, {30, 30, geomRed}, {6, 20, geomClear}, {33, 20, geomClear}} {
		if got := img.RGBAAt(tc.x, tc.y); got != tc.want {
			t.Errorf("pixel (%d,%d) = %v, want %v", tc.x, tc.y, got, tc.want)
		}
	}
}

func TestSVGPathGeometryCascade(t *testing.T) {
	for _, tc := range []struct {
		name, style, path string
		x, y              int
		want              color.RGBA
	}{
		{"stylesheet beats attribute", `path { d: path("M20 0H30V10H20Z") }`, `<path d="M0 0H10V10H0Z"/>`, 25, 5, geomRed},
		{"attribute position vacated", `path { d: path("M20 0H30V10H20Z") }`, `<path d="M0 0H10V10H0Z"/>`, 5, 5, geomClear},
		{"inline beats stylesheet", `path { d: path("M0 0H10V10H0Z") }`, `<path style="d:path('M20 0H30V10H20Z')"/>`, 25, 5, geomRed},
		{"important beats inline", `path { d: path("M0 0H10V10H0Z") !important }`, `<path style="d:path('M20 0H30V10H20Z')"/>`, 25, 5, geomClear},
		{"specificity", `#a { d: path("M20 0H30V10H20Z") } path { d: path("M0 0H10V10H0Z") }`, `<path id="a"/>`, 25, 5, geomRed},
		{"source order", `path { d: path("M0 0H10V10H0Z") } path { d: path("M20 0H30V10H20Z") }`, `<path/>`, 25, 5, geomRed},
		{"none suppresses attribute", `path { d: none }`, `<path d="M0 0H10V10H0Z"/>`, 5, 5, geomClear},
		{"initial suppresses attribute", `path { d: initial }`, `<path d="M0 0H10V10H0Z"/>`, 5, 5, geomClear},
		{"invalid function falls back to attribute", `path { d: path(M20 0H30V10H20Z) }`, `<path d="M0 0H10V10H0Z"/>`, 5, 5, geomRed},
		{"invalid trailing token falls back", `path { d: path("M20 0H30V10H20Z") red }`, `<path d="M0 0H10V10H0Z"/>`, 5, 5, geomRed},
		{"invalid higher rule falls back to lower", `path { d: path("M0 0H10V10H0Z") } .c { d: bogus }`, `<path class="c"/>`, 5, 5, geomRed},
		{"escaped path data", `path { d: path("M20 0H30V10H20\5a") }`, `<path/>`, 25, 5, geomRed},
	} {
		t.Run(tc.name, func(t *testing.T) {
			img, err := decodeSVG([]byte(`<svg width="40" height="40" fill="red"><style>` + tc.style + `</style>` + tc.path + `</svg>`))
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
		{"height", "calc(1px + 2px)", "calc(1px + 2px)", true},
		{"height", "var(--h)", "var(--h)", true},
		{"height", "inherit", "inherit", true},
		{"height", "1e99px", "1e99px", false},
		{"d", `path("M0 0H1V1Z")`, "M0 0H1V1Z", true},
		{"d", `PATH( 'M0 0H1V1Z' )`, "M0 0H1V1Z", true},
		{"d", "none", "", true},
		{"d", "unset", "", true},
		{"d", `path("M0 0H1V1\5a")`, "M0 0H1V1Z", true},
		{"d", `path(M0 0H1V1Z)`, `path(M0 0H1V1Z)`, false},
		{"d", `path("M0 0") junk`, `path("M0 0") junk`, false},
		{"d", `path("M0 0)`, `path("M0 0)`, false},
	} {
		got, ok := svgGeometryDeclaration(tc.property, tc.value)
		if ok != tc.ok || ok && got != tc.want {
			t.Errorf("svgGeometryDeclaration(%q, %q) = %q, %v; want %q, %v", tc.property, tc.value, got, ok, tc.want, tc.ok)
		}
	}
}

func TestSVGStylesheetPathVisual(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "testdata", "svg", "style-path.svg"))
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
		{80, 30, color.RGBA{34, 153, 85, 255}},
		{132, 30, color.RGBA{}},
	} {
		if got := img.RGBAAt(tc.x, tc.y); got != tc.want {
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
	// "Before" strips d declarations, matching the old renderer. The first
	// two paths disappear and the third falls back to its d attribute.
	before := regexp.MustCompile(`\bd:\s*(?:path\((?:"[^"]*"|'[^']*')\)|none)\s*;?`).ReplaceAll(data, nil)
	write("SVG_PATH_BEFORE_VISUAL_PATH", before)
	write("SVG_PATH_VISUAL_PATH", data)
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
