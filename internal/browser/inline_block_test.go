package browser

import (
	"image"
	"image/color"
	"testing"
)

var grey = color.RGBA{128, 128, 128, 255}

// Combining inline text backgrounds (#197) and empty inline-blocks (#192)
// must not paint an ancestor's line fragment over the atomic box.
func TestEmptyInlineBlockAboveInlineBackground(t *testing.T) {
	const markup = `<body style="margin:0"><div><span style="background:yellow">before` +
		`<span style="display:inline-block;width:20px;height:10px;background:grey;border:2px solid blue"></span>` +
		`after</span></div></body>`
	viewport := image.Rect(0, 0, 400, 100)
	got, err := LayoutWithViewport(styledForLayout(t, markup), viewport)
	if err != nil {
		t.Fatal(err)
	}
	spans := collectBoxes(got.Root, "span")
	if len(spans) != 1 {
		t.Fatalf("got %d span boxes, want one atomic box", len(spans))
	}
	var fragments []InlineBackground
	var collect func(*Box)
	collect = func(b *Box) {
		fragments = append(fragments, b.InlineBackgrounds...)
		for _, child := range b.Children {
			collect(child)
		}
	}
	collect(got.Root)
	if len(fragments) != 1 {
		t.Fatalf("got %d background fragments, want only the text ancestor", len(fragments))
	}
	box, fragment := spans[0], fragments[0].Rect
	if fragment.Min.X >= box.Rect.Min.X || fragment.Max.X <= box.Rect.Max.X {
		t.Fatalf("ancestor background %v must span the box %v", fragment, box.Rect)
	}
	img := painted(t, markup, viewport)
	pixel(t, img, box.Content.Min.X, box.Content.Min.Y, grey)
	pixel(t, img, box.Rect.Min.X, box.Rect.Min.Y, color.RGBA{0, 0, 255, 255})
	pixel(t, img, fragment.Min.X, fragment.Max.Y-1, color.RGBA{255, 255, 0, 255})
	pixel(t, img, fragment.Max.X-1, fragment.Max.Y-1, color.RGBA{255, 255, 0, 255})
}

func spanRects(t *testing.T, source string) []image.Rectangle {
	t.Helper()
	got, err := LayoutWithViewport(styledForLayout(t, source), image.Rect(0, 0, 400, 400))
	if err != nil {
		t.Fatal(err)
	}
	var rects []image.Rectangle
	for _, b := range collectBoxes(got.Root, "span") {
		rects = append(rects, b.Rect)
	}
	return rects
}

// An empty inline-block with explicit dimensions generates a box of that size
// on a line of its own parent, instead of disappearing (#192).
func TestEmptyInlineBlockGeneratesSizedBox(t *testing.T) {
	for _, tc := range []struct {
		name, divStyle string
		want           image.Rectangle
		divHeight      int
	}{
		// Without line-height the strut still sets the line box height, and
		// the inline-block's bottom margin edge sits on the baseline.
		{"strut", "background:yellow", image.Rect(0, 6, 10, 16), 20},
		// With line-height: 0 the box alone determines the line height.
		{"line-height zero", "background:yellow;line-height:0", image.Rect(0, 0, 10, 10), 10},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rects := spanRects(t, `<body style="margin:0"><div style="`+tc.divStyle+
				`"><span style="display:inline-block;width:10px;height:10px;background:grey"></span></div></body>`)
			if len(rects) != 1 {
				t.Fatalf("got %d span boxes %v, want 1", len(rects), rects)
			}
			if rects[0] != tc.want {
				t.Errorf("span rect = %v, want %v", rects[0], tc.want)
			}
			divs := divRects(t, `<body style="margin:0"><div style="`+tc.divStyle+
				`"><span style="display:inline-block;width:10px;height:10px;background:grey"></span></div></body>`)
			if len(divs) != 1 || divs[0].Dy() != tc.divHeight {
				t.Errorf("div rects = %v, want one %d tall", divs, tc.divHeight)
			}
		})
	}
}

