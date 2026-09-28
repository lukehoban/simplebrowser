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
