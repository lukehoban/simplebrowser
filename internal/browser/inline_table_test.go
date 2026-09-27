package browser

import (
	"image"
	"image/color"
	"testing"
)

// layoutMarkup lays out markup in a viewport and returns the root box.
func layoutMarkup(t *testing.T, markup string, viewport image.Rectangle) *Box {
	t.Helper()
	doc := styledForLayout(t, markup)
	layout, err := LayoutWithViewport(doc, viewport)
	if err != nil {
		t.Fatal(err)
	}
	return layout.Root
}

// firstRun returns the first text run whose text matches want anywhere in the
// box tree, so tests can compare baselines across inline fragments.
func firstRun(b *Box, want string) (TextRun, bool) {
	for _, run := range b.Text {
		if run.Text == want {
			return run, true
		}
	}
	for _, child := range b.Children {
		if run, ok := firstRun(child, want); ok {
			return run, true
		}
	}
	return TextRun{}, false
}

func boxParent(root, target *Box) *Box {
	for _, child := range root.Children {
		if child == target {
			return root
		}
		if parent := boxParent(child, target); parent != nil {
			return parent
		}
	}
	return nil
}

func TestInlineTablesShareOneLine(t *testing.T) {
	root := layoutMarkup(t, `<body style="margin:0;width:400px">`+
		`<table style="display:inline-table;border:2px solid red"><tr><td>A</td></tr></table>`+
		`<table style="display:inline-table;border:2px solid blue"><tr><td>B</td></tr></table>`+
		`</body>`, image.Rect(0, 0, 400, 200))
	tables := collectBoxes(root, "table")
	if len(tables) != 2 {
		t.Fatalf("got %d table boxes, want 2", len(tables))
	}
	first, second := tables[0], tables[1]
	if boxText(first) != "A" || boxText(second) != "B" {
		t.Fatalf("table text = %q, %q, want \"A\", \"B\"", boxText(first), boxText(second))
	}
	if first.Rect.Min.Y != second.Rect.Min.Y {
		t.Errorf("tables start at y %d and %d, want the same line", first.Rect.Min.Y, second.Rect.Min.Y)
	}
	if second.Rect.Min.X != first.Rect.Max.X {
		t.Errorf("second table starts at x %d, want %d (immediately after the first)",
			second.Rect.Min.X, first.Rect.Max.X)
	}
	if first.Rect.Dx() <= 0 || first.Rect.Dy() <= 0 {
		t.Errorf("first table rect %v has no area", first.Rect)
	}
}

func TestInlineTableSitsBetweenSurroundingText(t *testing.T) {
	root := layoutMarkup(t, `<body style="margin:0;width:400px"><div>before `+
		`<table style="display:inline-table"><tr><td>cell</td></tr></table>`+
		` after</div></body>`, image.Rect(0, 0, 400, 200))
	tables := collectBoxes(root, "table")
	if len(tables) != 1 {
		t.Fatalf("got %d table boxes, want 1", len(tables))
	}
	before, ok := firstRun(root, "before ")
	if !ok {
		t.Fatal("missing leading text run")
	}
	after, ok := firstRun(root, " after")
	if !ok {
		t.Fatal("missing trailing text run")
	}
	if before.Rect.Min.Y != after.Rect.Min.Y {
		t.Errorf("surrounding text on different lines: %v and %v", before.Rect, after.Rect)
	}
	table := tables[0]
	if table.Rect.Min.X < before.Rect.Max.X {
		t.Errorf("table %v starts before the preceding text %v", table.Rect, before.Rect)
	}
	if after.Rect.Min.X < table.Rect.Max.X {
		t.Errorf("trailing text %v starts before the table ends %v", after.Rect, table.Rect)
	}
	// The inline table aligns on the baseline of its first row, so its cell
	// text shares the baseline of the text around it.
	cell, ok := firstRun(table, "cell")
	if !ok {
		t.Fatal("missing cell text run")
	}
	if cell.Rect.Min.Y != before.Rect.Min.Y {
		t.Errorf("cell text at y %d, want the surrounding baseline at %d",
			cell.Rect.Min.Y, before.Rect.Min.Y)
	}
}

