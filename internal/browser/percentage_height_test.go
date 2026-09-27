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

func TestPercentageHeightInlineBlockContainingBlocks(t *testing.T) {
	for _, tc := range []struct {
		name, parentHeight, boxHeight string
		wantBox, wantChild            int
	}{
		{"definite percentage chain", "200px", "50%", 100, 50},
		{"explicit inline-block height", "auto", "100px", 100, 50},
		{"indefinite percentage chain", "auto", "50%", 20, 20},
		{"auto interrupts definite chain", "200px", "auto", 20, 20},
		{"definite zero", "0px", "50%", 0, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			boxes := percentHeightLayout(t, `<body style="margin:0"><section style="height:`+tc.parentHeight+`">
<span><div id="atomic" style="display:inline-block;width:80px;height:`+tc.boxHeight+`;padding:5px">
<div id="child" style="height:50%"><div style="height:20px">text</div></div>
</div></span></section></body>`, "atomic", "child")
			if got := boxes["atomic"].Content.Dy(); got != tc.wantBox {
				t.Errorf("inline-block content height = %d, want %d", got, tc.wantBox)
			}
			if got := boxes["child"].Content.Dy(); got != tc.wantChild {
				t.Errorf("child height = %d, want %d", got, tc.wantChild)
			}
		})
	}
}

func TestPercentageHeightEmptyInlineBlock(t *testing.T) {
	for _, tc := range []struct {
		parentHeight string
		want         int
	}{{"200px", 100}, {"auto", 0}, {"0px", 0}} {
		t.Run(tc.parentHeight, func(t *testing.T) {
			boxes := percentHeightLayout(t, `<body style="margin:0"><section style="height:`+tc.parentHeight+`">
<span id="empty" style="display:inline-block;width:40px;height:50%;padding:5px"></span>
</section></body>`, "empty")
			if got := boxes["empty"].Content.Dy(); got != tc.want {
				t.Fatalf("empty inline-block content height = %d, want %d", got, tc.want)
			}
		})
	}
}

func TestPercentageHeightNestedInlineBlockPaint(t *testing.T) {
	const markup = `<body style="margin:0"><section style="height:200px;line-height:0">` +
		`<span id="outer" style="display:inline-block;width:80px;height:50%;background:blue">` +
		`<span id="inner" style="display:inline-block;width:40px;height:50%;background:yellow"></span>` +
		`</span></section></body>`
	boxes := percentHeightLayout(t, markup, "outer", "inner")
	if boxes["outer"].Content.Dy() != 100 || boxes["inner"].Content.Dy() != 50 {
		t.Fatalf("nested inline-block heights = %d, %d; want 100, 50",
			boxes["outer"].Content.Dy(), boxes["inner"].Content.Dy())
	}
	img := painted(t, markup, image.Rect(0, 0, 400, 400))
	outer, inner := boxes["outer"].Content, boxes["inner"].Content
	pixel(t, img, inner.Min.X+1, inner.Min.Y+1, color.RGBA{255, 255, 0, 255})
	pixel(t, img, inner.Min.X+1, inner.Max.Y-1, color.RGBA{255, 255, 0, 255})
	pixel(t, img, outer.Max.X-1, outer.Max.Y-1, color.RGBA{0, 0, 255, 255})
}

func TestPercentageHeightInlineBlockPositionedChildAndBaseline(t *testing.T) {
	const markup = `<body style="margin:0"><section style="height:200px;line-height:20px">before` +
		`<div id="atomic" style="display:inline-block;position:relative;width:100px;height:50%;padding:5px">` +
		`first<br>last<div id="abs" style="position:absolute;left:0;top:0;width:10px;height:50%">` +
		`<div id="nested" style="height:50%"></div><div>out</div></div></div>after</section></body>`
	layout, err := LayoutWithViewport(styledForLayout(t, markup), image.Rect(0, 0, 400, 400))
	if err != nil {
		t.Fatal(err)
	}
	boxes := boxesByID(layout.Root, "atomic", "abs", "nested")
	if got := boxes["atomic"].Content.Dy(); got != 100 {
		t.Fatalf("inline-block height = %d, want 100", got)
	}
	if got := boxes["abs"].Content.Dy(); got != 55 {
		t.Fatalf("absolute height = %d, want half of 110px padding box", got)
	}
	if got := boxes["nested"].Content.Dy(); got != 27 {
		t.Fatalf("nested height = %d, want 27", got)
	}
	if boxes["abs"].Rect.Min != boxes["atomic"].Rect.Min {
		t.Errorf("positioned child origin = %v, want %v", boxes["abs"].Rect.Min, boxes["atomic"].Rect.Min)
	}
	runs := map[string]TextRun{}
	var collect func(*Box)
	collect = func(b *Box) {
		for _, run := range b.Text {
			runs[run.Text] = run
		}
		for _, child := range b.Children {
			collect(child)
		}
	}
	collect(layout.Root)
	for _, text := range []string{"before", "last", "after"} {
		if _, ok := runs[text]; !ok {
			t.Fatalf("missing text run %q", text)
		}
	}
	if runs["before"].Rect.Min.Y != runs["last"].Rect.Min.Y ||
		runs["after"].Rect.Min.Y != runs["last"].Rect.Min.Y {
		t.Fatalf("last in-flow line must supply baseline, ignoring positioned text: %v", runs)
	}
}
