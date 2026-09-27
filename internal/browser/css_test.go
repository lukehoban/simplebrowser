package browser

import (
	"image/color"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestCSSSelectorsAndRecovery(t *testing.T) {
	s := ParseCSS(`/* initial */ div.card#main.hot > a.link, * .item { color: #f60; broken; margin: 2px 0 ! important; }
	[unsupported] { color: red } h1, h2 { font-size: 1.5rem } p { color: blue; }`)
	if len(s.Rules) != 3 {
		t.Fatalf("rules: %+v", s.Rules)
	}
	first := s.Rules[0]
	want := []Selector{
		{Parts: []SelectorPart{{Tag: "div", ID: "main", Classes: []string{"card", "hot"}}, {Combinator: ">", Tag: "a", Classes: []string{"link"}}}},
		{Parts: []SelectorPart{{Tag: "*"}, {Combinator: " ", Classes: []string{"item"}}}},
	}
	if !reflect.DeepEqual(first.Selectors, want) {
		t.Fatalf("selectors: %+v", first.Selectors)
	}
	if len(first.Declarations) != 2 || !first.Declarations[1].Important {
		t.Fatalf("declarations: %+v", first.Declarations)
	}
	if len(s.Rules[1].Selectors) != 2 || s.Rules[2].Declarations[0].Property != "color" {
		t.Fatalf("recovery: %+v", s.Rules)
	}
}

func TestCSSValuesAndQuotedDelimiters(t *testing.T) {
	d := ParseDeclarations(`background: url("a;b:c.png"); content: "semi;:!important";
	width: 25%; height: 1.25em; font-size: 12pt; padding: 2rem 3px;
	opacity: .5; color: rgba(255, 0, 128, .5) !important;
	bad declaration; background-color: transparent; empty: ; --custom: foo;`)
	if len(d) != 10 {
		t.Fatalf("declarations: %+v", d)
	}
	checks := []struct {
		index int
		kind  string
		unit  string
	}{
		{0, "url", ""}, {1, "string", ""}, {2, "percentage", "%"},
		{3, "length", "em"}, {4, "length", "pt"}, {5, "length", "rem"},
		{6, "number", ""}, {7, "color", ""}, {8, "color", ""}, {9, "keyword", ""},
	}
	for _, c := range checks {
		if d[c.index].Values[0].Kind != c.kind || d[c.index].Values[0].Unit != c.unit {
			t.Errorf("%d: %+v", c.index, d[c.index])
		}
	}
	if d[0].Values[0].Text != "a;b:c.png" || d[1].Important || !d[7].Important ||
		d[7].Values[0].Color != (color.RGBA{255, 0, 128, 128}) {
		t.Fatalf("values: %+v", d)
	}
	for _, tc := range []struct {
		text string
		want color.RGBA
	}{
		{"#abc", color.RGBA{170, 187, 204, 255}},
		{"#12345678", color.RGBA{18, 52, 86, 120}},
		{"rgb(100%, 0%, 50%)", color.RGBA{255, 0, 128, 255}},
		{"orange", color.RGBA{255, 165, 0, 255}},
	} {
		got, ok := parseColor(tc.text)
		if !ok || got != tc.want {
			t.Errorf("%s: %v %v", tc.text, got, ok)
		}
	}
	for _, invalid := range []string{"#zzzzzz", "rgb(999,0,0)", "rgba(0,0,0,2)"} {
		if _, ok := parseColor(invalid); ok {
			t.Errorf("accepted %s", invalid)
		}
	}
}

func TestFontShorthandExpansionParsing(t *testing.T) {
	declarations := ParseDeclarations(
		`font: italic small-caps 700 18px/1.5 "Open Sans", Arial, sans-serif !important`)
	if len(declarations) != 1 {
		t.Fatalf("declarations = %+v", declarations)
	}
	got := expandDeclaration(declarations[0])
	want := map[string]string{
		"font-style":   "italic",
		"font-variant": "small-caps",
		"font-weight":  "700",
		"font-size":    "18px",
		"line-height":  "1.5",
		"font-family":  `"Open Sans", Arial, sans-serif`,
	}
	if len(got) != len(want) {
		t.Fatalf("expanded declarations = %+v", got)
	}
	for _, declaration := range got {
		if declaration.Value != want[declaration.Property] || !declaration.Important {
			t.Errorf("expanded declaration = %+v, want value %q and important", declaration, want[declaration.Property])
		}
	}
}

func TestBorderShorthandExpansionRetainsWidthKeywords(t *testing.T) {
	for _, value := range []string{
		`black solid thin`,
		`medium solid black`,
		`solid thick black`,
	} {
		declarations := ParseDeclarations(`border: ` + value + ` !important`)
		if len(declarations) != 1 {
			t.Fatalf("border %q declarations = %+v", value, declarations)
		}
		got := expandDeclaration(declarations[0])
		if len(got) != 4 {
			t.Fatalf("border %q expanded declarations = %+v", value, got)
		}
		for i, side := range []string{"top", "right", "bottom", "left"} {
			if got[i].Property != "border-"+side || got[i].Value != value || !got[i].Important {
				t.Errorf("border %q side %s = %+v", value, side, got[i])
			}
		}
	}
}

func TestExtractStylesRelativeAndInline(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "css"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "css", "main.css"), []byte("a { color: red }"), 0600); err != nil {
		t.Fatal(err)
	}
	html := `<style>p { color: blue }</style><link rel="alternate STYLESHEET" href="css/main.css">
	<p style="color: #ff6600 !important">Hello</p><style>p { margin: 2px }</style>`
	doc, err := parse(Resource{URL: filepath.Join(dir, "index.html"), Body: []byte(html)})
	if err != nil {
		t.Fatal(err)
	}
	sheets, inline, err := ExtractStyles(doc, &Fetcher{})
	if err != nil {
		t.Fatal(err)
	}
	if len(sheets) != 3 || sheets[0].Rules[0].Declarations[0].Value != "blue" ||
		sheets[1].URL != filepath.Join(dir, "css", "main.css") ||
		sheets[2].Rules[0].Declarations[0].Property != "margin" {
		t.Fatalf("sheets: %+v", sheets)
	}
	var p *Node
	for _, n := range doc.Root.Children {
		if n.Name == "p" {
			p = n
		}
	}
	if len(inline[p]) != 1 || !inline[p][0].Important {
		t.Fatalf("inline: %+v", inline)
	}
	if got, err := ResolveCSSURL("https://example.com/path/page", "../main.css"); err != nil || got != "https://example.com/main.css" {
		t.Fatalf("URL: %q, %v", got, err)
	}
	if len(UserAgentStylesheet().Rules) < 10 {
		t.Fatal("missing UA defaults")
	}
}

