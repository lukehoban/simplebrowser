package browser

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"testing"
)

func TestTokenizer(t *testing.T) {
	tests := []struct {
		name, input string
		want        []Token
	}{
		{"basic", `<!DOCTYPE html><DiV ID='a&amp;b' disabled data-x=hello id=duplicate>Hi &copy; &#x1F600;<!--ok--></DIV>`,
			[]Token{{Type: DoctypeToken, Data: "html"}, {Type: StartTagToken, Name: "div", Attributes: []Attribute{
				{Name: "id", Value: "a&b"}, {Name: "disabled", Boolean: true}, {Name: "data-x", Value: "hello"},
			}}, {Type: TextToken, Data: "Hi © 😀"}, {Type: CommentToken, Data: "ok"}, {Type: EndTagToken, Name: "div"}}},
		{"raw", `<SCRIPT>if (a < b && x) { s = "&amp;"; }</ScRiPt><style>a::before{content:"<b>"}</style>`,
			[]Token{{Type: StartTagToken, Name: "script"}, {Type: TextToken, Data: `if (a < b && x) { s = "&amp;"; }`},
				{Type: EndTagToken, Name: "script"}, {Type: StartTagToken, Name: "style"},
				{Type: TextToken, Data: `a::before{content:"<b>"}`}, {Type: EndTagToken, Name: "style"}}},
		{"foreign CDATA", `<svg><![CDATA[<path> &amp;]]></svg><![CDATA[outside]]>`,
			[]Token{{Type: StartTagToken, Name: "svg"}, {Type: TextToken, Data: `<path> &amp;`},
				{Type: EndTagToken, Name: "svg"}, {Type: CommentToken, Data: "[CDATA[outside]]"}}},
		{"CDATA in SVG HTML integration point", `<svg><foreignObject><div><![CDATA[html]]></div></foreignObject></svg>`,
			[]Token{{Type: StartTagToken, Name: "svg"}, {Type: StartTagToken, Name: "foreignobject"},
				{Type: StartTagToken, Name: "div"}, {Type: CommentToken, Data: "[CDATA[html]]"},
				{Type: EndTagToken, Name: "div"}, {Type: EndTagToken, Name: "foreignobject"}, {Type: EndTagToken, Name: "svg"}}},
		{"CDATA after SVG HTML breakout", `<svg><g><div><![CDATA[html]]></div></g></svg>`,
			[]Token{{Type: StartTagToken, Name: "svg"}, {Type: StartTagToken, Name: "g"},
				{Type: StartTagToken, Name: "div"}, {Type: CommentToken, Data: "[CDATA[html]]"},
				{Type: EndTagToken, Name: "div"}, {Type: EndTagToken, Name: "g"}, {Type: EndTagToken, Name: "svg"}}},
		{"CDATA after SVG var breakout", `<svg><g><var><![CDATA[x]]></var></g></svg>`,
			[]Token{{Type: StartTagToken, Name: "svg"}, {Type: StartTagToken, Name: "g"},
				{Type: StartTagToken, Name: "var"}, {Type: CommentToken, Data: "[CDATA[x]]"},
				{Type: EndTagToken, Name: "var"}, {Type: EndTagToken, Name: "g"}, {Type: EndTagToken, Name: "svg"}}},
		{"CDATA in MathML HTML integration point", `<math><annotation-xml encoding="text/html"><div><![CDATA[html]]></div></annotation-xml></math>`,
			[]Token{{Type: StartTagToken, Name: "math"}, {Type: StartTagToken, Name: "annotation-xml", Attributes: []Attribute{{Name: "encoding", Value: "text/html"}}},
				{Type: StartTagToken, Name: "div"}, {Type: CommentToken, Data: "[CDATA[html]]"},
				{Type: EndTagToken, Name: "div"}, {Type: EndTagToken, Name: "annotation-xml"}, {Type: EndTagToken, Name: "math"}}},
		{"CDATA remains text inside SVG style", `<svg><style><![CDATA[a { color: red }]]></style></svg>`,
			[]Token{{Type: StartTagToken, Name: "svg"}, {Type: StartTagToken, Name: "style"},
				{Type: TextToken, Data: "a { color: red }"}, {Type: EndTagToken, Name: "style"}, {Type: EndTagToken, Name: "svg"}}},
		{"unterminated raw", `<script>x < b &amp;`, []Token{{Type: StartTagToken, Name: "script"}, {Type: TextToken, Data: "x < b &amp;"}}},
		{"ambiguous references", `<a href="?a=1&amp;b=2" title="&copy= &copy; &#65; &bogus;">&amp &bogus; &#x110000;</a>`,
			[]Token{{Type: StartTagToken, Name: "a", Attributes: []Attribute{{Name: "href", Value: "?a=1&b=2"}, {Name: "title", Value: "&copy= © A &bogus;"}}},
				{Type: TextToken, Data: "& &bogus; �"}, {Type: EndTagToken, Name: "a"}}},
		{"malformed", `x<3<!nope><p a="unfinished`, []Token{{Type: TextToken, Data: "x"}, {Type: TextToken, Data: "<"},
			{Type: TextToken, Data: "3"}, {Type: CommentToken, Data: "nope"}, {Type: StartTagToken, Name: "p", Attributes: []Attribute{{Name: "a", Value: "unfinished"}}}}},
		{"void syntax", `<br/><input checked value=1>`, []Token{{Type: StartTagToken, Name: "br", SelfClosing: true},
			{Type: StartTagToken, Name: "input", Attributes: []Attribute{{Name: "checked", Boolean: true}, {Name: "value", Value: "1"}}}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tokenizer := NewTokenizer(tt.input)
			var got []Token
			for i := 0; i <= len(tt.input)+1; i++ {
				token := tokenizer.Next()
				if token.Type == EOFToken {
					break
				}
				got = append(got, token)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("tokens = %#v\nwant %#v", got, tt.want)
			}
			if tokenizer.Next().Type != EOFToken {
				t.Fatal("tokenizer did not terminate")
			}
		})
	}
}

