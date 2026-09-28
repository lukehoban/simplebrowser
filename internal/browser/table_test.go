package browser

import (
	"bytes"
	"image"
	"image/color"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// collectBoxes returns boxes for elements named name in document order. Inline
// flow boxes reuse their parent's node, so those duplicates are skipped.
func collectBoxes(b *Box, name string) []*Box {
	var result []*Box
	var walk func(box, parent *Box)
	walk = func(box, parent *Box) {
		if box == nil {
			return
		}
		if box.Node != nil && strings.EqualFold(box.Node.Name, name) &&
			(parent == nil || parent.Node != box.Node) {
			result = append(result, box)
		}
		for _, child := range box.Children {
			walk(child, box)
		}
	}
	walk(b, nil)
	return result
}

func boxText(b *Box) string {
	var out strings.Builder
	var walk func(*Box)
	walk = func(box *Box) {
		for _, run := range box.Text {
			out.WriteString(run.Text)
		}
		for _, child := range box.Children {
			walk(child)
		}
	}
	walk(b)
	return out.String()
}

func TestTableAdjacentCellsRetainFractionalTextPen(t *testing.T) {
	source, err := os.ReadFile(filepath.Join("..", "..", "testdata", "wpt", "colors", "color-applies-to-001.xht"))
	if err != nil {
		t.Fatal(err)
	}
	doc := styledForLayout(t, string(source))
	layout, err := LayoutWithViewport(doc, image.Rect(0, 0, 800, 600))
	if err != nil {
		t.Fatal(err)
	}
	var runs []TextRun
	var walk func(*Box)
	walk = func(box *Box) {
		if box == nil {
			return
		}
		runs = append(runs, box.Text...)
		for _, child := range box.Children {
			walk(child)
		}
	}
	walk(layout.Root)
	faces := newFaceSet()
	defer faces.close()
	found := 0
	for _, run := range runs {
		if run.Text != "Filler" {
			continue
		}
		var following *TextRun
		for i := range runs {
			if strings.HasSuffix(runs[i].Text, "Text") && runs[i].Rect.Min.Y == run.Rect.Min.Y {
				following = &runs[i]
				break
			}
		}
		if following == nil {
			t.Fatalf("no adjacent second-cell Text run for row at y=%d: %+v", run.Rect.Min.Y, runs)
		}
		want := run.PenX + faces.metrics(run.Style).advance(run.Text)
		if following.PenX != want {
			t.Fatalf("second-cell text pen = %v, want exact first-cell end %v", following.PenX, want)
		}
		if want&63 == 0 {
			t.Fatalf("fixture no longer exercises a fractional boundary: %v", want)
		}
		found++
	}
	if found != 2 {
		t.Fatalf("found %d fractional table rows, want 2", found)
	}
}

func TestTableNestedFlexFilenameTextUsesItsOwnInlineOrigin(t *testing.T) {
	source, err := os.ReadFile(filepath.Join("..", "..", "testdata", "github-vscode", "repros", "filename-clipping.html"))
	if err != nil {
		t.Fatal(err)
	}
	layout, err := LayoutWithViewport(styledForLayout(t, string(source)), image.Rect(0, 0, 800, 600))
	if err != nil {
		t.Fatal(err)
	}
	filenameIDs := []string{"filename-0", "filename-1", "filename-2", "filename-3", "filename-4", "filename-5"}
	filenames := boxesByID(layout.Root, filenameIDs...)
	filenameIDByText := map[string]string{
		".agents/skills/launch": "filename-0",
		".config":               "filename-1",
		".devcontainer":         "filename-2",
		".eslint-plugin-local":  "filename-3",
		".github":               "filename-4",
		".vscode":               "filename-5",
	}
	runs := make(map[string]*TextRun, len(filenameIDs))
	var walk func(*Box)
	walk = func(box *Box) {
		for i := range box.Text {
			if id, ok := filenameIDByText[box.Text[i].Text]; ok {
				runs[id] = &box.Text[i]
			}
		}
		for _, child := range box.Children {
			walk(child)
		}
	}
	walk(layout.Root)
	for _, id := range filenameIDs {
		run := runs[id]
		if run == nil {
			t.Fatalf("filename text run for %s not found", id)
		}
		filename := filenames[id]
		if run.Rect.Min.X < filename.Rect.Min.X || run.Rect.Max.X > filename.Rect.Max.X {
			t.Errorf("%s text %q at %v escapes its truncation box %v", id, run.Text, run.Rect, filename.Rect)
		}
	}
}

// checkGeometry asserts that every box has non-negative geometry and that no
// table is wider than the viewport it was laid out in.
func checkGeometry(t *testing.T, root *Box, viewport image.Rectangle) {
	t.Helper()
	var walk func(*Box)
	walk = func(b *Box) {
		if b == nil {
			return
		}
		if b.Rect.Dx() < 0 || b.Rect.Dy() < 0 || b.Content.Dx() < 0 || b.Content.Dy() < 0 {
			t.Fatalf("negative geometry: rect %v content %v", b.Rect, b.Content)
		}
		if b.Node != nil && strings.EqualFold(b.Node.Name, "table") && b.Rect.Dx() > viewport.Dx() {
			t.Fatalf("table overflows viewport width: %v in %v", b.Rect, viewport)
		}
		for _, child := range b.Children {
			walk(child)
		}
	}
	walk(root)
}

func TestTableColumnsRowsAndCellPadding(t *testing.T) {
	doc := styledForLayout(t, `<table cellpadding="2" cellspacing="3" width="240">`+
		`<tr><td>one</td><td>two</td></tr><tr><td>three</td><td height="30">four</td></tr></table>`)
	got, err := LayoutWithViewport(doc, image.Rect(0, 0, 300, 400))
	if err != nil {
		t.Fatal(err)
	}
	checkGeometry(t, got.Root, got.Viewport)

	tables := collectBoxes(got.Root, "table")
	if len(tables) != 1 || tables[0].Rect.Dx() != 240 {
		t.Fatalf("table width = %+v", tables[0].Rect)
	}
	rows := collectBoxes(got.Root, "tr")
	if len(rows) != 2 {
		t.Fatalf("rows = %d", len(rows))
	}
	if rows[0].Rect.Min.Y != tables[0].Rect.Min.Y+3 {
		t.Fatalf("cellspacing not applied above first row: %v in %v", rows[0].Rect, tables[0].Rect)
	}
	if rows[1].Rect.Min.Y != rows[0].Rect.Max.Y+3 {
		t.Fatalf("rows not separated by cellspacing: %v %v", rows[0].Rect, rows[1].Rect)
	}
	if rows[1].Rect.Dy() != 30+2+2 {
		t.Fatalf("explicit cell height ignored: %v", rows[1].Rect)
	}
	cells := collectBoxes(got.Root, "td")
	if len(cells) != 4 {
		t.Fatalf("cells = %d", len(cells))
	}
	// Columns line up across rows and cellpadding insets the content box.
	if cells[0].Rect.Min.X != cells[2].Rect.Min.X || cells[1].Rect.Min.X != cells[3].Rect.Min.X {
		t.Fatalf("columns do not line up: %v %v %v %v", cells[0].Rect, cells[1].Rect, cells[2].Rect, cells[3].Rect)
	}
	if cells[0].Content.Min.X != cells[0].Rect.Min.X+2 || cells[0].Content.Min.Y != cells[0].Rect.Min.Y+2 {
		t.Fatalf("cellpadding not applied: rect %v content %v", cells[0].Rect, cells[0].Content)
	}
	if cells[1].Rect.Min.X != cells[0].Rect.Max.X+3 {
		t.Fatalf("cells not separated by cellspacing: %v %v", cells[0].Rect, cells[1].Rect)
	}
	if boxText(cells[3]) != "four" {
		t.Fatalf("cell text = %q", boxText(cells[3]))
	}
}

func TestTableColspanAndPercentageWidths(t *testing.T) {
	doc := styledForLayout(t, `<table cellspacing="0" cellpadding="0" width="200">`+
		`<tr><td width="50%">a</td><td>b</td></tr>`+
		`<tr><td colspan="2">wide content spanning both columns</td></tr></table>`)
	got, err := LayoutWithViewport(doc, image.Rect(0, 0, 400, 400))
	if err != nil {
		t.Fatal(err)
	}
	checkGeometry(t, got.Root, got.Viewport)

	cells := collectBoxes(got.Root, "td")
	if len(cells) != 3 {
		t.Fatalf("cells = %d", len(cells))
	}
	if cells[0].Rect.Dx() != 100 {
		t.Fatalf("percentage column width = %d, want 100", cells[0].Rect.Dx())
	}
	if cells[0].Rect.Dx()+cells[1].Rect.Dx() != 200 {
		t.Fatalf("columns do not fill the table: %v %v", cells[0].Rect, cells[1].Rect)
	}
	span := cells[2]
	if span.Rect.Min.X != cells[0].Rect.Min.X || span.Rect.Max.X != cells[1].Rect.Max.X {
		t.Fatalf("colspan cell does not cover both columns: %v", span.Rect)
	}
}

func TestFixedTableLayoutUsesColumnWidthsIncludingCellPadding(t *testing.T) {
	source := `<body style="margin:0"><table style="border-collapse:separate;border-left:23px solid white;` +
		`border-right:34px solid white;border-spacing:19px 0;height:100px;table-layout:fixed;width:758px">` +
		`<caption>caption</caption><col style="width:382px"><col style="width:262px">` +
		`<tr><td id="first" style="padding:0 127px;border-right:1px solid lime">first cell here</td>` +
		`<td style="padding:0 127px">second cell here</td></tr></table></body>`
	doc := styledForLayout(t, source)
	got, err := LayoutWithViewport(doc, image.Rect(0, 0, 800, 600))
	if err != nil {
		t.Fatal(err)
	}

	tables := collectBoxes(got.Root, "table")
	cells := collectBoxes(got.Root, "td")
	if len(tables) != 1 || len(cells) != 2 {
		t.Fatalf("tables = %d, cells = %d", len(tables), len(cells))
	}
	if cells[0].Rect != image.Rect(42, 20, 424, 120) {
		t.Fatalf("first fixed column geometry = %v, want (42,20)-(424,120)", cells[0].Rect)
	}
	if cells[0].Content.Min.X != 169 || cells[0].Content.Max.X != 296 {
		t.Fatalf("cell padding must fit inside 382px column: rect %v content %v", cells[0].Rect, cells[0].Content)
	}
	if tables[0].Rect != image.Rect(0, 20, 758, 120) {
		t.Fatalf("table border box = %v, want (0,20)-(758,120) below caption", tables[0].Rect)
	}

	img := painted(t, source, image.Rect(0, 0, 800, 160))
	pixel(t, img, 423, 40, color.RGBA{G: 255, A: 255})
	pixel(t, img, 422, 40, color.RGBA{R: 255, G: 255, B: 255, A: 255})
}

func TestTableRowspanCoversRows(t *testing.T) {
	doc := styledForLayout(t, `<table cellspacing="0" cellpadding="0">`+
		`<tr><td rowspan="2">tall</td><td>one</td></tr>`+
		`<tr><td>two</td></tr>`+
		`<tr><td>three</td><td>four</td></tr></table>`)
	got, err := LayoutWithViewport(doc, image.Rect(0, 0, 300, 400))
	if err != nil {
		t.Fatal(err)
	}
	checkGeometry(t, got.Root, got.Viewport)

	rows := collectBoxes(got.Root, "tr")
	cells := collectBoxes(got.Root, "td")
	if len(rows) != 3 || len(cells) != 5 {
		t.Fatalf("rows = %d cells = %d", len(rows), len(cells))
	}
	tall := cells[0]
	if tall.Rect.Min.Y != rows[0].Rect.Min.Y || tall.Rect.Max.Y != rows[1].Rect.Max.Y {
		t.Fatalf("rowspan cell does not cover two rows: %v rows %v %v", tall.Rect, rows[0].Rect, rows[1].Rect)
	}
	// The second row's own cell starts in the next column because the spanning
	// cell still occupies column zero.
	if cells[2].Rect.Min.X != cells[1].Rect.Min.X {
		t.Fatalf("rowspan occupancy ignored: %v %v", cells[1].Rect, cells[2].Rect)
	}
	if cells[3].Rect.Min.X != tall.Rect.Min.X {
		t.Fatalf("third row should reuse the spanned column: %v %v", cells[3].Rect, tall.Rect)
	}
}

func TestTableNestedHackerNewsLikeStructure(t *testing.T) {
	doc := styledForLayout(t, `<center><table id="hnmain" border="0" cellpadding="0" cellspacing="0" width="85%">`+
		`<tr><td bgcolor="#ff6600"><table border="0" cellpadding="0" cellspacing="0" width="100%">`+
		`<tr><td style="width:18px;padding-right:4px">L</td>`+
		`<td><span class="pagetop"><b><a href="news">Hacker News</a></b><a href="newest">new</a></span></td>`+
		`<td style="text-align:right"><a href="login">login</a></td></tr></table></td></tr>`+
		`<tr style="height:10px"></tr>`+
		`<tr id="bigbox"><td><table border="0" cellpadding="0" cellspacing="0">`+
		`<tr class="athing"><td align="right" valign="top">1.</td><td><a href="item">A story title</a></td></tr>`+
		`</table></td></tr></table></center>`)
	got, err := LayoutWithViewport(doc, image.Rect(0, 0, 800, 600))
	if err != nil {
		t.Fatal(err)
	}
	checkGeometry(t, got.Root, got.Viewport)

	tables := collectBoxes(got.Root, "table")
	if len(tables) != 3 {
		t.Fatalf("tables = %d, want outer plus two nested", len(tables))
	}
	outer := tables[0]
	if outer.Rect.Dx() != 680 {
		t.Fatalf("outer table width = %d, want 85%% of 800", outer.Rect.Dx())
	}
	if outer.Rect.Min.X != 60 || outer.Rect.Max.X != 740 {
		t.Fatalf("outer table should be centered by <center>: %v", outer.Rect)
	}
	header := tables[1]
	if !header.Rect.In(outer.Rect) {
		t.Fatalf("nested header table escapes its parent: %v not in %v", header.Rect, outer.Rect)
	}
	if header.Rect.Dx() != outer.Rect.Dx() {
		t.Fatalf("width:100%% nested table = %d, want %d", header.Rect.Dx(), outer.Rect.Dx())
	}
	logo := collectBoxes(header, "td")[0]
	if logo.Rect.Dx() != 18+4 {
		t.Fatalf("fixed 18px logo column with 4px padding = %d", logo.Rect.Dx())
	}
	stories := tables[2]
	if !stories.Rect.In(outer.Rect) || stories.Rect.Min.Y < header.Rect.Max.Y {
		t.Fatalf("story table misplaced: %v header %v outer %v", stories.Rect, header.Rect, outer.Rect)
	}
	outerRows := outer.Children
	if len(outerRows) != 3 || outerRows[1].Rect.Dy() != 10 {
		t.Fatalf("spacer row height not honored: %d rows, %+v", len(outerRows), outerRows)
	}
	if text := boxText(stories); !strings.Contains(text, "A story title") || !strings.Contains(text, "1.") {
		t.Fatalf("story cell text = %q", text)
	}
}

func TestTableHorizontalAlignment(t *testing.T) {
	tests := []struct {
		name      string
		source    string
		wantX     int
		wantWidth int
	}{
		{
			name:      "parent text align centers explicit width",
			source:    `<div style="text-align:center"><table width="80"><tr><td>x</td></tr></table></div>`,
			wantX:     60,
			wantWidth: 80,
		},
		{
			name:      "auto width is not centered by parent text align",
			source:    `<div style="text-align:center"><table><tr><td>x</td></tr></table></div>`,
			wantX:     0,
			wantWidth: 12,
		},
		{
			name:      "two auto margins center",
			source:    `<table style="width:80px;margin-left:auto;margin-right:auto"><tr><td>x</td></tr></table>`,
			wantX:     60,
			wantWidth: 80,
		},
		{
			name:      "left auto margin right aligns",
			source:    `<table style="width:80px;margin-left:auto;margin-right:10px"><tr><td>x</td></tr></table>`,
			wantX:     110,
			wantWidth: 80,
		},
		{
			name:      "right auto margin leaves table left aligned",
			source:    `<table style="width:80px;margin-left:10px;margin-right:auto"><tr><td>x</td></tr></table>`,
			wantX:     10,
			wantWidth: 80,
		},
		{
			name:      "table align attribute centers",
			source:    `<table width="80" align="center"><tr><td>x</td></tr></table>`,
			wantX:     60,
			wantWidth: 80,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := LayoutWithViewport(styledForLayout(t, tc.source), image.Rect(0, 0, 200, 100))
			if err != nil {
				t.Fatal(err)
			}
			tables := collectBoxes(got.Root, "table")
			if len(tables) != 1 {
				t.Fatalf("tables = %d, want 1", len(tables))
			}
			if tables[0].Rect.Min.X != tc.wantX || tables[0].Rect.Dx() != tc.wantWidth {
				t.Fatalf("table geometry = %v, want x=%d width=%d", tables[0].Rect, tc.wantX, tc.wantWidth)
			}
		})
	}
}