// The generated box paints its own background over its parent's.
func TestEmptyInlineBlockPaintsBackground(t *testing.T) {
	img := painted(t, `<body style="margin:0"><div style="background:yellow;line-height:0">`+
		`<span style="display:inline-block;width:10px;height:10px;background:grey"></span></div></body>`,
		image.Rect(0, 0, 40, 40))
	yellow := color.RGBA{255, 255, 0, 255}
	pixel(t, img, 0, 0, grey)
	pixel(t, img, 9, 9, grey)
	pixel(t, img, 10, 0, yellow)                         // to the right of the box, still on the line
	pixel(t, img, 0, 10, color.RGBA{255, 255, 255, 255}) // below the one-line div
}

// Borders and padding enlarge the generated box; margins offset it without
// being painted.
func TestEmptyInlineBlockBordersPaddingAndMargins(t *testing.T) {
	rects := spanRects(t, `<body style="margin:0"><div style="line-height:0">`+
		`<span style="display:inline-block;width:10px;height:10px;`+
		`padding:2px;border:3px solid black;margin:4px"></span></div></body>`)
	if len(rects) != 1 {
		t.Fatalf("got %d span boxes %v, want 1", len(rects), rects)
	}
	// Border box: 10 content + 2*2 padding + 2*3 border = 20, offset by the
	// 4px margins.
	if want := image.Rect(4, 4, 24, 24); rects[0] != want {
		t.Errorf("span rect = %v, want %v", rects[0], want)
	}
	divs := divRects(t, `<body style="margin:0"><div style="line-height:0">`+
		`<span style="display:inline-block;width:10px;height:10px;`+
		`padding:2px;border:3px solid black;margin:4px"></span></div></body>`)
	if len(divs) != 1 || divs[0].Dy() != 28 {
		t.Errorf("div rects = %v, want one 28 tall (20 box + 8 margins)", divs)
	}
}

// Inline flow is preserved: text before and after the box stays on the same
// line, and the box advances the pen like any other inline content.
func TestEmptyInlineBlockKeepsInlineFlow(t *testing.T) {
	got, err := LayoutWithViewport(styledForLayout(t,
		`<body style="margin:0;font:16px sans-serif"><div>ab`+
			`<span style="display:inline-block;width:20px;height:6px;background:grey"></span>cd</div></body>`),
		image.Rect(0, 0, 400, 400))
	if err != nil {
		t.Fatal(err)
	}
	var runs []TextRun
	var collect func(*Box)
	collect = func(b *Box) {
		runs = append(runs, b.Text...)
		for _, c := range b.Children {
			collect(c)
		}
	}
	collect(got.Root)
	if len(runs) != 2 {
		t.Fatalf("got %d text runs %v, want 2", len(runs), runs)
	}
	if runs[0].Text != "ab" || runs[1].Text != "cd" {
		t.Fatalf("runs = %q, %q; want ab, cd", runs[0].Text, runs[1].Text)
	}
	if runs[0].Rect.Min.Y != runs[1].Rect.Min.Y {
		t.Errorf("runs on different lines: %v and %v", runs[0].Rect, runs[1].Rect)
	}
	spans := spanRects(t, `<body style="margin:0;font:16px sans-serif"><div>ab`+
		`<span style="display:inline-block;width:20px;height:6px;background:grey"></span>cd</div></body>`)
	if len(spans) != 1 {
		t.Fatalf("got %d span boxes %v, want 1", len(spans), spans)
	}
	if spans[0].Min.X < runs[0].Rect.Max.X || runs[1].Rect.Min.X < spans[0].Max.X {
		t.Errorf("span %v is not between the text runs %v and %v",
			spans[0], runs[0].Rect, runs[1].Rect)
	}
}

// A box that does not fit in the remaining space starts a new line.
func TestEmptyInlineBlockWrapsToNextLine(t *testing.T) {
	got, err := LayoutWithViewport(styledForLayout(t,
		`<body style="margin:0;line-height:0"><div style="width:60px">`+
			`<span style="display:inline-block;width:40px;height:10px;background:grey"></span>`+
			`<span style="display:inline-block;width:40px;height:10px;background:grey"></span></div></body>`),
		image.Rect(0, 0, 400, 400))
	if err != nil {
		t.Fatal(err)
	}
	var rects []image.Rectangle
	for _, b := range collectBoxes(got.Root, "span") {
		rects = append(rects, b.Rect)
	}
	if len(rects) != 2 {
		t.Fatalf("got %d span boxes %v, want 2", len(rects), rects)
	}
	if rects[1].Min.Y <= rects[0].Min.Y {
		t.Errorf("second box %v did not wrap below the first %v", rects[1], rects[0])
	}
}

