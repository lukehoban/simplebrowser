package browser

import (
	"image"
	"image/color"
	"testing"
)

func TestMathComparisonLengthsGeometry(t *testing.T) {
	doc := styledForLayout(t, `<html style="font-size:20px"><body style="margin:0">
		<div id="wrap" style="width:400px">
			<div id="repro" style="width:min(250px, 300px);height:10px"></div>
			<div id="max" style="width:MAX(100px, 120px, 90px)"></div>
			<div id="pct-min" style="width:min(50%, 300px)"></div>
			<div id="pct-max" style="width:max(10%, 150px)"></div>
			<div id="pct-only" style="width:min(80%, 60%)"></div>
			<div id="clamp-mid" style="width:clamp(100px, 50%, 300px)"></div>
			<div id="clamp-low" style="width:clamp(250px, 10%, 300px)"></div>
			<div id="clamp-high" style="width:clamp(10px, 90%, 300px)"></div>
			<div id="clamp-conflict" style="width:clamp(200px, 50px, 100px)"></div>
			<div id="units" style="font-size:10px;width:min(30em, 50vw)"></div>
			<div id="nested" style="width:calc(min(50%, 300px) - 10px)"></div>
			<div id="nested-calc" style="width:calc(calc(20px + 10px) * 2)"></div>
			<div id="inner-calc" style="width:max(calc(100% - 50px), 10px)"></div>
			<div id="var" style="--w:220px;width:min(var(--w), 50%)"></div>
			<div id="var-args" style="--args:120px, 80px;width:max(var(--args))"></div>
			<div id="height" style="height:clamp(5px, 1px, 20px)"></div>
			<div id="margin" style="margin-left:min(10%, 25px);width:10px"></div>
			<div id="padding" style="padding:max(5px, 2%) 0;width:10px"></div>
		</div></body></html>`)
	got, err := LayoutWithViewport(doc, image.Rect(0, 0, 800, 600))
	if err != nil {
		t.Fatal(err)
	}
	boxes := boxesByID(got.Root, "repro", "max", "pct-min", "pct-max", "pct-only", "clamp-mid", "clamp-low",
		"clamp-high", "clamp-conflict", "units", "nested", "nested-calc", "inner-calc", "var", "var-args",
		"height", "margin", "padding")
	for _, tc := range []struct {
		id    string
		width int
	}{
		{"repro", 250}, {"max", 120}, {"pct-min", 200}, {"pct-max", 150}, {"pct-only", 240},
		{"clamp-mid", 200}, {"clamp-low", 250}, {"clamp-high", 300}, {"clamp-conflict", 200},
		{"units", 300}, {"nested", 190}, {"nested-calc", 60}, {"inner-calc", 350}, {"var", 200},
		{"var-args", 120},
	} {
		box := boxes[tc.id]
		if box == nil {
			t.Fatalf("missing #%s box", tc.id)
		}
		if box.Rect.Dx() != tc.width {
			t.Errorf("#%s width = %d, want %d (rect %v)", tc.id, box.Rect.Dx(), tc.width, box.Rect)
		}
	}
	if h := boxes["height"].Rect.Dy(); h != 5 {
		t.Errorf("clamp() height = %d, want 5", h)
	}
	if x := boxes["margin"].Rect.Min.X; x != 25 {
		t.Errorf("min() margin-left x = %d, want 25", x)
	}
	if p := boxes["padding"].Content.Min.Y - boxes["padding"].Rect.Min.Y; p != 8 {
		t.Errorf("max() padding-top = %d, want 8", p)
	}
}

