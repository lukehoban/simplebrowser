package browser

import (
	"image"
	"image/color"
	"strings"
	"testing"
)

// generatedBoxes returns every generated ::before/::after box in tree order,
// with the text it contains.
func generatedBoxes(t *testing.T, markup string, viewport image.Rectangle) []struct {
	Name string
	Rect image.Rectangle
	Text string
} {
	t.Helper()
	layout, err := LayoutWithViewport(styledForLayout(t, markup), viewport)
	if err != nil {
		t.Fatal(err)
	}
	var result []struct {
		Name string
		Rect image.Rectangle
		Text string
	}
	var walk func(*Box)
	walk = func(box *Box) {
		if box.Node != nil && (box.Node.Name == pseudoBeforeName || box.Node.Name == pseudoAfterName) {
			text := ""
			var collect func(*Box)
			collect = func(b *Box) {
				for _, run := range b.Text {
					text += run.Text
				}
				for _, child := range b.Children {
					collect(child)
				}
			}
			collect(box)
			result = append(result, struct {
				Name string
				Rect image.Rectangle
				Text string
			}{box.Node.Name, box.Rect, text})
			// A generated box can appear as an outer box and an inner
			// fragment sharing the same node; report the outermost only.
			return
		}
		for _, child := range box.Children {
			walk(child)
		}
	}
	walk(layout.Root)
	return result
}

// allText concatenates every text run in the laid-out document in tree order.
func allText(t *testing.T, markup string, viewport image.Rectangle) string {
	t.Helper()
	layout, err := LayoutWithViewport(styledForLayout(t, markup), viewport)
	if err != nil {
		t.Fatal(err)
	}
	var b strings.Builder
	var walk func(*Box)
	walk = func(box *Box) {
		for _, run := range box.Text {
			b.WriteString(run.Text)
			b.WriteString("|")
		}
		for _, child := range box.Children {
			walk(child)
		}
	}
	walk(layout.Root)
	return b.String()
}