func TestTableMalformedMarkupFallback(t *testing.T) {
	doc := styledForLayout(t, `<table cellspacing="0" cellpadding="0">`+
		`loose text<td>orphan cell</td>`+
		`<tr>row text<td colspan="oops">cell</td><td colspan="0">zero</td></tr>`+
		`<tbody><tr><td rowspan="99">huge span</td></tr></tbody></table>`)
	got, err := LayoutWithViewport(doc, image.Rect(0, 0, 300, 400))
	if err != nil {
		t.Fatal(err)
	}
	checkGeometry(t, got.Root, got.Viewport)

	tables := collectBoxes(got.Root, "table")
	if len(tables) != 1 || tables[0].Rect.Dy() <= 0 {
		t.Fatalf("malformed table geometry = %+v", tables)
	}
	text := boxText(tables[0])
	for _, want := range []string{"loose text", "orphan cell", "row text", "cell", "zero", "huge span"} {
		if !strings.Contains(text, want) {
			t.Fatalf("malformed table dropped %q from %q", want, text)
		}
	}
}

func TestTableNarrowViewportStaysPositive(t *testing.T) {
	doc := styledForLayout(t, `<table cellpadding="6" cellspacing="6" width="100%">`+
		`<tr><td width="200">first column</td><td>second column with much longer content</td></tr></table>`)
	for _, width := range []int{1, 8, 30, 120} {
		viewport := image.Rect(0, 0, width, 200)
		got, err := LayoutWithViewport(doc, viewport)
		if err != nil {
			t.Fatalf("width %d: %v", width, err)
		}
		checkGeometry(t, got.Root, viewport)
		tables := collectBoxes(got.Root, "table")
		if len(tables) != 1 {
			t.Fatalf("width %d: tables = %d", width, len(tables))
		}
		if tables[0].Rect.Dx() > width {
			t.Fatalf("width %d: table overflows = %v", width, tables[0].Rect)
		}
	}
}

