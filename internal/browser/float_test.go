package browser

import (
	"image"
	"image/color"
	"testing"
)

func TestFloatsPlaceAtEdgesAndWrapLines(t *testing.T) {
	const source = `<body style="margin:0"><div style="width:300px;display:flow-root">
	<div id="left" style="float:left;width:70px;height:40px;background:red;margin-right:10px"></div>
	<div id="right" style="float:right;width:60px;height:40px;background:blue;margin-left:10px"></div>
	<span>one two three four five six seven eight nine ten eleven twelve thirteen fourteen fifteen sixteen seventeen eighteen nineteen twenty</span>
	</div></body>`
	layout, err := LayoutWithViewport(styledForLayout(t, source), image.Rect(0, 0, 320, 160))
	if err != nil {
		t.Fatal(err)
	}
	boxes := boxesByID(layout.Root, "left", "right")
	if boxes["left"].Rect != image.Rect(0, 0, 70, 40) || boxes["right"].Rect != image.Rect(240, 0, 300, 40) {
		t.Fatalf("float rectangles: left %v right %v", boxes["left"].Rect, boxes["right"].Rect)
	}
	var before, after bool
	var visit func(*Box)
	visit = func(b *Box) {
		for _, run := range b.Text {
			if run.Rect.Min.Y < 40 {
				before = true
				if run.Rect.Min.X < 80 || run.Rect.Max.X > 230 {
					t.Errorf("run %q overlaps float margin boxes: %v", run.Text, run.Rect)
				}
			} else {
				after = true
			}
		}
		for _, child := range b.Children {
			visit(child)
		}
	}
	visit(layout.Root)
	if !before || !after {
		t.Fatalf("wanted lines beside and below floats; before=%v after=%v", before, after)
	}
	img := painted(t, source, image.Rect(0, 0, 320, 160))
	pixel(t, img, 1, 1, color.RGBA{255, 0, 0, 255})
	pixel(t, img, 299, 1, color.RGBA{0, 0, 255, 255})
}

func TestFloatTableAndBFCAvoidance(t *testing.T) {
	const source = `<body style="margin:0"><div style="width:300px;display:flow-root">
	<table id="table" style="float:right;width:100px;height:50px;background:blue;border-spacing:0"><tr><td>x</td></tr></table>
	<div id="avoid" style="overflow:hidden;height:20px;background:red">content</div>
	<div id="wide" style="overflow:hidden;width:250px;height:10px;background:green">below</div>
	</div></body>`
	layout, err := LayoutWithViewport(styledForLayout(t, source), image.Rect(0, 0, 320, 160))
	if err != nil {
		t.Fatal(err)
	}
	boxes := boxesByID(layout.Root, "table", "avoid", "wide")
	if boxes["table"].Rect.Min.X != 200 || boxes["avoid"].Rect.Max.X > 200 ||
		boxes["wide"].Rect.Min.Y < boxes["table"].Rect.Max.Y {
		t.Fatalf("table %v, BFC %v, wide BFC %v", boxes["table"].Rect, boxes["avoid"].Rect, boxes["wide"].Rect)
	}
}

func TestFloatContainingBFCExtendsToFloatBottom(t *testing.T) {
	layout, err := LayoutWithViewport(styledForLayout(t,
		`<body style="margin:0"><div id="context" style="display:flow-root;width:200px"><div style="float:left;width:40px;height:60px"></div></div><div id="next" style="height:10px"></div></body>`),
		image.Rect(0, 0, 300, 140))
	if err != nil {
		t.Fatal(err)
	}
	boxes := boxesByID(layout.Root, "context", "next")
	if boxes["context"].Rect.Dy() != 60 || boxes["next"].Rect.Min.Y != 60 {
		t.Fatalf("context %v next %v", boxes["context"].Rect, boxes["next"].Rect)
	}
}
