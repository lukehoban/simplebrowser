package browser

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"testing"
)

func TestFlexImageUsesReplacedLayoutAndPaint(t *testing.T) {
	doc := styledForLayout(t, `<body style="margin:0"><div style="display:flex;align-items:flex-start;width:60px"><img id="logo"><div id="next" style="width:10px;height:10px"></div></div></body>`)
	var imageNode *Node
	var find func(*StyledNode)
	find = func(n *StyledNode) {
		if n.Node != nil && n.Node.Name == "img" {
			imageNode = n.Node
		}
		for _, c := range n.Children {
			find(c)
		}
	}
	find(doc.StyleRoot)
	picture := image.NewRGBA(image.Rect(0, 0, 8, 4))
	for y := 0; y < 4; y++ {
		for x := 0; x < 8; x++ {
			picture.SetRGBA(x, y, color.RGBA{255, 0, 0, 255})
		}
	}
	doc.Images = map[*Node]image.Image{imageNode: picture}
	layout, err := LayoutWithViewport(doc, image.Rect(0, 0, 80, 40))
	if err != nil {
		t.Fatal(err)
	}
	b := boxesByID(layout.Root, "logo", "next")
	if b["logo"].Rect != image.Rect(0, 0, 8, 4) || b["next"].Rect.Min.X != 8 || len(b["logo"].Images) != 1 {
		t.Fatalf("image flex item: logo=%+v next=%+v", b["logo"], b["next"])
	}
	var buf bytes.Buffer
	if err := paint(layout, &buf, renderOptions{}); err != nil {
		t.Fatal(err)
	}
	rendered, err := png.Decode(&buf)
	if err != nil {
		t.Fatal(err)
	}
	pixel(t, rendered.(*image.RGBA), 2, 2, color.RGBA{255, 0, 0, 255})
}

func TestFlexBoundsBasisAndReversePixels(t *testing.T) {
	const source = `<body style="margin:0">
	<div style="display:flex;width:100px"><div id="min" style="width:80px;min-width:80px;height:10px;background:red"></div><div id="shrunk" style="width:80px;height:10px;background:blue"></div></div>
	<div style="display:flex;width:100px"><div id="max" style="flex:1;max-width:30px;height:10px;background:red"></div><div id="grown" style="flex:1;height:10px;background:blue"></div></div>
	<div style="display:flex;flex-direction:column;width:40px;height:80px"><div id="basis" style="height:20px;flex:0 0 40px;background:red"></div><div id="auto-basis" style="height:20px;flex:0 0 auto;background:blue"></div></div>
	<div style="display:flex;flex-direction:row-reverse;width:100px;justify-content:flex-start"><div id="reverse-a" style="width:20px;height:10px;background:red"></div><div id="reverse-b" style="width:10px;height:10px;background:blue"></div></div>
	<div style="display:flex;flex-direction:column-reverse;width:30px;height:60px"><div id="reverse-column" style="width:10px;height:20px;background:red"></div></div>
	<div style="display:flex;flex-direction:column;width:20px;height:100px"><div id="min-height" style="height:80px;min-height:80px;flex-shrink:1;background:red"></div><div id="remaining-height" style="height:80px;flex-shrink:1;background:blue"></div></div>
	<div style="display:flex;flex-direction:row-reverse;width:100px;justify-content:flex-end"><div id="reverse-end" style="width:20px;height:10px;background:red"></div></div>
	</body>`
	layout, err := LayoutWithViewport(styledForLayout(t, source), image.Rect(0, 0, 130, 320))
	if err != nil {
		t.Fatal(err)
	}
	b := boxesByID(layout.Root, "min", "shrunk", "max", "grown", "basis", "auto-basis", "reverse-a", "reverse-b", "reverse-column", "min-height", "remaining-height", "reverse-end")
	want := map[string]image.Rectangle{
		"min": image.Rect(0, 0, 80, 10), "shrunk": image.Rect(80, 0, 100, 10),
		"max": image.Rect(0, 10, 30, 20), "grown": image.Rect(30, 10, 100, 20),
		"basis": image.Rect(0, 20, 40, 60), "auto-basis": image.Rect(0, 60, 40, 80),
		"reverse-a": image.Rect(80, 100, 100, 110),
		"reverse-b": image.Rect(70, 100, 80, 110), "reverse-column": image.Rect(0, 150, 10, 170),
		"min-height": image.Rect(0, 170, 20, 250), "remaining-height": image.Rect(0, 250, 20, 270),
		"reverse-end": image.Rect(0, 270, 20, 280),
	}
	for id, rect := range want {
		if b[id] == nil || b[id].Rect != rect {
			t.Errorf("%s: got %v want %v", id, b[id], rect)
		}
	}
	img := painted(t, source, image.Rect(0, 0, 130, 320))
	pixel(t, img, 85, 5, color.RGBA{0, 0, 255, 255})
	pixel(t, img, 85, 105, color.RGBA{255, 0, 0, 255})
	pixel(t, img, 5, 155, color.RGBA{255, 0, 0, 255})
	pixel(t, img, 5, 255, color.RGBA{0, 0, 255, 255})
}

