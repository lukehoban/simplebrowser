package browser

import (
	"fmt"
	"image"
	"image/color"
	"os"
	"strings"
	"testing"
)

var (
	visRed   = color.RGBA{255, 0, 0, 255}
	visGreen = color.RGBA{0, 128, 0, 255}
	visBlue  = color.RGBA{0, 0, 255, 255}
	visWhite = color.RGBA{255, 255, 255, 255}
)

// geometryDump flattens every box, text run and image rectangle so two
// layouts can be compared for identical geometry.
func geometryDump(t *testing.T, markup string, viewport image.Rectangle) string {
	t.Helper()
	l, err := LayoutWithViewport(styledForLayout(t, markup), viewport)
	if err != nil {
		t.Fatal(err)
	}
	var b strings.Builder
	var walk func(*Box, int)
	walk = func(box *Box, depth int) {
		fmt.Fprintf(&b, "%*sbox %v %v\n", depth, "", box.Rect, box.Content)
		for _, run := range box.Text {
			fmt.Fprintf(&b, "%*stext %q %v %v\n", depth, "", run.Text, run.Rect, run.PenX)
		}
		for _, picture := range box.Images {
			fmt.Fprintf(&b, "%*simage %v\n", depth, "", picture.Rect)
		}
		for _, fragment := range box.InlineBackgrounds {
			fmt.Fprintf(&b, "%*sinline-bg %v\n", depth, "", fragment.Rect)
		}
		for _, child := range box.Children {
			walk(child, depth+1)
		}
	}
	walk(l.Root, 0)
	return b.String()
}

func TestVisibilityDoesNotChangeLayout(t *testing.T) {
	const template = `<body style="margin:0">
		<div style="%[1]swidth:40px;height:40px;background:red;border:2px solid black;padding:3px">hidden</div>
		<p style="margin:0">a <span style="%[1]sbackground:red">gap</span> b <img width="12" height="12" style="%[1]s"></p>
		<div style="%[1]sdisplay:flex;gap:4px"><span style="width:10px">x</span><span style="visibility:visible">y</span></div>
		<table style="%[1]s"><tr><td style="background:red">cell</td></tr></table>
		<div style="%[1]sposition:absolute;left:100px;top:5px;width:20px;height:20px;background:red"></div>
		</body>`
	viewport := image.Rect(0, 0, 200, 200)
	visible := geometryDump(t, fmt.Sprintf(template, ""), viewport)
	for _, value := range []string{"hidden", "collapse"} {
		got := geometryDump(t, fmt.Sprintf(template, "visibility:"+value+";"), viewport)
		if got != visible {
			t.Errorf("visibility:%s changed layout:\n got %s\nwant %s", value, got, visible)
		}
	}
}

func TestVisibilityHiddenIssueRepro(t *testing.T) {
	// #294: the hidden box keeps its 40px of layout space but paints nothing.
	img := painted(t, `<body style="margin:0"><div style="visibility:hidden;width:40px;height:40px;background:red">hidden</div>
		<div style="width:40px;height:40px;background:green"></div></body>`, image.Rect(0, 0, 80, 100))
	for y := 0; y < 40; y++ {
		for x := 0; x < 80; x++ {
			if got := img.RGBAAt(x, y); got != visWhite {
				t.Fatalf("hidden box painted %v at (%d,%d)", got, x, y)
			}
		}
	}
	pixel(t, img, 30, 20, visWhite)
	pixel(t, img, 20, 40, visGreen)
	pixel(t, img, 20, 79, visGreen)
	pixel(t, img, 20, 80, visWhite)
}

func TestVisibilityHiddenSkipsBordersAndBackgroundImages(t *testing.T) {
	img := painted(t, `<body style="margin:0"><div style="visibility:hidden;width:30px;height:30px;
		border:5px solid blue;background:linear-gradient(red, red)"></div></body>`, image.Rect(0, 0, 60, 60))
	pixel(t, img, 2, 2, visWhite)
	pixel(t, img, 20, 20, visWhite)
	pixel(t, img, 38, 38, visWhite)
}

func TestVisibilityVisibleDescendantPaints(t *testing.T) {
	img := painted(t, `<body style="margin:0"><div style="visibility:hidden;width:60px;height:40px;background:red">
		<div style="visibility:visible;width:20px;height:20px;background:blue"></div>
		<div style="width:20px;height:20px;background:red"></div>
		</div></body>`, image.Rect(0, 0, 80, 60))
	pixel(t, img, 10, 10, visBlue)
	pixel(t, img, 40, 10, visWhite) // hidden parent background
	pixel(t, img, 10, 30, visWhite) // sibling inherits hidden
}

func TestVisibilityHiddenInlineAndText(t *testing.T) {
	const markup = `<body style="margin:0;font:16px sans-serif"><p style="margin:0">
		<span id="s" style="%sbackground:red;color:red">XXXX</span></p></body>`
	viewport := image.Rect(0, 0, 100, 30)
	shown := painted(t, fmt.Sprintf(markup, ""), viewport)
	hidden := painted(t, fmt.Sprintf(markup, "visibility:hidden;"), viewport)
	if !containsColor(shown, visRed) {
		t.Fatal("control rendering has no red inline background")
	}
	for y := 0; y < 30; y++ {
		for x := 0; x < 100; x++ {
			if got := hidden.RGBAAt(x, y); got != visWhite {
				t.Fatalf("hidden inline painted %v at (%d,%d)", got, x, y)
			}
		}
	}
}

