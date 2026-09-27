package browser

import (
	"image"
	"image/color"
	"testing"
)

func percentHeightLayout(t *testing.T, markup string, ids ...string) map[string]*Box {
	t.Helper()
	got, err := LayoutWithViewport(styledForLayout(t, markup), image.Rect(0, 0, 400, 400))
	if err != nil {
		t.Fatal(err)
	}
	return boxesByID(got.Root, ids...)
}

// Issue #69 repro: body has an auto height, so the outer div's 50% height
// computes to auto and the box grows to its 100px child.
func TestPercentageHeightWithIndefiniteContainingBlockIsAuto(t *testing.T) {
	boxes := percentHeightLayout(t, `<style>body{margin:0} div div{height:100px}</style>
<body><div id="outer" style="height:50%"><div></div></div></body>`, "outer")
	if got, want := boxes["outer"].Rect, image.Rect(0, 0, 400, 100); got != want {
		t.Fatalf("outer border box = %v, want %v", got, want)
	}
}

func TestPercentageHeightResolvesAgainstDefiniteContainingBlock(t *testing.T) {
	boxes := percentHeightLayout(t, `<body style="margin:0">
<div id="parent" style="height:200px;padding:10px">
  <div id="half" style="height:50%;padding:5px"><div id="quarter" style="height:50%"></div></div>
</div></body>`, "parent", "half", "quarter")
	// Percentages resolve against the containing block's content height,
	// not its padding box and not the box's own content.
	if got := boxes["half"].Content.Dy(); got != 100 {
		t.Fatalf("half content height = %d, want 100", got)
	}
	if got := boxes["half"].Rect.Dy(); got != 110 {
		t.Fatalf("half border-box height = %d, want 110", got)
	}
	if got := boxes["quarter"].Rect.Dy(); got != 50 {
		t.Fatalf("quarter height = %d, want 50 (nested definite chain)", got)
	}
	if got := boxes["parent"].Content.Dy(); got != 200 {
		t.Fatalf("parent content height = %d, want 200", got)
	}
}

func TestPercentageHeightChainFromViewport(t *testing.T) {
	boxes := percentHeightLayout(t, `<html id="root" style="height:100%"><body id="body" style="margin:0;height:50%">
<div id="child" style="height:25%"></div></body></html>`, "root", "body", "child")
	if got := boxes["root"].Rect.Dy(); got != 400 {
		t.Fatalf("html height = %d, want viewport 400", got)
	}
	if got := boxes["body"].Rect.Dy(); got != 200 {
		t.Fatalf("body height = %d, want 200", got)
	}
	if got := boxes["child"].Rect.Dy(); got != 50 {
		t.Fatalf("child height = %d, want 50", got)
	}
}

func TestPercentageHeightInsideAutoParentOfDefiniteGrandparentIsAuto(t *testing.T) {
	boxes := percentHeightLayout(t, `<body style="margin:0"><div style="height:300px">
<div id="auto"><div id="pct" style="height:50%"><div style="height:20px"></div></div></div>
</div></body>`, "pct")
	if got := boxes["pct"].Rect.Dy(); got != 20 {
		t.Fatalf("pct height = %d, want 20 (auto parent makes it indefinite)", got)
	}
}

func TestAbsolutePercentageHeightUsesContainingBlockPaddingBox(t *testing.T) {
	boxes := percentHeightLayout(t, `<body style="margin:0">
<div style="position:relative;height:100px;padding:10px">
  <div id="abs" style="position:absolute;top:0;left:0;width:10px;height:50%">
    <div id="inner" style="height:50%"></div>
  </div>
</div></body>`, "abs", "inner")
	if got := boxes["abs"].Rect.Dy(); got != 60 {
		t.Fatalf("abs height = %d, want 60 (50%% of 120px padding box)", got)
	}
	if got := boxes["inner"].Rect.Dy(); got != 30 {
		t.Fatalf("inner height = %d, want 30", got)
	}
}

func TestPercentageHeightPaint(t *testing.T) {
	img := painted(t, `<body style="margin:0">
<div style="height:50%;background:#f00"><div style="height:40px;width:40px;background:#0f0"></div></div>
<div style="height:100px;background:#00f"><div style="height:50%;width:40px;background:#ff0"></div></div>
</body>`, image.Rect(0, 0, 200, 300))
	red := color.RGBA{255, 0, 0, 255}
	blue := color.RGBA{0, 0, 255, 255}
	yellow := color.RGBA{255, 255, 0, 255}
	pixel(t, img, 100, 20, red)  // indefinite 50% box is 40px (auto)
	pixel(t, img, 100, 45, blue) // next sibling starts at y=40, not 150
	pixel(t, img, 20, 60, yellow)
	pixel(t, img, 20, 100, blue) // yellow is 50px, not the parent's 100px
}