func TestXHTMLTokenizerRecognizesCDATA(t *testing.T) {
	tokenizer := NewXHTMLTokenizer(`<style>
<![CDATA[p::before { content: "<&amp;>"; }]]>
</style><![CDATA[tail &amp;]]>`)
	var got []Token
	for {
		token := tokenizer.Next()
		if token.Type == EOFToken {
			break
		}
		got = append(got, token)
	}
	want := []Token{
		{Type: StartTagToken, Name: "style"},
		{Type: TextToken, Data: "\n"},
		{Type: TextToken, Data: `p::before { content: "<&amp;>"; }`},
		{Type: TextToken, Data: "\n"},
		{Type: EndTagToken, Name: "style"},
		{Type: TextToken, Data: "tail &amp;"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("tokens = %#v\nwant %#v", got, want)
	}
}

func TestXHTMLTokenizerUnterminatedAndMalformedCDATA(t *testing.T) {
	t.Run("unterminated section consumes remaining input as text", func(t *testing.T) {
		tokenizer := NewXHTMLTokenizer(`<style><![CDATA[p { color: green }</style>`)
		if got := tokenizer.Next(); got.Type != StartTagToken || got.Name != "style" {
			t.Fatalf("start token = %#v", got)
		}
		if got := tokenizer.Next(); !reflect.DeepEqual(got, Token{Type: TextToken, Data: `p { color: green }</style>`}) {
			t.Fatalf("CDATA token = %#v", got)
		}
		if got := tokenizer.Next(); got.Type != EOFToken {
			t.Fatalf("final token = %#v", got)
		}
	})
	t.Run("malformed opener follows declaration recovery", func(t *testing.T) {
		tokenizer := NewXHTMLTokenizer(`<![CDATA broken]]><p>x</p>`)
		if got := tokenizer.Next(); !reflect.DeepEqual(got, Token{Type: CommentToken, Data: `[CDATA broken]]`}) {
			t.Fatalf("declaration token = %#v", got)
		}
	})
}

// compact renders tree shape without depending on pointer equality.
func compact(n *Node) string {
	switch n.Type {
	case TextNode:
		return `"` + n.Data + `"`
	case CommentNode:
		return "<!--" + n.Data + "-->"
	case DoctypeNode:
		return "<!doctype " + n.Data + ">"
	}
	result := n.Name
	if n.Type == DocumentNode {
		result = "#document"
	}
	result += "("
	for i, child := range n.Children {
		if i > 0 {
			result += ","
		}
		result += compact(child)
	}
	return result + ")"
}

func TestParseHTML(t *testing.T) {
	tests := []struct{ name, input, want string }{
		{"paragraph", `<p>one<b>two<p>three`, `#document(p("one",b("two")),p("three"))`},
		{"block closes p", `<p>before<div>after</div>`, `#document(p("before"),div("after"))`},
		{"list", `<ul><li>one<li>two<ul><li>inner</ul><li>three</ul>`,
			`#document(ul(li("one"),li("two",ul(li("inner"))),li("three")))`},
		{"definition list", `<dl><dt>term<dd>definition<dt>next</dl>`,
			`#document(dl(dt("term"),dd("definition"),dt("next")))`},
		{"table", `<table><tr><td>a<td>b<tr><th>c</table>`,
			`#document(table(tr(td("a"),td("b")),tr(th("c"))))`},
		{"HN fragment", `<table><tr class=athing><td><span class=rank>1.</span></td><td><a href=item?id=1>Story &amp; more</a><img src=x></td></tr><tr><td></td><td>comments</td></tr></table>`,
			`#document(table(tr(td(span("1.")),td(a("Story & more"),img())),tr(td(),td("comments"))))`},
		{"stray end and void", `</no><p>hello<br>world</b>!`, `#document(p("hello",br(),"world!"))`},
		{"document nodes", `<!doctype html><!--hi--><html><body>x</body></html>`,
			`#document(<!doctype html>,<!--hi-->,html(body("x")))`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := ParseHTML(tt.input)
			if got := compact(root); got != tt.want {
				t.Errorf("tree = %s\nwant = %s", got, tt.want)
			}
			var visit func(*Node)
			visit = func(n *Node) {
				for _, child := range n.Children {
					if child.Parent != n {
						t.Errorf("parent of %s is wrong", child.Name)
					}
					visit(child)
				}
			}
			visit(root)
		})
	}
}