func TestVisibilityVisibleTextInsideHiddenInline(t *testing.T) {
	img := painted(t, `<body style="margin:0;font:16px sans-serif"><div style="visibility:hidden;color:red">
		HIDDEN <span style="visibility:visible;color:blue">SHOWN</span></div></body>`, image.Rect(0, 0, 200, 30))
	if containsColor(img, visRed) {
		t.Error("hidden text painted")
	}
	if !containsColor(img, visBlue) {
		t.Error("visible descendant text not painted")
	}
}

func TestVisibilityHiddenImages(t *testing.T) {
	// A missing image paints a gray placeholder; hidden images skip it, both
	// inline and block-level, unless the image itself is visible.
	placeholder := color.RGBA{160, 160, 160, 255}
	for _, tc := range []struct {
		name, markup string
		want         bool
	}{
		{"inline", `<p style="margin:0;visibility:hidden"><img width="20" height="20"></p>`, false},
		{"block", `<div style="visibility:hidden"><img width="20" height="20" style="display:block"></div>`, false},
		{"own", `<img width="20" height="20" style="visibility:hidden">`, false},
		{"visible-in-hidden", `<p style="margin:0;visibility:hidden"><img width="20" height="20" style="visibility:visible"></p>`, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			img := painted(t, `<body style="margin:0">`+tc.markup+`</body>`, image.Rect(0, 0, 40, 40))
			if got := containsColor(img, placeholder); got != tc.want {
				t.Errorf("placeholder painted = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestVisibilityHiddenPositionedAndTableBoxes(t *testing.T) {
	img := painted(t, `<body style="margin:0">
		<table style="margin-top:30px;border-collapse:collapse;visibility:hidden">
			<tr><td style="width:20px;height:20px;background:red;border:2px solid red"></td>
			<td style="visibility:visible;width:20px;height:20px;background:blue"></td></tr></table>
		<div style="visibility:hidden;position:absolute;z-index:1;left:0;top:0;width:20px;height:20px;background:red">
			<div style="visibility:visible;position:absolute;left:30px;top:0;width:10px;height:10px;background:blue"></div>
		</div>
		</body>`, image.Rect(0, 0, 80, 80))
	pixel(t, img, 10, 10, visWhite)
	pixel(t, img, 35, 5, visBlue)
	if containsColor(img, visRed) {
		t.Error("hidden positioned or table box painted red")
	}
	found := false
	for x := 24; x < 60; x++ {
		if img.RGBAAt(x, 42) == visBlue {
			found = true
		}
	}
	if !found {
		t.Error("visible table cell in hidden table not painted")
	}
}

func TestVisibilityCascade(t *testing.T) {
	for _, tc := range []struct {
		name, parent, child string
		want                string
	}{
		{"initial", "", "", "visible"},
		{"inherited", "visibility:hidden", "", "hidden"},
		{"override", "visibility:hidden", "visibility:visible", "visible"},
		{"collapse", "", "visibility:COLLAPSE", "COLLAPSE"},
		{"invalid keeps inherited", "visibility:hidden", "visibility:bogus", "hidden"},
		{"invalid keeps earlier", "", "visibility:hidden;visibility:bogus", "hidden"},
		{"initial keyword", "visibility:hidden", "visibility:initial", "visible"},
		{"unset inherits", "visibility:hidden", "visibility:unset", "hidden"},
		{"inherit", "visibility:hidden", "visibility:inherit", "hidden"},
		{"var", "visibility:hidden;--v:visible", "visibility:var(--v)", "visible"},
		{"invalid var is unset", "visibility:hidden;--v:bogus", "visibility:var(--v)", "hidden"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			doc := styledForLayout(t, `<div id="p" style="`+tc.parent+`"><div id="c" style="`+tc.child+`">x</div></div>`)
			var child *Node
			var find func(*Node)
			find = func(n *Node) {
				if attr, ok := n.Attribute("id"); ok && attr.Value == "c" {
					child = n
				}
				for _, c := range n.Children {
					find(c)
				}
			}
			find(doc.Document.Root)
			if child == nil {
				t.Fatal("child not found")
			}
			if got := doc.Styles[child]["visibility"]; got != tc.want {
				t.Errorf("visibility = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestVisibilityFixture(t *testing.T) {
	source, err := os.ReadFile("../../testdata/visibility/index.html")
	if err != nil {
		t.Fatal(err)
	}
	img := painted(t, string(source), image.Rect(0, 0, 400, 300))
	if containsColor(img, visRed) {
		t.Error("fixture paints red from a hidden box")
	}
	if !containsColor(img, visGreen) {
		t.Error("fixture boxes after hidden content are missing")
	}
	if !containsColor(img, color.RGBA{0x33, 0x99, 0xcc, 255}) {
		t.Error("visible child of hidden parent is missing")
	}
}

func containsColor(img *image.RGBA, want color.RGBA) bool {
	b := img.Bounds()
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			if img.RGBAAt(x, y) == want {
				return true
			}
		}
	}
	return false
}
