package browser

import (
	"image"
	"image/color"
	"testing"
)

// Issue #218 repro: min-height grows an empty block and max-height caps a
// block whose content is taller (CSS 2.1 §10.7).
func TestMinMaxHeightIssueRepro(t *testing.T) {
	boxes := percentHeightLayout(t, `<body style="margin:0"><div id="a" style="min-height:50px"></div>
<div id="b" style="max-height:10px"><div id="inner" style="height:40px"></div></div></body>`, "a", "b", "inner")
	if got, want := boxes["a"].Rect, image.Rect(0, 0, 400, 50); got != want {
		t.Fatalf("#a border box = %v, want %v", got, want)
	}
	if got, want := boxes["b"].Rect, image.Rect(0, 50, 400, 60); got != want {
		t.Fatalf("#b border box = %v, want %v", got, want)
	}
	// Content overflows the capped box rather than being resized.
	if got := boxes["inner"].Rect.Dy(); got != 40 {
		t.Fatalf("inner height = %d, want 40", got)
	}
}

func TestMinMaxHeightClampsSpecifiedHeight(t *testing.T) {
	boxes := percentHeightLayout(t, `<body style="margin:0">
<div id="grow" style="height:20px;min-height:30px;padding:5px"></div>
<div id="cap" style="height:80px;max-height:60px"><div id="half" style="height:50%"></div></div>
<div id="none" style="height:15px;max-height:none"></div></body>`, "grow", "cap", "half", "none")
	if got := boxes["grow"].Content.Dy(); got != 30 {
		t.Fatalf("grow content height = %d, want 30", got)
	}
	if got := boxes["grow"].Rect.Dy(); got != 40 {
		t.Fatalf("grow border-box height = %d, want 40 (min-height is content-box)", got)
	}
	if got := boxes["cap"].Rect.Dy(); got != 60 {
		t.Fatalf("cap height = %d, want 60", got)
	}
	// Children resolve percentages against the clamped used height.
	if got := boxes["half"].Rect.Dy(); got != 30 {
		t.Fatalf("half height = %d, want 30", got)
	}
	if got := boxes["none"].Rect.Dy(); got != 15 {
		t.Fatalf("none height = %d, want 15", got)
	}
}

func TestMinHeightWinsOverMaxHeight(t *testing.T) {
	boxes := percentHeightLayout(t, `<body style="margin:0">
<div id="auto" style="min-height:40px;max-height:20px"></div>
<div id="fixed" style="height:10px;min-height:40px;max-height:20px"></div>
<div id="tall" style="min-height:40px;max-height:20px"><div style="height:90px"></div></div></body>`, "auto", "fixed", "tall")
	for _, id := range []string{"auto", "fixed", "tall"} {
		if got := boxes[id].Rect.Dy(); got != 40 {
			t.Errorf("#%s height = %d, want 40 (min-height wins)", id, got)
		}
	}
}

func TestFlexAutoHeightColumnUsesClampedMainSize(t *testing.T) {
	const source = `<body style="margin:0">
<div id="min" style="display:flex;flex-direction:column;min-height:100px;background:white">
  <div id="grow" style="flex:1;background:red"></div>
</div>
<div id="max" style="display:flex;flex-direction:column;max-height:100px">
  <div id="shrink-a" style="height:80px;background:blue"></div>
  <div id="shrink-b" style="height:80px;background:green"></div>
</div>
<div style="height:200px">
  <div id="percent" style="display:flex;flex-direction:column;min-height:50%">
    <div id="percent-a" style="flex:1;min-height:75%;background:blue"></div>
    <div id="percent-b" style="flex:1;min-height:75%;background:green"></div>
  </div>
</div></body>`
	boxes := percentHeightLayout(t, source, "min", "grow", "max", "shrink-a", "shrink-b", "percent", "percent-a", "percent-b")
	want := map[string]image.Rectangle{
		"min":       image.Rect(0, 0, 400, 100),
		"grow":      image.Rect(0, 0, 400, 100),
		"max":       image.Rect(0, 100, 400, 200),
		"shrink-a":  image.Rect(0, 100, 400, 150),
		"shrink-b":  image.Rect(0, 150, 400, 200),
		"percent":   image.Rect(0, 200, 400, 300),
		"percent-a": image.Rect(0, 200, 400, 250),
		"percent-b": image.Rect(0, 250, 400, 300),
	}
	for id, rect := range want {
		if boxes[id] == nil || boxes[id].Rect != rect {
			t.Errorf("%s: got %v, want %v", id, boxes[id], rect)
		}
	}

	img := painted(t, source, image.Rect(0, 0, 400, 600))
	pixel(t, img, 20, 99, color.RGBA{255, 0, 0, 255})
	pixel(t, img, 20, 100, color.RGBA{0, 0, 255, 255})
	pixel(t, img, 20, 149, color.RGBA{0, 0, 255, 255})
	pixel(t, img, 20, 150, color.RGBA{0, 128, 0, 255})
	pixel(t, img, 20, 199, color.RGBA{0, 128, 0, 255})
	pixel(t, img, 20, 299, color.RGBA{0, 128, 0, 255})
}