func TestGeneratedContentValues(t *testing.T) {
	for _, tc := range []struct {
		value   string
		want    string
		wantBox bool
	}{
		{`""`, "", true},
		{`"x"`, "x", true},
		{`'x'`, "x", true},
		{`"a" "b"`, "ab", true},
		{`"\201C"`, "\u201c", true},
		{`"a\"b"`, `a"b`, true},
		{`"line\
continued"`, "linecontinued", true},
		{"none", "", false},
		{"normal", "", false},
		{"NONE", "", false},
		{"", "", false},
		{`url(icon.png)`, "", false},
		{`attr(title)`, "", false},
		{`counter(section)`, "", false},
		{`open-quote`, "", false},
		{`"a" url(x.png)`, "", false},
		{`"unterminated`, "", false},
		// An escaped closing quote leaves the string unterminated.
		{`"x\"`, "", false},
		{`'x\'`, "", false},
		{`"x" "y\"`, "", false},
		{`"x\`, "", false},
		// Escaped newline forms are line continuations; unescaped ones make a
		// bad string.
		{"\"a\\\fb\"", "ab", true},
		{"\"a\\\rb\"", "ab", true},
		{"\"a\\\r\nb\"", "ab", true},
		{"\"a\fb\"", "", false},
		{"\"a\nb\"", "", false},
		{`"a\\"`, `a\`, true},
		{`"\41 B"`, "AB", true},
	} {
		got, ok := generatedContent(tc.value)
		if ok != tc.wantBox || got != tc.want {
			t.Errorf("generatedContent(%q) = %q, %v; want %q, %v", tc.value, got, ok, tc.want, tc.wantBox)
		}
	}
}

func TestGeneratedContentStringsSurroundElementText(t *testing.T) {
	got := allText(t, `<style>p::before{content:"[pre] "}p::after{content:" [post]"}</style>
		<body style="margin:0"><p>middle</p></body>`, image.Rect(0, 0, 300, 100))
	if want := "[pre] |middle| [post]|"; got != want {
		t.Fatalf("text runs = %q, want %q", got, want)
	}
}

func TestGeneratedContentEmptyInlineBlockGeometry(t *testing.T) {
	boxes := generatedBoxes(t, `<style>
		.a::after{content:"";display:inline-block;width:12px;height:12px;background:#202122}
		</style><body style="margin:0"><label class="a">x</label></body>`, image.Rect(0, 0, 200, 100))
	if len(boxes) != 1 {
		t.Fatalf("generated boxes = %d, want 1: %v", len(boxes), boxes)
	}
	if got := boxes[0].Rect; got.Dx() != 12 || got.Dy() != 12 || got.Min.X < 8 {
		t.Fatalf("::after rect = %v, want a 12x12 box after the text", got)
	}
}

func TestGeneratedContentBlockDisplayOrdersAroundChildren(t *testing.T) {
	boxes := generatedBoxes(t, `<style>
		div::before{content:"b";display:block}
		div::after{content:"a";display:block}
		</style><body style="margin:0"><div>middle</div></body>`, image.Rect(0, 0, 200, 200))
	if len(boxes) != 2 {
		t.Fatalf("generated boxes = %d, want 2: %v", len(boxes), boxes)
	}
	before, after := boxes[0], boxes[1]
	if before.Name != pseudoBeforeName || after.Name != pseudoAfterName {
		t.Fatalf("order = %q then %q", before.Name, after.Name)
	}
	if before.Text != "b" || after.Text != "a" {
		t.Fatalf("texts = %q, %q", before.Text, after.Text)
	}
	if !(before.Rect.Max.Y <= after.Rect.Min.Y) || before.Rect.Min.Y != 0 {
		t.Fatalf("block generated boxes stacked wrongly: %v then %v", before.Rect, after.Rect)
	}
	if before.Rect.Dx() != 200 || after.Rect.Dx() != 200 {
		t.Fatalf("block generated boxes should fill the container: %v, %v", before.Rect, after.Rect)
	}
}

func TestGeneratedContentInheritsAndCascades(t *testing.T) {
	// The generated box inherits from its originating element, a
	// pseudo-element adds type specificity, and legacy one-colon selectors
	// are pseudo-elements too.
	doc := styledForLayout(t, `<style>
		#id:before{content:"x";color:blue}
		p::before{color:red}
		p::before{font-weight:bold}
		</style><body><p id="id" style="color:green;font-size:20px">text</p></body>`)
	var found ComputedStyle
	var walk func(*StyledNode)
	walk = func(n *StyledNode) {
		if n.Node.Name == pseudoBeforeName {
			found = n.Style
		}
		for _, c := range n.Children {
			walk(c)
		}
	}
	walk(doc.StyleRoot)
	if found == nil {
		t.Fatal("no ::before box generated")
	}
	if found["color"] != "blue" {
		t.Errorf("color = %q, want blue (#id:before outranks p::before)", found["color"])
	}
	if found["font-weight"] != "bold" {
		t.Errorf("font-weight = %q, want bold", found["font-weight"])
	}
	if found["font-size"] != "20px" {
		t.Errorf("font-size = %q, want the inherited 20px", found["font-size"])
	}
}

func TestGeneratedContentSuppressedCases(t *testing.T) {
	viewport := image.Rect(0, 0, 200, 100)
	for _, markup := range []string{
		// Unsupported pseudo-elements never match.
		`<style>p::first-line{content:"x"}</style><p>text</p>`,
		`<style>p::marker{content:"x"}</style><p>text</p>`,
		// A pseudo-element is only valid in the last compound selector.
		`<style>p::before span{content:"x"}</style><p><span>text</span></p>`,
		// content without a pseudo-element does not generate a box.
		`<style>p{content:"x"}</style><p>text</p>`,
		// Replaced and void elements have no generated children.
		`<style>img::after{content:"x"}</style><img>`,
		`<style>br::after{content:"x"}</style><br>`,
		// display:none originating elements generate nothing.
		`<style>p{display:none}p::before{content:"x"}</style><p>text</p>`,
		// The generated box's own display:none suppresses it.
		`<style>p::before{content:"x";display:none}</style><p>text</p>`,
		// Unsupported content values generate no box.
		`<style>p::before{content:url(missing.png)}</style><p>text</p>`,
		`<style>p::before{content:counter(x)}</style><p>text</p>`,
	} {
		if boxes := generatedBoxes(t, markup, viewport); len(boxes) != 0 {
			t.Errorf("%s generated %v, want no generated box", markup, boxes)
		}
	}
}

func TestGeneratedContentDoesNotApplyPresentationalOrInlineStyle(t *testing.T) {
	// The style attribute and presentational attributes belong to the
	// element, not to its generated boxes.
	boxes := generatedBoxes(t, `<style>div::before{content:"";display:block;height:5px}</style>
		<body style="margin:0"><div style="height:40px;width:40px" width="40"></div></body>`,
		image.Rect(0, 0, 200, 200))
	if len(boxes) != 1 || boxes[0].Rect.Dy() != 5 {
		t.Fatalf("generated boxes = %v, want one 5px-tall box", boxes)
	}
}

func TestPaintGeneratedContentBackgroundAndText(t *testing.T) {
	img := painted(t, `<style>
		body{margin:0;background:white}
		.a::after{content:"";display:inline-block;width:12px;height:12px;background:#202122;vertical-align:top}
		.b::before{content:"AAA";color:#cc0000}
		</style><body><div class="a" style="font-size:16px">x</div><div class="b"></div></body>`,
		image.Rect(0, 0, 200, 100))
	// The generated square paints its background.
	dark := color.RGBA{0x20, 0x21, 0x22, 255}
	found := false
	for y := 0; y < 30 && !found; y++ {
		for x := 0; x < 60; x++ {
			if img.RGBAAt(x, y) == dark {
				found = true
				break
			}
		}
	}
	if !found {
		t.Error("no ::after background pixels painted")
	}
	// The generated text paints in its own color.
	red := 0
	for y := 0; y < 100; y++ {
		for x := 0; x < 200; x++ {
			if c := img.RGBAAt(x, y); c.R > 150 && c.G < 80 && c.B < 80 {
				red++
			}
		}
	}
	if red == 0 {
		t.Error("no ::before text pixels painted in the generated color")
	}
}

func TestGeneratedContentStableAcrossViewportRestyle(t *testing.T) {
	doc := styledForLayout(t, `<style>div::before{content:"";display:block;height:10px;width:50%}</style>
		<body style="margin:0"><div></div></body>`)
	first, err := LayoutWithViewport(doc, image.Rect(0, 0, 200, 100))
	if err != nil {
		t.Fatal(err)
	}
	second, err := LayoutWithViewport(first.Document, image.Rect(0, 0, 400, 100))
	if err != nil {
		t.Fatal(err)
	}
	var widths []int
	for _, l := range []Layout{first, second} {
		var walk func(*Box)
		walk = func(box *Box) {
			if box.Node != nil && box.Node.Name == pseudoBeforeName {
				widths = append(widths, box.Rect.Dx())
				if _, ok := l.Document.Styles[box.Node]; !ok {
					t.Errorf("generated node missing from the style map at viewport %v", l.Viewport)
				}
			}
			for _, child := range box.Children {
				walk(child)
			}
		}
		walk(l.Root)
	}
	if len(widths) != 2 || widths[0] != 100 || widths[1] != 200 {
		t.Fatalf("generated widths = %v, want [100 200]", widths)
	}
}

func TestGeneratedContentStringEscapesEndToEnd(t *testing.T) {
	viewport := image.Rect(0, 0, 300, 100)
	// An escaped closing quote never terminates the string, so the
	// declaration is invalid and generates no box.
	if got := allText(t, `<style>p::before{content:"x\"}</style><p>T</p>`, viewport); got != "T|" {
		t.Errorf("escaped closing quote: text = %q, want %q", got, "T|")
	}
	// A backslash before a form feed continues the string.
	if got := allText(t, "<style>p::before{content:\"a\\\fb\"}</style><p>T</p>", viewport); got != "ab|T|" {
		t.Errorf("escaped form feed: text = %q, want %q", got, "ab|T|")
	}
}

func TestGeneratedContentLegacySyntaxSpecificityMatchesDoubleColon(t *testing.T) {
	legacy, ok := parseSelectorGroup("p:before")
	if !ok || len(legacy) != 1 {
		t.Fatal("parse p:before")
	}
	modern, ok := parseSelectorGroup("p::before")
	if !ok || len(modern) != 1 {
		t.Fatal("parse p::before")
	}
	if a, b := specificity(legacy[0]), specificity(modern[0]); a != b || b != [3]int{0, 0, 2} {
		t.Fatalf("specificity(p:before) = %v, specificity(p::before) = %v; want both [0 0 2]", a, b)
	}
	viewport := image.Rect(0, 0, 300, 100)
	// With equal specificity, source order decides in both directions.
	for _, tc := range []struct{ css, want string }{
		{`p::before{content:"modern"}p:before{content:"legacy"}`, "legacy|T|"},
		{`p:before{content:"legacy"}p::before{content:"modern"}`, "modern|T|"},
		{`p::after{content:"modern"}p:after{content:"legacy"}`, "T|legacy|"},
		{`p:after{content:"legacy"}p::after{content:"modern"}`, "T|modern|"},
	} {
		if got := allText(t, "<style>"+tc.css+"</style><p>T</p>", viewport); got != tc.want {
			t.Errorf("%s: text = %q, want %q", tc.css, got, tc.want)
		}
	}
}
