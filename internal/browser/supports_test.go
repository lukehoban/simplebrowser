package browser

import (
	"image"
	"image/color"
	"os"
	"testing"
)

func TestSupportsConditionEvaluation(t *testing.T) {
	for _, tc := range []struct {
		condition string
		want      bool
	}{
		{"(display:block)", true},
		{"( display : inline-block )", true},
		{"(DISPLAY: Block)", true},
		{"(display: block !important)", true},
		{"(color: #0f0)", true},
		{"(color: rebeccapurple)", true},
		{"(margin: 0 auto 4px 10%)", true},
		{"(fill: none)", true},
		{"(color: inherit)", true},
		{"(not-a-property:1)", false},
		{"(display: bogus)", false},
		{"(display)", false},
		{"(display:)", false},
		// Features this engine does not render must not be claimed.
		{"(mask-image: none)", false},
		{"(-webkit-mask-image: none)", false},
		{"(display: grid)", false},
		{"(display: flex)", false},
		{"(width: round(1.5px, 1px))", false},
		{"(width: calc(1px + 1px))", false},
		{"(float: left)", false},
		{"(opacity: 0.5)", false},
		// Boolean operators and nesting.
		{"not (mask-image: none)", true},
		{"NOT (display: block)", false},
		{"(display: block) and (color: red)", true},
		{"(display: block) and (display: grid)", false},
		{"(display: grid) or (display: block)", true},
		{"(display: grid) or (mask-image: none)", false},
		{"(display: block) and (color: red) and (width: 1px)", true},
		{"((display: grid) or (display: block)) and (not (mask-image: none))", true},
		{"not ((display: block) and (display: grid))", true},
		{"(not (display: grid))", true},
		{"/* c */ (display: block)", true},
		// Malformed or unsupported grammar is false, never an error.
		{"(display: block) and (color: red) or (width: 1px)", false},
		{"not (display: grid) and (display: block)", false},
		{"not(display: grid)", false},
		// Whitespace is required only after the keyword.
		{"(display: block)and (color: red)", true},
		{"(display: block) and(color: red)", false},
		{"selector(a > b)", false},
		{"not selector(a > b)", true},
		{"(display: block", false},
		{"display: block", false},
		{"", false},
		{"(display: block) (color: red)", false},
		{"(foo bar)", false},
		{"not (foo bar)", true},
		{"(content: ')')", false},
	} {
		if got := supportsConditionMatches(tc.condition); got != tc.want {
			t.Errorf("supportsConditionMatches(%q) = %v, want %v", tc.condition, got, tc.want)
		}
	}
}

func TestSupportsDeeplyNestedConditionIsBounded(t *testing.T) {
	condition := ""
	for i := 0; i < 1000; i++ {
		condition += "("
	}
	condition += "display: block"
	for i := 0; i < 1000; i++ {
		condition += ")"
	}
	if supportsConditionMatches(condition) {
		t.Fatal("over-deep nesting should fail closed")
	}
}

func TestParseCSSSupportsBlocks(t *testing.T) {
	sheet := ParseCSS(`
		@supports (display:block) { .yes { color: green } }
		@supports (not-a-property:1) { .no { color: red } }
		@supports (mask-image: none) { .mask { color: red } }
		@supports not (mask-image: none) { .fallback { color: green } }
		@supports(display:block){ .tight { color: green } }
		@supportsx (display:block) { .bogus { color: red } }
		@media screen and (min-width: 640px) {
			@supports (display: block) {
				.media-supports { color: green }
				@media (max-width: 700px) { .inner { color: red } }
			}
			@supports (display: grid) { .grid { color: red } }
		}
		@supports (display: block) {
			@media print { .print { color: red } }
			@supports not (display: flex) { .nested { color: green } }
		}
		.after { color: blue }`)
	var got []string
	media := map[string]string{}
	for _, rule := range sheet.Rules {
		name := rule.Selectors[0].Parts[0].Classes[0]
		got = append(got, name)
		media[name] = rule.Media
	}
	want := []string{"yes", "fallback", "tight", "media-supports", "inner", "print", "nested", "after"}
	if len(got) != len(want) {
		t.Fatalf("rules = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("rules = %v, want %v", got, want)
		}
	}
	if media["media-supports"] != "screen and (min-width: 640px)" ||
		media["inner"] != "screen and (min-width: 640px) and (max-width: 700px)" ||
		media["print"] != "print" || media["nested"] != "" {
		t.Fatalf("media conditions not preserved through @supports: %#v", media)
	}
}

// Moon/Vector icon pattern: mask-image is not rendered, so the background
// fallback must win, and the Moon repro's two boxes must both be green.
func TestSupportsCascadeSelectsFallback(t *testing.T) {
	green := color.RGBA{0, 128, 0, 255}
	img := painted(t, `<body style="margin:0">
<style>
div { width: 100px; height: 20px; background: red }
@supports (mask-image: none) { .icon { background: red } }
@supports not (mask-image: none) { .icon { background: green } }
@media (min-width: 1px) { @supports (display: block) { .nested { background: green } } }
</style>
<div class="icon"></div><div class="nested"></div></body>`, image.Rect(0, 0, 200, 60))
	pixel(t, img, 50, 10, green)
	pixel(t, img, 50, 30, green)

	source, err := os.ReadFile("../../testdata/wikipedia-moon/repros/supports.html")
	if err != nil {
		t.Fatal(err)
	}
	img = painted(t, string(source), image.Rect(0, 0, 400, 140))
	// Boxes are 300x40 at x=16; sample right of the text.
	pixel(t, img, 290, 12, green)
	pixel(t, img, 290, 68, green)
}

func TestSVGStylesheetSupports(t *testing.T) {
	img, err := decodeSVG([]byte(`<svg width="40" height="20">
	<style>
	rect { fill: red }
	@supports (fill: green) { .a { fill: green } }
	@supports (mask-image: none) { .b { fill: blue } }
	@supports not (mask-image: none) { .b { fill: green } }
	</style>
	<rect class="a" x="0" y="0" width="20" height="20"/>
	<rect class="b" x="20" y="0" width="20" height="20"/>
	</svg>`))
	if err != nil {
		t.Fatal(err)
	}
	for _, x := range []int{10, 30} {
		if got := img.RGBAAt(x, 10); got != (color.RGBA{0, 128, 0, 255}) {
			t.Errorf("pixel (%d,10) = %v, want green", x, got)
		}
	}
}