func TestMinMaxHeightPercentages(t *testing.T) {
	boxes := percentHeightLayout(t, `<body style="margin:0">
<div style="height:200px">
  <div id="min" style="min-height:25%"></div>
  <div id="max" style="max-height:10%"><div style="height:90px"></div></div>
  <div id="calc" style="min-height:calc(10% + 5px)"></div>
</div>
<div>
  <div id="indefinite-min" style="min-height:50%"><div style="height:12px"></div></div>
  <div id="indefinite-max" style="max-height:1%"><div style="height:12px"></div></div>
  <div id="indefinite-calc" style="max-height:calc(1% + 1px)"><div style="height:12px"></div></div>
</div></body>`, "min", "max", "calc", "indefinite-min", "indefinite-max", "indefinite-calc")
	want := map[string]int{
		"min": 50, "max": 20, "calc": 25,
		// An indefinite basis means min-height:0 and max-height:none.
		"indefinite-min": 12, "indefinite-max": 12, "indefinite-calc": 12,
	}
	for id, h := range want {
		if got := boxes[id].Rect.Dy(); got != h {
			t.Errorf("#%s height = %d, want %d", id, got, h)
		}
	}
}

func TestMinHeightKeepsLastChildMarginInside(t *testing.T) {
	boxes := percentHeightLayout(t, `<body style="margin:0">
<div id="unclamped" style="min-height:10px"><div style="height:20px;margin-bottom:15px"></div></div>
<div id="after1" style="height:1px"></div>
<div id="clamped" style="min-height:50px"><div style="height:20px;margin-bottom:15px"></div></div>
<div id="after2" style="height:1px"></div></body>`, "unclamped", "after1", "clamped", "after2")
	// min-height has no effect: the child's margin collapses through.
	if got := boxes["unclamped"].Rect.Dy(); got != 20 {
		t.Fatalf("unclamped height = %d, want 20", got)
	}
	if got := boxes["after1"].Rect.Min.Y; got != 35 {
		t.Fatalf("after1 top = %d, want 35", got)
	}
	// min-height applies, so the margin stays inside the 50px box.
	if got, want := boxes["clamped"].Rect, image.Rect(0, 36, 400, 86); got != want {
		t.Fatalf("clamped border box = %v, want %v", got, want)
	}
	if got := boxes["after2"].Rect.Min.Y; got != 86 {
		t.Fatalf("after2 top = %d, want 86", got)
	}
}

func TestPaintMinMaxHeightBackgrounds(t *testing.T) {
	img := painted(t, `<body style="margin:0"><div style="min-height:50px;background:green"></div>
<div style="max-height:10px;background:blue;overflow:hidden"><div style="height:40px;background:red"></div></div></body>`,
		image.Rect(0, 0, 100, 100))
	green := color.RGBA{0, 128, 0, 255}
	red := color.RGBA{255, 0, 0, 255}
	white := color.RGBA{255, 255, 255, 255}
	pixel(t, img, 50, 0, green)
	pixel(t, img, 50, 49, green)
	pixel(t, img, 50, 50, red)
	pixel(t, img, 50, 59, red)
	pixel(t, img, 50, 60, white)
}

// A 40x20 SVG data URL gives replaced elements a non-square intrinsic ratio.
const wideTestImage = `data:image/svg+xml;utf8,<svg xmlns='http://www.w3.org/2000/svg' width='40' height='20'><rect width='40' height='20' fill='%23008000'/></svg>`