func TestTableHackerNewsFixtureGeometry(t *testing.T) {
	path := filepath.Join("..", "..", "testdata", "hn", "news.html")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	document := Document{Root: ParseHTML(string(data)), BaseURL: path}
	styled, err := style(document, &Fetcher{})
	if err != nil {
		t.Fatal(err)
	}
	viewport := image.Rect(0, 0, 800, 600)
	got, err := LayoutWithViewport(styled, viewport)
	if err != nil {
		t.Fatal(err)
	}
	checkGeometry(t, got.Root, viewport)

	tables := collectBoxes(got.Root, "table")
	if len(tables) < 3 {
		t.Fatalf("fixture tables = %d", len(tables))
	}
	// The mobile-only max-width:750px rule must not apply at the 800px
	// viewport; the body's default 8px margins leave 784px for the table.
	if tables[0].Rect.Dx() != 666 {
		t.Fatalf("hnmain width = %d, want 85%% of the 784px body content", tables[0].Rect.Dx())
	}
	rows := collectBoxes(got.Root, "tr")
	cells := collectBoxes(got.Root, "td")
	if len(rows) < 50 || len(cells) < 100 {
		t.Fatalf("fixture rows = %d cells = %d", len(rows), len(cells))
	}
	if got.Root.Children[0].Rect.Dy() < 600 {
		t.Fatalf("fixture page height = %d, expected a tall front page", got.Root.Children[0].Rect.Dy())
	}
	if text := boxText(tables[0]); !strings.Contains(text, "Hacker News") {
		t.Fatal("fixture header text missing from table layout")
	}
}

