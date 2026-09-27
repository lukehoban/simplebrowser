package browser

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"testing"
)

// boxWithText returns the outermost box for an element named name whose
// descendant text is text.
func boxWithText(t *testing.T, root *Box, name, text string) *Box {
	t.Helper()
	for _, b := range collectBoxes(root, name) {
		if boxText(b) == text {
			return b
		}
	}
	t.Fatalf("no <%s> box with text %q", name, text)
	return nil
}

const captionFixtureStyle = `<style>
body { margin: 0 }
div { width: 98px }
p { margin: 16px 0; height: auto }
.cell { display: table-cell }
.top { display: table-caption; caption-side: top }
.bottom { display: table-caption; caption-side: bottom }
</style>`

// The minimal repro of #179 (WPT tables/caption-position-001): a misparented
// cell and caption are grouped into one anonymous table whose caption-side
// decides the order, independently of source order.
func TestAnonymousTableCaptionSideOrdersCaptionAndCell(t *testing.T) {
	tests := []struct {
		name, body   string
		first, later string
	}{
		{"top caption after cell", `<div><p class="cell">CELL</p> <p class="top">CAPTION</p></div>`, "CAPTION", "CELL"},
		{"bottom caption before cell", `<div><p class="bottom">CAPTION</p> <p class="cell">CELL</p></div>`, "CELL", "CAPTION"},
		{"top caption before cell", `<div><p class="top">CAPTION</p> <p class="cell">CELL</p></div>`, "CAPTION", "CELL"},
		{"bottom caption after cell", `<div><p class="cell">CELL</p> <p class="bottom">CAPTION</p></div>`, "CELL", "CAPTION"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			doc := styledForLayout(t, captionFixtureStyle+tc.body)
			got, err := LayoutWithViewport(doc, image.Rect(0, 0, 200, 200))
			if err != nil {
				t.Fatal(err)
			}
			first := boxWithText(t, got.Root, "p", tc.first)
			later := boxWithText(t, got.Root, "p", tc.later)
			if first.Rect.Max.Y > later.Rect.Min.Y {
				t.Fatalf("%s %v should precede %s %v", tc.first, first.Rect, tc.later, later.Rect)
			}
			// The caption keeps its vertical margins (it is a block
			// container); the cell ignores its margins.
			caption := boxWithText(t, got.Root, "p", "CAPTION")
			cell := boxWithText(t, got.Root, "p", "CELL")
			if tc.first == "CAPTION" {
				if caption.Rect.Min.Y != 16 || cell.Rect.Min.Y != caption.Rect.Max.Y+16 {
					t.Fatalf("caption %v, cell %v: want caption at 16 and cell 16px below", caption.Rect, cell.Rect)
				}
			} else if cell.Rect.Min.Y != 0 || caption.Rect.Min.Y != cell.Rect.Max.Y+16 {
				t.Fatalf("cell %v, caption %v: want cell at 0 and caption 16px below", cell.Rect, caption.Rect)
			}
			// The anonymous table shrinks to its content, so both boxes share
			// the cell's width rather than the 98px container.
			if caption.Rect.Min.X != 0 || cell.Rect.Min.X != 0 || caption.Rect.Dx() != cell.Rect.Dx() || cell.Rect.Dx() >= 98 {
				t.Fatalf("caption %v and cell %v should share one shrink-to-fit column", caption.Rect, cell.Rect)
			}
		})
	}
}

func TestTableCaptionSideOrdersRealTableCaptions(t *testing.T) {
	doc := styledForLayout(t, `<body style="margin:0"><table style="border-spacing:0">`+
		`<tr><td style="padding:0">ROW</td></tr>`+
		`<caption style="caption-side:bottom">BELOW</caption>`+
		`<caption>ABOVE</caption>`+
		`</table></body>`)
	got, err := LayoutWithViewport(doc, image.Rect(0, 0, 200, 200))
	if err != nil {
		t.Fatal(err)
	}
	above := boxWithText(t, got.Root, "caption", "ABOVE")
	row := boxWithText(t, got.Root, "td", "ROW")
	below := boxWithText(t, got.Root, "caption", "BELOW")
	if !(above.Rect.Max.Y <= row.Rect.Min.Y && row.Rect.Max.Y <= below.Rect.Min.Y) {
		t.Fatalf("want ABOVE %v, ROW %v, BELOW %v in order", above.Rect, row.Rect, below.Rect)
	}
}