func TestFlexSupportsClaimsOnlyImplementedAlignment(t *testing.T) {
	if supportsConditionMatches("(align-items: baseline)") {
		t.Fatal("baseline alignment is not implemented")
	}
	if !supportsConditionMatches("(align-items: center)") {
		t.Fatal("center alignment is implemented")
	}
}

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

// Main-axis percentage min/max sizes resolve against the container's definite
// main size: height for columns, width for rows (#247 review).
func TestFlexMainAxisPercentMinMaxResolveAgainstMainSize(t *testing.T) {
	const source = `<body style="margin:0">
	<div style="display:flex;flex-direction:column;width:20px;height:100px"><div id="col-a" style="height:80px;background:blue"></div><div id="col-b" style="height:80px;min-height:75%;background:red"></div></div>
	<div style="display:flex;flex-direction:column;width:20px;height:100px"><div id="colmax-a" style="flex:1;max-height:30%;background:blue"></div><div id="colmax-b" style="flex:1;background:red"></div></div>
	<div style="display:flex;width:100px;height:20px"><div id="row-a" style="width:80px;height:10px"></div><div id="row-b" style="width:80px;min-width:75%;height:10px"></div></div>
	<div style="display:flex;flex-direction:column;width:20px"><div id="auto-a" style="height:30px;min-height:75%"></div><div id="auto-b" style="height:10px"></div></div>
	</body>`
	layout, err := LayoutWithViewport(styledForLayout(t, source), image.Rect(0, 0, 120, 300))
	if err != nil {
		t.Fatal(err)
	}
	b := boxesByID(layout.Root, "col-a", "col-b", "colmax-a", "colmax-b", "row-a", "row-b", "auto-a", "auto-b")
	want := map[string]image.Rectangle{
		"col-a": image.Rect(0, 0, 20, 25), "col-b": image.Rect(0, 25, 20, 100),
		"colmax-a": image.Rect(0, 100, 20, 130), "colmax-b": image.Rect(0, 130, 20, 200),
		"row-a": image.Rect(0, 200, 25, 210), "row-b": image.Rect(25, 200, 100, 210),
		// An auto-height column has no definite main size; the percentage
		// minimum behaves as auto and items keep their specified heights.
		"auto-a": image.Rect(0, 220, 20, 250), "auto-b": image.Rect(0, 250, 20, 260),
	}
	for id, rect := range want {
		if b[id] == nil || b[id].Rect != rect {
			t.Errorf("%s: got %v want %v", id, b[id], rect)
		}
	}
	img := painted(t, source, image.Rect(0, 0, 120, 300))
	pixel(t, img, 10, 20, color.RGBA{0, 0, 255, 255})
	pixel(t, img, 10, 30, color.RGBA{255, 0, 0, 255})
}