func TestInlineTableVerticalAlignments(t *testing.T) {
	for _, tc := range []struct {
		name  string
		align string
		check func(t *testing.T, line, table *Box, baseline, ascent, descent int)
	}{
		{"baseline", "baseline", func(t *testing.T, _ *Box, table *Box, baseline, _, _ int) {
			cell, ok := firstRun(table, "cell")
			if !ok {
				t.Fatal("missing cell text")
			}
			faces := newFaceSet()
			defer faces.close()
			cellAscent, _ := faces.metrics(cell.Style).lineMetrics()
			if got := cell.Rect.Min.Y + cellAscent; got != baseline {
				t.Fatalf("first-row baseline = %d, want surrounding baseline %d", got, baseline)
			}
		}},
		{"top", "top", func(t *testing.T, line, table *Box, _, _, _ int) {
			if table.Rect.Min.Y != line.Rect.Min.Y {
				t.Fatalf("table top = %d, line top = %d", table.Rect.Min.Y, line.Rect.Min.Y)
			}
		}},
		{"middle", "middle", func(t *testing.T, _ *Box, table *Box, baseline, _, _ int) {
			if got := table.Rect.Min.Y + table.Rect.Dy()/2; got != baseline-4 {
				t.Fatalf("table midpoint = %d, want %d", got, baseline-4)
			}
		}},
		{"bottom", "bottom", func(t *testing.T, line, table *Box, _, _, _ int) {
			if table.Rect.Max.Y != line.Rect.Max.Y {
				t.Fatalf("table bottom = %d, line bottom = %d", table.Rect.Max.Y, line.Rect.Max.Y)
			}
		}},
		{"text-top", "text-top", func(t *testing.T, _ *Box, table *Box, baseline, ascent, _ int) {
			if table.Rect.Min.Y != baseline-ascent {
				t.Fatalf("table top = %d, parent text top = %d", table.Rect.Min.Y, baseline-ascent)
			}
		}},
		{"text-bottom", "text-bottom", func(t *testing.T, _ *Box, table *Box, baseline, _, descent int) {
			if table.Rect.Max.Y != baseline+descent {
				t.Fatalf("table bottom = %d, parent text bottom = %d", table.Rect.Max.Y, baseline+descent)
			}
		}},
		{"length", "12px", func(t *testing.T, _ *Box, table *Box, baseline, _, _ int) {
			cell, ok := firstRun(table, "cell")
			if !ok {
				t.Fatal("missing cell text")
			}
			faces := newFaceSet()
			defer faces.close()
			cellAscent, _ := faces.metrics(cell.Style).lineMetrics()
			if got := cell.Rect.Min.Y + cellAscent; got != baseline-12 {
				t.Fatalf("raised first-row baseline = %d, want %d", got, baseline-12)
			}
		}},
		{"percentage", "50%", func(t *testing.T, _ *Box, table *Box, baseline, ascent, descent int) {
			cell, ok := firstRun(table, "cell")
			if !ok {
				t.Fatal("missing cell text")
			}
			faces := newFaceSet()
			defer faces.close()
			cellAscent, _ := faces.metrics(cell.Style).lineMetrics()
			want := baseline - int(float64(ascent+descent)*0.5+0.5)
			if got := cell.Rect.Min.Y + cellAscent; got != want {
				t.Fatalf("percentage-shifted baseline = %d, want %d", got, want)
			}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			markup := `<body style="margin:0;font:16px sans-serif"><div>A` +
				`<table style="display:inline-table;vertical-align:` + tc.align +
				`;border:2px solid red;border-spacing:0"><tr><td style="height:28px">cell</td></tr></table>z</div></body>`
			root := layoutMarkup(t, markup, image.Rect(0, 0, 300, 120))
			tables := collectBoxes(root, "table")
			if len(tables) != 1 {
				t.Fatalf("got %d tables, want 1", len(tables))
			}
			line := boxParent(root, tables[0])
			if line == nil {
				t.Fatal("missing containing line box")
			}
			run, ok := firstRun(line, "A")
			if !ok {
				t.Fatal("missing surrounding text")
			}
			faces := newFaceSet()
			ascent, descent := faces.metrics(run.Style).lineMetrics()
			faces.close()
			tc.check(t, line, tables[0], run.Rect.Min.Y+ascent, ascent, descent)
		})
	}
}

func TestPaintInlineTableVerticalAlignTop(t *testing.T) {
	const markup = `<body style="margin:0;font:16px sans-serif">A` +
		`<table style="display:inline-table;vertical-align:top;border:4px solid red;border-spacing:0">` +
		`<tr><td style="height:32px">cell</td></tr></table>z</body>`
	viewport := image.Rect(0, 0, 240, 100)
	root := layoutMarkup(t, markup, viewport)
	tables := collectBoxes(root, "table")
	if len(tables) != 1 {
		t.Fatalf("got %d tables, want 1", len(tables))
	}
	line := boxParent(root, tables[0])
	if line == nil {
		t.Fatal("missing containing line box")
	}
	img := painted(t, markup, viewport)
	pixel(t, img, tables[0].Rect.Min.X+1, line.Rect.Min.Y+1, color.RGBA{255, 0, 0, 255})
}

func TestInlineTableWrapsWhenLineIsFull(t *testing.T) {
	root := layoutMarkup(t, `<body style="margin:0;width:150px">`+
		`<table style="display:inline-table"><tr><td>aaaa aaaa</td></tr></table>`+
		`<table style="display:inline-table"><tr><td>bbbb bbbb</td></tr></table>`+
		`</body>`, image.Rect(0, 0, 400, 200))
	tables := collectBoxes(root, "table")
	if len(tables) != 2 {
		t.Fatalf("got %d table boxes, want 2", len(tables))
	}
	first, second := tables[0], tables[1]
	if second.Rect.Min.Y < first.Rect.Max.Y {
		t.Errorf("second table %v did not wrap below the first %v", second.Rect, first.Rect)
	}
	if second.Rect.Min.X != first.Rect.Min.X {
		t.Errorf("wrapped table starts at x %d, want %d", second.Rect.Min.X, first.Rect.Min.X)
	}
}