func TestCaptionsSpanGridOutsideBorder(t *testing.T) {
	for _, tc := range []struct {
		name, tableStyle string
		width            int
	}{
		{"intrinsic", "border-spacing:0", 164},
		{"explicit fixed", "border-spacing:0;width:220px;table-layout:fixed", 220},
		{"explicit auto", "border-spacing:0;width:220px", 224},
	} {
		t.Run(tc.name, func(t *testing.T) {
			source := `<body style="margin:0"><table style="border:2px solid black;` + tc.tableStyle + `">` +
				`<caption style="caption-side:bottom;background:yellow;text-align:center;height:12px">BELOW</caption>` +
				`<tr><td style="width:40px;height:16px;padding:0;background:red">A</td>` +
				`<td style="width:120px;padding:0;background:blue">B</td></tr>` +
				`<caption style="background:yellow;text-align:center;height:12px">ABOVE</caption></table>` +
				`<div style="height:5px;background:green">next</div></body>`
			doc := styledForLayout(t, source)
			got, err := LayoutWithViewport(doc, image.Rect(0, 0, 300, 100))
			if err != nil {
				t.Fatal(err)
			}
			table := collectBoxes(got.Root, "table")[0]
			above := boxWithText(t, got.Root, "caption", "ABOVE")
			below := boxWithText(t, got.Root, "caption", "BELOW")
			a := boxWithText(t, got.Root, "td", "A")
			b := boxWithText(t, got.Root, "td", "B")
			next := boxWithText(t, got.Root, "div", "next")
			if above.Rect != image.Rect(0, 0, tc.width, 12) ||
				table.Rect.Min.Y != 12 || table.Rect.Dx() != tc.width ||
				below.Rect != image.Rect(0, table.Rect.Max.Y, tc.width, table.Rect.Max.Y+12) ||
				next.Rect.Min.Y != below.Rect.Max.Y {
				t.Fatalf("above %v, grid %v, below %v, next %v: captions must span outside border",
					above.Rect, table.Rect, below.Rect, next.Rect)
			}
			if a.Rect.Min.X != 2 || b.Rect.Min.X < a.Rect.Max.X || b.Rect.Max.X > tc.width-2 {
				t.Fatalf("grid columns moved: %v %v", a.Rect, b.Rect)
			}
			img := painted(t, source, image.Rect(0, 0, 300, 100))
			pixel(t, img, tc.width-1, 5, color.RGBA{255, 255, 0, 255})
			pixel(t, img, tc.width-1, table.Rect.Max.Y+5, color.RGBA{255, 255, 0, 255})
			pixel(t, img, 0, table.Rect.Min.Y+2, color.RGBA{0, 0, 0, 255})
			pixel(t, img, 0, next.Rect.Min.Y, color.RGBA{0, 128, 0, 255})
			for _, caption := range []*Box{above, below} {
				if ink := textRunBounds(caption); ink.Min.X <= a.Rect.Max.X {
					t.Fatalf("centered caption text %v should not be confined to first column %v", ink, a.Rect)
				}
			}
		})
	}
}

// Consecutive misparented cells share one anonymous row, while ordinary
// siblings and whitespace around them stay in normal flow.
func TestAnonymousTableGroupsConsecutiveCells(t *testing.T) {
	doc := styledForLayout(t, `<body style="margin:0"><div>before</div>`+
		`<span style="display:table-cell">A</span> <span style="display:table-cell">B</span>`+
		`<div>after</div></body>`)
	got, err := LayoutWithViewport(doc, image.Rect(0, 0, 200, 200))
	if err != nil {
		t.Fatal(err)
	}
	a := boxWithText(t, got.Root, "span", "A")
	b := boxWithText(t, got.Root, "span", "B")
	after := boxWithText(t, got.Root, "div", "after")
	if a.Rect.Min.Y != b.Rect.Min.Y || a.Rect.Max.X > b.Rect.Min.X {
		t.Fatalf("cells A %v and B %v should share a row", a.Rect, b.Rect)
	}
	if a.Rect.Min.Y != 20 || after.Rect.Min.Y != a.Rect.Max.Y {
		t.Fatalf("A %v, after %v: anonymous table should sit between the blocks", a.Rect, after.Rect)
	}
}