func TestTableLayoutConcurrent(t *testing.T) {
	doc := styledForLayout(t, `<table cellpadding="3" cellspacing="2" width="90%">`+
		`<tr><td rowspan="2">a</td><td colspan="2">b c d</td></tr>`+
		`<tr><td width="40%">e</td><td>f</td></tr></table>`)
	viewport := image.Rect(0, 0, 320, 200)
	reference, err := LayoutWithViewport(doc, viewport)
	if err != nil {
		t.Fatal(err)
	}
	want := collectBoxes(reference.Root, "td")
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 10; j++ {
				got, err := LayoutWithViewport(doc, viewport)
				if err != nil {
					t.Errorf("concurrent layout: %v", err)
					return
				}
				cells := collectBoxes(got.Root, "td")
				if len(cells) != len(want) {
					t.Errorf("cells = %d, want %d", len(cells), len(want))
					return
				}
				for k := range cells {
					if cells[k].Rect != want[k].Rect {
						t.Errorf("cell %d rect = %v, want %v", k, cells[k].Rect, want[k].Rect)
						return
					}
				}
			}
		}()
	}
	wg.Wait()
}

func TestTableCollapsedRowGroupBordersAndFooterOrder(t *testing.T) {
	// Mirrors WPT block-formatting-contexts-003-ref: the footer group renders
	// last, spacing is dropped, and adjoining group borders collapse into one
	// 1px gap between rows.
	doc := styledForLayout(t, `<body style="margin:0"><table style="border-collapse:collapse;width:100%">`+
		`<thead style="border-bottom:1px solid black"><tr><td style="padding:0;height:20px">h</td></tr></thead>`+
		`<tfoot style="border-top:1px solid black"><tr><td style="padding:0;height:20px">f</td></tr></tfoot>`+
		`<tbody style="border-top:1px solid black;border-bottom:1px solid black"><tr><td style="padding:0;height:20px">b</td></tr></tbody>`+
		`</table>`)
	got, err := LayoutWithViewport(doc, image.Rect(0, 0, 400, 400))
	if err != nil {
		t.Fatal(err)
	}

	cells := collectBoxes(got.Root, "td")
	if len(cells) != 3 {
		t.Fatalf("cells = %d", len(cells))
	}
	var order string
	var tops []int
	for _, c := range cells {
		order += boxText(c)
		tops = append(tops, c.Rect.Min.Y)
	}
	if order != "hbf" || tops[0] != 0 || tops[1] != 21 || tops[2] != 42 {
		t.Fatalf("order %q tops %v", order, tops)
	}
	if cells[0].Rect.Min.X != 0 || cells[0].Rect.Dx() != 400 {
		t.Fatalf("collapsed table cell rect = %v, want full width with no spacing", cells[0].Rect)
	}
	tbody := collectBoxes(got.Root, "tbody")[0]
	if tbody.Rect != image.Rect(0, 20, 400, 42) {
		t.Fatalf("tbody rect = %v, want to cover its collapsed borders", tbody.Rect)
	}
	table := collectBoxes(got.Root, "table")[0]
	if table.Rect.Dy() != 62 {
		t.Fatalf("table height = %d, want 62", table.Rect.Dy())
	}
}

