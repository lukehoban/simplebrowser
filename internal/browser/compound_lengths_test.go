package browser

import (
	"image"
	"image/color"
	"math"
	"strings"
	"testing"
)

func TestCompoundLengthsComputedAndGeometry(t *testing.T) {
	doc := styledForLayout(t, `<html style="font-size:20px"><body style="margin:0">
		<table id="table" style="font-size:10px;border-spacing:1em 2ex">
			<tr><td style="width:10px;height:10px;background:red"></td></tr>
		</table>
		<div id="border" style="width:10px;height:10px;border:2vw solid red"></div>
		<div id="bg" style="width:120px;height:100px;background:linear-gradient(red,red) no-repeat 2vw 3vh/10vw 10vh"></div>
		</body></html>`)
	for _, tc := range []struct {
		viewport image.Rectangle
		width    float64
		height   float64
	}{
		{image.Rect(0, 0, 800, 600), 800, 600},
		{image.Rect(0, 0, 400, 300), 400, 300},
	} {
		got, err := LayoutWithViewport(doc, tc.viewport)
		if err != nil {
			t.Fatal(err)
		}
		table := styledElementByID(got.Document.StyleRoot, "table").Style
		ex := ratiosFor(table).ex * 20
		if want := "10px " + formatPixels(ex); table["border-spacing"] != want {
			t.Errorf("%v border-spacing = %q, want %q", tc.viewport, table["border-spacing"], want)
		}
		if grid := buildTableGrid(styledElementByID(got.Document.StyleRoot, "table")); grid.hspacing != 10 {
			t.Errorf("horizontal table spacing = %d, want 10", grid.hspacing)
		} else if want := int(math.Round(ex)); grid.vspacing != want {
			t.Errorf("vertical table spacing = %d, want %d", grid.vspacing, want)
		}
		border := styledElementByID(got.Document.StyleRoot, "border").Style
		if border["border-left"] != formatPixels(tc.width*.02)+" solid red" ||
			borderWidth(border, "left") != int(math.Round(tc.width*.02)) {
			t.Errorf("border shorthand = %q, width = %d", border["border-left"], borderWidth(border, "left"))
		}
		bg := styledElementByID(got.Document.StyleRoot, "bg").Style
		if bg["background-position"] != formatPixels(tc.width*.02)+" "+formatPixels(tc.height*.03) ||
			bg["background-size"] != formatPixels(tc.width*.1)+" "+formatPixels(tc.height*.1) {
			t.Errorf("background position/size = %q / %q", bg["background-position"], bg["background-size"])
		}
	}
}

func TestCompoundLengthsPreserveOpaqueValuesAndCascade(t *testing.T) {
	for _, tc := range []struct{ property, input, want string }{
		{"background-position", `left 2ch, right 1vw`, `left 10px, right 8px`},
		{"background-size", `auto 2em, 10vmin 5vmax`, `auto 20px, 60px 40px`},
		{"border-top", `1em solid rgb(2, 3, 4)`, `10px solid rgb(2, 3, 4)`},
		{"background-position", `url("1em 3vw") 2vw`, `url("1em 3vw") 16px`},
		{"background-position", `"2em 3vw" 1em`, `"2em 3vw" 10px`},
		{"background-size", `var(--x, 1em) 2vw`, `var(--x, 1em) 16px`},
	} {
		values := ComputedStyle{"font-size": "10px", "font-family": "monospace", tc.property: tc.input}
		resolveViewportRelativeValues(values, image.Pt(800, 600))
		resolveFontRelativeValues(values, 20)
		if tc.property == "background-position" && strings.HasPrefix(tc.input, "left") {
			tc.want = "left " + formatPixels(20*ratiosFor(values).ch) + ", right 8px"
		}
		if values[tc.property] != tc.want {
			t.Errorf("%s: got %q, want %q", tc.property, values[tc.property], tc.want)
		}
	}
	doc := styledForLayout(t, `<style>#target { background-position:1em 2ch !important }</style>
		<div id="target" style="font-size:10px;background-position:20vw 20vh"></div>`)
	style := styledElementByID(doc.StyleRoot, "target").Style
	if !strings.HasPrefix(style["background-position"], "10px ") {
		t.Errorf("important rule lost to inline declaration: %q", style["background-position"])
	}
}