// Review finding on #444: blockified replaced elements are laid out by
// layoutReplacedBlock and must honour min-height/max-height too.
func TestMinMaxHeightBlockReplaced(t *testing.T) {
	img := func(id, style string) string {
		return `<img id="` + id + `" src="` + wideTestImage + `" style="display:block;` + style + `">`
	}
	boxes := percentHeightLayout(t, `<body style="margin:0">`+
		img("placeholder", "min-height:100px")+
		img("grow", "min-height:50px")+
		img("cap", "max-height:10px")+
		img("fixed-width", "width:80px;max-height:30px")+
		img("both", "width:30px;height:30px;min-height:45px")+
		img("height-clamped", "height:60px;max-height:25px;padding:2px")+
		img("conflict", "min-height:40px;max-height:10px")+
		img("none", "max-height:none")+
		`<div style="height:200px">`+img("pct", "min-height:25%")+`</div>`+
		`<div>`+img("pct-indefinite", "min-height:50%;max-height:1%")+`</div>`+
		`<div id="after" style="height:1px"></div></body>`,
		"placeholder", "grow", "cap", "fixed-width", "both", "height-clamped", "conflict", "none", "pct", "pct-indefinite", "after")
	want := map[string]image.Point{
		"placeholder":    {200, 100}, // 40x20 image, width follows the ratio
		"grow":           {100, 50},
		"cap":            {20, 10},
		"fixed-width":    {80, 30}, // explicit width keeps; only height clamps
		"both":           {30, 45},
		"height-clamped": {50, 25},
		"conflict":       {80, 40}, // min-height wins
		"none":           {40, 20},
		"pct":            {100, 50},
		"pct-indefinite": {40, 20}, // indefinite basis: min 0, max none
	}
	for id, size := range want {
		if got := boxes[id].Content.Size(); got != size {
			t.Errorf("#%s content size = %v, want %v", id, got, size)
		}
	}
	if got, want := boxes["height-clamped"].Rect.Dy(), 29; got != want {
		t.Errorf("height-clamped border-box height = %d, want %d (content-box)", got, want)
	}
	// Following flow starts below the clamped replaced boxes.
	if got, want := boxes["after"].Rect.Min.Y, boxes["pct-indefinite"].Rect.Max.Y; got != want {
		t.Errorf("after top = %d, want %d", got, want)
	}
}

func TestMinMaxHeightBlockReplacedPercentageHeight(t *testing.T) {
	img := func(id, style string) string {
		return `<img id="` + id + `" src="` + wideTestImage + `" style="display:block;` + style + `">`
	}
	boxes := percentHeightLayout(t, `<body style="margin:0">
<div style="height:200px">`+
		img("specified", "height:50%")+
		img("min", "height:50%;min-height:125px")+
		img("max", "height:50%;max-height:75px")+
		`</div><div>`+img("indefinite", "height:50%")+`</div><div style="height:0px">`+
		img("zero", "height:50%")+`</div></body>`,
		"specified", "min", "max", "indefinite", "zero")
	want := map[string]image.Point{
		"specified":  {200, 100},
		"min":        {250, 125},
		"max":        {150, 75},
		"indefinite": {40, 20}, // percentage height is auto with an indefinite basis
		"zero":       {0, 0},   // zero is still a definite percentage basis
	}
	for id, size := range want {
		if got := boxes[id].Content.Size(); got != size {
			t.Errorf("#%s content size = %v, want %v", id, got, size)
		}
	}
}

func TestPaintMinHeightBlockReplaced(t *testing.T) {
	img := painted(t, `<body style="margin:0"><img src="`+wideTestImage+`" style="display:block;min-height:50px">
<div style="height:10px;background:blue"></div></body>`, image.Rect(0, 0, 150, 100))
	green := color.RGBA{0, 128, 0, 255}
	blue := color.RGBA{0, 0, 255, 255}
	white := color.RGBA{255, 255, 255, 255}
	pixel(t, img, 5, 5, green)
	pixel(t, img, 95, 45, green)
	pixel(t, img, 105, 25, white)
	pixel(t, img, 50, 55, blue)
}

func TestPaintPercentageHeightBlockReplaced(t *testing.T) {
	img := painted(t, `<body style="margin:0"><div style="height:200px"><img src="`+wideTestImage+`" style="display:block;height:50%"></div></body>`,
		image.Rect(0, 0, 400, 200))
	green := color.RGBA{0, 128, 0, 255}
	white := color.RGBA{255, 255, 255, 255}
	pixel(t, img, 199, 99, green)
	pixel(t, img, 200, 99, white)
	pixel(t, img, 50, 100, white)
}