// The WPT tables/border-collapse-empty-row case lays four collapsed-border
// inline tables side by side. Stacking them vertically compressed the whole
// page; this keeps the four-up row.
func TestInlineTableEmptyRowFourTablesStayOnOneLine(t *testing.T) {
	markup := `<body style="margin:0;width:600px"><style>table{display:inline-table;border-collapse:collapse;border:2px solid blue}td{border:2px solid green}</style>`
	for _, label := range []string{"one", "two", "three", "four"} {
		markup += `<table><tr><td>` + label + `</td></tr><tr></tr></table>`
	}
	markup += `</body>`
	root := layoutMarkup(t, markup, image.Rect(0, 0, 600, 300))
	tables := collectBoxes(root, "table")
	if len(tables) != 4 {
		t.Fatalf("got %d table boxes, want 4", len(tables))
	}
	for i, table := range tables {
		if table.Rect.Min.Y != tables[0].Rect.Min.Y {
			t.Errorf("table %d starts at y %d, want %d", i, table.Rect.Min.Y, tables[0].Rect.Min.Y)
		}
		if i > 0 && table.Rect.Min.X < tables[i-1].Rect.Max.X {
			t.Errorf("table %d at %v overlaps table %d at %v", i, table.Rect, i-1, tables[i-1].Rect)
		}
	}
}

func TestPaintInlineTablesSideBySideKeepBorders(t *testing.T) {
	red := color.RGBA{255, 0, 0, 255}
	blue := color.RGBA{0, 0, 255, 255}
	markup := `<body style="margin:0;width:200px;font:16px sans-serif">` +
		`<table style="display:inline-table;border:4px solid red;border-spacing:0"><tr><td>A</td></tr></table>` +
		`<table style="display:inline-table;border:4px solid blue;border-spacing:0"><tr><td>B</td></tr></table>` +
		`</body>`
	root := layoutMarkup(t, markup, image.Rect(0, 0, 200, 100))
	tables := collectBoxes(root, "table")
	if len(tables) != 2 {
		t.Fatalf("got %d table boxes, want 2", len(tables))
	}

	img := painted(t, markup, image.Rect(0, 0, 200, 100))
	for _, probe := range []struct {
		box  *Box
		want color.RGBA
	}{{tables[0], red}, {tables[1], blue}} {
		mid := (probe.box.Rect.Min.Y + probe.box.Rect.Max.Y) / 2
		pixel(t, img, probe.box.Rect.Min.X+1, mid, probe.want)
		pixel(t, img, probe.box.Rect.Max.X-1, mid, probe.want)
	}
}

// The two kinds of atomic inline box must share the pen and retain their own
// paint layers above an ancestor inline background.
func TestInlineTableAndEmptyInlineBlockShareLineAndPaint(t *testing.T) {
	const markup = `<body style="margin:0"><div><span style="background:yellow">start` +
		`<table style="display:inline-table;border:3px solid red;border-spacing:0"><tr><td>T</td></tr></table>` +
		`<span style="display:inline-block;width:16px;height:12px;background:grey;border:2px solid blue"></span>` +
		`end</span></div></body>`
	viewport := image.Rect(0, 0, 400, 100)
	root := layoutMarkup(t, markup, viewport)
	tables, spans := collectBoxes(root, "table"), collectBoxes(root, "span")
	if len(tables) != 1 || len(spans) != 1 {
		t.Fatalf("got %d tables and %d atomic spans, want one each", len(tables), len(spans))
	}
	table, block := tables[0], spans[0]
	before, okBefore := firstRun(root, "start")
	after, okAfter := firstRun(root, "end")
	if !okBefore || !okAfter {
		t.Fatalf("missing surrounding text: before=%v after=%v", okBefore, okAfter)
	}
	if before.Rect.Min.Y != after.Rect.Min.Y ||
		before.Rect.Max.X > table.Rect.Min.X+1 || // rounded glyph bounds may overlap by 1px
		table.Rect.Max.X > block.Rect.Min.X ||
		block.Rect.Max.X > after.Rect.Min.X {
		t.Errorf("line order: before=%v table=%v block=%v after=%v",
			before.Rect, table.Rect, block.Rect, after.Rect)
	}
	img := painted(t, markup, viewport)
	pixel(t, img, table.Rect.Min.X+1, table.Rect.Min.Y+table.Rect.Dy()/2, color.RGBA{255, 0, 0, 255})
	pixel(t, img, block.Rect.Min.X, block.Rect.Min.Y, color.RGBA{0, 0, 255, 255})
	pixel(t, img, block.Content.Min.X, block.Content.Min.Y, color.RGBA{128, 128, 128, 255})
}