func TestCompoundBackgroundPixels(t *testing.T) {
	img := painted(t, `<div style="margin:0;width:120px;height:100px;
		background:linear-gradient(red,red) no-repeat 2vw 3vh/10vw 10vh;
		background-color:green"></div>`, image.Rect(0, 0, 800, 600))
	pixel(t, img, 15, 18, color.RGBA{0, 128, 0, 255})
	pixel(t, img, 16, 18, color.RGBA{255, 0, 0, 255})
	pixel(t, img, 95, 77, color.RGBA{255, 0, 0, 255})
	pixel(t, img, 96, 78, color.RGBA{0, 128, 0, 255})

	// Font-relative components must use this element's font, not the 16px
	// fallback in px(), for both background positioning and tile size.
	fontImg := painted(t, `<div style="margin:0;font-size:10px;width:50px;height:40px;
		background:linear-gradient(red,red) no-repeat 1em 2em/2em 1em;
		background-color:green"></div>`, image.Rect(0, 0, 800, 600))
	pixel(t, fontImg, 9, 20, color.RGBA{0, 128, 0, 255})
	pixel(t, fontImg, 10, 20, color.RGBA{255, 0, 0, 255})
	pixel(t, fontImg, 29, 29, color.RGBA{255, 0, 0, 255})
	pixel(t, fontImg, 30, 29, color.RGBA{0, 128, 0, 255})
}

func TestCalcLengthDeclarationsGeometryAndPixels(t *testing.T) {
	doc := styledForLayout(t, `<html style="font-size:20px"><body style="margin:0">
		<div id="wrap" style="width:400px">
			<div id="fixed" style="width:calc(250px * 1.1);height:20px;background:green"></div>
			<div id="percent" style="width:calc(100% - 20px);height:20px;background:blue"></div>
			<div id="half" style="width:calc(100% * .5)"></div>
			<div id="units" style="font-size:10px;width:calc(10em + 10vw);height:calc(10vh / 2)"></div>
			<div id="variable" style="--size:250px;width:calc(var(--size) * 1.1)"></div>
			<div id="fallback" style="width:99px;width:calc(1px + 2)"></div>
			<div id="divide" style="width:98px;width:calc(10px / 0)"></div>
		</div></body></html>`)
	got, err := LayoutWithViewport(doc, image.Rect(0, 0, 800, 600))
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		id            string
		width, height int
	}{
		{"fixed", 275, 20},
		{"percent", 380, 20},
		{"half", 200, 0},
		{"units", 180, 30},
		{"variable", 275, 0},
		{"fallback", 99, 0},
		{"divide", 98, 0},
	} {
		box := boxesByID(got.Root, tc.id)[tc.id]
		if box == nil {
			t.Fatalf("missing #%s box", tc.id)
		}
		if box.Rect.Dx() != tc.width || tc.height != 0 && box.Rect.Dy() != tc.height {
			t.Errorf("#%s rect = %v, want width %d height %d", tc.id, box.Rect, tc.width, tc.height)
		}
	}
	img := painted(t, `<body style="margin:0"><div style="width:400px">
		<div style="width:calc(250px * 1.1);height:20px;background:green"></div>
		<div style="width:calc(100% - 20px);height:20px;background:blue"></div>
		</div></body>`, image.Rect(0, 0, 800, 600))
	pixel(t, img, 274, 5, color.RGBA{0, 128, 0, 255})
	pixel(t, img, 275, 5, color.RGBA{255, 255, 255, 255})
	pixel(t, img, 379, 25, color.RGBA{0, 0, 255, 255})
	pixel(t, img, 380, 25, color.RGBA{255, 255, 255, 255})
}

func TestCalcInvalidVariableUsesUnsetAndValidSupports(t *testing.T) {
	doc := styledForLayout(t, `<style>
		#supported { width:calc(100% - 20px) }
		#invalid { width:40px }
		</style><div id="supported" style="width:calc(100% - 20px)"></div>
		<div id="invalid" style="--bad:1; width:calc(var(--bad) + 2px)"></div>`)
	supported := styledElementByID(doc.StyleRoot, "supported")
	if !featureSupported("width", "calc(100% - 20px)") {
		t.Error("@supports rejected a supported calc() length")
	}

	if got := supported.Style["width"]; got != "calc(max(0px, calc(100% - 20px)))" {
		t.Errorf("computed width before layout = %q", got)
	}
	invalid := styledElementByID(doc.StyleRoot, "invalid")
	if invalid.Style["width"] == "calc(var(--bad) + 2px)" || invalid.Style["width"] == "40px" {
		t.Errorf("invalid substituted width should behave as unset, got %q", invalid.Style["width"])
	}
}

func TestCalcExpressionNestingIsBounded(t *testing.T) {
	expression := "calc(" + strings.Repeat("(", maxCSSMathDepth+1) + "1px" +
		strings.Repeat(")", maxCSSMathDepth+1) + ")"
	if validCalcDeclaration("width", expression) {
		t.Fatal("excessively nested calc() expression was accepted")
	}
}