func TestFlexWrapLinesGeometryAndPixels(t *testing.T) {
	red, blue, green := color.RGBA{255, 0, 0, 255}, color.RGBA{0, 0, 255, 255}, color.RGBA{0, 128, 0, 255}
	cases := []struct {
		name, source string
		want         map[string]image.Rectangle
		pixels       map[image.Point]color.RGBA
	}{{
		name:   "issue repro starts a second line",
		source: `<div id="c" style="display:flex;flex-wrap:wrap;width:100px;gap:10px"><div id="a" style="flex:none;width:60px;height:20px;background:red"></div><div id="b" style="flex:none;width:60px;height:20px;background:blue"></div></div><div id="after" style="height:5px"></div>`,
		want: map[string]image.Rectangle{"c": image.Rect(0, 0, 100, 50), "a": image.Rect(0, 0, 60, 20),
			"b": image.Rect(0, 30, 60, 50), "after": image.Rect(0, 50, 200, 55)},
		pixels: map[image.Point]color.RGBA{{5, 5}: red, {5, 35}: blue, {70, 5}: {255, 255, 255, 255}, {5, 25}: {255, 255, 255, 255}},
	}, {
		name:   "per-line flexing and wrap-reverse cross placement",
		source: `<div id="c" style="display:flex;flex-wrap:wrap-reverse;width:180px;gap:6px 10px"><div id="a" style="width:50px;height:20px;background:red"></div><div id="b" style="width:50px;height:30px;background:green"></div><div id="d" style="width:50px;height:20px"></div><div id="grow" style="flex:1 1 60px;height:20px;background:blue"></div></div>`,
		want: map[string]image.Rectangle{"c": image.Rect(0, 0, 180, 56), "grow": image.Rect(0, 0, 180, 20),
			"a": image.Rect(0, 36, 50, 56), "b": image.Rect(60, 26, 110, 56), "d": image.Rect(120, 36, 170, 56)},
		pixels: map[image.Point]color.RGBA{{175, 5}: blue, {5, 40}: red, {5, 30}: {255, 255, 255, 255}, {65, 30}: green},
	}, {
		name:   "column wrap with stretched align-content lines",
		source: `<div style="display:flex;flex-direction:column;flex-wrap:wrap;height:70px;width:180px;gap:10px"><div id="a" style="height:30px;width:40px;background:red"></div><div id="b" style="height:30px;width:40px"></div><div id="d" style="height:30px;width:40px;background:blue"></div></div>`,
		want:   map[string]image.Rectangle{"a": image.Rect(0, 0, 40, 30), "b": image.Rect(0, 40, 40, 70), "d": image.Rect(95, 0, 135, 30)},
		pixels: map[image.Point]color.RGBA{{100, 5}: blue, {90, 5}: {255, 255, 255, 255}},
	}, {
		name:   "align-content center on a definite row height",
		source: `<div style="display:flex;flex-wrap:wrap;align-content:center;width:100px;height:100px;row-gap:10px"><div id="a" style="width:60px;height:20px"></div><div id="b" style="width:60px;height:20px"></div></div>`,
		want:   map[string]image.Rectangle{"a": image.Rect(0, 25, 60, 45), "b": image.Rect(0, 55, 60, 75)},
	}, {
		name:   "wrapped line stretches auto-height items",
		source: `<div style="display:flex;flex-wrap:wrap;width:100px"><div id="a" style="width:60px;height:15px"></div><div id="e" style="width:30px"></div><div id="b" style="width:60px;height:20px"></div><div id="d" style="width:30px"></div></div>`,
		want:   map[string]image.Rectangle{"a": image.Rect(0, 0, 60, 15), "e": image.Rect(60, 0, 90, 15), "b": image.Rect(0, 15, 60, 35), "d": image.Rect(60, 15, 90, 35)},
	}, {
		name:   "auto-height column never wraps",
		source: `<div style="display:flex;flex-direction:column;flex-wrap:wrap;width:50px"><div id="a" style="height:30px"></div><div id="b" style="height:30px"></div></div>`,
		want:   map[string]image.Rectangle{"a": image.Rect(0, 0, 50, 30), "b": image.Rect(0, 30, 50, 60)},
	}, {
		name:   "shrink-to-fit wrapping row sizes to its items on one line",
		source: `<div id="c" style="float:left;display:flex;flex-wrap:wrap;column-gap:8px"><div id="a" style="width:20px;height:20px;background:red"></div><div id="b" style="width:20px;height:20px;background:blue"></div></div>`,
		want:   map[string]image.Rectangle{"c": image.Rect(0, 0, 48, 20), "a": image.Rect(0, 0, 20, 20), "b": image.Rect(28, 0, 48, 20)},
		pixels: map[image.Point]color.RGBA{{5, 5}: red, {40, 5}: blue},
	}, {
		name:   "nowrap keeps shrinking on one line",
		source: `<div style="display:flex;width:100px"><div id="a" style="width:60px;height:10px"></div><div id="b" style="width:60px;height:10px"></div></div>`,
		want:   map[string]image.Rectangle{"a": image.Rect(0, 0, 50, 10), "b": image.Rect(50, 0, 100, 10)},
	}}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			source := `<body style="margin:0">` + tc.source + `</body>`
			layout, err := LayoutWithViewport(styledForLayout(t, source), image.Rect(0, 0, 200, 120))
			if err != nil {
				t.Fatal(err)
			}
			ids := make([]string, 0, len(tc.want))
			for id := range tc.want {
				ids = append(ids, id)
			}
			b := boxesByID(layout.Root, ids...)
			for id, rect := range tc.want {
				if b[id] == nil || b[id].Rect != rect {
					t.Errorf("%s: got %v want %v", id, b[id], rect)
				}
			}
			if len(tc.pixels) > 0 {
				img := painted(t, source, image.Rect(0, 0, 200, 120))
				for p, c := range tc.pixels {
					pixel(t, img, p.X, p.Y, c)
				}
			}
		})
	}
}
