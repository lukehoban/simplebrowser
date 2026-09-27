package browser

import (
	"image"
	"image/color"
	"testing"
)

// #216: top/bottom percentages on absolute and fixed boxes refer to the
// containing block's height (CSS 2.1 §9.3.2), not its width. Every
// containing block below is deliberately wider than it is tall.
func TestPercentageVerticalOffsetsUseContainingBlockHeight(t *testing.T) {
	viewport := image.Rect(0, 0, 400, 300)
	for _, tt := range []struct {
		name, source string
		want         image.Rectangle
	}{
		{
			// The issue repro.
			name: "top",
			source: `<body style="margin:0"><div style="position:relative;height:100px">
			<div id="a" style="position:absolute;top:50%;width:10px;height:10px"></div></div></body>`,
			want: image.Rect(0, 50, 10, 60),
		},
		{
			name: "bottom",
			source: `<body style="margin:0"><div style="position:relative;height:100px">
			<div id="a" style="position:absolute;bottom:25%;width:10px;height:10px"></div></div></body>`,
			want: image.Rect(0, 65, 10, 75),
		},
		{
			// The basis is the padding box: 20 + 100 + 20 = 140px tall, and
			// offsets start at the padding edge inside the 5px border.
			name: "padding box",
			source: `<body style="margin:0"><div style="position:relative;height:100px;padding:20px;border:5px solid black">
			<div id="a" style="position:absolute;top:50%;left:0;width:10px;height:10px"></div></div></body>`,
			want: image.Rect(5, 75, 15, 85),
		},
		{
			name: "right and bottom",
			source: `<body style="margin:0"><div style="position:relative;width:200px;height:100px">
			<div id="a" style="position:absolute;right:10%;bottom:10%;width:10px;height:10px"></div></div></body>`,
			want: image.Rect(170, 80, 180, 90),
		},
		{
			// No positioned ancestor: the initial containing block (viewport).
			name: "initial containing block",
			source: `<body style="margin:0">
			<div id="a" style="position:absolute;top:10%;width:10px;height:10px"></div></body>`,
			want: image.Rect(0, 30, 10, 40),
		},
		{
			// Fixed boxes resolve against the viewport even inside a
			// positioned ancestor.
			name: "fixed",
			source: `<body style="margin:0"><div style="position:relative;height:100px">
			<div id="a" style="position:fixed;bottom:10%;width:10px;height:10px"></div></div></body>`,
			want: image.Rect(0, 260, 10, 270),
		},
		{
			// Over-constrained vertical auto margins: offsets 20px and 40px
			// of a 200px-tall block leave 100px, split 50/50.
			name: "auto margins",
			source: `<body style="margin:0"><div style="position:relative;height:200px">
			<div id="a" style="position:absolute;top:10%;bottom:20%;height:40px;margin:auto 0;width:10px"></div></div></body>`,
			want: image.Rect(0, 70, 10, 110),
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			layout, err := LayoutWithViewport(styledForLayout(t, tt.source), viewport)
			if err != nil {
				t.Fatal(err)
			}
			if got := boxesByID(layout.Root, "a")["a"].Rect; got != tt.want {
				t.Fatalf("rect %v, want %v", got, tt.want)
			}
		})
	}
}

// Pixel check for the WPT bottom-offset-percentage-001 shape: the green
// bottom:50% box exactly covers a red top:50px box in a 200px-tall block.
func TestPaintBottomPercentageOffsetCoversReference(t *testing.T) {
	img := painted(t, `<body style="margin:0"><div style="position:relative;width:100px;height:200px">
	<div style="position:absolute;top:50px;width:50px;height:50px;background:red"></div>
	<div style="position:absolute;bottom:50%;width:50px;height:50px;background:green"></div>
	</div></body>`, image.Rect(0, 0, 200, 300))
	green := color.RGBA{0, 128, 0, 255}
	white := color.RGBA{255, 255, 255, 255}
	pixel(t, img, 0, 50, green)
	pixel(t, img, 49, 99, green)
	pixel(t, img, 25, 49, white)
	pixel(t, img, 25, 100, white)
}
