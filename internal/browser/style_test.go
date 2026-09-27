package browser

import "testing"

func styledElementByID(root *StyledNode, id string) *StyledNode {
	if root == nil {
		return nil
	}
	if attr, ok := root.Node.Attribute("id"); ok && attr.Value == id {
		return root
	}
	for _, child := range root.Children {
		if found := styledElementByID(child, id); found != nil {
			return found
		}
	}
	return nil
}

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

func TestComputedFontSizesAndFontRelativeLengths(t *testing.T) {
	doc, err := parse(Resource{URL: "index.html", Body: []byte(`
		<html id=root style="font-size:20px;margin-left:1rem"><body>
			<div id=pt style="font-size:12pt"></div>
			<div id=em style="font-size:1.5em"><span id=percent style="font-size:50%">
				<b id=nested style="font-size:2em"></b>
			</span></div>
			<div id=small style="font-size:small"></div>
			<div id=relative style="font-size:larger"></div>
			<div id=rem style="font-size:2rem"></div>
			<div id=margin style="font-size:10px;margin-left:2em;padding-top:.5em;width:3em"></div>
			<div id=inherited style="font-size:inherit"></div>
		</body></html>`)})
	if err != nil {
		t.Fatal(err)
	}
	styled, err := style(doc, &Fetcher{})
	if err != nil {
		t.Fatal(err)
	}
	for id, want := range map[string]string{
		"pt": "16px", "em": "30px", "percent": "15px", "nested": "30px",
		"small": "14.222222222222221px", "relative": "24px", "rem": "40px",
		"inherited": "20px",
	} {
		if got := styledElementByID(styled.StyleRoot, id).Style["font-size"]; got != want {
			t.Errorf("%s font-size = %q, want %q", id, got, want)
		}
	}
	margin := styledElementByID(styled.StyleRoot, "margin").Style
	if margin["margin-left"] != "20px" || margin["padding-top"] != "5px" || margin["width"] != "30px" {
		t.Fatalf("font-relative lengths were not resolved against own 10px size: %#v", margin)
	}
	if got := styledElementByID(styled.StyleRoot, "root").Style["margin-left"]; got != "20px" {
		t.Fatalf("root rem margin = %q, want 20px root computed size", got)
	}
}

func TestFontShorthandCascadeAndResets(t *testing.T) {
	doc, err := parse(Resource{URL: "index.html", Body: []byte(`
		<style>
		#full {
			font: italic small-caps 700 20px/150% "Open Sans", Courier, monospace !important;
			font-size: 40px;
			font-style: normal !important;
		}
		#reset {
			font-style: italic; font-weight: bold; line-height: 3; font-family: monospace;
			font: 18px serif;
		}
		#invalid {
			font-style: italic; font-weight: bold; font-size: 19px; line-height: 2; font-family: monospace;
			font: italic 24px/ "Broken";
			font: menu extra;
		}
		</style>
		<p id=full>full</p><p id=reset>reset</p><p id=invalid>invalid</p>`)})
	if err != nil {
		t.Fatal(err)
	}
	styled, err := style(doc, &Fetcher{})
	if err != nil {
		t.Fatal(err)
	}

	full := styledElementByID(styled.StyleRoot, "full").Style
	if full["font-style"] != "normal" || full["font-weight"] != "700" ||
		full["font-size"] != "20px" || full["line-height"] != "30px" ||
		full["font-family"] != `"Open Sans", Courier, monospace` {
		t.Fatalf("full shorthand computed style = %#v", full)
	}
	reset := styledElementByID(styled.StyleRoot, "reset").Style
	if reset["font-style"] != "normal" || reset["font-weight"] != "normal" ||
		reset["font-size"] != "18px" || reset["line-height"] != "normal" ||
		reset["font-family"] != "serif" {
		t.Fatalf("omitted shorthand values did not reset = %#v", reset)
	}
	invalid := styledElementByID(styled.StyleRoot, "invalid").Style
	if invalid["font-style"] != "italic" || invalid["font-weight"] != "bold" ||
		invalid["font-size"] != "19px" || invalid["line-height"] != "2" ||
		invalid["font-family"] != "monospace" {
		t.Fatalf("invalid shorthand changed longhands = %#v", invalid)
	}
}

func TestInvalidFontShorthandsAreIgnored(t *testing.T) {
	for _, value := range []string{
		`italic 16px`, `16px/ serif`, `16px "unterminated`,
		`italic italic 16px serif`, `16px serif,,sans-serif`,
		`caption extra`, `italic caption`, `caption/2`, `"menu"`,
		`wide 16px serif`, `16px "Quoted Family" extra`,
	} {
		t.Run(value, func(t *testing.T) {
			declaration := ParseDeclarations("font:" + value)[0]
			if got := expandDeclaration(declaration); len(got) != 0 {
				t.Fatalf("expandDeclaration(%q) = %+v, want ignored", value, got)
			}
		})
	}
}