// Whitespace-only and zero-sized inline-blocks still generate a line box, but
// nothing visible.
func TestEmptyInlineBlockWithoutDimensions(t *testing.T) {
	for _, content := range []string{"", " \n\t "} {
		rects := spanRects(t, `<body style="margin:0;line-height:0"><div>`+
			`<span style="display:inline-block">`+content+`</span></div></body>`)
		if len(rects) != 1 {
			t.Fatalf("content %q: got %d span boxes %v, want 1", content, len(rects), rects)
		}
		if !rects[0].Empty() {
			t.Errorf("content %q: span rect = %v, want an empty box", content, rects[0])
		}
	}
}

func TestEmptyInlineBlockWithoutDimensionsKeepsBaseline(t *testing.T) {
	for _, content := range []string{"", " \n\t "} {
		markup := `<body style="margin:0;font:16px sans-serif;line-height:20px">before` +
			`<span id="atomic" style="display:inline-block">` + content + `</span>after</body>`
		layout, err := LayoutWithViewport(styledForLayout(t, markup), image.Rect(0, 0, 400, 400))
		if err != nil {
			t.Fatal(err)
		}
		box := boxesByID(layout.Root, "atomic")["atomic"]
		if box == nil || !box.Content.Empty() {
			t.Fatalf("content %q: empty inline-block = %#v", content, box)
		}
		var runs []TextRun
		var collect func(*Box)
		collect = func(b *Box) {
			runs = append(runs, b.Text...)
			for _, child := range b.Children {
				collect(child)
			}
		}
		collect(layout.Root)
		if len(runs) != 2 || runs[0].Text != "before" || runs[1].Text != "after" {
			t.Fatalf("content %q: text runs = %v, want before and after", content, runs)
		}
		faces := newFaceSet()
		ascent, _ := faces.metrics(runs[0].Style).lineMetrics()
		faces.close()
		if got, want := box.Rect.Max.Y, runs[0].Rect.Min.Y+ascent; got != want {
			t.Errorf("content %q: inline-block bottom = %d, want baseline %d", content, got, want)
		}
		if runs[0].Rect.Min.Y != runs[1].Rect.Min.Y {
			t.Errorf("content %q: adjacent text baselines differ: %v and %v",
				content, runs[0].Rect, runs[1].Rect)
		}
	}
}

func TestInlineBlockRetainsTextFreeBlockDescendants(t *testing.T) {
	for _, tc := range []struct {
		name, atomicStyle, childStyle     string
		wantAtomicHeight, wantChildHeight int
	}{
		{
			name:             "definite child grows auto height",
			atomicStyle:      "width:100px",
			childStyle:       "height:40px;background:red",
			wantAtomicHeight: 40,
			wantChildHeight:  40,
		},
		{
			name:             "percentage child uses definite height",
			atomicStyle:      "width:100px;height:80px",
			childStyle:       "height:50%;background:blue",
			wantAtomicHeight: 80,
			wantChildHeight:  40,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			markup := `<body style="margin:0;line-height:0"><div id="atomic" style="display:inline-block;` +
				tc.atomicStyle + `"><div id="child" style="` + tc.childStyle + `"></div></div></body>`
			layout, err := LayoutWithViewport(styledForLayout(t, markup), image.Rect(0, 0, 400, 400))
			if err != nil {
				t.Fatal(err)
			}
			boxes := boxesByID(layout.Root, "atomic", "child")
			if boxes["atomic"] == nil || boxes["child"] == nil {
				t.Fatalf("missing generated boxes: %v", boxes)
			}
			if got := boxes["atomic"].Content.Dy(); got != tc.wantAtomicHeight {
				t.Errorf("inline-block content height = %d, want %d", got, tc.wantAtomicHeight)
			}
			if got := boxes["child"].Content.Dy(); got != tc.wantChildHeight {
				t.Errorf("child content height = %d, want %d", got, tc.wantChildHeight)
			}
			img := painted(t, markup, image.Rect(0, 0, 400, 400))
			wantColor := color.RGBA{255, 0, 0, 255}
			if tc.name == "percentage child uses definite height" {
				wantColor = color.RGBA{0, 0, 255, 255}
			}
			pixel(t, img, boxes["child"].Content.Min.X, boxes["child"].Content.Min.Y, wantColor)
			pixel(t, img, boxes["child"].Content.Max.X-1, boxes["child"].Content.Max.Y-1, wantColor)
		})
	}
}

