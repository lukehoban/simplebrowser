package browser

import (
	"image"
	"testing"
)

// classBoxes returns the outermost box for each element with the given class
// attribute, in document order.
func classBoxes(root *Box, class string) []*Box {
	var result []*Box
	var walk func(box, parent *Box)
	walk = func(box, parent *Box) {
		if box == nil {
			return
		}
		if box.Node != nil && (parent == nil || parent.Node != box.Node) {
			if a, ok := box.Node.Attribute("class"); ok && a.Value == class {
				result = append(result, box)
			}
		}
		for _, child := range box.Children {
			walk(child, box)
		}
	}
	walk(root, nil)
	return result
}

// styledByClass returns the first styled node with the given class attribute.
func styledByClass(root *StyledNode, class string) *StyledNode {
	if root == nil {
		return nil
	}
	if a, ok := root.Node.Attribute("class"); ok && a.Value == class {
		return root
	}
	for _, child := range root.Children {
		if found := styledByClass(child, class); found != nil {
			return found
		}
	}
	return nil
}

// WPT colors/color-applies-to-001: color set on a table-row-group inherits
// through rows into cells, overriding the table's color.
func TestRowGroupColorInheritsToCells(t *testing.T) {
	doc := styledForLayout(t, `<style>
		.t { display: table; color: red }
		.g { display: table-row-group; color: green }
		.r { display: table-row }
		.c { display: table-cell }
	</style><div class="t"><div class="g"><div class="r"><div class="c">x</div></div></div></div>`)
	for _, class := range []string{"g", "r", "c"} {
		n := styledByClass(doc.StyleRoot, class)
		if n == nil || n.Style["color"] != "green" {
			t.Fatalf("%s color = %v", class, n)
		}
	}
}

// border-spacing is inherited, so a CSS table nested in an HTML table picks up
// the outer table's spacing rather than the HTML table default.
func TestBorderSpacingInherits(t *testing.T) {
	doc := styledForLayout(t, `<table cellspacing="5"><tr><td>`+
		`<div class="inner" style="display:table"></div></td></tr></table>`)
	if n := styledByClass(doc.StyleRoot, "inner"); n == nil || n.Style["border-spacing"] != "5px" {
		t.Fatalf("inner border-spacing = %v", n)
	}
}

// A display:table element that is not an HTML <table> has the CSS initial
// border-spacing of 0; only the UA <table> rule adds 2px.
func TestCSSTableHasNoDefaultBorderSpacing(t *testing.T) {
	doc := styledForLayout(t, `<style>.t{display:table}.r{display:table-row}.c{display:table-cell}</style>`+
		`<div class="t"><div class="r"><div class="c">ab</div><div class="c">cd</div></div>`+
		`<div class="r"><div class="c">ef</div><div class="c">gh</div></div></div>`+
		`<table><tr><td class="h">ab</td><td class="h">cd</td></tr></table>`)
	got, err := LayoutWithViewport(doc, image.Rect(0, 0, 400, 400))
	if err != nil {
		t.Fatal(err)
	}
	checkGeometry(t, got.Root, got.Viewport)

	table := classBoxes(got.Root, "t")
	cells := classBoxes(got.Root, "c")
	if len(table) != 1 || len(cells) != 4 {
		t.Fatalf("tables=%d cells=%d", len(table), len(cells))
	}
	if cells[0].Rect.Min != table[0].Rect.Min {
		t.Fatalf("first cell %v not at table origin %v", cells[0].Rect, table[0].Rect)
	}
	if cells[1].Rect.Min.X != cells[0].Rect.Max.X {
		t.Fatalf("horizontal gap between cells: %v then %v", cells[0].Rect, cells[1].Rect)
	}
	if cells[2].Rect.Min.Y != cells[0].Rect.Max.Y {
		t.Fatalf("vertical gap between rows: %v then %v", cells[0].Rect, cells[2].Rect)
	}

	html := classBoxes(got.Root, "h")
	if len(html) != 2 || html[1].Rect.Min.X-html[0].Rect.Max.X != 2 {
		t.Fatalf("HTML table should keep 2px UA spacing: %v", html)
	}
}
