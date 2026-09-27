package browser

import (
	"reflect"
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
		!reflect.DeepEqual(third[0].Parts[0].PseudoClasses, []string{"not("}) ||
		!reflect.DeepEqual(third[0].Parts[1].PseudoClasses, []string{":before"}) {
		t.Fatalf("functional pseudo parsed as %#v", third)
	}
}

func TestPseudoClassSpecificity(t *testing.T) {
	for css, want := range map[string][3]int{
		"a:link":              {0, 1, 1},
		".subline a:link":     {0, 2, 1},
		"#x a:hover:visited":  {1, 2, 1},
		"p::before":           {0, 0, 2},
		".titleline > a:link": {0, 2, 1},
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
		{"a:not(.x)", "t", false},
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
