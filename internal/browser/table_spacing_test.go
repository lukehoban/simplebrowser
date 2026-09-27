package browser

import (
	"image"
	"image/color"
	"math"
	"testing"
)

func tableSpacingLayout(t *testing.T, markup string, viewport image.Rectangle) (tables, rows, cells []*Box) {
	t.Helper()
	got, err := LayoutWithViewport(styledForLayout(t, markup), viewport)
	if err != nil {
		t.Fatal(err)
	}
	checkGeometry(t, got.Root, got.Viewport)
	return collectBoxes(got.Root, "table"), collectBoxes(got.Root, "tr"), collectBoxes(got.Root, "td")
}

// Two-value border-spacing separates columns by the first component and rows
// by the second (CSS 2.1 §17.6.1), including font-relative components.
func TestTableDistinctBorderSpacingGeometry(t *testing.T) {
	cell := `<td style="width:10px;height:10px;padding:0"></td>`
	tables, rows, cells := tableSpacingLayout(t, `<body style="margin:0">`+
		`<table style="font-size:10px;border-spacing:1em 2ex">`+
		`<tr>`+cell+cell+`</tr><tr>`+cell+cell+`</tr></table>`, image.Rect(0, 0, 400, 300))
	if len(tables) != 1 || len(rows) != 2 || len(cells) != 4 {
		t.Fatalf("tables/rows/cells = %d/%d/%d", len(tables), len(rows), len(cells))
	}
	ratios := ratiosFor(ComputedStyle{"font-size": "10px"})
	v := int(math.Round(2 * ratios.ex * 10))
	if v == 10 {
		t.Fatalf("test needs distinct components, got 2ex = %d", v)
	}
	table := tables[0].Rect
	if cells[0].Rect.Min.X != table.Min.X+10 || cells[1].Rect.Min.X != cells[0].Rect.Max.X+10 {
		t.Errorf("horizontal spacing: table %v cells %v %v", table, cells[0].Rect, cells[1].Rect)
	}
	if rows[0].Rect.Min.Y != table.Min.Y+v || rows[1].Rect.Min.Y != rows[0].Rect.Max.Y+v {
		t.Errorf("vertical spacing %d: table %v rows %v %v", v, table, rows[0].Rect, rows[1].Rect)
	}
	if table.Dx() != 3*10+2*10 || table.Dy() != 3*v+2*10 {
		t.Errorf("table size = %v, want %dx%d", table, 50, 3*v+20)
	}
	if rows[0].Rect.Min.X != table.Min.X+10 || rows[0].Rect.Max.X != table.Max.X-10 {
		t.Errorf("row inset uses horizontal spacing: row %v table %v", rows[0].Rect, table)
	}
}

// border-spacing is inherited: a nested table without its own declaration uses
// both components of its ancestor's value.
func TestTableDistinctBorderSpacingInherited(t *testing.T) {
	cell := `<td style="width:10px;height:10px;padding:0"></td>`
	tables, rows, _ := tableSpacingLayout(t, `<body style="margin:0">`+
		`<table style="border-spacing:3px 7px"><tr><td style="padding:0">`+
		`<div style="display:table"><div style="display:table-row">`+
		`<div style="display:table-cell;width:10px;height:10px"></div>`+
		`<div style="display:table-cell;width:10px;height:10px"></div></div></div>`+
		`</td></tr><tr>`+cell+`</tr></table>`, image.Rect(0, 0, 400, 300))
	if len(tables) != 1 || len(rows) != 2 {
		t.Fatalf("tables/rows = %d/%d", len(tables), len(rows))
	}
	if rows[0].Rect.Min.Y != tables[0].Rect.Min.Y+7 || rows[1].Rect.Min.Y != rows[0].Rect.Max.Y+7 {
		t.Errorf("outer vertical spacing: table %v rows %v %v", tables[0].Rect, rows[0].Rect, rows[1].Rect)
	}
	inner := buildTableGrid(styledElementByTag(t, `<table style="border-spacing:3px 7px"><tr><td>`+
		`<div id="inner" style="display:table"></div></td></tr></table>`, "inner"))
	if inner.hspacing != 3 || inner.vspacing != 7 {
		t.Errorf("inherited spacing = %d %d, want 3 7", inner.hspacing, inner.vspacing)
	}
	// The inner CSS table is 3 + 10 + 3 + 10 + 3 wide and 7 + 10 + 7 tall.
	if rows[0].Rect.Dy() != 24 {
		t.Errorf("inner table height via outer row = %d, want 24", rows[0].Rect.Dy())
	}
}