func TestTableCollapsedRowGroupBordersPaintOnlyWinningEdge(t *testing.T) {
	tests := []struct {
		name       string
		bottom     string
		top        string
		wantWinner color.RGBA
	}{
		{
			name:       "previous wider border wins",
			bottom:     "4px solid blue",
			top:        "2px solid red",
			wantWinner: color.RGBA{0, 0, 255, 255},
		},
		{
			name:       "following wider border wins",
			bottom:     "2px solid blue",
			top:        "4px solid red",
			wantWinner: color.RGBA{255, 0, 0, 255},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			img := painted(t, `<body style="margin:0"><table style="border-collapse:collapse;width:80px">`+
				`<tbody style="border-bottom:`+tc.bottom+`"><tr><td style="padding:0;height:10px;font-size:0"></td></tr></tbody>`+
				`<tbody style="border-top:`+tc.top+`"><tr><td style="padding:0;height:10px;font-size:0"></td></tr></tbody>`+
				`</table>`, image.Rect(0, 0, 80, 30))
			for y := 10; y < 14; y++ {
				pixel(t, img, 10, y, tc.wantWinner)
			}
		})
	}
}

func TestTableCollapsedRowCellBorderConflict(t *testing.T) {
	repro := styledForLayout(t, `<body style="margin:0"><table style="border-collapse:collapse">`+
		`<tr style="border-bottom:3px solid black"><td style="padding:0">a</td></tr>`+
		`<tr><td style="padding:0;border-top:2px solid red">b</td></tr></table></body>`)
	reproLayout, err := LayoutWithViewport(repro, image.Rect(0, 0, 80, 80))
	if err != nil {
		t.Fatal(err)
	}
	reproCells := collectBoxes(reproLayout.Root, "td")
	if len(reproCells) != 2 {
		t.Fatalf("issue repro cells = %d, want 2", len(reproCells))
	}
	if got := reproCells[1].Rect.Min.Y - reproCells[0].Rect.Max.Y; got != 3 {
		t.Fatalf("issue repro shared gap = %dpx, want winning 3px row border (%v, %v)",
			got, reproCells[0].Rect, reproCells[1].Rect)
	}

	read := func(name string) string {
		t.Helper()
		data, err := os.ReadFile(filepath.Join("..", "..", "testdata", "wpt-local", name))
		if err != nil {
			t.Fatal(err)
		}
		return string(data)
	}
	test, reference := read("collapsed-row-cell-border.html"), read("collapsed-row-cell-border-ref.html")
	viewport := image.Rect(0, 0, 80, 50)

	doc := styledForLayout(t, test)
	layout, err := LayoutWithViewport(doc, viewport)
	if err != nil {
		t.Fatal(err)
	}
	cells := collectBoxes(layout.Root, "td")
	if len(cells) != 2 {
		t.Fatalf("cells = %d, want 2", len(cells))
	}
	if got := cells[1].Rect.Min.Y - cells[0].Rect.Max.Y; got != 3 {
		t.Fatalf("shared border gap = %dpx, want winning row border width 3px (%v, %v)",
			got, cells[0].Rect, cells[1].Rect)
	}

	got, want := painted(t, test, viewport), painted(t, reference, viewport)
	if !bytes.Equal(got.Pix, want.Pix) {
		differing := 0
		for i := 0; i < len(got.Pix); i += 4 {
			if !bytes.Equal(got.Pix[i:i+4], want.Pix[i:i+4]) {
				differing++
			}
		}
		t.Fatalf("render differs from explicit collapsed-edge reference at %d pixels", differing)
	}
}

