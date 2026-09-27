package browser

import (
	"image"
	"image/color"
	"testing"
)

var grey = color.RGBA{128, 128, 128, 255}

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
	rects := spanRects(t, `<body style="margin:0;line-height:0"><div>`+
		`<span style="display:inline-block"></span></div></body>`)
	if len(rects) != 1 {
		t.Fatalf("got %d span boxes %v, want 1", len(rects), rects)
	}
	if !rects[0].Empty() {
		t.Errorf("span rect = %v, want an empty box", rects[0])
	}
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