func TestParseHTMLForeignCDATA(t *testing.T) {
	root := ParseHTML(`<svg><style><![CDATA[#shape { fill: blue }]]></style><path id=shape/></svg>`)
	svg := root.Children[0]
	style := svg.Children[0]
	if got := style.Children[0].Data; got != "#shape { fill: blue }" {
		t.Fatalf("CDATA style text = %q, want stylesheet text", got)
	}
}

func TestParseResourceUsesEffectiveRedirectSource(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/start" {
			http.Redirect(w, r, "/final/page.html", http.StatusFound)
			return
		}

		w.Write([]byte(`<a href="../next">next</a>`))
	}))
	defer server.Close()
	resource, err := (&Fetcher{}).Fetch(server.URL + "/start")
	if err != nil {
		t.Fatal(err)
	}
	document, err := parse(resource)
	if err != nil {
		t.Fatal(err)
	}
	if document.Resource.Source != server.URL+"/start" || document.BaseURL != server.URL+"/final/page.html" {
		t.Fatalf("requested=%q base=%q", document.Resource.Source, document.BaseURL)
	}
	if got := compact(document.Root); got != `#document(a("next"))` {
		t.Fatalf("tree = %s", got)
	}
	if attr, ok := document.Root.Children[0].Attribute("HREF"); !ok || attr.Value != "../next" {
		t.Fatalf("href = %#v, %v", attr, ok)
	}
}

func TestParseSelectsXHTMLMode(t *testing.T) {
	markup := `<style><![CDATA[p { color: green }]]></style><p>x</p>`
	tests := []struct {
		name     string
		resource Resource
		wantText string
	}{
		{"local xht", Resource{URL: filepath.Join(t.TempDir(), "page.xht"), Body: []byte(markup)}, `p { color: green }`},
		{"HTTP media type", Resource{URL: "https://example.test/page", ContentType: "application/xhtml+xml; charset=UTF-8", Body: []byte(markup)}, `p { color: green }`},
		{"ordinary HTML raw text", Resource{URL: "https://example.test/page.html", ContentType: "text/html", Body: []byte(markup)}, `<![CDATA[p { color: green }]]>`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			doc, err := parse(tt.resource)
			if err != nil {
				t.Fatal(err)
			}
			style := doc.Root.Children[0]
			if style.Name != "style" || len(style.Children) != 1 || style.Children[0].Data != tt.wantText {
				t.Fatalf("style tree = %s, want text %q", compact(style), tt.wantText)
			}
		})
	}
}
