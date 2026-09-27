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

// A shrink-to-fit box sizes to its max-content width, in which consecutive
// floats sit side by side rather than stacking (#349). A clearing float
// starts a new line, so it contributes only its own width.
func TestShrinkToFitWidthSumsConsecutiveFloats(t *testing.T) {
	const source = `<body style="margin:0">
	<div id="row" style="float:right"><div id="a" style="float:left;width:90px;height:20px;margin-right:8px"></div><div id="b" style="float:left;width:40px;height:20px;margin-right:8px"></div><div id="c" style="float:left;width:30px;height:20px"></div></div>
	<div id="cleared" style="float:left"><div style="float:left;width:90px;height:20px"></div><div style="float:left;clear:left;width:40px;height:20px"></div></div>
	</body>`
	layout, err := LayoutWithViewport(styledForLayout(t, source), image.Rect(0, 0, 400, 100))
	if err != nil {
		t.Fatal(err)
	}
	boxes := boxesByID(layout.Root, "row", "a", "b", "c", "cleared")
	if got := boxes["row"].Rect; got != image.Rect(224, 0, 400, 20) {
		t.Errorf("float row %v, want 176px wide and one float tall at the right edge", got)
	}
	if boxes["a"].Rect.Min.Y != 0 || boxes["b"].Rect.Min.X != 322 || boxes["c"].Rect != image.Rect(370, 0, 400, 20) {
		t.Errorf("inner floats should share one line: a %v b %v c %v", boxes["a"].Rect, boxes["b"].Rect, boxes["c"].Rect)
	}
	if got := boxes["cleared"].Rect; got != image.Rect(0, 0, 90, 40) {
		t.Errorf("clearing float container %v, want 90x40", got)
	}
}

// Flex items establish independent formatting contexts: they contain their
// floats, and a stretch re-layout must not see floats from the first pass
// (#349). Column items are also measured by laying them out.
func TestFlexItemsContainTheirFloats(t *testing.T) {
	const source = `<body style="margin:0">
	<div id="row" style="display:flex"><div id="item"><div id="float" style="float:left;width:50px;height:30px"></div></div><div id="tall" style="width:10px;height:40px"></div></div>
	<div id="column" style="display:flex;flex-direction:column"><div id="citem"><div id="cfloat" style="float:right;width:20px;height:25px"></div></div></div>
	</body>`
	layout, err := LayoutWithViewport(styledForLayout(t, source), image.Rect(0, 0, 400, 200))
	if err != nil {
		t.Fatal(err)
	}
	boxes := boxesByID(layout.Root, "row", "item", "float", "column", "citem", "cfloat")
	if got := boxes["float"].Rect; got != image.Rect(0, 0, 50, 30) {
		t.Errorf("float in stretched flex item %v, want at the item's top", got)
	}
	if got := boxes["item"].Rect; got != image.Rect(0, 0, 50, 40) {
		t.Errorf("stretched flex item %v, want 50x40", got)
	}
	if got := boxes["row"].Rect; got.Dy() != 40 {
		t.Errorf("row container %v, want 40px tall", got)
	}
	if got := boxes["citem"].Rect; got != image.Rect(0, 40, 400, 65) {
		t.Errorf("column flex item %v, want to contain its 25px float", got)
	}
	if got := boxes["cfloat"].Rect; got != image.Rect(380, 40, 400, 65) {
		t.Errorf("float in column flex item %v", got)
	}
}