func TestTextFreeBlockInlineBlockUsesBottomBaseline(t *testing.T) {
	const markup = `<body style="margin:0;font:16px sans-serif;line-height:20px">before` +
		`<span id="atomic" style="display:inline-block;width:20px"><div style="height:10px"></div></span>` +
		`after</body>`
	layout, err := LayoutWithViewport(styledForLayout(t, markup), image.Rect(0, 0, 400, 400))
	if err != nil {
		t.Fatal(err)
	}
	box := boxesByID(layout.Root, "atomic")["atomic"]
	if box == nil {
		t.Fatal("missing inline-block")
	}
	var runs []TextRun
	var collect func(*Box)
	collect = func(b *Box) {
		runs = append(runs, b.Text...)
		for _, child := range b.Children {
			collect(child)
		}
	}
	collect(layout.Root)
	if len(runs) != 2 || runs[0].Text != "before" || runs[1].Text != "after" {
		t.Fatalf("text runs = %v, want before and after", runs)
	}
	if runs[0].Rect.Min.Y != runs[1].Rect.Min.Y {
		t.Errorf("adjacent text baselines differ: %v and %v", runs[0].Rect, runs[1].Rect)
	}
	// With no in-flow line box in the atomic content, its bottom margin edge
	// supplies the baseline and aligns with the surrounding text baseline.
	faces := newFaceSet()
	defer faces.close()
	ascent, _ := faces.metrics(runs[0].Style).lineMetrics()
	if got, want := box.Rect.Max.Y, runs[0].Rect.Min.Y+ascent; got != want {
		t.Errorf("inline-block bottom = %d, want surrounding baseline %d", got, want)
	}
}

// Non-empty inline-blocks have independent child layout even when their tag
// normally generates a block. Their last line (not their bottom edge) supplies
// the baseline to adjacent text.
func TestNonEmptyInlineBlockGeometryAndBaseline(t *testing.T) {
	const markup = `<body style="margin:0"><section style="width:300px;line-height:20px">` +
		`before<div style="display:inline-block;width:120px;height:60px;padding:3px;` +
		`border:2px solid blue;background:#eee"><span>first<br>last</span></div>after` +
		`</section></body>`
	layout, err := LayoutWithViewport(styledForLayout(t, markup), image.Rect(0, 0, 300, 200))
	if err != nil {
		t.Fatal(err)
	}
	divs := collectBoxes(layout.Root, "div")
	if len(divs) != 1 {
		t.Fatalf("div boxes = %d, want 1", len(divs))
	}
	box := divs[0]
	if box.Rect.Dx() != 130 || box.Rect.Dy() != 70 || box.Content.Dx() != 120 || box.Content.Dy() != 60 {
		t.Fatalf("border/content boxes = %v / %v, want 130x70 / 120x60", box.Rect, box.Content)
	}
	if !box.AtomicInline {
		t.Fatal("non-empty inline-block not in atomic paint layer")
	}
	var outer, inner []TextRun
	for _, section := range collectBoxes(layout.Root, "section") {
		for _, child := range section.Children {
			outer = append(outer, child.Text...)
		}
	}
	for _, child := range box.Children {
		inner = append(inner, child.Text...)
	}
	if len(outer) != 2 || len(inner) != 2 || outer[0].Text != "before" ||
		outer[1].Text != "after" || inner[0].Text != "first" || inner[1].Text != "last" {
		t.Fatalf("outer runs %v, inner runs %v", outer, inner)
	}
	if outer[0].Rect.Min.Y != outer[1].Rect.Min.Y || outer[1].Rect.Min.X < box.Rect.Max.X {
		t.Fatalf("adjacent text not on same line beside box: %v %v %v", outer[0].Rect, box.Rect, outer[1].Rect)
	}
	if outer[1].Rect.Min.Y != inner[1].Rect.Min.Y {
		t.Errorf("last line baseline differs from adjacent text: %v / %v", inner[1].Rect, outer[1].Rect)
	}
}