func TestCalcFlexBasisResolvesPercentageAgainstContainer(t *testing.T) {
	doc := styledForLayout(t, `<body style="margin:0">
		<div style="display:flex;width:400px"><div id="mixed" style="flex-basis:calc(50% - 10px);flex-shrink:0;height:5px"></div></div>
		<div style="display:flex;width:400px"><div id="plain" style="flex-basis:50%;flex-shrink:0;height:5px"></div></div>
		<div style="display:flex;width:400px"><div id="fixed" style="flex-basis:calc(100px + 20px);flex-shrink:0;height:5px"></div></div>
		<div style="display:flex;width:400px"><div id="em" style="font-size:10px;flex-basis:calc(25% + 2em);flex-shrink:0;height:5px"></div></div>
		<div style="display:flex;width:400px"><div id="negative" style="flex-basis:calc(10% - 100px);flex-shrink:0;height:5px"></div></div>
		</body>`)
	got, err := LayoutWithViewport(doc, image.Rect(0, 0, 800, 600))
	if err != nil {
		t.Fatal(err)
	}
	for id, want := range map[string]int{"mixed": 190, "plain": 200, "fixed": 120, "em": 120, "negative": 0} {
		box := boxesByID(got.Root, id)[id]
		if box == nil {
			t.Fatalf("missing #%s box", id)
		}
		if box.Rect.Dx() != want {
			t.Errorf("#%s width = %d, want %d", id, box.Rect.Dx(), want)
		}
	}
	if !featureSupported("flex-basis", "calc(50% - 10px)") {
		t.Error("@supports rejected a supported flex-basis calc()")
	}
}

func TestCalcPercentageRejectedWhereGrammarDisallowsIt(t *testing.T) {
	if featureSupported("border-width", "calc(10% + 1px)") {
		t.Error("@supports accepted a percentage border-width calc()")
	}
	if !featureSupported("border-width", "calc(1em + 1px)") {
		t.Error("@supports rejected a length-only border-width calc()")
	}
	doc := styledForLayout(t, `<div id="b" style="border:2px solid red;border-left-width:calc(10% + 1px)"></div>`)
	if got := borderWidth(styledElementByID(doc.StyleRoot, "b").Style, "left"); got != 2 {
		t.Errorf("left border width = %d, want invalid calc() ignored (2)", got)
	}
}

func TestCalcBoxShorthandComponents(t *testing.T) {
	for _, tc := range []struct {
		property, value string
	}{
		{"margin", "calc(10% - 1px)"},
		{"padding", "calc(10% - 1px)"},
		{"border-width", "calc(1px + 2px)"},
		{"margin", "calc(10% - 1px) 0"},
		{"padding", "0 calc(10% - 1px)"},
		{"border-width", "calc(1px + 2px) 2px"},
	} {
		t.Run(tc.property+"/"+tc.value, func(t *testing.T) {
			if !featureSupported(tc.property, tc.value) {
				t.Fatalf("@supports rejected %s: %q", tc.property, tc.value)
			}
			expanded := expandDeclaration(Declaration{Property: tc.property, Value: tc.value})
			if len(expanded) != 4 {
				t.Fatalf("expandDeclaration() returned %d longhands, want 4: %+v", len(expanded), expanded)
			}
			want := map[string]bool{}
			for _, side := range []string{"top", "right", "bottom", "left"} {
				if tc.property == "border-width" {
					want["border-"+side+"-width"] = true
				} else {
					want[tc.property+"-"+side] = true
				}
			}
			for i, d := range expanded {
				if !want[d.Property] {
					t.Errorf("longhand %d property = %q", i, d.Property)
				}
			}
		})
	}
	doc := styledForLayout(t, `<style>
		@supports (margin:calc(10% - 1px) 0) { #supported { padding:calc(10% - 1px) 0 } }
		</style><div id="supported" style="margin:calc(10% - 1px) 0;border:solid;
		border-width:calc(1px + 2px) 2px"></div>`)
	style := styledElementByID(doc.StyleRoot, "supported").Style
	for _, property := range []string{"margin-top", "margin-right", "margin-bottom", "margin-left",
		"padding-top", "padding-right", "padding-bottom", "padding-left",
		"border-top-width", "border-right-width", "border-bottom-width", "border-left-width"} {
		if style[property] == "" {
			t.Errorf("shorthand expansion did not set %s: style=%+v", property, style)
		}
	}
	for _, tc := range []struct {
		property, value string
	}{
		{"margin", "calc(10% - 1px) bogus"},
		{"padding", "calc(1px + 1)"},
		{"border-width", "calc(10% + 1px)"},
	} {
		if featureSupported(tc.property, tc.value) {
			t.Errorf("@supports accepted invalid %s: %q", tc.property, tc.value)
		}
	}
}