func TestTableCollapsedOuterEdgesResolveAgainstCells(t *testing.T) {
	markup := `<body style="margin:0"><table style="border-collapse:collapse;border:5px solid red">` +
		`<tr><td style="padding:0;width:30px;height:20px;border:3px solid blue">cell</td></tr></table></body>`
	doc := styledForLayout(t, markup)
	layout, err := LayoutWithViewport(doc, image.Rect(0, 0, 80, 80))
	if err != nil {
		t.Fatal(err)
	}
	tables, cells := collectBoxes(layout.Root, "table"), collectBoxes(layout.Root, "td")
	if len(tables) != 1 || len(cells) != 1 {
		t.Fatalf("boxes = table %d, cell %d; want one each", len(tables), len(cells))
	}
	if tables[0].BorderWidths == nil || *tables[0].BorderWidths != [4]int{3, 3, 3, 3} {
		t.Fatalf("table used outer halves = %v, want [3 3 3 3]", tables[0].BorderWidths)
	}
	if cells[0].BorderWidths == nil || *cells[0].BorderWidths != [4]int{2, 2, 2, 2} {
		t.Fatalf("cell used inner halves = %v, want [2 2 2 2]", cells[0].BorderWidths)
	}
	img := painted(t, markup, image.Rect(0, 0, 80, 80))
	// Each perimeter edge is one continuous winning border, not adjacent
	// table and cell borders. The corner is table-owned and remains red.
	for _, p := range []image.Point{{3, 3}, {35, 3}, {3, 25}, {35, 25}} {
		if got := img.At(p.X, p.Y); got != (color.RGBA{255, 0, 0, 255}) {
			t.Fatalf("outer edge pixel at %v = %v, want red", p, got)
		}
	}
}

