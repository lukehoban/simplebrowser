package browser

import (
	"image"
	"image/color"
	"testing"
)

// The #68 repro: the first paragraph flows beside the float, and the cleared
// one starts below it.
func TestClearLeftMovesBlockBelowFloat(t *testing.T) {
	const source = `<body style="margin:0">
	<div id="float" style="float:left;width:32px;height:32px;background:red"></div>
	<p id="flow" style="margin:0;height:16px;background:green">Should flow beside the float.</p>
	<p id="clear" style="clear:left;margin:0;height:16px;background:blue">Should clear the float.</p>
	</body>`
	layout, err := LayoutWithViewport(styledForLayout(t, source), image.Rect(0, 0, 320, 120))
	if err != nil {
		t.Fatal(err)
	}
	boxes := boxesByID(layout.Root, "flow", "clear")
	// boxesByID may return a paragraph's line box, so compare top edges.
	if boxes["flow"].Rect.Min.Y != 0 || boxes["clear"].Rect.Min.Y != 32 {
		t.Fatalf("flow %v clear %v", boxes["flow"].Rect, boxes["clear"].Rect)
	}
	img := painted(t, source, image.Rect(0, 0, 320, 120))
	pixel(t, img, 310, 34, color.RGBA{0, 0, 255, 255})
	pixel(t, img, 310, 8, color.RGBA{0, 128, 0, 255})
	pixel(t, img, 1, 20, color.RGBA{255, 0, 0, 255})
}

// Clearance is computed from the hypothetical collapsed position and, once
// introduced, separates the cleared block's top margin from the preceding
// margins. A block already below the floats gets no clearance.
func TestClearanceAndMarginCollapse(t *testing.T) {
	const source = `<body style="margin:0"><div style="display:flow-root;width:300px">
	<div style="float:left;width:40px;height:60px"></div>
	<div style="float:right;width:40px;height:30px"></div>
	<div id="a" style="height:10px;margin-bottom:20px"></div>
	<div id="right" style="clear:right;margin-top:10px;height:10px"></div>
	<div id="both" style="clear:both;margin-top:5px;height:10px;margin-bottom:10px"></div>
	<div id="after" style="margin-top:4px;height:10px"></div>
	<div id="none" style="clear:none;height:10px"></div>
	</div></body>`
	layout, err := LayoutWithViewport(styledForLayout(t, source), image.Rect(0, 0, 320, 200))
	if err != nil {
		t.Fatal(err)
	}
	boxes := boxesByID(layout.Root, "right", "both", "after", "none")
	for id, y := range map[string]int{"right": 30, "both": 60, "after": 80, "none": 90} {
		if boxes[id].Rect.Min.Y != y {
			t.Errorf("%s top = %d, want %d (%v)", id, boxes[id].Rect.Min.Y, y, boxes[id].Rect)
		}
	}
}

func TestClearOnlyAppliesToNamedSide(t *testing.T) {
	layout, err := LayoutWithViewport(styledForLayout(t, `<body style="margin:0"><div style="display:flow-root;width:300px">
	<div style="float:right;width:40px;height:50px"></div>
	<div id="left" style="clear:left;height:10px"></div>
	<div id="right" style="clear:right;height:10px"></div>
	</div></body>`), image.Rect(0, 0, 320, 200))
	if err != nil {
		t.Fatal(err)
	}
	boxes := boxesByID(layout.Root, "left", "right")
	if boxes["left"].Rect.Min.Y != 0 || boxes["right"].Rect.Min.Y != 50 {
		t.Fatalf("left %v right %v", boxes["left"].Rect, boxes["right"].Rect)
	}
}

func TestClearedFloatAndLineWrapping(t *testing.T) {
	layout, err := LayoutWithViewport(styledForLayout(t, `<body style="margin:0"><div style="display:flow-root;width:300px">
	<div id="a" style="float:left;width:50px;height:40px"></div>
	<div id="b" style="float:left;clear:left;width:60px;height:20px;margin-top:5px"></div>
	<span>alpha beta gamma</span>
	<div id="c" style="clear:both;height:10px"></div>
	</div></body>`), image.Rect(0, 0, 320, 200))
	if err != nil {
		t.Fatal(err)
	}
	boxes := boxesByID(layout.Root, "a", "b", "c")
	if boxes["b"].Rect != image.Rect(0, 45, 60, 65) {
		t.Fatalf("cleared float %v", boxes["b"].Rect)
	}
	if boxes["c"].Rect.Min.Y != 65 {
		t.Fatalf("clear:both block %v", boxes["c"].Rect)
	}
	// Inline content still wraps beside the first float (no clear on text).
	var visit func(*Box)
	found := false
	visit = func(b *Box) {
		for _, run := range b.Text {
			if run.Rect.Min.Y < 40 {
				found = true
				if run.Rect.Min.X < 50 {
					t.Errorf("run %q overlaps float: %v", run.Text, run.Rect)
				}
			}
		}
		for _, child := range b.Children {
			visit(child)
		}
	}
	visit(layout.Root)
	if !found {
		t.Fatal("wanted text beside the first float")
	}
}

func TestClearAppliesToTablesAndBFCRoots(t *testing.T) {
	layout, err := LayoutWithViewport(styledForLayout(t, `<body style="margin:0"><div style="display:flow-root;width:300px">
	<div style="float:left;width:40px;height:30px"></div>
	<table id="table" style="clear:left;border-spacing:0"><tr><td style="padding:0">x</td></tr></table>
	<div style="float:left;width:40px;height:30px"></div>
	<div id="root" style="clear:both;overflow:hidden;height:10px"></div>
	</div></body>`), image.Rect(0, 0, 320, 200))
	if err != nil {
		t.Fatal(err)
	}
	boxes := boxesByID(layout.Root, "table", "root")
	if boxes["table"].Rect.Min.Y != 30 || boxes["table"].Rect.Min.X != 0 {
		t.Fatalf("table %v", boxes["table"].Rect)
	}
	if boxes["root"].Rect.Min.Y < boxes["table"].Rect.Max.Y+30 || boxes["root"].Rect.Min.X != 0 {
		t.Fatalf("table %v root %v", boxes["table"].Rect, boxes["root"].Rect)
	}
}
