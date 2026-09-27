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
