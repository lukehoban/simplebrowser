package browser

import (
	"bytes"
	"image"
	"image/color"
	"testing"
)

func TestSiblingSelectorGrammarAndSpecificity(t *testing.T) {
	for _, tc := range []struct {
		css  string
		ops  []string
		spec [3]int
	}{
		{"section > p.a + p#b ~ em.x", []string{"", ">", "+", "~"}, [3]int{1, 2, 4}},
		{"a+b", []string{"", "+"}, [3]int{0, 0, 2}},
		{"a ~ b > c", []string{"", "~", ">"}, [3]int{0, 0, 3}},
	} {
		parsed, ok := parseSelectorGroup(tc.css)
		if !ok || len(parsed) != 1 || len(parsed[0].Parts) != len(tc.ops) {
			t.Fatalf("%q parsed as %+v (%v)", tc.css, parsed, ok)
		}
		for i, op := range tc.ops {
			if parsed[0].Parts[i].Combinator != op {
				t.Errorf("%q part %d combinator = %q, want %q", tc.css, i, parsed[0].Parts[i].Combinator, op)
			}
		}
		if got := specificity(parsed[0]); got != tc.spec {
			t.Errorf("%q specificity %v, want %v", tc.css, got, tc.spec)
		}
	}
	for _, bad := range []string{"a+", "a ~", "+ a", "~ a", "a ++ b", "a + > b", "a ~ + b", "a, b +", "a +, b", "a + / b"} {
		if selectors, ok := parseSelectorGroup(bad); ok {
			t.Errorf("accepted invalid %q: %+v", bad, selectors)
		}
	}
	if rules := ParseCSS("p + { color:red } p { color:green }").Rules; len(rules) != 1 {
		t.Fatalf("malformed combinator swallowed next rule: %+v", rules)
	}
	negated, ok := parseSelectorGroup("b:not(section > i + b, #key ~ b)")
	if !ok || len(negated) != 1 || specificity(negated[0]) != [3]int{1, 0, 2} {
		t.Fatalf("negated sibling argument grammar/specificity: %+v, %v", negated, ok)
	}
	for _, bad := range []string{"b:not(i +)", "b:not(+ i)", "b:not(i ~ > b)", "b:not(i ~ b:hoverz)"} {
		if selectors, ok := parseSelectorGroup(bad); ok {
			t.Errorf("accepted invalid negated sibling %q: %+v", bad, selectors)
		}
	}
}

func TestSiblingSelectorsIgnoreNonElementsAndRespectParent(t *testing.T) {
	root := ParseHTML(`<section><i id=a></i>text<!--skip--><b id=b></b><em id=c></em><b id=d></b><div><b id=n></b></div></section><b id=outside></b>`)
	nodes := map[string]*Node{}
	var walk func(*Node)
	walk = func(n *Node) {
		if id, ok := n.Attribute("id"); ok {
			nodes[id.Value] = n
		}
		for _, child := range n.Children {
			walk(child)
		}
	}
	walk(root)
	for _, tc := range []struct {
		sel, id string
		want    bool
	}{
		{"i + b", "b", true}, {"i + em", "c", false},
		{"i ~ em", "c", true}, {"i ~ b", "d", true},
		{"i + b + em + b", "d", true}, {"i ~ b", "n", false},
		{"section > i ~ b", "outside", false},
		{"div + b", "outside", false}, {"section + b", "outside", true},
		{"b:not(i + b)", "b", false}, {"b:not(i + b)", "d", true},
		{"b:not(i ~ b)", "d", false}, {"b:not(i ~ b)", "outside", true},
	} {
		selectors, ok := parseSelectorGroup(tc.sel)
		if !ok || matchesSelector(nodes[tc.id], selectors[0]) != tc.want {
			t.Errorf("%q matches #%s = %v, want %v", tc.sel, tc.id, ok && matchesSelector(nodes[tc.id], selectors[0]), tc.want)
		}
	}
}

func TestSiblingSelectorCascadeAndPixels(t *testing.T) {
	const blocks = `<div class=row><i></i><b></b><b></b><em></em></div>`
	const base = `<style>body{margin:0}.row > *{display:block;width:40px;height:12px;background:#ff0000}</style>`
	const rules = `<style>.row > i + b {background:#008000}.row > i ~ em {background:#0000ff}
	.row > b {background:#ff0000}.row > i + b {background:#008000}
	.row > b + b {background:#008000}</style>`
	got := painted(t, base+rules+blocks, image.Rect(0, 0, 80, 60))
	want := painted(t, base+`<div class=row><i></i><b style="background:#008000"></b><b style="background:#008000"></b><em style="background:#0000ff"></em></div>`, image.Rect(0, 0, 80, 60))
	if !bytes.Equal(got.Pix, want.Pix) {
		t.Fatal("sibling combinator pixel render differs from explicit reference")
	}
	pixel(t, got, 2, 16, color.RGBA{0, 128, 0, 255})
}
