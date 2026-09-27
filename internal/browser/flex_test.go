package browser

import (
	"image"
	"image/color"
	"testing"
)

func TestFlexRowGapAlignmentAndPixels(t *testing.T) {
	const source = `<body style="margin:0"><div id="row" style="display:flex;width:300px;height:60px;gap:12px;align-items:center">
		<div id="first" style="width:80px;height:20px;background:red"></div>
		<div id="second" style="width:60px;height:40px;background:blue"></div>
	</div></body>`
	layout, err := LayoutWithViewport(styledForLayout(t, source), image.Rect(0, 0, 320, 100))
	if err != nil {
		t.Fatal(err)
	}
	boxes := boxesByID(layout.Root, "row", "first", "second")
	if boxes["first"].Rect != image.Rect(0, 20, 80, 40) ||
		boxes["second"].Rect != image.Rect(92, 10, 152, 50) {
		t.Fatalf("flex row boxes: first=%v second=%v", boxes["first"].Rect, boxes["second"].Rect)
	}
	img := painted(t, source, image.Rect(0, 0, 320, 100))
	pixel(t, img, 10, 25, color.RGBA{255, 0, 0, 255})
	pixel(t, img, 85, 25, color.RGBA{255, 255, 255, 255})
	pixel(t, img, 100, 25, color.RGBA{0, 0, 255, 255})
}

func TestFlexMainAxisGrowShrinkAndJustification(t *testing.T) {
	const source = `<body style="margin:0">
	<div id="grow" style="display:flex;width:300px;gap:10px">
		<div id="fixed" style="width:40px;height:10px"></div>
		<div id="one" style="flex:1;height:10px"></div>
		<div id="two" style="flex:2;height:10px"></div>
	</div>
	<div id="shrink" style="display:flex;width:100px">
		<div id="wide-a" style="width:80px;height:10px;flex-shrink:1"></div>
		<div id="wide-b" style="width:80px;height:10px;flex-shrink:1"></div>
	</div>
	<div id="justify" style="display:flex;width:200px;justify-content:space-between">
		<div id="start" style="width:20px;height:10px"></div>
		<div id="end" style="width:20px;height:10px"></div>
	</div></body>`
	layout, err := LayoutWithViewport(styledForLayout(t, source), image.Rect(0, 0, 320, 100))
	if err != nil {
		t.Fatal(err)
	}
	b := boxesByID(layout.Root, "fixed", "one", "two", "wide-a", "wide-b", "start", "end")
	if b["fixed"].Rect.Dx() != 40 || b["one"].Rect.Dx() != 80 || b["two"].Rect.Dx() != 160 ||
		b["one"].Rect.Min.X != 50 || b["two"].Rect.Min.X != 140 {
		t.Fatalf("grow geometry: fixed=%v one=%v two=%v", b["fixed"].Rect, b["one"].Rect, b["two"].Rect)
	}
	if b["wide-a"].Rect.Dx() != 50 || b["wide-b"].Rect != image.Rect(50, 10, 100, 20) {
		t.Fatalf("shrink geometry: a=%v b=%v", b["wide-a"].Rect, b["wide-b"].Rect)
	}
	if b["start"].Rect.Min.X != 0 || b["end"].Rect.Min.X != 180 {
		t.Fatalf("space-between geometry: start=%v end=%v", b["start"].Rect, b["end"].Rect)
	}
}

func TestFlexSizingAccountsForPaddingAndPercentageBasis(t *testing.T) {
	const source = `<body style="margin:0">
	<div style="display:flex;width:200px">
		<div id="padded" style="flex:1;padding:0 10px;height:10px"></div>
		<div id="fixed-size" style="width:50px;height:10px"></div>
	</div>
	<div style="display:flex;width:200px">
		<div id="percentage" style="flex:0 0 50%;height:10px"></div>
		<div id="remainder" style="flex:1;height:10px"></div>
	</div></body>`
	layout, err := LayoutWithViewport(styledForLayout(t, source), image.Rect(0, 0, 240, 80))
	if err != nil {
		t.Fatal(err)
	}
	b := boxesByID(layout.Root, "padded", "fixed-size", "percentage", "remainder")
	if b["padded"].Rect.Dx() != 150 || b["fixed-size"].Rect.Min.X != 150 {
		t.Fatalf("padding must count in the flex main size: padded=%v fixed=%v", b["padded"].Rect, b["fixed-size"].Rect)
	}
	if b["percentage"].Rect.Dx() != 100 || b["remainder"].Rect != image.Rect(100, 10, 200, 20) {
		t.Fatalf("percentage basis geometry: percentage=%v remainder=%v", b["percentage"].Rect, b["remainder"].Rect)
	}
}

func TestFlexColumnAndInlineFlex(t *testing.T) {
	const source = `<body style="margin:0">
	<div id="column" style="display:flex;flex-direction:column;width:80px;height:110px;gap:10px">
		<div id="top" style="flex:1;background:red"></div>
		<div id="bottom" style="flex:1;background:blue"></div>
	</div>
	<p style="margin:0">A<span id="inline" style="display:inline-flex;width:50px;height:20px"><b style="width:20px"></b><i style="width:20px"></i></span>Z</p>
	</body>`
	layout, err := LayoutWithViewport(styledForLayout(t, source), image.Rect(0, 0, 200, 180))
	if err != nil {
		t.Fatal(err)
	}
	b := boxesByID(layout.Root, "column", "top", "bottom", "inline")
	if b["top"].Rect != image.Rect(0, 0, 80, 50) || b["bottom"].Rect != image.Rect(0, 60, 80, 110) {
		t.Fatalf("column geometry: top=%v bottom=%v", b["top"].Rect, b["bottom"].Rect)
	}
	if b["inline"] == nil || b["inline"].Rect.Dx() != 50 || b["inline"].Rect.Dy() != 20 {
		t.Fatalf("inline-flex should be one 50x20 atomic inline: %#v", b["inline"])
	}
}
