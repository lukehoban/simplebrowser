package browser

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"testing"
)

var (
	maskDark  = color.RGBA{0x20, 0x21, 0x22, 255}
	maskWhite = color.RGBA{255, 255, 255, 255}
)

// leftHalfSVG is opaque on its left half and transparent on its right half.
// The values use no double quotes so they can appear in a style attribute.
const leftHalfSVG = `url(data:image/svg+xml;base64,` + "PHN2ZyB4bWxucz0naHR0cDovL3d3dy53My5vcmcvMjAwMC9zdmcnIHdpZHRoPScyMCcgaGVpZ2h0PScyMCc+PHJlY3Qgd2lkdGg9JzEwJyBoZWlnaHQ9JzIwJy8+PC9zdmc+" + `)`

// solidSVG is a fully opaque 10x10 image.
const solidSVG = `url('data:image/svg+xml;utf8,<svg xmlns=%22http://www.w3.org/2000/svg%22 width=%2210%22 height=%2210%22><rect width=%2210%22 height=%2210%22/></svg>')`

func TestMaskImageUsesAlphaOfSVG(t *testing.T) {
	img := painted(t, `<body style="margin:0">
		<div style="width:20px;height:20px;background:#202122;mask-image:`+leftHalfSVG+`"></div></body>`,
		image.Rect(0, 0, 40, 30))
	pixel(t, img, 4, 10, maskDark)
	pixel(t, img, 15, 10, maskWhite)
}

// A mask whose image cannot be loaded is a transparent black layer, so the
// element (the Moon icons' solid background squares) is hidden, not opaque.
func TestMaskImageFailedLoadHidesElement(t *testing.T) {
	img := painted(t, `<body style="margin:0">
		<div style="width:20px;height:20px;background:#202122;mask-image:url(missing-icon.svg)"></div></body>`,
		image.Rect(0, 0, 40, 30))
	for _, x := range []int{2, 10, 18} {
		pixel(t, img, x, 10, maskWhite)
	}
}

func TestMaskNoneLeavesElementUnmasked(t *testing.T) {
	img := painted(t, `<body style="margin:0">
		<div style="width:20px;height:20px;background:#202122;mask-image:none, none"></div></body>`,
		image.Rect(0, 0, 40, 30))
	pixel(t, img, 15, 10, maskDark)
}

// Vector's icon pattern: prefixed and unprefixed longhands, a centered
// no-repeat mask with an explicit size inside a larger box.
func TestWebkitMaskLonghandsSizePositionRepeat(t *testing.T) {
	img := painted(t, `<body style="margin:0">
		<div style="width:40px;height:40px;background:#202122;
			-webkit-mask-image:`+solidSVG+`;-webkit-mask-position:center;
			-webkit-mask-repeat:no-repeat;-webkit-mask-size:calc(max(calc(1rem - 4px), 10px))"></div></body>`,
		image.Rect(0, 0, 60, 60))
	// The 12x12 mask tile sits at (14,14)-(26,26).
	pixel(t, img, 20, 20, maskDark)
	pixel(t, img, 15, 15, maskDark)
	pixel(t, img, 12, 20, maskWhite)
	pixel(t, img, 27, 20, maskWhite)
	pixel(t, img, 5, 5, maskWhite)
}

func TestMaskSizeContainCoverPreserveAspectRatio(t *testing.T) {
	for _, tc := range []struct {
		name, size  string
		dark, white image.Point
	}{
		{"contain", "contain", image.Pt(19, 19), image.Pt(20, 10)},
		{"cover", "cover", image.Pt(39, 19), image.Pt(40, 10)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			img := painted(t, `<body style="margin:0">
				<div style="width:40px;height:20px;background:#202122;mask-image:`+solidSVG+`;
					mask-size:`+tc.size+`;mask-repeat:no-repeat"></div></body>`, image.Rect(0, 0, 50, 30))
			pixel(t, img, tc.dark.X, tc.dark.Y, maskDark)
			pixel(t, img, tc.white.X, tc.white.Y, maskWhite)
		})
	}
	// With an aspect ratio opposite the positioning area's, cover must
	// overshoot horizontally; the left half stays opaque after centering.
	img := painted(t, `<body style="margin:0">
		<div style="width:40px;height:20px;background:#202122;mask-image:`+leftHalfSVG+`;
			mask-size:cover;mask-position:center;mask-repeat:no-repeat"></div></body>`, image.Rect(0, 0, 50, 30))
	pixel(t, img, 19, 10, maskDark)
	pixel(t, img, 20, 10, maskWhite)
}

func TestMaskSpacedCalcSizeAndPosition(t *testing.T) {
	for _, tc := range []struct {
		name, style string
	}{
		{"longhands", `mask-size:calc(50% + 2px) 10px;mask-position:calc(50% + 2px) 0px;mask-repeat:no-repeat`},
		{"shorthand", `mask:` + solidSVG + ` calc(50% + 2px) 0px / calc(50% + 2px) 10px no-repeat`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			style := tc.style
			if tc.name == "longhands" {
				style += ";mask-image:" + solidSVG
			}
			img := painted(t, `<body style="margin:0"><div style="width:40px;height:40px;
				background:#202122;`+style+`"></div></body>`, image.Rect(0, 0, 50, 50))
			// Size is 22x10; the horizontal free space is 18, so
			// calc(50% + 2px) positions its left edge at x=11.
			pixel(t, img, 10, 5, maskWhite)
			pixel(t, img, 11, 0, maskDark)
			pixel(t, img, 32, 9, maskDark)
			pixel(t, img, 33, 5, maskWhite)
			pixel(t, img, 20, 10, maskWhite)
		})
	}
}