func TestSystemFontShorthandCascadeAndResets(t *testing.T) {
	for keyword, size := range systemFontSizes {
		t.Run(keyword, func(t *testing.T) {
			doc, err := parse(Resource{URL: "index.html", Body: []byte(`
				<style>
				#system { font: ` + keyword + ` !important; font-size: 30px;
					font-family: monospace; font-weight: bold }
				#override { font: ` + keyword + `; font-size: 18px }
				#invalid { font: italic 20px monospace; font: ` + keyword + ` extra }
				</style>
				<div id=parent style="font:italic bold 28px/2 monospace">
				<p id=system>system</p><p id=override>override</p>
				<p id=invalid>invalid</p></div>`)})
			if err != nil {
				t.Fatal(err)
			}
			styled, err := style(doc, &Fetcher{})
			if err != nil {
				t.Fatal(err)
			}
			for _, tc := range []struct {
				id, wantSize, wantStyle, wantWeight, wantLine, wantFamily string
			}{
				{"system", size, "normal", "normal", "normal", "sans-serif"},
				{"override", "18px", "normal", "normal", "normal", "sans-serif"},
				{"invalid", "20px", "italic", "normal", "normal", "monospace"},
			} {
				got := styledElementByID(styled.StyleRoot, tc.id).Style
				if got["font-size"] != tc.wantSize || got["font-style"] != tc.wantStyle ||
					got["font-weight"] != tc.wantWeight || got["line-height"] != tc.wantLine ||
					got["font-family"] != tc.wantFamily {
					t.Errorf("%s computed font = %#v", tc.id, got)
				}
			}
		})
	}
}

func TestBackgroundShorthandSlashExpansion(t *testing.T) {
	doc, err := parse(Resource{URL: "index.html", Body: []byte(`
		<div id=compact style="background:url(tile.png) center/4px 4px"></div>
		<div id=spaced style="background:url(tile.png) center / 4px 4px no-repeat"></div>
		<div id=color style="background:url(tile.png) center / 4px 4px no-repeat #123456"></div>`)})
	if err != nil {
		t.Fatal(err)
	}
	styled, err := style(doc, &Fetcher{})
	if err != nil {
		t.Fatal(err)
	}

	for _, id := range []string{"compact", "spaced", "color"} {
		got := styledElementByID(styled.StyleRoot, id).Style
		if got["background-position"] != "center" || got["background-size"] != "4px 4px" {
			t.Errorf("%s background position/size = %q / %q", id,
				got["background-position"], got["background-size"])
		}
	}
	if got := styledElementByID(styled.StyleRoot, "compact").Style["background-repeat"]; got != "repeat" {
		t.Errorf("compact background-repeat = %q, want repeat", got)
	}
	spaced := styledElementByID(styled.StyleRoot, "spaced").Style
	if spaced["background-repeat"] != "no-repeat" {
		t.Errorf("spaced background-repeat = %q, want no-repeat", spaced["background-repeat"])
	}
	withColor := styledElementByID(styled.StyleRoot, "color").Style
	if withColor["background-repeat"] != "no-repeat" || withColor["background-color"] != "#123456" {
		t.Errorf("trailing shorthand values leaked into size: %#v", withColor)
	}
}

func TestStyleAppliesXHTMLCDATAStylesheet(t *testing.T) {
	doc, err := parse(Resource{
		URL:  "fixture.xht",
		Body: []byte(`<style><![CDATA[p { color: green }]]></style><p>pass</p>`),
	})
	if err != nil {
		t.Fatal(err)
	}
	styled, err := style(doc, &Fetcher{})
	if err != nil {
		t.Fatal(err)
	}
	p := styled.StyleRoot.Children[1]
	if p.Node.Name != "p" || p.Style["color"] != "green" {
		t.Fatalf("paragraph style = %#v", p.Style)
	}
}

// The UA stylesheet must not reset color on body, or a root color would never
// reach descendant text (WPT colors/color-176, issue #47).
func TestStyleRootColorInheritsToDescendants(t *testing.T) {
	cases := []struct {
		name, css, want string
	}{
		{"root color", `html { color: green }`, "green"},
		{"initial color", ``, "black"},
		{"body overrides root", `html { color: green } body { color: red }`, "red"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			doc, err := parse(Resource{URL: "index.html", Body: []byte(
				`<html><head><style>` + tc.css + `</style></head><body><p>Text</p></body></html>`)})
			if err != nil {
				t.Fatal(err)
			}
			styled, err := style(doc, &Fetcher{})
			if err != nil {
				t.Fatal(err)
			}
			var p, text *StyledNode
			var walk func(*StyledNode)
			walk = func(n *StyledNode) {
				if n.Node.Name == "p" {
					p = n
					if len(n.Children) > 0 {
						text = n.Children[0]
					}
				}
				for _, child := range n.Children {
					walk(child)
				}
			}
			walk(styled.StyleRoot)
			if p == nil || text == nil {
				t.Fatal("styled tree lost paragraph text")
			}
			if p.Style["color"] != tc.want || text.Style["color"] != tc.want {
				t.Fatalf("p color = %q, text color = %q, want %q", p.Style["color"], text.Style["color"], tc.want)
			}
		})
	}
}