func TestNonEmptyInlineBlockShrinkWrapAndPaint(t *testing.T) {
	const markup = `<body style="margin:0"><div style="width:100px;line-height:0">` +
		`<span style="background:yellow;line-height:20px">x` +
		`<span style="display:inline-block;background:grey;border:2px solid blue;` +
		`padding:3px;margin:4px;line-height:20px">word</span>z</span>` +
		`<div style="display:inline-block;width:70px;height:20px;background:grey">item</div>` +
		`</div></body>`
	layout, err := LayoutWithViewport(styledForLayout(t, markup), image.Rect(0, 0, 120, 160))
	if err != nil {
		t.Fatal(err)
	}
	spans := collectBoxes(layout.Root, "span")
	if len(spans) != 1 { // only the nested atomic span has a border box
		t.Fatalf("span boxes = %d, want 1", len(spans))
	}
	box := spans[0]
	if box.Content.Dx() < 25 || box.Content.Dx() > 45 ||
		box.Rect.Dx() != box.Content.Dx()+10 || box.Rect.Dy() != box.Content.Dy()+10 {
		t.Fatalf("shrink-to-fit content %v, border %v", box.Content, box.Rect)
	}
	divs := collectBoxes(layout.Root, "div")
	if len(divs) != 2 || divs[1].Rect.Min.Y <= box.Rect.Min.Y {
		t.Fatalf("70px inline-block should wrap as a unit: %v; previous %v", divs, box.Rect)
	}
	img := painted(t, markup, image.Rect(0, 0, 120, 160))
	pixel(t, img, box.Rect.Min.X, box.Rect.Min.Y, color.RGBA{0, 0, 255, 255})
	pixel(t, img, box.Rect.Min.X+3, box.Rect.Min.Y+3, grey)
	pixel(t, img, box.Content.Min.X, box.Content.Min.Y, grey) // no duplicate border over the text
	// The yellow ancestor background crosses the box but cannot overpaint it.
	pixel(t, img, box.Rect.Max.X-4, box.Rect.Min.Y+3, grey)
}

// WPT css/CSS2/tables/border-collapse-empty-row uses grey inline-block spans
// as cell content with line-height: 0; the cells must be as tall as the span
// plus their collapsed borders, and the grey must paint.
func TestEmptyInlineBlockInCollapsedTableCell(t *testing.T) {
	const markup = `<style>
		body { margin: 0 }
		table { display: table; border-collapse: collapse }
		td { border: 10px solid black; line-height: 0; padding: 0 }
		span { display: inline-block; width: 10px; height: 10px; background: grey }
	</style><body><table>
		<tr><td><span></span></td><td><span></span></td></tr>
	</table></body>`
	got, err := LayoutWithViewport(styledForLayout(t, markup), image.Rect(0, 0, 200, 200))
	if err != nil {
		t.Fatal(err)
	}
	cells := collectBoxes(got.Root, "td")
	if len(cells) != 2 {
		t.Fatalf("got %d cells, want 2", len(cells))
	}
	for i, cell := range cells {
		if cell.Content.Dx() != 10 || cell.Content.Dy() != 10 {
			t.Errorf("cell %d content = %v, want 10x10", i, cell.Content)
		}
	}
	var spans []image.Rectangle
	for _, b := range collectBoxes(got.Root, "span") {
		spans = append(spans, b.Rect)
	}
	if len(spans) != 2 {
		t.Fatalf("got %d span boxes %v, want 2", len(spans), spans)
	}
	img := painted(t, markup, image.Rect(0, 0, 200, 200))
	for i, span := range spans {
		if span.Dx() != 10 || span.Dy() != 10 {
			t.Errorf("span %d rect = %v, want 10x10", i, span)
		}
		pixel(t, img, span.Min.X, span.Min.Y, grey)
		pixel(t, img, span.Max.X-1, span.Max.Y-1, grey)
	}
}