func TestMaskRepeatTilesByDefault(t *testing.T) {
	img := painted(t, `<body style="margin:0">
		<div style="width:40px;height:20px;background:#202122;mask-image:`+leftHalfSVG+`"></div></body>`,
		image.Rect(0, 0, 60, 30))
	pixel(t, img, 4, 10, maskDark)
	pixel(t, img, 15, 10, maskWhite)
	pixel(t, img, 24, 10, maskDark)
	pixel(t, img, 35, 10, maskWhite)
}

func TestMaskShorthand(t *testing.T) {
	img := painted(t, `<body style="margin:0">
		<div style="width:40px;height:40px;background:#202122;
			mask:`+solidSVG+` center / 10px no-repeat"></div>
		<div style="width:40px;height:40px;background:#202122;mask:`+solidSVG+` red"></div></body>`,
		image.Rect(0, 0, 60, 90))
	pixel(t, img, 20, 20, maskDark)
	pixel(t, img, 10, 10, maskWhite)
	// An invalid shorthand (masks have no color) is dropped: unmasked.
	pixel(t, img, 5, 45, maskDark)
}

func TestMaskImageGradientUsesAlpha(t *testing.T) {
	img := painted(t, `<body style="margin:0">
		<div style="width:20px;height:100px;background:#202122;
			mask-image:linear-gradient(black, transparent)"></div></body>`,
		image.Rect(0, 0, 40, 110))
	top, bottom := img.RGBAAt(10, 2), img.RGBAAt(10, 97)
	if top.R > 0x30 || bottom.R < 0xf0 {
		t.Fatalf("gradient mask top=%v bottom=%v; want dark to white", top, bottom)
	}
}

// Descendants, including their own backgrounds and text, are painted into the
// masked element's layer and clipped by its mask.
func TestMaskClipsDescendants(t *testing.T) {
	green := color.RGBA{0, 128, 0, 255}
	img := painted(t, `<body style="margin:0">
		<div style="width:20px;height:20px;mask-image:`+leftHalfSVG+`">
			<div style="width:20px;height:20px;background:green"></div>
			<div style="position:absolute;top:0;left:0;width:20px;height:20px;background:green"></div>
		</div></body>`,
		image.Rect(0, 0, 40, 30))
	pixel(t, img, 4, 10, green)
	pixel(t, img, 15, 10, maskWhite)
}

// A masked element forms a stacking context in the z-index:0 layer, like
// opacity, so it paints above a following non-positioned sibling it overlaps.
func TestMaskedElementPaintsAsStackingContext(t *testing.T) {
	blue := color.RGBA{0, 0, 255, 255}
	img := painted(t, `<body style="margin:0">
		<div style="width:20px;height:20px;background:blue;mask-image:linear-gradient(black, black)"></div>
		<div style="margin-top:-10px;width:20px;height:20px;background:red"></div></body>`,
		image.Rect(0, 0, 40, 40))
	pixel(t, img, 10, 15, blue)
	pixel(t, img, 10, 25, color.RGBA{255, 0, 0, 255})
}

// Stylesheet-relative mask URLs resolve against the stylesheet, and the
// -webkit- alias and unprefixed property load one shared resource.
func TestMaskImageURLResolvesAgainstStylesheet(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "styles"), 0o755); err != nil {
		t.Fatal(err)
	}
	files := map[string]string{
		"icon.svg": `<svg xmlns="http://www.w3.org/2000/svg" width="20" height="20"><rect width="10" height="20"/></svg>`,
		"styles/site.css": `.icon{width:20px;height:20px;background:#202122;
			-webkit-mask-image:url(../icon.svg);mask-image:url(../icon.svg)}`,
		"index.html": `<link rel="stylesheet" href="styles/site.css"><body style="margin:0"><div class="icon"></div></body>`,
	}
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(root, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	path := filepath.Join(root, "index.html")
	doc, err := parse(Resource{URL: path, Body: []byte(files["index.html"])})
	if err != nil {
		t.Fatal(err)
	}
	styled, err := style(doc, &Fetcher{})
	if err != nil {
		t.Fatal(err)
	}
	layout, err := LayoutWithViewport(styled, image.Rect(0, 0, 40, 30))
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
	pixel(t, img, 4, 10, maskDark)
	pixel(t, img, 15, 10, maskWhite)
}

func TestMaskSizeComputesNestedMathFunctions(t *testing.T) {
	doc := styledForLayout(t, `<div style="font-size:20px;mask-size:calc(max(calc(1em - 4px), 10px)) min(5px, 2em), max(8px, 50%)"></div>`)
	for node, style := range doc.Styles {
		if node.Name != "div" {
			continue
		}
		// A percentage argument cannot be compared at computed time.
		if got, want := style["mask-size"], "16px 5px, max(8px, 50%)"; got != want {
			t.Fatalf("mask-size = %q, want %q", got, want)
		}
		return
	}
	t.Fatal("div not styled")
}

func TestValidBackgroundAllowsComponentsAfterSize(t *testing.T) {
	for value, want := range map[string]bool{
		"url(a.png) center / 20px no-repeat":      true,
		"url(a.png) center / 20px auto no-repeat": true,
		"url(a.png) center / cover repeat-x red":  true,
		"url(a.png) center / 20px left":           false,
		"url(a.png) center / 20px / 10px":         false,
	} {
		if got := validBackground(value); got != want {
			t.Errorf("validBackground(%q) = %v, want %v", value, got, want)
		}
	}
}
