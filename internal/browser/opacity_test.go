package browser

import (
	"image"
	"image/color"
	"testing"
)

var (
	opacityWhite = color.RGBA{255, 255, 255, 255}
	opacityRed   = color.RGBA{255, 0, 0, 255}
	opacityBlue  = color.RGBA{0, 0, 255, 255}
)

// Reduced repro for #352: the live GitHub logged-out header has an
// opacity:0 ::before overlay inside a media range query. When the overlay's
// box grew past the header, painting it opaquely hid the whole first viewport.
// A transparent generated box must paint nothing, while content under it and
// the originating element's own content still paint.
func TestOpacityZeroGeneratedOverlayPaintsNothing(t *testing.T) {
	img := painted(t, `<style>
		body{margin:0}
		header{position:relative;height:20px;color:blue}
		@media (width<=1011.98px){header::before{content:"";opacity:0;position:absolute;
			top:0;left:0;width:100%;height:300px;background-color:#0d1117}}
		.below{height:20px;background:red}
	</style><header><div style="width:10px;height:10px;background:blue"></div></header>
	<div class="below"></div>`, image.Rect(0, 0, 80, 60))
	pixel(t, img, 5, 5, opacityBlue)
	pixel(t, img, 40, 5, opacityWhite)
	pixel(t, img, 40, 30, opacityRed)
	pixel(t, img, 40, 50, opacityWhite)
}

// opacity:0 hides the element's whole group: its own background, text,
// in-flow descendants and positioned descendants, even ones that override
// opacity back to 1.
func TestOpacityZeroHidesWholeSubtree(t *testing.T) {
	img := painted(t, `<body style="margin:0">
		<div style="opacity:0;width:40px;height:20px;background:red">
			<div style="width:10px;height:10px;background:blue;opacity:1"></div>
			<div style="position:absolute;top:0;left:20px;width:10px;height:10px;background:blue"></div>
			<span style="color:red">XXXX</span>
		</div></body>`, image.Rect(0, 0, 60, 40))
	for _, pt := range []image.Point{{5, 5}, {25, 5}, {35, 15}, {5, 15}} {
		pixel(t, img, pt.X, pt.Y, opacityWhite)
	}
}

// Fractional opacity composites the element as one group, so overlapping
// descendants do not show a darker seam where they overlap.
func TestFractionalOpacityCompositesGroup(t *testing.T) {
	img := painted(t, `<body style="margin:0">
		<div style="opacity:0.5;width:40px;height:20px">
			<div style="width:30px;height:20px;background:red"></div>
			<div style="position:absolute;top:0;left:10px;width:30px;height:20px;background:red"></div>
		</div>
		<div style="opacity:50%;width:10px;height:10px;background:blue"></div></body>`, image.Rect(0, 0, 60, 40))
	half := img.RGBAAt(5, 10)
	if half.R != 255 || half.G < 120 || half.G > 135 || half.G != half.B {
		t.Fatalf("half-opacity red over white = %v, want about (255,127,127)", half)
	}
	pixel(t, img, 20, 10, half) // overlap region: same as a single layer
	pixel(t, img, 35, 10, half)
	pixel(t, img, 50, 10, opacityWhite)
	if got := img.RGBAAt(5, 25); got.B != 255 || got.R < 120 || got.R > 135 {
		t.Errorf("50%% opacity blue = %v, want about (127,127,255)", got)
	}
}

// Opacity below 1 forms a stacking context that paints in the z-index 0
// layer, above a later overlapping non-positioned sibling (CSS 2.1 Appendix E).
func TestOpacityElementPaintsAsStackingContext(t *testing.T) {
	img := painted(t, `<body style="margin:0">
		<div style="width:20px;height:20px;background:blue;opacity:0.999"></div>
		<div style="margin-top:-10px;width:20px;height:20px;background:red"></div></body>`,
		image.Rect(0, 0, 40, 40))
	if got := img.RGBAAt(10, 15); got.B < 250 || got.R > 5 {
		t.Errorf("overlap pixel = %v, want the opacity element (blue) on top", got)
	}
	pixel(t, img, 10, 25, opacityRed)
}

// Invalid and absent opacity keep the initial value 1; out-of-range numbers
// clamp.
func TestElementOpacityValues(t *testing.T) {
	for value, want := range map[string]float64{
		"": 1, "auto": 1, "0": 0, "0.25": 0.25, "40%": 0.4, "2": 1, "-1": 0,
	} {
		if got := elementOpacity(ComputedStyle{"opacity": value}); got != want {
			t.Errorf("elementOpacity(%q) = %v, want %v", value, got, want)
		}
	}
}
