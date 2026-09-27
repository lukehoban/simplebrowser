package browser

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func decodeSVGString(t *testing.T, src string) *svgImage {
	t.Helper()
	img, err := decodeSVG([]byte(src))
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	return img
}

func near(got, want color.RGBA, tol int) bool {
	d := func(a, b uint8) bool { return int(a)-int(b) <= tol && int(b)-int(a) <= tol }
	return d(got.R, want.R) && d(got.G, want.G) && d(got.B, want.B) && d(got.A, want.A)
}

const svgOpen = `<svg xmlns="http://www.w3.org/2000/svg" xmlns:xlink="http://www.w3.org/1999/xlink" `

// The repro from #84: a red-to-blue horizontal gradient, not solid black.
func TestSVGLinearGradientIssueRepro(t *testing.T) {
	img := decodeSVGString(t, svgOpen+`width="10" height="10"><defs><linearGradient id="g"><stop offset="0" stop-color="red"/><stop offset="1" stop-color="blue"/></linearGradient></defs><rect width="10" height="10" fill="url(#g)"/></svg>`)
	prevR := 256
	for x := 0; x < 10; x++ {
		c := img.RGBAAt(x, 5)
		want := uint8(math.Round(255 * (1 - (float64(x)+0.5)/10)))
		if !near(c, color.RGBA{want, 0, 255 - want, 255}, 1) {
			t.Errorf("x=%d pixel = %v, want red %d blue %d", x, c, want, 255-want)
		}
		if int(c.R) >= prevR {
			t.Errorf("red not decreasing at x=%d", x)
		}
		prevR = int(c.R)
		if c != img.RGBAAt(x, 0) || c != img.RGBAAt(x, 9) {
			t.Errorf("column %d not uniform", x)
		}
	}
}

func TestSVGGradientPixels(t *testing.T) {
	red, blue := color.RGBA{255, 0, 0, 255}, color.RGBA{0, 0, 255, 255}
	for _, tc := range []struct {
		name, defs, body string
		x, y             int
		want             color.RGBA
	}{
		{"vertical vector", `<linearGradient id="g" x2="0" y2="1"><stop stop-color="red"/><stop offset="1" stop-color="blue"/></linearGradient>`,
			`<rect width="20" height="20" fill="url(#g)"/>`, 3, 0, color.RGBA{249, 0, 6, 255}},
		{"percent offsets and pad", `<linearGradient id="g"><stop offset="25%" stop-color="red"/><stop offset="75%" stop-color="blue"/></linearGradient>`,
			`<rect width="20" height="20" fill="url(#g)"/>`, 2, 10, red},
		{"pad end", `<linearGradient id="g"><stop offset="25%" stop-color="red"/><stop offset="75%" stop-color="blue"/></linearGradient>`,
			`<rect width="20" height="20" fill="url(#g)"/>`, 17, 10, blue},
		{"bounding box follows geometry", `<linearGradient id="g"><stop stop-color="red"/><stop offset="1" stop-color="blue"/></linearGradient>`,
			`<rect x="10" width="10" height="20" fill="url(#g)"/>`, 10, 5, color.RGBA{242, 0, 13, 255}},
		{"user space units", `<linearGradient id="g" gradientUnits="userSpaceOnUse" x1="0" x2="20"><stop stop-color="red"/><stop offset="1" stop-color="blue"/></linearGradient>`,
			`<rect x="10" width="10" height="20" fill="url(#g)"/>`, 10, 5, color.RGBA{121, 0, 134, 255}},
		{"user space percent", `<linearGradient id="g" gradientUnits="userSpaceOnUse" x2="50%"><stop stop-color="red"/><stop offset="1" stop-color="blue"/></linearGradient>`,
			`<rect width="20" height="20" fill="url(#g)"/>`, 15, 5, blue},
		{"repeat", `<linearGradient id="g" x2=".5" spreadMethod="repeat"><stop stop-color="red"/><stop offset="1" stop-color="blue"/></linearGradient>`,
			`<rect width="20" height="20" fill="url(#g)"/>`, 10, 5, color.RGBA{242, 0, 13, 255}},
		{"reflect", `<linearGradient id="g" x2=".5" spreadMethod="reflect"><stop stop-color="red"/><stop offset="1" stop-color="blue"/></linearGradient>`,
			`<rect width="20" height="20" fill="url(#g)"/>`, 10, 5, color.RGBA{13, 0, 242, 255}},
		{"gradientTransform", `<linearGradient id="g" gradientTransform="rotate(90)"><stop stop-color="red"/><stop offset="1" stop-color="blue"/></linearGradient>`,
			`<rect width="20" height="20" fill="url(#g)"/>`, 19, 0, color.RGBA{249, 0, 6, 255}},
		{"stop opacity", `<linearGradient id="g"><stop stop-color="red" stop-opacity=".5"/><stop offset="1" stop-color="red" stop-opacity=".5"/></linearGradient>`,
			`<rect width="20" height="20" fill="url(#g)"/>`, 10, 10, color.RGBA{128, 0, 0, 128}},
		{"fill-opacity scales gradient", `<linearGradient id="g"><stop stop-color="blue"/><stop offset="1" stop-color="blue"/></linearGradient>`,
			`<rect width="20" height="20" fill="url(#g)" fill-opacity=".25"/>`, 10, 10, color.RGBA{0, 0, 64, 64}},
		{"stop-color in style", `<linearGradient id="g"><stop style="stop-color: #00ff00"/><stop offset="1" style="stop-color: #00ff00; stop-opacity: 1"/></linearGradient>`,
			`<rect width="20" height="20" fill="url(#g)"/>`, 10, 10, color.RGBA{0, 255, 0, 255}},
		{"non-monotonic offset clamps", `<linearGradient id="g"><stop offset=".6" stop-color="red"/><stop offset=".2" stop-color="blue"/></linearGradient>`,
			`<rect width="20" height="20" fill="url(#g)"/>`, 13, 10, blue},
		{"single stop is solid", `<linearGradient id="g"><stop stop-color="blue"/></linearGradient>`,
			`<rect width="20" height="20" fill="url(#g)"/>`, 1, 1, blue},
		{"no stops paints nothing", `<linearGradient id="g"/>`,
			`<rect width="20" height="20" fill="url(#g)"/>`, 10, 10, color.RGBA{}},
		{"degenerate vector uses last stop", `<linearGradient id="g" x2="0"><stop stop-color="red"/><stop offset="1" stop-color="blue"/></linearGradient>`,
			`<rect width="20" height="20" fill="url(#g)"/>`, 10, 10, blue},
		{"radial centre", `<radialGradient id="g"><stop stop-color="red"/><stop offset="1" stop-color="blue"/></radialGradient>`,
			`<rect width="20" height="20" fill="url(#g)"/>`, 10, 10, color.RGBA{236, 0, 18, 255}},
		{"radial pad corner", `<radialGradient id="g"><stop stop-color="red"/><stop offset="1" stop-color="blue"/></radialGradient>`,
			`<rect width="20" height="20" fill="url(#g)"/>`, 0, 0, blue},
		{"radial focus", `<radialGradient id="g" fx="0.1"><stop stop-color="red"/><stop offset="1" stop-color="blue"/></radialGradient>`,
			`<rect width="20" height="20" fill="url(#g)"/>`, 2, 10, color.RGBA{242, 0, 13, 255}},
		{"gradient stroke", `<linearGradient id="g" gradientUnits="userSpaceOnUse" x1="0" x2="20"><stop stop-color="red"/><stop offset="1" stop-color="blue"/></linearGradient>`,
			`<path d="M0 10H20" stroke="url(#g)" stroke-width="4"/>`, 19, 10, color.RGBA{6, 0, 249, 255}},
		{"bounding-box stroke on zero-height line paints nothing", `<linearGradient id="g"><stop stop-color="red"/><stop offset="1" stop-color="blue"/></linearGradient>`,
			`<path d="M0 10H20" stroke="url(#g)" stroke-width="4"/>`, 10, 10, color.RGBA{}},
		{"inherited paint uses child bounds", `<linearGradient id="g"><stop stop-color="red"/><stop offset="1" stop-color="blue"/></linearGradient>`,
			`<g fill="url(#g)"><rect width="10" height="20"/><rect x="10" width="10" height="20"/></g>`, 10, 5, color.RGBA{242, 0, 13, 255}},
		{"case-sensitive ID", `<linearGradient id="Grad"><stop stop-color="blue"/><stop offset="1" stop-color="blue"/></linearGradient>`,
			`<rect width="20" height="20" fill="url('#Grad')"/>`, 10, 10, blue},
		{"forward reference", ``,
			`<rect width="20" height="20" fill="url(#late)"/><linearGradient id="late"><stop stop-color="blue"/><stop offset="1" stop-color="blue"/></linearGradient>`, 10, 10, blue},
		{"missing reference with fallback", ``,
			`<rect width="20" height="20" fill="url(#missing) red"/>`, 10, 10, red},
		{"missing reference without fallback", ``,
			`<rect width="20" height="20" fill="url(#missing)"/>`, 10, 10, color.RGBA{}},
		{"invalid pattern uses fallback", `<pattern id="p" width="invalid" height="4"><rect width="2" height="2"/></pattern>`,
			`<rect width="20" height="20" fill="url(#p) blue"/>`, 10, 10, blue},
		{"external reference is not fetched", ``,
			`<rect width="20" height="20" fill="url(other.svg#g) red"/>`, 10, 10, red},
		{"reference to non-gradient", `<rect id="r" width="1" height="1"/>`,
			`<rect width="20" height="20" fill="url(#r) none"/>`, 10, 10, color.RGBA{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			img := decodeSVGString(t, svgOpen+`width="20" height="20"><defs>`+tc.defs+`</defs>`+tc.body+`</svg>`)
			if got := img.RGBAAt(tc.x, tc.y); !near(got, tc.want, 2) {
				t.Errorf("pixel (%d,%d) = %v, want %v", tc.x, tc.y, got, tc.want)
			}
		})
	}
}

