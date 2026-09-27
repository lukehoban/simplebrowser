package browser

import "testing"

func TestStyleSelectorsCascadeAndInheritance(t *testing.T) {
	doc, err := parse(Resource{URL: "index.html", Body: []byte(`
		<style>
		* { font-family: sans-serif } #story p { color: blue }
		div.note { color: red; padding: 1px 2px }
		p { color: green !important; text-align: right }
		</style>
		<div id=story class=note><p style="color: black !important">Hello</p></div>`)})
	if err != nil {
		t.Fatal(err)
	}
	styled, err := style(doc, &Fetcher{})
	if err != nil {
		t.Fatal(err)
	}
	var div, p *StyledNode
	var walk func(*StyledNode)
	walk = func(n *StyledNode) {
		if n.Node.Name == "div" {
			div = n
		}
		if n.Node.Name == "p" {
			p = n
		}
		for _, child := range n.Children {
			walk(child)
		}
	}
	walk(styled.StyleRoot)
	if div == nil || p == nil {
		t.Fatal("styled tree lost DOM nodes")
	}
	if div.Style["color"] != "red" || div.Style["padding-top"] != "1px" || div.Style["padding-right"] != "2px" {
		t.Fatalf("div computed style: %#v", div.Style)
	}
	if p.Style["color"] != "black" || p.Style["font-family"] != "sans-serif" {
		t.Fatalf("p computed style: %#v", p.Style)
	}
}

func TestStylePresentationalAttributesAndDefaults(t *testing.T) {
	doc, _ := parse(Resource{URL: "index.html", Body: []byte(
		`<table cellpadding="3" cellspacing="2" bgcolor="#fff"><tr align="center"><td width="20">x</td></tr></table>`)})
	styled, err := style(doc, &Fetcher{})
	if err != nil {
		t.Fatal(err)
	}
	var table, tr, td *StyledNode
	var walk func(*StyledNode)
	walk = func(n *StyledNode) {
		switch n.Node.Name {
		case "table":
			table = n
		case "tr":
			tr = n
		case "td":
			td = n
		}
		for _, child := range n.Children {
			walk(child)
		}
	}
	walk(styled.StyleRoot)
	if table.Style["padding-top"] != "3px" || table.Style["border-spacing"] != "2px" ||
		table.Style["background-color"] != "#fff" || tr.Style["text-align"] != "center" ||
		td.Style["width"] != "20px" || td.Style["display"] != "table-cell" {
		t.Fatalf("presentational styles: table=%#v tr=%#v td=%#v", table.Style, tr.Style, td.Style)
	}
}