func TestResolveCSSURLLocalAbsoluteAndRelative(t *testing.T) {
	dir := t.TempDir()
	base := filepath.Join(dir, "pages", "page.html")
	absolute := filepath.Join(dir, "assets", "tile.png")
	relativeWant, err := filepath.Abs(filepath.Join("assets", "tile.png"))
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		base, ref, want string
	}{
		{base, absolute, absolute},
		{base, "../assets/tile.png", absolute},
		{base, "tile.png", filepath.Join(dir, "pages", "tile.png")},
		{filepath.Join("pages", "page.html"), "../assets/tile.png", relativeWant},
		{"https://example.com/pages/page.html", "/assets/tile.png", "https://example.com/assets/tile.png"},
		{"https://example.com/pages/page.html", "../assets/tile.png", "https://example.com/assets/tile.png"},
	} {
		got, err := ResolveCSSURL(tc.base, tc.ref)
		if err != nil || got != tc.want {
			t.Errorf("ResolveCSSURL(%q, %q) = %q, %v; want %q", tc.base, tc.ref, got, err, tc.want)
		}
	}
}

func TestCSSMalformedInputTerminates(t *testing.T) {
	for _, input := range []string{
		"/* unterminated", `p { content: "unterminated`, "p { color: red; broken: ; }",
		"p > { color: red } h1 { color: blue }", "a { background: url('a;b'); color: red }",
	} {
		_ = ParseCSS(input)
	}
}

func TestExtractStylesHTTPRedirectBaseAndFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/start":
			http.Redirect(w, r, "/pages/page.html", http.StatusFound)
		case "/pages/page.html":
			_, _ = io.WriteString(w, `<link rel="icon" href="missing.css"><link rel="stylesheet" href="../css/site.css">`)
		case "/css/site.css":
			_, _ = io.WriteString(w, "a { color: #ff6600; }")
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	f := &Fetcher{}
	resource, err := f.Fetch(server.URL + "/start")
	if err != nil {
		t.Fatal(err)
	}
	doc, err := parse(resource)
	if err != nil {
		t.Fatal(err)
	}
	sheets, _, err := ExtractStyles(doc, f)
	if err != nil || len(sheets) != 1 || sheets[0].URL != server.URL+"/css/site.css" {
		t.Fatalf("sheets: %+v, %v", sheets, err)
	}
	bad, _ := parse(Resource{URL: server.URL + "/pages/page.html", Body: []byte(`<link rel=stylesheet href="missing.css">`)})
	if _, _, err := ExtractStyles(bad, f); err == nil {
		t.Fatal("missing linked stylesheet should return an error")
	}
}