func TestAnonymousTableCaptionOrderPixelsMatchTable(t *testing.T) {
	green := `style="display:table-cell;background:green;width:40px;height:20px;margin:0;padding:0"`
	red := `style="display:table-caption;caption-side:%s;background:red;height:10px;margin:0"`
	tests := []struct{ name, test, reference string }{
		{
			"top",
			`<body style="margin:0"><div><p ` + green + `></p><p ` + fmt.Sprintf(red, "top") + `></p></div></body>`,
			`<body style="margin:0"><table style="border-spacing:0"><caption style="background:red;height:10px"></caption>` +
				`<tr><td style="background:green;width:40px;height:20px;padding:0"></td></tr></table></body>`,
		},
		{
			"bottom",
			`<body style="margin:0"><div><p ` + fmt.Sprintf(red, "bottom") + `></p><p ` + green + `></p></div></body>`,
			`<body style="margin:0"><table style="border-spacing:0">` +
				`<tr><td style="background:green;width:40px;height:20px;padding:0"></td></tr>` +
				`<caption style="caption-side:bottom;background:red;height:10px"></caption></table></body>`,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			viewport := image.Rect(0, 0, 80, 60)
			got := painted(t, tc.test, viewport)
			want := painted(t, tc.reference, viewport)
			if !bytes.Equal(got.Pix, want.Pix) {
				t.Fatal("anonymous table render differs from the equivalent table")
			}
			topColor := color.RGBA{255, 0, 0, 255}
			if tc.name == "bottom" {
				topColor = color.RGBA{0, 128, 0, 255}
			}
			if c := got.RGBAAt(5, 2); c != topColor {
				t.Fatalf("top pixel = %v, want %v", c, topColor)
			}
		})
	}
}

// Shrink-to-fit table cells measure misparented cells as one anonymous table
// row, not as stacked blocks.
func TestAnonymousTableIntrinsicWidthSumsCells(t *testing.T) {
	doc := styledForLayout(t, `<body style="margin:0"><table style="border-spacing:0"><tr><td style="padding:0">`+
		`<p style="display:table-cell">AAA</p><p style="display:table-cell">BBB</p>`+
		`</td></tr></table></body>`)
	got, err := LayoutWithViewport(doc, image.Rect(0, 0, 300, 100))
	if err != nil {
		t.Fatal(err)
	}
	a := boxWithText(t, got.Root, "p", "AAA")
	b := boxWithText(t, got.Root, "p", "BBB")
	outer := collectBoxes(got.Root, "td")
	if len(outer) != 1 {
		t.Fatalf("outer cells = %d", len(outer))
	}
	if a.Rect.Min.Y != b.Rect.Min.Y || outer[0].Rect.Dx() != b.Rect.Max.X-a.Rect.Min.X {
		t.Fatalf("outer cell %v should fit cells A %v and B %v side by side", outer[0].Rect, a.Rect, b.Rect)
	}
	for _, cell := range []*Box{a, b} {
		// Allow one pixel for the fractional pen phase carried between cells.
		if text := textRunBounds(cell); cell.Rect.Dx()+1 < text.Dx() {
			t.Fatalf("cell %v is narrower than its text %v", cell.Rect, text)
		}
	}
}

func textRunBounds(b *Box) image.Rectangle {
	var r image.Rectangle
	for _, run := range b.Text {
		r = r.Union(run.Rect)
	}
	for _, child := range b.Children {
		r = r.Union(textRunBounds(child))
	}
	return r
}