// href inherits unspecified attributes and stops, but not coordinates from
// a gradient of the other kind; cycles terminate.
func TestSVGGradientHrefInheritance(t *testing.T) {
	src := svgOpen + `width="20" height="20"><defs>
		<linearGradient id="base" x2="0" y2="1" spreadMethod="reflect"><stop stop-color="red"/><stop offset="1" stop-color="blue"/></linearGradient>
		<linearGradient id="child" xlink:href="#base"/>
		<linearGradient id="own" href="#base" x2="1" y2="0"><stop stop-color="#0f0"/><stop offset="1" stop-color="#0f0"/></linearGradient>
		<radialGradient id="radial" href="#base"/>
		<linearGradient id="a" href="#b"><stop stop-color="blue"/><stop offset="1" stop-color="blue"/></linearGradient>
		<linearGradient id="b" href="#a"/>
		</defs>
		<rect width="5" height="20" fill="url(#child)"/>
		<rect x="5" width="5" height="20" fill="url(#own)"/>
		<rect x="10" width="5" height="20" fill="url(#radial)"/>
		<rect x="15" width="5" height="20" fill="url(#b)"/></svg>`
	img := decodeSVGString(t, src)
	for _, p := range []struct {
		x, y int
		want color.RGBA
		note string
	}{
		{2, 0, color.RGBA{249, 0, 6, 255}, "inherited vertical vector"},
		{2, 19, color.RGBA{6, 0, 249, 255}, "inherited stops"},
		{7, 10, color.RGBA{0, 255, 0, 255}, "own stops win"},
		{12, 10, color.RGBA{242, 0, 13, 255}, "radial takes stops but not linear coordinates"},
		{12, 0, color.RGBA{13, 0, 242, 255}, "radial distance from its own centre"},
		{17, 10, color.RGBA{0, 0, 255, 255}, "cycle resolves through the chain"},
	} {
		if got := img.RGBAAt(p.x, p.y); !near(got, p.want, 3) {
			t.Errorf("%s: pixel (%d,%d) = %v, want %v", p.note, p.x, p.y, got, p.want)
		}
	}
}

func TestSVGUserSpacePatternPixels(t *testing.T) {
	img := decodeSVGString(t, svgOpen+`width="8" height="8"><style>#p { width: 4px; height: 4px }</style><defs>
		<pattern id="p" patternUnits="userSpaceOnUse">
			<rect width="2" height="2" fill="red"/>
		</pattern>
	</defs><rect width="8" height="8" fill="url(#p) blue"/></svg>`)
	red := color.RGBA{255, 0, 0, 255}
	for _, point := range [][2]int{{0, 0}, {1, 1}, {4, 0}, {5, 5}} {
		if got := img.RGBAAt(point[0], point[1]); !near(got, red, 1) {
			t.Errorf("pattern square pixel %v = %v, want red", point, got)
		}
	}
	for _, point := range [][2]int{{2, 0}, {0, 2}, {3, 3}, {7, 7}} {
		if got := img.RGBAAt(point[0], point[1]); got.A != 0 {
			t.Errorf("pattern gap pixel %v = %v, want transparent", point, got)
		}
	}
}

func TestSVGPatternGeometryCascadeAndHref(t *testing.T) {
	src := svgOpen + `width="24" height="8"><style>.dot { fill: #00ff00 }</style><defs>
		<pattern id="base" width="4" height="4" patternUnits="userSpaceOnUse">
			<rect class="dot" width="2" height="2"/>
		</pattern>
		<pattern id="shifted" href="#base" patternTransform="translate(2 0)"/>
	</defs>
	<rect width="12" height="8" fill="url(#shifted)"/>
	<rect x="12" width="12" height="8" fill="none" stroke="url(#base)" stroke-width="2"/>
	</svg>`
	img := decodeSVGString(t, src)
	green := color.RGBA{0, 255, 0, 255}
	if got := img.RGBAAt(2, 0); !near(got, green, 1) {
		t.Errorf("href/cascade/transformed tile pixel = %v, want green", got)
	}
	if got := img.RGBAAt(13, 0); !near(got, green, 1) {
		t.Errorf("pattern stroke pixel = %v, want green", got)
	}
}