func styledElementByTag(t *testing.T, markup, id string) *StyledNode {
	t.Helper()
	n := styledElementByID(styledForLayout(t, markup).StyleRoot, id)
	if n == nil {
		t.Fatalf("no element #%s", id)
	}
	return n
}

// border-collapse drops both components; narrow tables shrink only the
// horizontal gaps and keep the specified vertical gaps.
func TestTableDistinctBorderSpacingCollapseAndNarrow(t *testing.T) {
	cell := `<td style="padding:0;height:10px"></td>`
	tables, rows, _ := tableSpacingLayout(t, `<body style="margin:0">`+
		`<table style="border-spacing:4px 9px;border-collapse:collapse">`+
		`<tr>`+cell+`</tr><tr>`+cell+`</tr></table>`, image.Rect(0, 0, 400, 300))
	if rows[0].Rect.Min.Y != tables[0].Rect.Min.Y || rows[1].Rect.Min.Y != rows[0].Rect.Max.Y {
		t.Errorf("collapsed spacing not dropped: table %v rows %v %v", tables[0].Rect, rows[0].Rect, rows[1].Rect)
	}

	tables, rows, cells := tableSpacingLayout(t, `<body style="margin:0">`+
		`<table style="border-spacing:40px 6px"><tr>`+cell+cell+`</tr><tr>`+cell+cell+`</tr></table>`,
		image.Rect(0, 0, 60, 300))
	if tables[0].Rect.Dx() > 60 {
		t.Errorf("narrow table overflows: %v", tables[0].Rect)
	}
	if cells[0].Rect.Min.X-tables[0].Rect.Min.X != 20 {
		t.Errorf("horizontal spacing not shrunk to 60/3: table %v cell %v", tables[0].Rect, cells[0].Rect)
	}
	if rows[0].Rect.Min.Y != tables[0].Rect.Min.Y+6 || rows[1].Rect.Min.Y != rows[0].Rect.Max.Y+6 {
		t.Errorf("vertical spacing changed in narrow table: table %v rows %v %v", tables[0].Rect, rows[0].Rect, rows[1].Rect)
	}
}

// Painted table backgrounds show through distinct horizontal and vertical gaps.
func TestTableDistinctBorderSpacingPixels(t *testing.T) {
	red, green := color.RGBA{255, 0, 0, 255}, color.RGBA{0, 128, 0, 255}
	cell := `<td style="width:10px;height:10px;padding:0;background:red"></td>`
	img := painted(t, `<body style="margin:0"><table style="background:green;border-spacing:2px 6px">`+
		`<tr>`+cell+cell+`</tr><tr>`+cell+cell+`</tr></table>`, image.Rect(0, 0, 100, 100))
	// Columns: 2..12 and 14..24. Rows: 6..16 and 22..32.
	pixel(t, img, 1, 10, green)
	pixel(t, img, 2, 6, red)
	pixel(t, img, 11, 15, red)
	pixel(t, img, 12, 10, green)
	pixel(t, img, 14, 10, red)
	pixel(t, img, 5, 5, green)
	pixel(t, img, 5, 16, green)
	pixel(t, img, 5, 21, green)
	pixel(t, img, 5, 22, red)
	pixel(t, img, 23, 31, red)
	pixel(t, img, 23, 32, green)
	pixel(t, img, 23, 37, green)
	pixel(t, img, 26, 20, color.RGBA{255, 255, 255, 255})
}
