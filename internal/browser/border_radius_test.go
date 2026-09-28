package browser

import (
	"image"
	"image/color"
	"testing"
)

func TestRoundedBackgroundAndBorderPixels(t *testing.T) {
	img := painted(t, `<body style="margin:0"><div style="width:20px;height:20px;border:2px solid blue;background:red;border-radius:8px"></div></body>`, image.Rect(0, 0, 40, 40))
	white, blue, red := color.RGBA{255, 255, 255, 255}, color.RGBA{0, 0, 255, 255}, color.RGBA{255, 0, 0, 255}
	pixel(t, img, 0, 0, white)
	pixel(t, img, 4, 1, blue)
	pixel(t, img, 4, 4, red)
	pixel(t, img, 12, 0, blue)
	pixel(t, img, 12, 12, red)
	pixel(t, img, 27, 0, white)
	pixel(t, img, 0, 27, white)
	pixel(t, img, 27, 27, white)
}

func TestRadiusShorthandCornersAndNormalization(t *testing.T) {
	img := painted(t, `<body style="margin:0"><div style="width:20px;height:10px;background:red;border-radius:12px 0 0 0 / 8px 0 0 0;border-top-left-radius:80% 80%"></div></body>`, image.Rect(0, 0, 35, 25))
	white, red := color.RGBA{255, 255, 255, 255}, color.RGBA{255, 0, 0, 255}
	pixel(t, img, 0, 0, white)
	pixel(t, img, 19, 0, red)
	pixel(t, img, 0, 9, red)
	pixel(t, img, 19, 9, red)
	r := usedRadii(ComputedStyle{
		"border-top-left-radius": "80% 80%", "border-top-right-radius": "80% 80%",
		"border-bottom-left-radius": "80% 80%", "border-bottom-right-radius": "80% 80%",
	}, image.Rect(0, 0, 20, 10))
	for _, corner := range r {
		if corner.x != 10 || corner.y != 5 {
			t.Fatalf("normalized radii: %+v", r)
		}
	}
}

func TestRadiusZeroAndInvalidDeclaration(t *testing.T) {
	white, red := color.RGBA{255, 255, 255, 255}, color.RGBA{255, 0, 0, 255}
	for _, css := range []string{
		"border-radius:8px;border-radius:0",
		"border-radius:8px;border-top-left-radius:0",
		"border-radius:0;border-radius:-3px 8px",
	} {
		img := painted(t, `<body style="margin:0"><div style="width:20px;height:20px;background:red;`+css+`"></div></body>`, image.Rect(0, 0, 30, 30))
		pixel(t, img, 0, 0, red)
		pixel(t, img, 20, 20, white)
	}
}

func TestRadiusMathFunctionsComputedValues(t *testing.T) {
	doc := styledForLayout(t, `<div id="radius" style="
		border-radius:calc(5px + 5px) min(30px, 12px) /
			max(3px, 6px) clamp(4px, 7px, 9px)"></div>`)
	style := styledElementByID(doc.StyleRoot, "radius").Style
	want := map[string]string{
		"border-top-left-radius":     "10px 6px",
		"border-top-right-radius":    "12px 7px",
		"border-bottom-right-radius": "10px 6px",
		"border-bottom-left-radius":  "12px 7px",
	}
	for property, expected := range want {
		if got := style[property]; got != expected {
			t.Errorf("%s = %q, want %q", property, got, expected)
		}
	}
}

func TestRadiusMathFunctionsResolvePercentagesAgainstBorderBox(t *testing.T) {
	img := painted(t, `<body style="margin:0">
		<div style="width:20px;height:10px;background:red;
			border-radius:calc(25% + 5px) / clamp(2px, 50%, 8px)"></div>
		<div style="width:20px;height:10px;background:blue;
			border-top-left-radius:max(25%, 8px) min(75%, 6px)"></div>
	</body>`, image.Rect(0, 0, 30, 25))
	white := color.RGBA{255, 255, 255, 255}
	pixel(t, img, 0, 0, white)
	pixel(t, img, 10, 0, color.RGBA{255, 0, 0, 255})
	pixel(t, img, 0, 10, white)
	pixel(t, img, 8, 10, color.RGBA{0, 0, 255, 255})

	radii := usedRadii(ComputedStyle{
		"border-top-left-radius": "calc(50% - 2px) calc(clamp(1px, 50%, 20px))",
	}, image.Rect(0, 0, 20, 10))
	if got := radii[0]; got != (cornerRadius{x: 8, y: 5}) {
		t.Fatalf("used mixed percentage radii = %+v, want {8 5}", got)
	}
}

func TestRadiusCalcDivisionDoesNotSplitShorthand(t *testing.T) {
	doc := styledForLayout(t, `<div id="radius" style="
		border-radius:calc(20px / 2) / calc(12px / 2)"></div>`)
	style := styledElementByID(doc.StyleRoot, "radius").Style
	for _, corner := range radiusCorners {
		if got := style["border-"+corner+"-radius"]; got != "10px 6px" {
			t.Errorf("%s radius = %q, want %q", corner, got, "10px 6px")
		}
	}
}

func TestRoundedBackgroundDoesNotClipChildren(t *testing.T) {
	img := painted(t, `<body style="margin:0"><div style="width:20px;height:20px;background:red;border-radius:10px"><div style="width:2px;height:2px;background:blue"></div></div></body>`, image.Rect(0, 0, 30, 30))
	pixel(t, img, 0, 0, color.RGBA{0, 0, 255, 255})
	pixel(t, img, 3, 0, color.RGBA{255, 255, 255, 255})
}

func TestRoundedGradientBackground(t *testing.T) {
	img := painted(t, `<body style="margin:0"><div style="width:20px;height:20px;border-radius:10px;background-image:linear-gradient(red,red)"></div></body>`, image.Rect(0, 0, 30, 30))
	pixel(t, img, 0, 0, color.RGBA{255, 255, 255, 255})
	pixel(t, img, 10, 10, color.RGBA{255, 0, 0, 255})
}

func TestRoundedTransparentBorderShowsOwnBackground(t *testing.T) {
	// Backgrounds paint under the border box (background-clip: border-box),
	// so a transparent or translucent rounded border shows the box's own
	// background, not the parent's, exactly like the square-corner path.
	img := painted(t, `<body style="margin:0;background:lime"><div style="width:20px;height:20px;border:4px solid transparent;background:red;border-radius:8px"></div><div style="width:20px;height:20px;border:4px solid rgba(0,0,255,0.5);background:red;border-radius:8px"></div></body>`, image.Rect(0, 0, 40, 60))
	lime, red := color.RGBA{0, 255, 0, 255}, color.RGBA{255, 0, 0, 255}
	pixel(t, img, 0, 0, lime)
	pixel(t, img, 14, 1, red)
	pixel(t, img, 1, 14, red)
	pixel(t, img, 14, 14, red)
	pixel(t, img, 0, 28, lime)
	// Translucent border pixels composite over the box's own red background
	// identically to the square-corner border path.
	square := painted(t, `<body style="margin:0;background:lime"><div style="width:20px;height:20px;border:4px solid rgba(0,0,255,0.5);background:red"></div></body>`, image.Rect(0, 0, 40, 40))
	want := square.RGBAAt(14, 1)
	if want.R == 0 || want.G != 0 {
		t.Fatalf("square translucent border pixel = %+v, want red beneath", want)
	}
	pixel(t, img, 14, 29, want)
	pixel(t, img, 1, 42, want)
}
