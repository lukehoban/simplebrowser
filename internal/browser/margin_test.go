package browser

import (
	"image"
	"testing"
)

// divRects lays out source in a 400px viewport and returns the border-box
// top and bottom of each div in document order.
func divRects(t *testing.T, source string) []image.Rectangle {
	t.Helper()
	got, err := LayoutWithViewport(styledForLayout(t, source), image.Rect(0, 0, 400, 400))
	if err != nil {
		t.Fatal(err)
	}
	var rects []image.Rectangle
	for _, b := range collectBoxes(got.Root, "div") {
		rects = append(rects, b.Rect)
	}
	return rects
}

func wantTops(t *testing.T, rects []image.Rectangle, tops ...int) {
	t.Helper()
	if len(rects) != len(tops) {
		t.Fatalf("got %d divs %v, want %d", len(rects), rects, len(tops))
	}
	for i, top := range tops {
		if rects[i].Min.Y != top {
			t.Fatalf("div %d top = %d, want %d (rects %v)", i, rects[i].Min.Y, top, rects)
		}
	}
}

func TestCollapsedMarginArithmetic(t *testing.T) {
	for _, tc := range []struct {
		margins []int
		want    int
	}{
		{[]int{10, 20}, 20},
		{[]int{20, -5}, 15},
		{[]int{-4, -9}, -9},
		{[]int{0, 1}, 1},
		{[]int{12, -3, 7, -8}, 4},
	} {
		m := collapsedMargin{}
		for _, v := range tc.margins {
			m = m.add(v)
		}
		if m.value() != tc.want {
			t.Errorf("collapse%v = %d, want %d", tc.margins, m.value(), tc.want)
		}
	}
}

func TestSiblingMarginsCollapse(t *testing.T) {
	const body = `<body style="margin:0"><div style="border:1px solid black">`
	for _, tc := range []struct {
		name   string
		first  string
		second string
		top    int
	}{
		// The outer border box starts at 0; its content starts at 1.
		{"positive", "margin-bottom:10px", "margin-top:20px", 1 + 10 + 20},
		{"mixed", "margin-bottom:20px", "margin-top:-5px", 1 + 10 + 15},
		{"negative", "margin-bottom:-4px", "margin-top:-9px", 1 + 10 - 9},
		{"one-sided", "", "margin-top:1px", 1 + 10 + 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rects := divRects(t, body+`<div style="height:10px;`+tc.first+`"></div><div style="height:10px;`+tc.second+`"></div></div>`)
			wantTops(t, rects, 0, 1, tc.top)
		})
	}
}

func TestBordersAndPaddingSeparateMargins(t *testing.T) {
	// A border on the parent keeps the first child's margin inside it.
	rects := divRects(t, `<body style="margin:0"><div style="border-top:2px solid black"><div style="margin-top:10px;height:5px"></div></div>`)
	wantTops(t, rects, 0, 12)
	// Padding does the same.
	rects = divRects(t, `<body style="margin:0"><div style="padding-top:3px"><div style="margin-top:10px;height:5px"></div></div>`)
	wantTops(t, rects, 0, 13)
	// Bottom padding keeps the last child's bottom margin inside the parent.
	rects = divRects(t, `<body style="margin:0"><div style="padding-bottom:1px"><div style="margin-bottom:10px;height:5px"></div></div><div style="height:5px"></div>`)
	wantTops(t, rects, 0, 0, 16)
}

func TestFirstAndLastChildMarginsCollapseThroughParent(t *testing.T) {
	// WPT block-formatting-contexts-003: the first child's 1px top margin
	// collapses with the parent's (and the preceding paragraph's) margins, so
	// no parent background shows above it; later siblings stay 1px apart.
	rects := divRects(t, `<body style="margin:0"><p style="margin:0 0 16px;height:10px"></p>`+
		`<div id="outer"><div style="margin-top:1px;height:20px"></div><div style="margin-top:1px;height:20px"></div></div>`)
	wantTops(t, rects, 26, 26, 47)
	if rects[0].Max.Y != 67 {
		t.Fatalf("outer bottom = %d, want 67", rects[0].Max.Y)
	}
	// The last child's bottom margin escapes its parent and collapses with
	// the next sibling's top margin.
	rects = divRects(t, `<body style="margin:0"><div><div style="margin-bottom:12px;height:5px"></div></div><div style="margin-top:4px;height:5px"></div>`)
	wantTops(t, rects, 0, 0, 17)
	if rects[0].Max.Y != 5 {
		t.Fatalf("parent bottom = %d, want 5", rects[0].Max.Y)
	}
}

func TestFormattingContextsAndInlineContentBlockCollapse(t *testing.T) {
	// overflow:hidden establishes a block formatting context.
	rects := divRects(t, `<body style="margin:0"><div style="overflow:hidden"><div style="margin-top:10px;height:5px"></div></div>`)
	wantTops(t, rects, 0, 10)
	// A line box between blocks separates their margins.
	got, err := LayoutWithViewport(styledForLayout(t, `<body style="margin:0"><div style="margin-bottom:10px;height:5px"></div>text<div style="margin-top:10px;height:5px"></div>`), image.Rect(0, 0, 400, 400))
	if err != nil {
		t.Fatal(err)
	}
	divs := collectBoxes(got.Root, "div")
	body := collectBoxes(got.Root, "body")[0]
	var line *Box
	for _, c := range body.Children {
		if c.Node == body.Node {
			line = c
		}
	}
	if line == nil || line.Rect.Min.Y != 15 || divs[1].Rect.Min.Y != line.Rect.Max.Y+10 {
		t.Fatalf("line %v, divs %v", line, divs)
	}
	// Whitespace between blocks does not.
	rects = divRects(t, "<body style=\"margin:0\"><div style=\"margin-bottom:10px;height:5px\"></div>\n  <div style=\"margin-top:10px;height:5px\"></div>")
	wantTops(t, rects, 0, 15)
}

func TestTableCellMarginsStayInsideCell(t *testing.T) {
	got, err := LayoutWithViewport(styledForLayout(t, `<body style="margin:0"><table cellspacing="0" cellpadding="0"><tr><td><div style="margin:4px 0 6px;height:10px"></div></td></tr></table>`), image.Rect(0, 0, 400, 400))
	if err != nil {
		t.Fatal(err)
	}
	td := collectBoxes(got.Root, "td")[0]
	div := collectBoxes(got.Root, "div")[0]
	if div.Rect.Min.Y != td.Rect.Min.Y+4 || td.Rect.Dy() != 20 {
		t.Fatalf("td %v div %v", td.Rect, div.Rect)
	}
}