func TestTableCollapsedRowCellBorderConflictWithTransparentCells(t *testing.T) {
	read := func(name string) string {
		t.Helper()
		data, err := os.ReadFile(filepath.Join("..", "..", "testdata", "wpt-local", name))
		if err != nil {
			t.Fatal(err)
		}
		return string(data)
	}
	test := read("collapsed-row-cell-transparent-border.html")
	reference := read("collapsed-row-cell-transparent-border-ref.html")
	viewport := image.Rect(0, 0, 80, 50)
	got, want := painted(t, test, viewport), painted(t, reference, viewport)
	if !bytes.Equal(got.Pix, want.Pix) {
		differing := 0
		for i := 0; i < len(got.Pix); i += 4 {
			if !bytes.Equal(got.Pix[i:i+4], want.Pix[i:i+4]) {
				differing++
			}
		}
		t.Fatalf("transparent-cell collapsed edge differs from explicit reference at %d pixels", differing)
	}
	// The losing row border must not paint inside the upper cell. Only the
	// resolved three-pixel edge is black.
	for y := 17; y < 20; y++ {
		pixel(t, got, 10, y, color.RGBA{255, 255, 255, 255})
	}
	for y := 20; y < 23; y++ {
		pixel(t, got, 10, y, color.RGBA{0, 0, 0, 255})
	}
}

func TestTableCollapsedAdjacentCellsMatchReference(t *testing.T) {
	read := func(name string) string {
		t.Helper()
		data, err := os.ReadFile(filepath.Join("..", "..", "testdata", "wpt-local", name))
		if err != nil {
			t.Fatal(err)
		}
		return string(data)
	}
	viewport := image.Rect(0, 0, 800, 600)
	got := painted(t, read("collapsed-border-conflict.html"), viewport)
	want := painted(t, read("collapsed-border-conflict-ref.html"), viewport)
	if !bytes.Equal(got.Pix, want.Pix) {
		differing := 0
		for i := 0; i < len(got.Pix); i += 4 {
			if !bytes.Equal(got.Pix[i:i+4], want.Pix[i:i+4]) {
				differing++
			}
		}
		t.Fatalf("two-cell shared border differs from explicit reference at %d pixels", differing)
	}
}

func TestTableCollapsedOuterCellTrailingBordersContributeToGeometry(t *testing.T) {
	doc := styledForLayout(t, `<body style="margin:0"><table style="border-collapse:collapse">`+
		`<caption style="border:4px solid green">caption</caption>`+
		`<tr><td style="border:4px solid orange;width:100px;height:30px;padding:0">cell 1</td></tr>`+
		`</table></body>`)
	got, err := LayoutWithViewport(doc, image.Rect(0, 0, 300, 120))
	if err != nil {
		t.Fatal(err)
	}
	table := collectBoxes(got.Root, "table")
	cells := collectBoxes(got.Root, "td")
	if len(table) != 1 || len(cells) != 1 {
		t.Fatalf("tables=%d cells=%d", len(table), len(cells))
	}
	// The caption is a block container, so its 4px borders add to its 20px
	// line: the cell starts below the 28px caption border box.
	captions := collectBoxes(got.Root, "caption")
	if want := image.Rect(0, 0, 110, 28); len(captions) != 1 || captions[0].Rect != want {
		t.Fatalf("captions = %v, want one at %v", captions, want)
	}
	if want := image.Rect(0, 28, 110, 68); cells[0].Rect != want {
		t.Fatalf("collapsed cell rect = %v, want %v", cells[0].Rect, want)
	}
	if table[0].Rect != image.Rect(0, 28, 110, 68) {
		t.Fatalf("collapsed table border box = %v, want (0,28)-(110,68)", table[0].Rect)
	}
}

func TestTableCollapsedBorderOffsetReftestPixels(t *testing.T) {
	tests := []struct {
		name      string
		test      string
		reference string
	}{
		{
			name: "enclosing border",
			test: `<body><div style="position:absolute;border:4px solid green">` +
				`<table style="border-collapse:collapse"><tr><td style="padding:0;border:4px solid orange;width:100px;height:30px;text-align:center">cell 1</td></tr></table></div></body>`,
			reference: `<body><!-- comments do not affect the static position --><div style="position:absolute;border:4px solid green">` +
				`<table style="border-spacing:0"><tr><td style="padding:0;border:4px solid orange;width:100px;height:30px;text-align:center">cell 1</td></tr></table></div></body>`,
		},
		{
			name: "caption border",
			test: `<body><table style="border-collapse:collapse"><caption style="border:4px solid green">caption</caption>` +
				`<tr><td style="padding:0;border:4px solid orange;width:100px;height:30px;text-align:center">cell 1</td></tr></table></body>`,
			reference: `<body><table style="border-spacing:0"><caption style="border:4px solid green">caption</caption>` +
				`<tr><td style="padding:0;border:4px solid orange;width:102px;height:32px;text-align:center">cell 1</td></tr></table></body>`,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			viewport := image.Rect(0, 0, 180, 100)
			got := painted(t, tc.test, viewport)
			want := painted(t, tc.reference, viewport)
			if !bytes.Equal(got.Pix, want.Pix) {
				differing := 0
				for i := 0; i < len(got.Pix); i += 4 {
					if !bytes.Equal(got.Pix[i:i+4], want.Pix[i:i+4]) {
						differing++
					}
				}
				t.Fatalf("render differs from reference at %d pixels", differing)
			}
		})
	}
}