func TestMathComparisonComputedValues(t *testing.T) {
	doc := styledForLayout(t, `<div id="fixed" style="width:min(250px, 300px)"></div>
		<div id="mixed" style="width:min(50%, 300px)"></div>
		<div id="bad-mixed" style="width:40px;width:min(10px, 2)"></div>
		<div id="bad-clamp" style="width:41px;width:clamp(10px, 20px)"></div>
		<div id="bad-clamp4" style="width:42px;width:clamp(1px, 2px, 3px, 4px)"></div>
		<div id="bad-empty" style="width:43px;width:max()"></div>
		<div id="bad-unit" style="width:44px;width:min(10px, 5deg)"></div>
		<div id="bad-border" style="border:2px solid red;border-left-width:min(10%, 1px)"></div>
		<div id="invalid-var" style="--bad:2; width:45px; width:min(var(--bad), 10px)"></div>`)
	for id, want := range map[string]string{
		"fixed":      "250px",
		"mixed":      "calc(min(50%, 300px))",
		"bad-mixed":  "40px",
		"bad-clamp":  "41px",
		"bad-clamp4": "42px",
		"bad-empty":  "43px",
		"bad-unit":   "44px",
	} {
		if got := styledElementByID(doc.StyleRoot, id).Style["width"]; got != want {
			t.Errorf("#%s computed width = %q, want %q", id, got, want)
		}
	}
	if got := borderWidth(styledElementByID(doc.StyleRoot, "bad-border").Style, "left"); got != 2 {
		t.Errorf("percentage min() border width = %d, want 2", got)
	}
	// Invalid at computed-value time: behaves as unset (auto), not the
	// lower-priority 45px and not the raw expression.
	if got := styledElementByID(doc.StyleRoot, "invalid-var").Style["width"]; got == "45px" || got == "min(2, 10px)" {
		t.Errorf("invalid substituted min() should behave as unset, got %q", got)
	}
	for _, v := range []string{"min(250px, 300px)", "max(10%, 1em)", "clamp(1px, 50%, 100px)", "calc(min(1px, 2px) + 1px)"} {
		if !featureSupported("width", v) {
			t.Errorf("@supports rejected width:%s", v)
		}
	}
	for _, v := range []string{"min(1px, 2)", "clamp(1px, 2px)", "minmax(1px, 2px)", "round(10px, 3px)", "min(1px 2px)"} {
		if featureSupported("width", v) {
			t.Errorf("@supports accepted width:%s", v)
		}
	}
}

func TestMathComparisonPixels(t *testing.T) {
	img := painted(t, `<body style="margin:0"><div style="width:400px">
		<div style="width:min(250px, 300px);height:20px;background:green"></div>
		<div style="width:clamp(100px, 50%, 300px);height:20px;background:blue"></div>
		</div></body>`, image.Rect(0, 0, 800, 600))
	pixel(t, img, 249, 5, color.RGBA{0, 128, 0, 255})
	pixel(t, img, 250, 5, color.RGBA{255, 255, 255, 255})
	pixel(t, img, 199, 25, color.RGBA{0, 0, 255, 255})
	pixel(t, img, 200, 25, color.RGBA{255, 255, 255, 255})
}

func TestMathComparisonFlexBasisAndCustomPropertyCalc(t *testing.T) {
	doc := styledForLayout(t, `<body style="margin:0">
		<div style="display:flex;width:400px"><div id="basis" style="flex-basis:min(50%, 150px);flex-shrink:0;height:5px"></div></div>
		<div style="display:flex;width:400px"><div id="basis-clamp" style="flex-basis:clamp(10px, 25%, 50px);flex-shrink:0;height:5px"></div></div>
		<div style="width:200px"><div id="from-var" style="--card:calc(50% - 10px);width:var(--card)"></div></div>
		<div style="width:200px"><div id="auto-height" style="height:min(50%, 30px)"><div style="height:7px"></div></div></div>
	</body>`)
	got, err := LayoutWithViewport(doc, image.Rect(0, 0, 800, 600))
	if err != nil {
		t.Fatal(err)
	}
	boxes := boxesByID(got.Root, "basis", "basis-clamp", "from-var", "auto-height")
	if w := boxes["basis"].Rect.Dx(); w != 150 {
		t.Errorf("flex-basis:min() width = %d, want 150", w)
	}
	if w := boxes["basis-clamp"].Rect.Dx(); w != 50 {
		t.Errorf("flex-basis:clamp() width = %d, want 50", w)
	}
	if w := boxes["from-var"].Rect.Dx(); w != 90 {
		t.Errorf("var() holding calc() width = %d, want 90", w)
	}
	// A percentage height against an indefinite containing block height
	// behaves as auto, including inside a comparison function.
	if h := boxes["auto-height"].Rect.Dy(); h != 7 {
		t.Errorf("min() percentage height with indefinite basis = %d, want auto (7)", h)
	}
}
