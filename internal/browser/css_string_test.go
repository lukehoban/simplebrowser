package browser

import (
	"image"
	"image/color"
	"strings"
	"testing"
)

// ruleSummary lists "{prop:value;...}" for each parsed rule so tests can
// compare which rules and declarations survived error recovery.
func ruleSummary(sheet Stylesheet) string {
	var parts []string
	for _, rule := range sheet.Rules {
		var decls []string
		for _, d := range rule.Declarations {
			decls = append(decls, d.Property+":"+d.Value)
		}
		parts = append(parts, "{"+strings.Join(decls, ";")+"}")
	}
	return strings.Join(parts, " ")
}

func TestScanCSSString(t *testing.T) {
	for _, tc := range []struct {
		in   string
		end  int
		bad  bool
		desc string
	}{
		{`"abc" x`, 5, false, "closed"},
		{`'a"b' x`, 5, false, "other quote inside"},
		{`"a\"b" x`, 6, false, "escaped quote"},
		{"\"a\nb\"", 2, true, "raw LF is bad, newline not consumed"},
		{"\"a\rb\"", 2, true, "raw CR is bad"},
		{"\"a\fb\"", 2, true, "raw FF is bad"},
		{"\"a\\\nb\" x", 6, false, "escaped LF continues"},
		{"\"a\\\r\nb\" x", 7, false, "escaped CRLF continues"},
		{`"abc`, 4, false, "EOF ends cleanly"},
		{`"abc\`, 5, false, "EOF after backslash"},
	} {
		end, bad := scanCSSString(tc.in, 0)
		if end != tc.end || bad != tc.bad {
			t.Errorf("%s: scanCSSString(%q) = %d, %v; want %d, %v", tc.desc, tc.in, end, bad, tc.end, tc.bad)
		}
	}
}

func TestCloseCSSStringAtEOF(t *testing.T) {
	for in, want := range map[string]string{
		`p{content:"OK`:        `p{content:"OK"`,
		`p{content:'OK`:        `p{content:'OK'`,
		`p{content:"OK\`:       `p{content:"OK"`,
		`p{content:"x\"}`:      `p{content:"x\"}"`,
		`p{content:"OK"}`:      `p{content:"OK"}`,
		`p{content:"\\"}`:      `p{content:"\\"}`,
		`p{content:"`:          `p{content:""`,
		`/* "open`:             `/* "open`,
		"p{content:\"bad\n}":   "p{content:\"bad\n}",
		"p{content:\"bad\n\"x": "p{content:\"bad\n\"x\"",
	} {
		if got := closeCSSStringAtEOF(in); got != want {
			t.Errorf("closeCSSStringAtEOF(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestCSSBadStringRecovery(t *testing.T) {
	for _, tc := range []struct{ name, css, want string }{
		// Declarations: the bad string drops only its declaration when the
		// next line starts cleanly.
		{"declaration dropped, next kept", "p{color:\"bad\n;color:red}q{color:blue}",
			"{color:red} {color:blue}"},
		{"escaped newline continues", "p{font-family:\"a\\\nb\";color:red}",
			"{font-family:\"a\\\nb\";color:red}"},
		{"custom property dropped", "p{--x:\"bad\n;--y:1}", "{--y:1}"},
		// Tokenizing restarts after the newline, so a later quote opens a new
		// string that swallows the following rule (the issue's repro).
		{"following rule swallowed", "p::before{content:\"bad\nstring\"; } p::after{content:\"A\"}", "{}"},
		// Selectors and at-rule preludes: the whole rule is dropped and the
		// next rule applies.
		{"selector bad string", "a[title=\"x\n]{color:red} p{color:blue}", "{color:blue}"},
		{"selector bad string then quote", "a\"x\n{color:red}p{color:blue}", "{color:blue}"},
		{"media prelude bad string", "@media \"x\n{p{color:red}} q{color:blue}", "{color:blue}"},
		{"supports prelude bad string", "@supports (content:\"x\n){p{color:red}} q{color:blue}", "{color:blue}"},
		// Strings inside comments are not tokens.
		{"quote in comment", "/* \"x\n */p{color:red}", "{color:red}"},
		// EOF closes the string and every open block.
		{"string at EOF", `p::before{content:"OK`, `{content:"OK"}`},
		{"nested block at EOF", `@media screen{p{content:"OK`, `{content:"OK"}`},
		{"comment at EOF", `p{color:red /* open`, `{color:red}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := ruleSummary(ParseCSS(tc.css)); got != tc.want {
				t.Fatalf("ParseCSS(%q) = %s, want %s", tc.css, got, tc.want)
			}
		})
	}
}

func TestInlineStyleStringAtEOF(t *testing.T) {
	decls := ParseDeclarations(`color:red;font-family:"Open`)
	if len(decls) != 2 || decls[1].Value != `"Open"` {
		t.Fatalf("declarations = %+v, want font-family \"Open\"", decls)
	}
	if decls := ParseDeclarations("font-family:\"bad\n;color:red"); len(decls) != 1 || decls[0].Property != "color" {
		t.Fatalf("declarations = %+v, want only color", decls)
	}
}

// The issue's two repros end to end: a string open at EOF is valid generated
// text, and a raw newline ends a bad string whose recovery swallows the next
// rule exactly as Chrome does.
func TestCSSStringTerminationEndToEnd(t *testing.T) {
	viewport := image.Rect(0, 0, 300, 100)
	if got := allText(t, `<style>p::before{content:"OK</style><p>x</p>`, viewport); got != "OK|x|" {
		t.Errorf("string at EOF: text = %q, want %q", got, "OK|x|")
	}
	if got := allText(t, "<style>p::before{content:\"bad\nstring\"; } p::after{content:\"A\"}</style><p>x</p>", viewport); got != "x|" {
		t.Errorf("bad string: text = %q, want %q", got, "x|")
	}
}

// Pixel regression: the rule after a bad-string line still paints, and the
// declaration with the bad string does not.
func TestCSSBadStringRecoveryPaint(t *testing.T) {
	img := painted(t, "<style>body{margin:0}"+
		"div{width:20px;height:20px;background:red}\n"+
		".a{font-family:\"bad\n}.a{background:green}\n"+
		".b{background:\"bad\n}.b{background:green}\n"+
		"</style><div class=a></div><div class=b></div>", image.Rect(0, 0, 40, 40))
	green := color.RGBA{0, 128, 0, 255}
	pixel(t, img, 10, 10, green)
	pixel(t, img, 10, 30, green)
}