func TestBoxShorthandInvalidComponentIsAtomic(t *testing.T) {
	for _, tc := range []struct{ property, valid, invalid, prefix string }{
		{"margin", "4px", "calc(10% - 1px) bogus", "margin"},
		{"padding", "4px", "2px bogus", "padding"},
		{"border-width", "4px", "calc(1px + 2px) bogus", "border"},
		{"border-width", "4px", "calc(10% + 1px) 2px", "border"},
		{"margin", "4px", "calc(1px + 2) 3px", "margin"},
	} {
		t.Run(tc.property+"/"+tc.invalid, func(t *testing.T) {
			doc := styledForLayout(t, `<div id="target" style="`+tc.property+`:`+tc.valid+`;`+tc.property+`:`+tc.invalid+`"></div>`)
			style := styledElementByID(doc.StyleRoot, "target").Style
			for _, side := range []string{"top", "right", "bottom", "left"} {
				name := tc.prefix + "-" + side
				if tc.property == "border-width" {
					name += "-width"
				}
				if got := style[name]; got != tc.valid {
					t.Errorf("%s = %q, want %q; shorthand must be discarded as a unit", name, got, tc.valid)
				}
			}
			if featureSupported(tc.property, tc.invalid) {
				t.Errorf("@supports accepted invalid %s: %s", tc.property, tc.invalid)
			}
		})
	}
	// A substituted invalid shorthand wins the cascade but must invalidate
	// all four sides rather than leaving valid components or earlier values.
	doc := styledForLayout(t, `<div id="target" style="--bad:bogus;margin:4px;margin:calc(10% - 1px) var(--bad)"></div>`)
	style := styledElementByID(doc.StyleRoot, "target").Style
	for _, side := range []string{"top", "right", "bottom", "left"} {
		if got := style["margin-"+side]; got == "4px" || got == "calc(10% - 1px)" {
			t.Errorf("invalid substituted margin-%s = %q", side, got)
		}
	}
}

func TestCalcFlexBasisIndefiniteColumnUsesContent(t *testing.T) {
	source := `<body style="margin:0">
		<div style="display:flex;flex-direction:column;width:400px">
			<div id="mixed" style="flex-basis:calc(50% - 10px);background:green;height:24px"></div>
			<div id="plain" style="flex-basis:50%;background:blue;height:18px"></div>
			<div id="fixed" style="flex-basis:calc(20px + 10px);background:red;height:24px"></div>
		</div>
		<div style="display:flex;flex-direction:column;width:400px;height:200px">
			<div id="definite" style="flex-basis:calc(50% - 10px);background:green;height:24px"></div>
		</div></body>`
	doc := styledForLayout(t, source)
	got, err := LayoutWithViewport(doc, image.Rect(0, 0, 800, 600))
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		id   string
		y, h int
	}{
		{"mixed", 0, 24}, {"plain", 24, 18}, {"fixed", 42, 30},
		{"definite", 72, 90},
	} {
		box := boxesByID(got.Root, tc.id)[tc.id]
		if box == nil || box.Rect.Min.Y != tc.y || box.Rect.Dy() != tc.h {
			t.Errorf("#%s box = %v, want y=%d height=%d", tc.id, box, tc.y, tc.h)
		}
	}
	img := painted(t, source, image.Rect(0, 0, 800, 600))
	pixel(t, img, 2, 23, color.RGBA{0, 128, 0, 255})
	pixel(t, img, 2, 24, color.RGBA{0, 0, 255, 255})
	pixel(t, img, 2, 42, color.RGBA{255, 0, 0, 255})
}
func TestCalcHeightPercentageNeedsDefiniteContainingBlock(t *testing.T) {
	n := &StyledNode{Style: ComputedStyle{"height": "calc(50% - 10px)"}}
	if _, definite := specifiedHeight(n, 200, false); definite {
		t.Fatal("calc height with a percentage was definite without a containing-block height")
	}
	if got, definite := specifiedHeight(n, 200, true); !definite || got != 90 {
		t.Fatalf("calc height with a definite 200px basis = (%d, %t), want (90, true)", got, definite)
	}

	doc := styledForLayout(t, `<body style="margin:0"><div id="parent">
		<div id="calculated" style="height:calc(50% - 10px)">text</div>
		<div id="automatic">text</div>
		</div></body>`)
	got, err := LayoutWithViewport(doc, image.Rect(0, 0, 800, 600))
	if err != nil {
		t.Fatal(err)
	}
	calculated := boxesByID(got.Root, "calculated")["calculated"]
	automatic := boxesByID(got.Root, "automatic")["automatic"]
	if calculated == nil || automatic == nil {
		t.Fatal("missing calculated or automatic box")
	}
	if calculated.Rect.Dy() == 0 || calculated.Rect.Dy() != automatic.Rect.Dy() {
		t.Errorf("indefinite percentage calc height = %d, auto sibling height = %d; want auto behavior",
			calculated.Rect.Dy(), automatic.Rect.Dy())
	}
}