// emptyRowTable builds a collapsed two-column table whose content rows are
// separated by cell-less rows of the given style (WPT
// tables/border-collapse-empty-row). rowStyle may be empty.
func emptyRowTable(rowStyle, bottomBorder string) string {
	var b strings.Builder
	b.WriteString(`<body style="margin:0"><table style="border-collapse:collapse">`)
	for i := 0; i < 3; i++ {
		cell := `<td style="border:10px solid black;padding:0;width:10px;height:10px`
		if bottomBorder != "" && i < 2 {
			cell += ";border-bottom:" + bottomBorder
		}
		cell += `"></td>`
		b.WriteString("<tr>" + cell + cell + "</tr>")
		if rowStyle != "-" && i < 2 {
			b.WriteString(`<tr style="` + rowStyle + `"></tr>`)
		}
	}
	b.WriteString("</table></body>")
	return b.String()
}

func TestTableCollapsedEmptyRowsKeepVerticalBordersContinuous(t *testing.T) {
	viewport := image.Rect(0, 0, 80, 140)
	for _, tc := range []struct {
		name, rowStyle, reference string
	}{
		{name: "zero height", rowStyle: "", reference: ""},
		{name: "smaller than half border", rowStyle: "height:2px", reference: "12px solid black"},
		{name: "equal to half border", rowStyle: "height:5px", reference: "15px solid black"},
		{name: "equal to both halves", rowStyle: "height:10px", reference: "20px solid black"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := painted(t, emptyRowTable(tc.rowStyle, ""), viewport)
			want := painted(t, emptyRowTable("-", tc.reference), viewport)
			if !bytes.Equal(got.Pix, want.Pix) {
				differing := 0
				for i := 0; i < len(got.Pix); i += 4 {
					if !bytes.Equal(got.Pix[i:i+4], want.Pix[i:i+4]) {
						differing++
					}
				}
				t.Fatalf("%d pixels differ from the cell-border reference", differing)
			}
		})
	}
}

func TestTableCollapsedEmptyRowBandsUseAdjacentBorderHalves(t *testing.T) {
	// A 30px empty row keeps a 10px gap between the 10px outer halves of
	// the neighboring red bottom and blue top borders; the band never
	// paints the cells' green backgrounds into the row.
	img := painted(t, `<body style="margin:0"><table style="border-collapse:collapse">`+
		`<tr><td style="border:0;border-bottom:20px solid red;padding:0;width:20px;height:10px;background:green"></td></tr>`+
		`<tr style="height:30px"></tr>`+
		`<tr><td style="border:0;border-top:20px solid blue;padding:0;width:20px;height:10px;background:green"></td></tr>`+
		`</table></body>`, image.Rect(0, 0, 40, 120))
	red, blue, white := color.RGBA{255, 0, 0, 255}, color.RGBA{0, 0, 255, 255}, color.RGBA{255, 255, 255, 255}
	for _, tc := range []struct {
		y    int
		want color.RGBA
	}{
		{29, red}, {30, red}, {39, red}, {40, white}, {49, white}, {50, blue}, {59, blue}, {60, blue},
	} {
		if got := img.RGBAAt(10, tc.y); got != tc.want {
			t.Errorf("pixel at y=%d = %v, want %v", tc.y, got, tc.want)
		}
	}
}

func TestTableCollapsedEmptyRowBandsAreBorderOnlyFragments(t *testing.T) {
	doc := styledForLayout(t, emptyRowTable("height:4px", ""))
	got, err := LayoutWithViewport(doc, image.Rect(0, 0, 80, 140))
	if err != nil {
		t.Fatal(err)
	}
	rows := collectBoxes(got.Root, "tr")
	if len(rows) != 5 {
		t.Fatalf("rows = %d, want 5", len(rows))
	}
	empty := rows[1]
	if len(empty.Children) != 4 {
		t.Fatalf("empty row fragments = %d, want 4 (two cells above, two below)", len(empty.Children))
	}
	for _, band := range empty.Children {
		if !band.BorderOnly || band.BorderWidths == nil {
			t.Fatalf("band %v is not a border-only fragment", band.Rect)
		}
		if band.Rect.Min.Y < empty.Rect.Min.Y || band.Rect.Max.Y > empty.Rect.Max.Y || band.Rect.Dy() != 4 {
			t.Fatalf("band %v escapes empty row %v", band.Rect, empty.Rect)
		}
	}
	// Separated-border tables never bridge empty rows.
	doc = styledForLayout(t, `<body style="margin:0"><table><tr><td style="border:10px solid black"></td></tr>`+
		`<tr style="height:4px"></tr><tr><td style="border:10px solid black"></td></tr></table></body>`)
	got, err = LayoutWithViewport(doc, image.Rect(0, 0, 80, 140))
	if err != nil {
		t.Fatal(err)
	}
	if rows := collectBoxes(got.Root, "tr"); len(rows) != 3 || len(rows[1].Children) != 0 {
		t.Fatalf("separated empty row gained fragments: %+v", rows)
	}
}
