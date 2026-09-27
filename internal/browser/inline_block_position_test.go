package browser

import (
	"image"
	"image/color"
	"testing"
)

func TestStaticInlineBlockExternalPositionedDescendants(t *testing.T) {
	for _, tc := range []struct {
		name, prefix string
		wrapped      bool
	}{
		{"first line", "", false},
		{"wrapped line", `<span style="display:inline-block;width:170px;height:10px"></span>`, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			markup := `<body style="margin:0"><section style="position:relative;height:200px;padding:10px;line-height:0">` +
				`<div style="height:30px"></div><div style="width:180px">` + tc.prefix +
				`<span id="atomic" style="display:inline-block;width:80px;padding:4px;line-height:20px;background:yellow">text` +
				`<span id="abs" style="position:absolute;left:25%;top:10px;width:10px;height:50%;background:red"></span>` +
				`<span id="fixed" style="position:fixed;left:300px;top:40px;width:10px;height:20px;background:blue"></span>` +
				`</span></div></section></body>`
			boxes := percentHeightLayout(t, markup, "atomic", "abs", "fixed")
			if got, want := boxes["abs"].Rect, image.Rect(100, 10, 110, 120); got != want {
				t.Errorf("absolute box = %v, want %v", got, want)
			}
			if got, want := boxes["fixed"].Rect, image.Rect(300, 40, 310, 60); got != want {
				t.Errorf("fixed box = %v, want %v", got, want)
			}
			if boxes["atomic"].Rect.Dy() != 28 {
				t.Errorf("atomic height = %v, want 28 (positioned children excluded)", boxes["atomic"].Rect)
			}
			if tc.wrapped && boxes["atomic"].Rect.Min.Y <= 40 {
				t.Errorf("inline-block did not wrap: %v", boxes["atomic"].Rect)
			}
			img := painted(t, markup, image.Rect(0, 0, 400, 400))
			pixel(t, img, 105, 15, color.RGBA{255, 0, 0, 255})
			pixel(t, img, 305, 45, color.RGBA{0, 0, 255, 255})
		})
	}
}

func TestStaticInlineBlockAutoPositionAndLocalRelativeAncestor(t *testing.T) {
	markup := `<body style="margin:0"><section style="position:relative;height:200px;line-height:0">` +
		`<div style="height:30px"></div><span id="atomic" style="display:inline-block;width:90px;line-height:20px">x` +
		`<span id="auto" style="position:absolute;width:10px;height:10px"></span>` +
		`<span id="local" style="display:block;position:relative;width:50px;height:20px">` +
		`<span id="inner" style="position:absolute;left:5px;top:5px;width:10px;height:10px"></span>` +
		`</span></span></section></body>`
	boxes := percentHeightLayout(t, markup, "atomic", "auto", "local", "inner")
	if got := boxes["auto"].Rect.Min.Y; got < 30 {
		t.Errorf("auto static position not translated with inline block: %v", boxes["auto"].Rect)
	}

	if got, want := boxes["inner"].Rect.Min, boxes["local"].Rect.Min.Add(image.Pt(5, 5)); got != want {
		t.Errorf("local positioned descendant = %v, want %v", got, want)
	}
}

func TestNestedInlineBlockKeepsLocalContainingBlock(t *testing.T) {
	markup := `<body style="margin:0"><section style="height:40px;line-height:0">` +
		`<span id="outer" style="display:inline-block;position:relative;width:100px;height:30px">` +
		`<span id="nested" style="display:inline-block;width:40px;height:20px">text` +
		`<span id="abs" style="position:absolute;left:7px;top:9px;width:10px;height:50%"></span>` +
		`</span></span></section></body>`
	boxes := percentHeightLayout(t, markup, "outer", "abs")
	if got, want := boxes["abs"].Rect,
		image.Rect(boxes["outer"].Rect.Min.X+7, boxes["outer"].Rect.Min.Y+9,
			boxes["outer"].Rect.Min.X+17, boxes["outer"].Rect.Min.Y+24); got != want {
		t.Errorf("nested absolute box = %v, want %v", got, want)
	}
}

func TestExternalPositionedTextDoesNotSetInlineBlockBaseline(t *testing.T) {
	markup := `<body style="margin:0"><section style="position:relative;line-height:20px">before` +
		`<span id="atomic" style="display:inline-block;width:60px">first<br>last` +
		`<span style="position:absolute;top:0;left:0;line-height:80px">out</span>` +
		`</span>after</section></body>`
	layout, err := LayoutWithViewport(styledForLayout(t, markup), image.Rect(0, 0, 400, 400))
	if err != nil {
		t.Fatal(err)
	}
	var runs = map[string]TextRun{}
	var walk func(*Box)
	walk = func(b *Box) {
		for _, run := range b.Text {
			runs[run.Text] = run
		}
		for _, child := range b.Children {
			walk(child)
		}
	}
	walk(layout.Root)
	if runs["last"].Rect.Min.Y != runs["before"].Rect.Min.Y ||
		runs["last"].Rect.Min.Y != runs["after"].Rect.Min.Y {
		t.Errorf("positioned text changed baseline: before=%v last=%v after=%v",
			runs["before"].Rect, runs["last"].Rect, runs["after"].Rect)
	}
}
