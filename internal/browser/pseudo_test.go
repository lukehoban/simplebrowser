package browser

import (
	"bytes"
	"image"
	"image/color"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestParsePseudoClassSelectors(t *testing.T) {
	sheet := ParseCSS(`a:link, a:visited { color: red }
		.subtext a:HOVER { x: y }
		li:not(.a, .b) > a::before { x: y }
		a: { color: red }
		a:not(b { color: red }`)
	if len(sheet.Rules) != 3 {
		t.Fatalf("rules = %d, want 3 (invalid selectors dropped): %#v", len(sheet.Rules), sheet.Rules)
	}
	first := sheet.Rules[0].Selectors
	if len(first) != 2 || !reflect.DeepEqual(first[0].Parts[0], SelectorPart{Tag: "a", PseudoClasses: []string{"link"}}) ||
		!reflect.DeepEqual(first[1].Parts[0].PseudoClasses, []string{"visited"}) {
		t.Fatalf("a:link, a:visited parsed as %#v", first)
	}
	if got := sheet.Rules[1].Selectors[0].Parts[1].PseudoClasses; !reflect.DeepEqual(got, []string{"hover"}) {
		t.Fatalf("hover pseudo = %#v", got)
	}
	third := sheet.Rules[2].Selectors
	if len(third) != 1 || len(third[0].Parts) != 2 || third[0].Parts[1].Combinator != ">" ||
		len(third[0].Parts[0].Negations) != 1 || len(third[0].Parts[0].Negations[0]) != 2 ||
		!reflect.DeepEqual(third[0].Parts[1].PseudoClasses, []string{":before"}) {
		t.Fatalf("functional pseudo parsed as %#v", third)
	}
}

func TestPseudoClassSpecificity(t *testing.T) {
	for css, want := range map[string][3]int{
		"a:link":                   {0, 1, 1},
		".subline a:link":          {0, 2, 1},
		"#x a:hover:visited":       {1, 2, 1},
		"p::before":                {0, 0, 2},
		".titleline > a:link":      {0, 2, 1},
		":not(*)":                  {0, 0, 0},
		"p:not(span)":              {0, 0, 2},
		".x:not(:last-child)":      {0, 2, 0},
		":not(.a, #b)":             {1, 0, 0},
		":not(#b, .a.b)":           {1, 0, 0},
		":not(div > .a, span.x.y)": {0, 2, 1},
		":not(:not(#a))":           {1, 0, 0},
		":not(.a):not(.b)":         {0, 2, 0},
		"p:last-child":             {0, 1, 1},
	} {
		sel := ParseCSS(css + "{}").Rules[0].Selectors[0]
		if got := specificity(sel); got != want {
			t.Errorf("specificity(%q) = %v, want %v", css, got, want)
		}
	}
}

func TestPseudoClassMatching(t *testing.T) {
	doc, err := parse(Resource{URL: "index.html", Body: []byte(`<div class=titleline><a id=t href="x">T</a><span><a id=deep href=y>D</a></span></div>
		<span class=subline><a id=s href=z>S</a><a id=nohref>N</a><a id=empty href="">E</a>
		<area id=area href=x><area id=barearea></span><link id=stylesheet href=style.css>`)})
	if err != nil {
		t.Fatal(err)
	}
	nodes := map[string]*Node{}
	var walk func(*Node)
	walk = func(n *Node) {
		if id, ok := n.Attribute("id"); ok {
			nodes[id.Value] = n
		}
		for _, c := range n.Children {
			walk(c)
		}
	}
	walk(doc.Root)
	cases := []struct {
		selector string
		id       string
		want     bool
	}{
		{"a:link", "t", true},
		{"a:any-link", "s", true},
		{":link", "s", true},
		{"a:link", "nohref", false},
		{"a:link", "empty", true},
		{"area:link", "area", true},
		{"area:any-link", "barearea", false},
		{":any-link", "stylesheet", false},
		{"span:link", "s", false},
		{"a:visited", "t", false},
		{"a:hover", "t", false},
		{"a:active", "t", false},
		{"a:focus", "t", false},
		{"a:frobnicate", "t", false},
		{"a:link:frobnicate", "t", false},
		{"a:link:hover", "t", false},
		{"a:not(.x)", "t", true},
		{"a::before", "t", false},
		{".titleline > a", "t", true},
		{".titleline > a", "deep", false},
		{".titleline a", "deep", true},
		{".subline a:link", "s", true},
		{".subline a:link", "nohref", false},
	}
	for _, c := range cases {
		sel := ParseCSS(c.selector + "{}").Rules[0].Selectors[0]
		if got := matchesSelector(nodes[c.id], sel); got != c.want {
			t.Errorf("%q matches #%s = %v, want %v", c.selector, c.id, got, c.want)
		}
	}
}

func TestNegationInvalidArgumentsFailClosed(t *testing.T) {
	for _, selector := range []string{
		":not()", ":not( )", ":not(.x,)", ":not(, .x)", ":not(.x,,.y)",
		":not(> .x)", ":not(.x >)", ":not(.x + .y)", ":not([title])",
		":not(:unknown)", ":not(.x, :unknown)", ":not(::before)",
		":not(:not(:unknown))", ":not(:is(.x))", ":not(:last-child())",
		":not(.x))", ":not(.x", ":not(:not(.x)",
		strings.Repeat(":not(", maxNegationDepth+1) + ".x" + strings.Repeat(")", maxNegationDepth+1),
	} {
		t.Run(selector, func(t *testing.T) {
			sheet := ParseCSS("p, " + selector + "{color:red}")
			if len(sheet.Rules) != 0 {
				t.Fatalf("invalid unforgiving selector list accepted: %#v", sheet.Rules)
			}
		})
	}
	deep := strings.Repeat(":not(", maxNegationDepth) + ".x" + strings.Repeat(")", maxNegationDepth)
	if len(ParseCSS(deep+"{}").Rules) != 1 {
		t.Fatal("supported depth rejected")
	}
}

func TestStructuralPseudoClassMatching(t *testing.T) {
	doc := styledForLayout(t, `<div id=parent><span id=first class=x>first</span>
			<span id=last class=x>last</span> trailing text <!-- comment --></div>
			<p><b id=only></b></p>
			<div><i id=visible></i><i id=hidden style="display:none"></i></div>`)
	nodes := map[string]*Node{}
	var walk func(*Node)
	walk = func(n *Node) {
		if id, ok := n.Attribute("id"); ok {
			nodes[id.Value] = n
		}
		for _, c := range n.Children {
			walk(c)
		}
	}
	walk(doc.StyleRoot.Node)
	for _, c := range []struct {
		selector, id string
		want         bool
	}{
		{":last-child", "first", false},
		{":last-child", "last", true},
		{":LAST-CHILD", "only", true},
		{".x:NOT(:LAST-CHILD)", "first", true},
		{".x:not(:last-child)", "last", false},
		{":not(.x)", "first", false},
		{":not(*)", "first", false},
		{":not(span)", "first", false},
		{":not(#missing, .absent)", "first", true},
		{":not(#missing, .x)", "first", false},
		{":not(:not(.x))", "first", true},
		{":not(:not(.x))", "only", false},
		{":not(div > span)", "first", false},
		{":not(p span)", "first", true},
		{"div > span:not(:last-child)", "first", true},
		{"div:not(.missing) > span:last-child", "last", true},
		{":not(.missing):not(:last-child)", "first", true},
		{":not(:hover)", "first", true},
		{":last-child", "visible", false},
		{":last-child", "hidden", true},
	} {
		sel := ParseCSS(c.selector + "{}").Rules[0].Selectors[0]
		if got := matchesSelector(nodes[c.id], sel); got != c.want {
			t.Errorf("%s matches #%s = %v, want %v", c.selector, c.id, got, c.want)
		}
	}
	for _, n := range []*Node{{Type: TextNode}, {Type: CommentNode}, {Type: DocumentNode}} {
		for _, selector := range []string{"*", ":last-child", ":not(.x)"} {
			if matchesSelector(n, ParseCSS(selector + "{}").Rules[0].Selectors[0]) {
				t.Errorf("%s matched non-element type %v", selector, n.Type)
			}
		}
	}
	if !matchesSelector(&Node{Type: ElementNode, Name: "span"}, ParseCSS(":last-child{}").Rules[0].Selectors[0]) {
		t.Fatal("parentless element should be last-child under Selectors 4")
	}
}

func TestNegationCascade(t *testing.T) {
	for _, c := range []struct{ css, want string }{
		{`#target {color:blue} :not(#absent) {color:red}`, "red"},
		{`:not(#absent) {color:red} #target {color:blue}`, "blue"},
		{`:not(span) {color:red} .x {color:blue}`, "blue"},
		{`:not(*) {color:red} div {color:blue}`, "blue"},
		{`:not(.absent, #absent) {color:red} .x {color:blue}`, "red"},
		{`:not(#absent, .a.b) {color:red} #target.x {color:blue}`, "blue"},
		{`:not(.absent) {color:red} .x {color:blue}`, "blue"},
		{`:not(#absent) {color:red} .x {color:blue !important}`, "blue"},
		{`:not(.absent) {color:red !important} #target {color:blue}`, "red"},
		{`div {color:blue} p, :not(:unknown) {color:red}`, "blue"},
		{`div:last-child {color:red} .x {color:blue}`, "red"},
	} {
		doc := styledForLayout(t, "<style>"+c.css+"</style><div id=target class=x></div>")
		var found bool
		var walk func(*StyledNode)
		walk = func(n *StyledNode) {
			if id, ok := n.Node.Attribute("id"); ok && id.Value == "target" {
				found = true
				if got := n.Style["color"]; got != c.want {
					t.Errorf("%s: color=%s, want %s", c.css, got, c.want)
				}
			}
			for _, child := range n.Children {
				walk(child)
			}
		}
		walk(doc.StyleRoot)
		if !found {
			t.Fatal("missing target")
		}
	}
}

func TestNegationInlinePriority(t *testing.T) {
	for _, c := range []struct {
		inline, rule, want string
	}{
		{"color:blue", "color:red", "blue"},
		{"color:blue", "color:red!important", "red"},
		{"color:blue!important", "color:red!important", "blue"},
	} {
		doc := styledForLayout(t, `<style>:not(#a):not(#b){`+c.rule+`}</style><p style="`+c.inline+`">text</p>`)
		for node, style := range doc.Styles {
			if node.Name == "p" && style["color"] != c.want {
				t.Errorf("inline %q, rule %q: got %q, want %q", c.inline, c.rule, style["color"], c.want)
			}
		}
	}
}

func TestStructuralSelectorPixels(t *testing.T) {
	markup := `<p><span class=x>first</span><span class=x>last</span> <!-- trailing --></p>`
	actual := painted(t, markup+`<style>.x:not(:last-child){color:red} .x:last-child{background:lime}</style>`, image.Rect(0, 0, 240, 80))
	reference := painted(t, `<p><span style="color:red">first</span><span style="background:lime">last</span> <!-- trailing --></p>`, actual.Bounds())
	if !bytes.Equal(actual.Pix, reference.Pix) {
		t.Fatal("issue #191 repro differs from explicit inline styles")
	}
	// Text-bearing inline backgrounds are a separate painting limitation:
	// assert the cascade below, and exercise background pixels on blocks.
	for _, want := range []color.RGBA{{255, 0, 0, 255}} {
		found := false
		for y := 0; y < actual.Bounds().Dy(); y++ {
			for x := 0; x < actual.Bounds().Dx(); x++ {
				if actual.RGBAAt(x, y) == want {
					found = true
				}
			}
			blocks := painted(t, `<style>p{margin:0;width:40px;height:20px} p:not(:last-child){background:red} p:last-child{background:lime}</style><div><p></p><p></p> tail <!-- comment --></div>`, actual.Bounds())
			pixel(t, blocks, 10, 10, color.RGBA{255, 0, 0, 255})
			pixel(t, blocks, 10, 30, color.RGBA{0, 255, 0, 255})

			doc := styledForLayout(t, markup+`<style>.x:not(:last-child){color:red} .x:last-child{background:lime}</style>`)
			var spans []*StyledNode
			var walk func(*StyledNode)
			walk = func(n *StyledNode) {
				if n.Node.Name == "span" {
					spans = append(spans, n)
				}
				for _, c := range n.Children {
					walk(c)
				}
			}
			walk(doc.StyleRoot)
			if len(spans) != 2 || spans[0].Style["color"] != "red" || spans[1].Style["background-color"] != "lime" {
				t.Fatalf("issue repro computed styles: %#v", spans)
			}
		}
		if !found {
			t.Errorf("missing expected color %v", want)
		}
	}
}

func TestEmptyRowReferenceSelectors(t *testing.T) {
	source, err := os.ReadFile(filepath.Join("..", "..", "testdata", "wpt", "tables", "border-collapse-empty-row-ref.html"))
	if err != nil {
		t.Fatal(err)
	}
	// Independently identify all but each table's final row with explicit
	// classes, leaving every other byte of the pinned reference unchanged.
	explicit := strings.ReplaceAll(string(source), "tr:not(:last-child)", "tr.nonlast")
	tables := strings.Split(explicit, "</table>")
	for i, table := range tables {
		last := strings.LastIndex(table, "<tr>")
		if last >= 0 {
			tables[i] = strings.ReplaceAll(table[:last], "<tr>", `<tr class="nonlast">`) + table[last:]
		}
	}
	actual := painted(t, string(source), image.Rect(0, 0, 800, 600))
	reference := painted(t, strings.Join(tables, "</table>"), actual.Bounds())
	if !bytes.Equal(actual.Pix, reference.Pix) {
		t.Fatal("pinned empty-row reference differs from explicit row classes")
	}
}
func TestLinkPseudoClassOverridesUserAgentLinkStyle(t *testing.T) {
	doc, err := parse(Resource{URL: "index.html", Body: []byte(`<style>
		a:link { color: #000000; text-decoration: none }
		a:visited { color: #ff0000 }
		.subline a:link, .subline a:visited { color: #828282 }
		.subline a:hover { text-decoration: underline }
		</style>
		<span class=titleline><a id=title href=x>Title</a></span>
		<span class=subline><a id=sub href=y>by</a></span>`)})
	if err != nil {
		t.Fatal(err)
	}
	styled, err := style(doc, &Fetcher{})
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]map[string]string{}
	var walk func(*StyledNode)
	walk = func(n *StyledNode) {
		if id, ok := n.Node.Attribute("id"); ok {
			got[id.Value] = n.Style
		}
		for _, c := range n.Children {
			walk(c)
		}
	}
	walk(styled.StyleRoot)
	if got["title"]["color"] != "#000000" || got["title"]["text-decoration"] != "none" {
		t.Fatalf("title link style: %#v", got["title"])
	}
	if got["sub"]["color"] != "#828282" || got["sub"]["text-decoration"] != "none" {
		t.Fatalf("subline link style: %#v", got["sub"])
	}
}
