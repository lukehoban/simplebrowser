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
			<div id="round-nearest" style="width:round(nearest, 257px, 10px)"></div>
			<div id="round-tie" style="width:round(255px, 10px)"></div>
			<div id="round-up" style="width:round(up, 251px, 10px)"></div>
			<div id="round-down" style="width:round(down, 259px, 10px)"></div>
			<div id="round-zero" style="width:round(to-zero, 259px, 10px)"></div>
			<div id="round-negative-step" style="width:round(nearest, 257px, -10px)"></div>
			<div id="round-whitespace" style="width:round( up, 9px, 2px)"></div>
			<div id="round-whitespace-around-comma" style="width:round( up , 9px , 2px )"></div>
			<div id="round-percent" style="width:round(up, 51%, 10%)"></div>
			<div id="abs" style="width:abs(-120px)"></div>
			<div id="abs-percent" style="width:abs(-50%)"></div>
			<div id="sign-percent" style="width:calc(sign(-50%) * -100px)"></div>
			<div id="nested-round" style="width:abs(round(nearest, -257px, 10px))"></div>
			<div id="round-var" style="--step:10px;width:round(nearest, 257px, var(--step))"></div>
			<div id="clamp-none-low" style="width:clamp(none, 90%, 300px)"></div>
			<div id="clamp-none-high" style="width:clamp(100px, 50%, none)"></div>
			<div id="clamp-none-both" style="width:clamp(none, 50%, none)"></div>
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
	for _, tc := range []struct {
		id    string
		width int
	}{
		{"round-nearest", 260}, {"round-tie", 260}, {"round-up", 260}, {"round-down", 250},
		{"round-zero", 250}, {"round-negative-step", 260}, {"round-whitespace", 10},
		{"round-whitespace-around-comma", 10}, {"round-percent", 240},
		{"abs", 120}, {"abs-percent", 200}, {"sign-percent", 100}, {"nested-round", 260},
		{"round-var", 260}, {"clamp-none-low", 300}, {"clamp-none-high", 200},
		{"clamp-none-both", 200},
	} {
		box := boxesByID(got.Root, tc.id)[tc.id]
		if box == nil {
			t.Errorf("missing #%s box", tc.id)
			continue
		}
		if box.Rect.Dx() != tc.width {
			t.Errorf("#%s width = %d, want %d", tc.id, box.Rect.Dx(), tc.width)
		}
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
		<div id="invalid-var" style="--bad:2; width:45px; width:min(var(--bad), 10px)"></div>
		<div id="zero-step" style="width:46px;width:round(10px, 0px)"></div>
		<div id="bad-round-strategy" style="width:47px;width:round(sideways, 10px, 2px)"></div>
		<div id="bad-round-arity" style="width:48px;width:round(10px)"></div>
		<div id="bad-abs-arity" style="width:49px;width:abs(10px, 2px)"></div>
		<div id="bad-sign-type" style="width:50px;width:sign(10px)"></div>
		<div id="bad-clamp-none-middle" style="width:51px;width:clamp(1px, none, 3px)"></div>`)
	for id, want := range map[string]string{
		"fixed":                 "250px",
		"mixed":                 "calc(max(0px, min(50%, 300px)))",
		"bad-mixed":             "40px",
		"bad-clamp":             "41px",
		"bad-clamp4":            "42px",
		"bad-empty":             "43px",
		"bad-unit":              "44px",
		"zero-step":             "46px",
		"bad-round-strategy":    "47px",
		"bad-round-arity":       "48px",
		"bad-abs-arity":         "49px",
		"bad-sign-type":         "50px",
		"bad-clamp-none-middle": "51px",
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
	for _, v := range []string{"min(1px, 2)", "clamp(1px, 2px)", "minmax(1px, 2px)", "round(10px, 0px)", "round(sideways, 10px, 2px)", "round(10px)", "abs(10px, 2px)", "sign(10px)", "clamp(1px, none, 3px)", "min(1px 2px)"} {
		if featureSupported("width", v) {
			t.Errorf("@supports accepted width:%s", v)
		}
	}
	for _, v := range []string{"round(10px, 3px)", "round(up, 10px, 3px)", "round( up, 9px, 2px)", "abs(-10px)", "calc(sign(-10px) * 10px)", "clamp(none, 50%, 100px)", "clamp(1px, 2px, none)"} {
		if !featureSupported("width", v) {
			t.Errorf("@supports rejected width:%s", v)
		}
	}
}

func TestMathNonNegativePropertyRanges(t *testing.T) {
	doc := styledForLayout(t, `<body style="margin:0">
		<div id="fixed" style="padding:min(-1px, 10px);width:20px;height:10px"></div>
		<div id="var" style="--negative:-2px;padding:clamp(-10px, var(--negative), 5px);width:20px;height:10px"></div>
		<div style="width:100px"><div id="deferred-low" style="width:10px;padding-top:calc(10% - 20px);height:10px"></div></div>
		<div style="width:300px"><div id="deferred-high" style="width:10px;padding-top:calc(10% - 20px);height:10px"></div></div>
		<div id="border" style="border:solid;border-left-width:min(-1px, 10px)"></div>
		<div id="sized" style="width:max(-5px, -1px);height:10px"></div>
		<div id="neighbor" style="padding:clamp(1px, 5px, 10px);margin-left:min(-5px, 1px);width:10px"></div>
		<div id="gap" style="display:flex;width:20px;gap:min(-2px, 10px)"><div id="gap-a" style="width:10px;height:5px"></div><div id="gap-b" style="width:10px;height:5px"></div></div>
	</body>`)
	got, err := LayoutWithViewport(doc, image.Rect(0, 0, 800, 600))
	if err != nil {
		t.Fatal(err)
	}
	boxes := boxesByID(got.Root, "fixed", "var", "deferred-low", "deferred-high", "border", "sized", "gap-a", "gap-b")
	if got := styledElementByID(doc.StyleRoot, "fixed").Style["padding-top"]; got != "0px" {
		t.Errorf("negative min() padding computed to %q, want 0px", got)
	}
	if got := styledElementByID(doc.StyleRoot, "var").Style["padding-top"]; got != "0px" {
		t.Errorf("negative substituted clamp() padding computed to %q, want 0px", got)
	}
	if got := styledElementByID(doc.StyleRoot, "border").Style["border-left-width"]; got != "0px" {
		t.Errorf("negative min() border width computed to %q, want 0px", got)
	}
	if got := styledElementByID(doc.StyleRoot, "sized").Style["width"]; got != "0px" {
		t.Errorf("negative max() width computed to %q, want 0px", got)
	}
	if got := styledElementByID(doc.StyleRoot, "neighbor").Style["padding-top"]; got != "5px" {
		t.Errorf("valid neighboring clamp() padding computed to %q, want 5px", got)
	}
	if got := styledElementByID(doc.StyleRoot, "neighbor").Style["margin-left"]; got != "-5px" {
		t.Errorf("negative math margin computed to %q, want -5px", got)
	}
	if got := styledElementByID(doc.StyleRoot, "gap").Style["gap"]; got != "0px" {
		t.Errorf("negative math gap computed to %q, want 0px", got)
	}
	if p := boxes["deferred-low"].Content.Min.Y - boxes["deferred-low"].Rect.Min.Y; p != 0 {
		t.Errorf("deferred negative padding used value = %d, want 0", p)
	}
	if p := boxes["deferred-high"].Content.Min.Y - boxes["deferred-high"].Rect.Min.Y; p != 10 {
		t.Errorf("deferred positive padding used value = %d, want 10", p)
	}
	if x := boxes["gap-b"].Rect.Min.X; x != boxes["gap-a"].Rect.Max.X {
		t.Errorf("negative math gap placed second item at x=%d after first ended at %d", x, boxes["gap-a"].Rect.Max.X)
	}
	if !featureSupported("padding", "min(-1px, 10px)") {
		t.Error("@supports rejected valid negative math syntax for padding")
	}
	if !featureSupported("border-width", "max(-1px, 0px)") {
		t.Error("@supports rejected valid border-width math syntax")
	}
	if !featureSupported("width", "max(-5px, -1px)") {
		t.Error("@supports rejected valid negative math syntax for width")
	}
	if featureSupported("padding", "min(-1px, 2)") {
		t.Error("@supports accepted a dimensionally invalid padding expression")
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

func TestMathFunctionsDeferMixedPercentageCalculationsToLayout(t *testing.T) {
	doc := styledForLayout(t, `<body style="margin:0">
		<div style="width:400px"><div id="abs-positive" style="width:abs(calc(50% - 100px))"></div>
			<div id="round-mixed" style="width:round(nearest, 50%, 30px)"></div></div>
		<div style="width:100px"><div id="abs-negative" style="width:abs(calc(50% - 100px))"></div></div>
	</body>`)
	got, err := LayoutWithViewport(doc, image.Rect(0, 0, 800, 600))
	if err != nil {
		t.Fatal(err)
	}
	boxes := boxesByID(got.Root, "abs-positive", "abs-negative", "round-mixed")
	for id, want := range map[string]int{"abs-positive": 100, "abs-negative": 50, "round-mixed": 210} {
		if boxes[id] == nil || boxes[id].Rect.Dx() != want {
			t.Errorf("#%s width = %v, want %d", id, boxes[id], want)
		}
	}
}