func TestSVGPatternFallbacksAndBounds(t *testing.T) {
	for _, tc := range []struct {
		name, defs, fill string
		want             color.RGBA
	}{
		{"missing", ``, `url(#missing) red`, color.RGBA{255, 0, 0, 255}},
		{"malformed zero tile", `<pattern id="p" width="0" height="4" patternUnits="userSpaceOnUse"/>`, `url(#p) blue`, color.RGBA{0, 0, 255, 255}},
		{"object bounding box", `<pattern id="p" width=".5" height=".5"><rect width="1" height="1"/></pattern>`, `url(#p) blue`, color.RGBA{0, 0, 0, 255}},
		{"oversized tile", `<pattern id="p" width="5000" height="5000" patternUnits="userSpaceOnUse"><rect width="1" height="1"/></pattern>`, `url(#p) blue`, color.RGBA{0, 0, 255, 255}},
		{"self reference uses fallback", `<pattern id="p" width="4" height="4" patternUnits="userSpaceOnUse"><rect width="4" height="4" fill="url(#p) green"/></pattern>`, `url(#p)`, color.RGBA{0, 128, 0, 255}},
		{"external href does not supply content", `<pattern id="p" href="other.svg#p" width="4" height="4" patternUnits="userSpaceOnUse"/>`, `url(#p)`, color.RGBA{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			img := decodeSVGString(t, svgOpen+`width="8" height="8"><defs>`+tc.defs+`</defs><rect width="8" height="8" fill="`+tc.fill+`"/></svg>`)
			if got := img.RGBAAt(0, 0); !near(got, tc.want, 1) {
				t.Errorf("pixel = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestSVGObjectBoundingBoxPattern(t *testing.T) {
	red := color.RGBA{255, 0, 0, 255}
	for _, tc := range []struct {
		name, geometry, content string
		points                  [][2]int
		empty                   [][2]int
	}{
		{"default geometry, user-space content", "", `<rect width="4" height="4" fill="red"/>`,
			[][2]int{{2, 2}, {10, 2}}, [][2]int{{6, 2}, {22, 2}, {26, 2}}},
		{"bounding box content", `patternContentUnits="objectBoundingBox"`, `<rect width=".25" height=".5" fill="red"/>`,
			[][2]int{{2, 2}, {10, 2}, {21, 2}}, [][2]int{{6, 2}, {22, 2}, {26, 2}}},
		{"transformed tiles", `patternTransform="translate(0.25 0)" patternContentUnits="objectBoundingBox"`, `<rect width=".25" height=".5" fill="red"/>`,
			[][2]int{{6, 2}, {14, 2}, {26, 2}}, [][2]int{{2, 2}, {10, 2}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			src := svgOpen + `width="32" height="16"><defs><pattern id="p" width=".5" height=".5" ` + tc.geometry + `>` +
				tc.content + `</pattern></defs><rect width="16" height="8" fill="url(#p) blue"/>` +
				`<rect x="20" width="8" height="8" fill="url(#p)"/></svg>`
			img := decodeSVGString(t, src)
			for _, xy := range tc.points {
				if got := img.RGBAAt(xy[0], xy[1]); !near(got, red, 1) {
					t.Errorf("painted %v = %v, want red", xy, got)
				}
			}
			for _, xy := range tc.empty {
				if got := img.RGBAAt(xy[0], xy[1]); got.A != 0 {
					t.Errorf("gap %v = %v, want transparent", xy, got)
				}
			}
		})
	}
}

func TestSVGPatternIndependentContentUnitsAndBounds(t *testing.T) {
	red := color.RGBA{255, 0, 0, 255}
	img := decodeSVGString(t, svgOpen+`width="24" height="16"><defs>
		<pattern id="p" patternUnits="userSpaceOnUse" patternContentUnits="objectBoundingBox" width="8" height="8">
			<rect width=".25" height=".5" fill="red"/></pattern>
		<pattern id="q" patternUnits="objectBoundingBox" width="50%" height="100%" patternContentUnits="objectBoundingBox" href="#p"/>
	</defs><rect width="16" height="8" fill="url(#p)"/>
	<rect x="16" width="8" height="8" fill="none" stroke="url(#q)" stroke-width="2"/>
	<path d="M0 12H16" stroke="url(#q) blue" stroke-width="3"/></svg>`)
	if got := img.RGBAAt(2, 2); !near(got, red, 1) {
		t.Errorf("user-space geometry, bbox content = %v", got)
	}
	if got := img.RGBAAt(17, 0); !near(got, red, 1) {
		t.Errorf("bbox pattern stroke = %v", got)
	}
	if got := img.RGBAAt(4, 12); got.A != 0 {
		t.Errorf("degenerate line bounds = %v, want unpainted", got)
	}
}

func TestSVGObjectPatternFallbackAndInvalidGeometry(t *testing.T) {
	for _, tc := range []struct {
		name, pattern, shape string
		want                 color.RGBA
	}{
		{"huge box tile uses fallback", `<pattern id="p" width="500" height="1"><rect width="1" height="1"/></pattern>`,
			`<rect width="16" height="8" fill="url(#p) blue"/>`, color.RGBA{0, 0, 255, 255}},
		{"nonfinite tile uses fallback", `<pattern id="p" width="NaN" height=".5"/>`,
			`<rect width="16" height="8" fill="url(#p) blue"/>`, color.RGBA{0, 0, 255, 255}},
		{"negative tile uses fallback", `<pattern id="p" width="-.5" height=".5"/>`,
			`<rect width="16" height="8" fill="url(#p) blue"/>`, color.RGBA{0, 0, 255, 255}},
		{"invalid units uses fallback", `<pattern id="p" patternContentUnits="bad" width=".5" height=".5"/>`,
			`<rect width="16" height="8" fill="url(#p) blue"/>`, color.RGBA{0, 0, 255, 255}},
		{"degenerate box paints nothing", `<pattern id="p" width=".5" height=".5"><rect width="1" height="1" fill="red"/></pattern>`,
			`<path d="M0 4H16" stroke="url(#p) blue" stroke-width="2"/>`, color.RGBA{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			img := decodeSVGString(t, svgOpen+`width="16" height="8"><defs>`+tc.pattern+`</defs>`+tc.shape+`</svg>`)
			x, y := 4, 4
			if tc.name != "degenerate box paints nothing" {
				y = 2
			}
			if got := img.RGBAAt(x, y); !near(got, tc.want, 1) {
				t.Errorf("pixel = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestSVGObjectPatternHrefCSSAndShapeTransform(t *testing.T) {
	img := decodeSVGString(t, svgOpen+`width="32" height="12">
	<style>#derived { width: 50%; height: 100% } .mark { fill: #00ff00 }</style>
	<defs>
	  <pattern id="base" width="0" height="0" patternUnits="objectBoundingBox">
	    <rect class="mark" width=".25" height="1"/>
	  </pattern>
	  <pattern id="derived" href="#base" patternContentUnits="objectBoundingBox"/>
	</defs>
	<g transform="translate(4 0)">
	  <rect width="16" height="8" fill="url(#derived) blue"/>
	</g></svg>`)
	green := color.RGBA{0, 255, 0, 255}
	for _, xy := range [][2]int{{5, 2}, {13, 2}} {
		if got := img.RGBAAt(xy[0], xy[1]); !near(got, green, 1) {
			t.Errorf("transformed inherited CSS pattern at %v = %v", xy, got)
		}
	}
	if got := img.RGBAAt(9, 2); got.A != 0 {
		t.Errorf("transformed tile gap = %v", got)
	}
}

// Group opacity composites the group once; fill-opacity applies to each
// shape, so overlapping shapes show through each other.
func TestSVGGroupOpacityVersusPerShapeAlpha(t *testing.T) {
	shapes := `<rect width="12" height="20" fill="red"/><rect x="8" width="12" height="20" fill="blue"/>`
	group := decodeSVGString(t, svgOpen+`width="20" height="20"><g opacity=".5">`+shapes+`</g></svg>`)
	perShape := decodeSVGString(t, svgOpen+`width="20" height="20"><g fill-opacity=".5">`+shapes+`</g></svg>`)
	if got := group.RGBAAt(10, 10); !near(got, color.RGBA{0, 0, 128, 128}, 1) {
		t.Errorf("group overlap = %v, want only half-transparent blue", got)
	}
	if got := group.RGBAAt(2, 10); !near(got, color.RGBA{128, 0, 0, 128}, 1) {
		t.Errorf("group red = %v", got)
	}
	if got := perShape.RGBAAt(10, 10); !near(got, color.RGBA{64, 0, 128, 191}, 1) {
		t.Errorf("per-shape overlap = %v, want red showing through blue", got)
	}
	// A shape with both fill and stroke is one group: the stroke's inner
	// half hides the fill instead of blending with it.
	both := decodeSVGString(t, svgOpen+`width="20" height="20"><rect x="4" y="4" width="12" height="12" fill="red" stroke="blue" stroke-width="4" opacity=".5"/></svg>`)
	if got := both.RGBAAt(5, 10); !near(got, color.RGBA{0, 0, 128, 128}, 1) {
		t.Errorf("stroke over fill = %v, want half-transparent blue only", got)
	}
	if got := both.RGBAAt(10, 10); !near(got, color.RGBA{128, 0, 0, 128}, 1) {
		t.Errorf("fill = %v", got)
	}
	// opacity is not inherited: nested opacities multiply once each.
	nested := decodeSVGString(t, svgOpen+`width="20" height="20"><g opacity=".5"><g opacity="50%"><rect width="20" height="20" fill="red" opacity=".5"/></g></g></svg>`)
	if got := nested.RGBAAt(10, 10); !near(got, color.RGBA{32, 0, 0, 32}, 1) {
		t.Errorf("nested = %v, want 12.5%% red", got)
	}
}

func TestSVGOpacityElements(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		want       color.RGBA
	}{
		{"shape", `<rect width="20" height="20" fill="red" opacity=".5"/>`, color.RGBA{128, 0, 0, 128}},
		{"style", `<rect width="20" height="20" style="fill: red; opacity: 0.25"/>`, color.RGBA{64, 0, 0, 64}},
		{"combined with fill-opacity", `<rect width="20" height="20" fill="red" fill-opacity=".5" opacity=".5"/>`, color.RGBA{64, 0, 0, 64}},
		{"zero hides subtree", `<g opacity="0"><rect width="20" height="20" fill="red"/></g>`, color.RGBA{}},
		{"invalid ignored", `<rect width="20" height="20" fill="red" opacity="half"/>`, color.RGBA{255, 0, 0, 255}},
		{"clamped", `<rect width="20" height="20" fill="red" opacity="2"/>`, color.RGBA{255, 0, 0, 255}},
		{"root", `<rect width="20" height="20" fill="red"/>`, color.RGBA{128, 0, 0, 128}},
		{"use", `<defs><g id="t"><rect width="12" height="20" fill="red"/><rect x="8" width="12" height="20" fill="blue"/></g></defs><use href="#t" opacity=".5"/>`, color.RGBA{0, 0, 128, 128}},
		{"gradient in group", `<defs><linearGradient id="g"><stop stop-color="blue"/><stop offset="1" stop-color="blue"/></linearGradient></defs><g opacity=".5"><rect width="20" height="20" fill="red"/><rect width="20" height="20" fill="url(#g)"/></g>`, color.RGBA{0, 0, 128, 128}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := `width="20" height="20">`
			if tc.name == "root" {
				root = `width="20" height="20" opacity=".5">`
			}
			img := decodeSVGString(t, svgOpen+root+tc.body+`</svg>`)
			if got := img.RGBAAt(10, 10); !near(got, tc.want, 1) {
				t.Errorf("pixel = %v, want %v", got, tc.want)
			}
		})
	}
}

// Deep nesting falls back to folded alpha past the layer depth cap, and
// many sibling groups stay within the layer pixel budget.
func TestSVGOpacityResourceBounds(t *testing.T) {
	const depth = 40
	src := svgOpen + `width="20" height="20">` + strings.Repeat(`<g opacity=".9">`, depth) +
		`<rect width="20" height="20" fill="red"/><rect width="20" height="20" fill="red"/>` + strings.Repeat(`</g>`, depth) + `</svg>`
	img := decodeSVGString(t, src)
	want := 255 * math.Pow(.9, depth)
	if got := img.RGBAAt(5, 5); math.Abs(float64(got.A)-want) > 4 {
		t.Errorf("deep nesting alpha = %d, want about %.0f", got.A, want)
	}

	// Many sibling groups each get (and reuse) one layer, and still
	// composite as groups: the blue rect hides the red one inside each.
	var b strings.Builder
	b.WriteString(svgOpen + `width="64" height="64">`)
	for i := 0; i < 500; i++ {
		fmt.Fprintf(&b, `<g opacity=".5"><rect x="%d" width="2" height="2" fill="red"/><rect x="%d" width="2" height="2" fill="blue"/></g>`, i%60, i%60+1)
	}
	b.WriteString(`</svg>`)
	img = decodeSVGString(t, b.String())
	if got := img.RGBAAt(0, 0); got.A == 0 || got.R == 0 {
		t.Errorf("sibling groups pixel = %v", got)
	}
}

func TestParseSVGPaint(t *testing.T) {
	g := &svgGradient{stops: []svgStop{{0, color.NRGBA{255, 0, 0, 255}}, {1, color.NRGBA{0, 0, 255, 255}}}}
	one := &svgGradient{stops: []svgStop{{0, color.NRGBA{0, 255, 0, 255}}}}
	gs := &svgPaintServer{gradient: g}
	oneServer := &svgPaintServer{gradient: one}
	resolve := func(id string) *svgPaintServer {
		switch id {
		case "g":
			return gs
		case "one":
			return oneServer
		}
		return nil
	}
	inherited := svgPaintValue{color: color.NRGBA{1, 2, 3, 255}, ok: true}
	for _, tc := range []struct {
		in   string
		want svgPaintValue
	}{
		{"url(#g)", svgPaintValue{color: color.NRGBA{A: 255}, server: gs, ok: true}},
		{` URL( "#g" ) red`, svgPaintValue{color: color.NRGBA{A: 255}, server: gs, ok: true}},
		{"url(#one)", svgPaintValue{color: color.NRGBA{0, 255, 0, 255}, ok: true}},
		{"url(#nope) #00f", svgPaintValue{color: color.NRGBA{0, 0, 255, 255}, ok: true}},
		{"url(#nope) none", svgPaintValue{}},
		{"url(#nope)", svgPaintValue{}},
		{"url(#nope) url(#g)", svgPaintValue{}},
		{"url(#g", inherited},
		{"inherit", inherited},
		{"none", svgPaintValue{}},
		{"currentColor", svgPaintValue{color: color.NRGBA{9, 8, 7, 255}, ok: true}},
		{"red", svgPaintValue{color: color.NRGBA{255, 0, 0, 255}, ok: true}},
	} {
		if got := parseSVGPaint(tc.in, inherited, color.NRGBA{9, 8, 7, 255}, resolve); got != tc.want {
			t.Errorf("parseSVGPaint(%q) = %+v, want %+v", tc.in, got, tc.want)
		}
	}
}

func TestSVGCurrentColorPaints(t *testing.T) {
	img := decodeSVGString(t, svgOpen+`width="60" height="20" color="red">
		<style>.styled { color: #00ff00; fill: currentColor }</style>
		<rect width="20" height="20" fill="currentColor"/>
		<g color="blue"><rect x="20" width="20" height="20" fill="none" stroke="currentColor" stroke-width="8"/></g>
		<rect class="styled" x="40" width="20" height="20"/>
	</svg>`)
	for _, p := range []struct {
		x    int
		want color.RGBA
		note string
	}{
		{5, color.RGBA{255, 0, 0, 255}, "root color inherited by fill"},
		{21, color.RGBA{0, 0, 255, 255}, "group color inherited by stroke"},
		{50, color.RGBA{0, 255, 0, 255}, "stylesheet color on the same element"},
	} {
		if got := img.RGBAAt(p.x, 10); !near(got, p.want, 1) {
			t.Errorf("%s: pixel = %v, want %v", p.note, got, p.want)
		}
	}
}

func TestSVGCurrentColorGradientStops(t *testing.T) {
	img := decodeSVGString(t, svgOpen+`width="20" height="20">
		<style>#end { color: blue }</style>
		<defs color="red"><linearGradient id="g">
			<stop stop-color="currentColor"/>
			<stop id="end" offset="1" stop-color="currentColor"/>
		</linearGradient></defs>
		<rect width="20" height="20" fill="url(#g)"/>
	</svg>`)
	if got := img.RGBAAt(0, 10); !near(got, color.RGBA{249, 0, 6, 255}, 2) {
		t.Errorf("inherited start stop color = %v", got)
	}
	if got := img.RGBAAt(19, 10); !near(got, color.RGBA{6, 0, 249, 255}, 2) {
		t.Errorf("stylesheet end stop color = %v", got)
	}
}

// Object bounding boxes use curve extrema, not control points, so circles
// and ellipses map gradients onto their visible extent.
func TestSVGSegmentsBounds(t *testing.T) {
	circle := svgEllipse(map[string]string{"cx": "10", "cy": "20"}, 5, 5)
	x0, y0, x1, y1, ok := svgSegmentsBounds(circle)
	if !ok || math.Abs(x0-5) > 1e-9 || math.Abs(y0-15) > 1e-9 || math.Abs(x1-15) > 1e-9 || math.Abs(y1-25) > 1e-9 {
		t.Errorf("circle bounds = %v %v %v %v", x0, y0, x1, y1)
	}
	curve := parseSVGPath("M0 0C0 10 10 10 10 0", 100)
	_, _, _, y1, _ = svgSegmentsBounds(curve)
	if math.Abs(y1-7.5) > 1e-9 {
		t.Errorf("cubic max y = %v, want 7.5", y1)
	}
	quad := parseSVGPath("M0 0Q5 10 10 0", 100)
	if _, _, _, y1, _ = svgSegmentsBounds(quad); math.Abs(y1-5) > 1e-9 {
		t.Errorf("quad max y = %v, want 5", y1)
	}
}

func TestSVGPaintVisual(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "testdata", "svg", "paint-demo.svg"))
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
		{12, 30, color.RGBA{230, 85, 48, 255}, "linear start"},
		{98, 30, color.RGBA{56, 83, 198, 255}, "linear end"},
		{155, 60, color.RGBA{255, 246, 176, 255}, "radial centre"},
		{220, 30, color.RGBA{228, 85, 50, 255}, "reflect peak"},
		{250, 90, color.RGBA{148, 182, 217, 255}, "group overlap shows only blue"},
	} {
		if got := img.RGBAAt(p.x, p.y); !near(got, p.want, 4) {
			t.Errorf("%s pixel (%d,%d) = %v, want %v", p.note, p.x, p.y, got, p.want)
		}
	}
}

// Embedded stylesheets can set stop properties and group opacity.
func TestSVGPaintFromStylesheet(t *testing.T) {
	img := decodeSVGString(t, svgOpen+`width="20" height="20"><style>
		.s { stop-color: #0000ff } #fade { opacity: .5 }
		</style><defs><linearGradient id="g"><stop class="s"/><stop class="s" offset="1"/></linearGradient></defs>
		<g id="fade"><rect width="12" height="20" fill="red"/><rect x="8" width="12" height="20" fill="url(#g)"/></g></svg>`)
	if got := img.RGBAAt(10, 10); !near(got, color.RGBA{0, 0, 128, 128}, 1) {
		t.Errorf("overlap = %v, want half-transparent blue only", got)
	}
}

// The repro from #233: the 10×10 viewBox is fitted into each 20×20 tile.
func TestSVGPatternViewBoxIssueRepro(t *testing.T) {
	img := decodeSVGString(t, svgOpen+`width="40" height="20"><defs><pattern id="p" width="20" height="20" patternUnits="userSpaceOnUse" viewBox="0 0 10 10"><circle cx="5" cy="5" r="5" fill="red"/></pattern></defs><rect width="40" height="20" fill="url(#p) blue"/></svg>`)
	red := color.RGBA{255, 0, 0, 255}
	for _, p := range [][2]int{{10, 10}, {30, 10}, {10, 2}, {17, 10}, {37, 10}} {
		if got := img.RGBAAt(p[0], p[1]); !near(got, red, 1) {
			t.Errorf("circle pixel %v = %v, want red", p, got)
		}
	}
	for _, p := range [][2]int{{1, 1}, {21, 18}, {38, 1}} {
		if got := img.RGBAAt(p[0], p[1]); got.A != 0 {
			t.Errorf("tile corner %v = %v, want transparent", p, got)
		}
	}
}

func TestSVGPatternViewBoxAspectRatio(t *testing.T) {
	// 20×10 tiles; the 10×10 viewBox is a red square with a green top stripe.
	content := `<rect width="10" height="10" fill="red"/><rect width="10" height="2" fill="#00ff00"/>`
	red, green, clear := color.RGBA{255, 0, 0, 255}, color.RGBA{0, 255, 0, 255}, color.RGBA{}
	for _, tc := range []struct {
		name, attrs string
		want        map[[2]int]color.RGBA
	}{
		{"default xMidYMid meet", ``, map[[2]int]color.RGBA{{2, 5}: clear, {10, 5}: red, {10, 0}: green, {17, 5}: clear}},
		{"xMinYMid meet", `preserveAspectRatio="xMinYMid meet"`, map[[2]int]color.RGBA{{2, 5}: red, {12, 5}: clear, {17, 5}: clear}},
		{"xMaxYMax", `preserveAspectRatio="xMaxYMax"`, map[[2]int]color.RGBA{{2, 5}: clear, {17, 5}: red}},
		{"none stretches", `preserveAspectRatio="none"`, map[[2]int]color.RGBA{{1, 5}: red, {18, 5}: red, {18, 0}: green}},
		{"slice crops", `preserveAspectRatio="xMidYMid slice"`, map[[2]int]color.RGBA{{1, 0}: red, {18, 9}: red}},
		{"slice xMinYMin keeps top", `preserveAspectRatio="xMinYMin slice"`, map[[2]int]color.RGBA{{1, 0}: green, {18, 3}: green, {18, 5}: red}},
		{"defer prefix accepted", `preserveAspectRatio="defer xMinYMid"`, map[[2]int]color.RGBA{{2, 5}: red, {17, 5}: clear}},
		{"invalid align uses default", `preserveAspectRatio="xMinYMiddle"`, map[[2]int]color.RGBA{{2, 5}: clear, {10, 5}: red}},
		{"invalid meetOrSlice uses default", `preserveAspectRatio="xMinYMid crop"`, map[[2]int]color.RGBA{{2, 5}: clear, {10, 5}: red}},
		{"viewBox origin offset", `viewBox="5 0 10 10" preserveAspectRatio="none"`, map[[2]int]color.RGBA{{1, 5}: red, {11, 5}: clear}},
		{"patternTransform after viewport", `preserveAspectRatio="xMinYMid" patternTransform="translate(5 0)"`, map[[2]int]color.RGBA{{3, 5}: clear, {7, 5}: red, {13, 5}: red, {17, 5}: clear}},
		{"viewBox overrides content units", `patternContentUnits="objectBoundingBox" preserveAspectRatio="none"`, map[[2]int]color.RGBA{{1, 5}: red}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			attrs := tc.attrs
			if !strings.Contains(attrs, "viewBox") {
				attrs += ` viewBox="0 0 10 10"`
			}
			img := decodeSVGString(t, svgOpen+`width="40" height="10"><defs><pattern id="p" width="20" height="10" patternUnits="userSpaceOnUse" `+attrs+`>`+content+`</pattern></defs><rect width="40" height="10" fill="url(#p) blue"/></svg>`)
			for p, want := range tc.want {
				for _, x := range []int{p[0], p[0] + 20} {
					if got := img.RGBAAt(x, p[1]); !near(got, want, 1) {
						t.Errorf("pixel (%d,%d) = %v, want %v", x, p[1], got, want)
					}
				}
			}
		})
	}
}

func TestSVGPatternViewBoxInheritanceAndErrors(t *testing.T) {
	red, clear, blue := color.RGBA{255, 0, 0, 255}, color.RGBA{}, color.RGBA{0, 0, 255, 255}
	base := `<pattern id="base" width="20" height="10" patternUnits="userSpaceOnUse" viewBox="0 0 10 10" preserveAspectRatio="xMaxYMid"><rect width="10" height="10" fill="red"/></pattern>`
	for _, tc := range []struct {
		name, defs string
		want       map[int]color.RGBA // x at y=5
	}{
		{"viewBox and preserveAspectRatio inherited", base + `<pattern id="p" href="#base"/>`, map[int]color.RGBA{2: clear, 17: red}},
		{"preserveAspectRatio overridden independently", base + `<pattern id="p" href="#base" preserveAspectRatio="xMinYMid"/>`, map[int]color.RGBA{2: red, 17: clear}},
		{"invalid preserveAspectRatio inherits", base + `<pattern id="p" href="#base" preserveAspectRatio="bogus"/>`, map[int]color.RGBA{2: clear, 17: red}},
		{"viewBox overridden", base + `<pattern id="p" href="#base" viewBox="0 0 20 10"/>`, map[int]color.RGBA{2: red, 12: clear}},
		{"malformed viewBox inherits", base + `<pattern id="p" href="#base" viewBox="0 0 10"/>`, map[int]color.RGBA{2: clear, 17: red}},
		{"negative viewBox inherits", base + `<pattern id="p" href="#base" viewBox="0 0 -10 10"/>`, map[int]color.RGBA{2: clear, 17: red}},
		{"zero viewBox disables rendering", base + `<pattern id="p" href="#base" viewBox="0 0 0 10"/>`, map[int]color.RGBA{2: clear, 17: clear}},
		{"malformed viewBox without inheritance is ignored", `<pattern id="p" width="20" height="10" patternUnits="userSpaceOnUse" viewBox="a b c d"><rect width="4" height="10" fill="red"/></pattern>`, map[int]color.RGBA{2: red, 6: clear}},
		{"zero tile still uses fallback", `<pattern id="p" width="0" height="10" patternUnits="userSpaceOnUse" viewBox="0 0 10 10"/>`, map[int]color.RGBA{2: blue}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			img := decodeSVGString(t, svgOpen+`width="40" height="10"><defs>`+tc.defs+`</defs><rect width="40" height="10" fill="url(#p) blue"/></svg>`)
			for x, want := range tc.want {
				if got := img.RGBAAt(x, 5); !near(got, want, 1) {
					t.Errorf("pixel (%d,5) = %v, want %v", x, got, want)
				}
			}
		})
	}
}

// Without a viewBox, pattern content coordinates start at the tile origin (x, y).
func TestSVGPatternContentOriginIsTileOrigin(t *testing.T) {
	img := decodeSVGString(t, svgOpen+`width="8" height="8"><defs><pattern id="p" x="2" y="1" width="4" height="4" patternUnits="userSpaceOnUse"><rect width="2" height="2" fill="red"/></pattern></defs><rect width="8" height="8" fill="url(#p) blue"/></svg>`)
	red := color.RGBA{255, 0, 0, 255}
	for _, p := range [][2]int{{2, 1}, {3, 2}, {6, 5}, {7, 6}} {
		if got := img.RGBAAt(p[0], p[1]); !near(got, red, 1) {
			t.Errorf("tile-relative content pixel %v = %v, want red", p, got)
		}
	}

	for _, p := range [][2]int{{0, 0}, {4, 1}, {2, 3}, {1, 0}} {
		if got := img.RGBAAt(p[0], p[1]); got.A != 0 {
			t.Errorf("gap pixel %v = %v, want transparent", p, got)
		}
	}
}

// A viewBox defines the pattern content coordinate system even when both
// pattern unit attributes request objectBoundingBox. This also exercises an
// offset, non-square painted object's bound tile.
func TestSVGPatternViewBoxWithObjectBoundingBoxGeometry(t *testing.T) {
	img := decodeSVGString(t, svgOpen+`width="36" height="16"><defs>
		<pattern id="p" x=".25" y=".25" width=".5" height=".5"
			patternContentUnits="objectBoundingBox" viewBox="5 10 10 10"
			preserveAspectRatio="none"><rect x="5" y="10" width="5" height="10" fill="red"/></pattern>
	</defs><rect x="20" y="4" width="8" height="4" fill="url(#p)"/></svg>`)
	red := color.RGBA{255, 0, 0, 255}
	for _, xy := range [][2]int{{22, 5}, {23, 5}, {26, 5}} {
		if got := img.RGBAAt(xy[0], xy[1]); !near(got, red, 1) {
			t.Errorf("viewBox object-bounding-box tile at %v = %v, want red", xy, got)
		}
	}
	for _, xy := range [][2]int{{20, 5}, {24, 5}, {25, 5}, {22, 3}} {
		if got := img.RGBAAt(xy[0], xy[1]); got.A != 0 {
			t.Errorf("outside offset tile at %v = %v, want transparent", xy, got)
		}
	}
}

// patternCircleSVG fills the whole canvas with a circle tile, optionally
// under a pattern transform.
func patternCircleSVG(transform string) string {
	return svgOpen + `width="20" height="20" viewBox="0 0 20 20"><defs>
		<pattern id="p" width="10" height="10" patternUnits="userSpaceOnUse" ` + transform + `>
			<circle cx="5" cy="5" r="4" fill="#16a34a"/>
		</pattern>
	</defs><rect width="20" height="20" fill="url(#p) #ffffff"/></svg>`
}

// A magnified pattern should look like its content drawn at the output
// resolution, not like an upscaled low-resolution tile.
func TestSVGPatternTileFollowsDeviceScale(t *testing.T) {
	pattern := decodeSVGString(t, patternCircleSVG(""))
	// The same tile content, drawn directly, is the reference rendering.
	direct := decodeSVGString(t, svgOpen+`width="10" height="10" viewBox="0 0 10 10">
		<circle cx="5" cy="5" r="4" fill="#16a34a"/></svg>`)
	for _, scale := range []int{2, 4, 8} {
		got := pattern.rasterize(20*scale, 20*scale)
		want := direct.rasterize(10*scale, 10*scale)
		if got == nil || want == nil {
			t.Fatalf("scale %d: rasterize returned nil", scale)
		}
		diff, edge := 0, 0
		for y := 0; y < 10*scale; y++ {
			for x := 0; x < 10*scale; x++ {
				g, w := got.RGBAAt(x, y), want.RGBAAt(x, y)
				if w.A != 0 && w.A != 255 {
					edge++
				}
				if !near(g, w, 4) {
					diff++
				}
			}
		}
		if edge < 8*scale {
			t.Errorf("scale %d: reference has %d antialiased pixels, want a smooth edge", scale, edge)
		}
		if diff > 10*scale {
			t.Errorf("scale %d: %d pixels differ from the directly drawn tile, want a crisp tile", scale, diff)
		}
	}
}

// Sampling uses tile fractions, so the tiling phase and period must not move
// when the tile raster resolution changes, including under rotation and
// fractional scales.
func TestSVGPatternPhaseStableUnderScaleAndRotation(t *testing.T) {
	for _, tc := range []struct{ name, transform string }{
		{"plain", ""},
		{"rotated", `patternTransform="rotate(20)"`},
		{"skewed", `patternTransform="rotate(12) scale(1.3 0.8)"`},
	} {
		img := decodeSVGString(t, patternCircleSVG(tc.transform))
		const size = 20
		// A high-resolution render is the reference tiling; every other
		// output size must place the same features in the same cells.
		reference := img.rasterize(size*8, size*8)
		for _, scale := range []float64{1, 1.5, 2, 2.5, 3.75} {
			side := int(math.Round(size * scale))
			got := img.rasterize(side, side)
			if reference == nil || got == nil {
				t.Fatalf("%s: rasterize returned nil", tc.name)
			}
			// Compare coverage in a coarse grid of cells: a phase or period
			// shift moves tile features between cells, resolution alone does
			// not. Coarse renders quantize more, so the tolerance follows the
			// cell size in pixels.
			const cells = 5
			tol := 0.08 + 2/float64(side/cells)
			for cy := 0; cy < cells; cy++ {
				for cx := 0; cx < cells; cx++ {
					want := svgGreenCoverage(reference, cx, cy, cells)
					have := svgGreenCoverage(got, cx, cy, cells)
					if math.Abs(want-have) > tol {
						t.Errorf("%s scale %v: cell (%d,%d) coverage %.2f, want %.2f (tol %.2f)",
							tc.name, scale, cx, cy, have, want, tol)
					}
				}
			}
		}
	}
}

// svgGreenCoverage is the fraction of green pixels in one cell of a cells×cells
// grid over img.
func svgGreenCoverage(img *image.RGBA, cx, cy, cells int) float64 {
	b := img.Bounds()
	x0, x1 := b.Min.X+cx*b.Dx()/cells, b.Min.X+(cx+1)*b.Dx()/cells
	y0, y1 := b.Min.Y+cy*b.Dy()/cells, b.Min.Y+(cy+1)*b.Dy()/cells
	green, total := 0, 0
	for y := y0; y < y1; y++ {
		for x := x0; x < x1; x++ {
			c := img.RGBAAt(x, y)
			if c.G > uint8(int(c.R)+40) && c.G > uint8(int(c.B)+40) {
				green++
			}
			total++
		}
	}
	if total == 0 {
		return 0
	}
	return float64(green) / float64(total)
}

// Past the scaled-tile budgets painting falls back to the best cached tile
// rather than growing without bound, and stays deterministic.
func TestSVGPatternScaledTileBounds(t *testing.T) {
	img := decodeSVGString(t, patternCircleSVG(""))
	// A pattern is painted at many scales; the cache stays bounded.
	var server *svgPaintServer
	for _, shape := range img.shapes {
		if shape.fillServer != nil && shape.fillServer.pattern != nil {
			server = shape.fillServer
		}
	}
	if server == nil {
		t.Fatal("expected a pattern paint server")
	}
	for _, side := range []int{40, 60, 80, 100, 120, 200, 400} {
		if img.rasterize(side, side) == nil {
			t.Fatalf("rasterize(%d) returned nil", side)
		}
	}
	p := server.pattern
	if len(p.tiles.scaled) > maxSVGPatternTiles {
		t.Errorf("cached %d scaled tiles, want at most %d", len(p.tiles.scaled), maxSVGPatternTiles)
	}
	if p.tiles.scaledPixels > maxSVGPatternScaledPixels {
		t.Errorf("cached %d scaled pixels, want at most %d", p.tiles.scaledPixels, maxSVGPatternScaledPixels)
	}
	// Repeat renders at a scale past the budgets are identical.
	first := img.rasterize(900, 900)
	second := img.rasterize(900, 900)
	if first == nil || second == nil {
		t.Fatal("large rasterize returned nil")
	}
	if !bytes.Equal(first.Pix, second.Pix) {
		t.Error("renders past the scaled-tile budget are not deterministic")
	}
	// An enormous tile request never rasterizes past the side limit.
	huge := &svgPattern{x: 0, y: 0, width: 1e6, height: 1e6, tile: image.NewRGBA(image.Rect(0, 0, 2, 2)),
		tiles: &svgPatternTiles{content: img, unitW: 1e6, unitH: 1e6}}
	if got := huge.tileFor(svgAffine{a: 1000, d: 1000}); got != huge.tile {
		t.Error("oversized tile request should fall back to the base tile")
	}
}

// svgMatchesDirect reports how many pixels of got differ from want over
// want's bounds, and how many antialiased edge pixels want has.
func svgMatchesDirect(got, want *image.RGBA) (diff, edge int) {
	b := want.Bounds()
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			g, w := got.RGBAAt(x, y), want.RGBAAt(x, y)
			if w.A != 0 && w.A != 255 {
				edge++
			}
			if !near(g, w, 4) {
				diff++
			}
		}
	}
	return diff, edge
}

// Device-scale tiles compose with #233 viewBox fitting and #231
// objectBoundingBox units: a magnified tile matches its content drawn
// directly at the output resolution.
func TestSVGPatternDeviceScaleWithViewBoxAndObjectBoundingBox(t *testing.T) {
	direct := decodeSVGString(t, svgOpen+`width="10" height="10" viewBox="0 0 10 10">
		<circle cx="5" cy="5" r="4" fill="#16a34a"/></svg>`)
	for _, tc := range []struct{ name, pattern string }{
		{"viewBox", `<pattern id="p" width="10" height="10" patternUnits="userSpaceOnUse" viewBox="0 0 100 100" preserveAspectRatio="xMidYMid meet">
			<circle cx="50" cy="50" r="40" fill="#16a34a"/></pattern>`},
		{"objectBoundingBox", `<pattern id="p" width=".5" height=".5">
			<circle cx="5" cy="5" r="4" fill="#16a34a"/></pattern>`},
		{"objectBoundingBox content", `<pattern id="p" width=".5" height=".5" patternContentUnits="objectBoundingBox">
			<circle cx=".25" cy=".25" r=".2" fill="#16a34a"/></pattern>`},
		{"objectBoundingBox viewBox", `<pattern id="p" width=".5" height=".5" viewBox="0 0 1 1">
			<circle cx=".5" cy=".5" r=".4" fill="#16a34a"/></pattern>`},
	} {
		img := decodeSVGString(t, svgOpen+`width="20" height="20" viewBox="0 0 20 20"><defs>`+tc.pattern+
			`</defs><rect width="20" height="20" fill="url(#p) #ffffff"/></svg>`)
		for _, scale := range []int{1, 4} {
			got := img.rasterize(20*scale, 20*scale)
			want := direct.rasterize(10*scale, 10*scale)
			if got == nil || want == nil {
				t.Fatalf("%s scale %d: rasterize returned nil", tc.name, scale)
			}
			diff, edge := svgMatchesDirect(got, want)
			if scale > 1 && edge < 8*scale {
				t.Errorf("%s scale %d: reference has %d antialiased pixels", tc.name, scale, edge)
			}
			if diff > 10*scale {
				t.Errorf("%s scale %d: %d pixels differ from the directly drawn tile", tc.name, scale, diff)
			}
		}
	}
}

// objectBoundingBox patterns bind one tile per shape, so scaled tiles are
// also bounded across the document.
func TestSVGPatternScaledTilesBoundedPerDocument(t *testing.T) {
	var b strings.Builder
	b.WriteString(svgOpen + `width="256" height="256"><defs><pattern id="p" width="1" height="1">
		<circle cx="1" cy="1" r="1" fill="#16a34a"/></pattern></defs>`)
	// At 16× each 120×120 shape wants a 1920×1920 tile: under the per-tile
	// limit, but five exceed the document budget.
	for i := 0; i < 5; i++ {
		fmt.Fprintf(&b, `<rect x="%d" y="%d" width="120" height="120" fill="url(#p)"/>`, i*30, i*30)
	}
	b.WriteString(`</svg>`)
	img := decodeSVGString(t, b.String())
	var servers []*svgPaintServer
	budgets := map[*svgPatternBudget]bool{}
	for _, shape := range img.shapes {
		if s := shape.fillServer; s != nil && s.pattern != nil && s.pattern.tiles != nil {
			servers = append(servers, s)
			budgets[s.pattern.tiles.budget] = true
		}
	}
	if len(servers) != 5 || len(budgets) != 1 {
		t.Fatalf("got %d bound tiles sharing %d budgets, want 5 sharing 1", len(servers), len(budgets))
	}
	device := svgAffine{a: 16, d: 16}
	var total int64
	scaled := 0
	for _, s := range servers {
		tile := s.pattern.tileFor(s.toLocal.then(device))
		if tile.Bounds().Dx() == 1920 {
			scaled++
		} else if tile != s.pattern.tile {
			t.Errorf("tile past the budget is %v, want the base tile", tile.Bounds())
		}
		total += s.pattern.tiles.scaledPixels
	}
	if scaled != 4 || total > maxSVGPatternScaledDocPixels {
		t.Errorf("scaled %d tiles using %d pixels, want 4 within %d", scaled, total, maxSVGPatternScaledDocPixels)
	}
	// The same requests again resolve to the same cached rasters.
	for _, s := range servers {
		first := s.pattern.tileFor(s.toLocal.then(device))
		if again := s.pattern.tileFor(s.toLocal.then(device)); again != first {
			t.Error("repeated tile request is not deterministic")
		}
	}
}
