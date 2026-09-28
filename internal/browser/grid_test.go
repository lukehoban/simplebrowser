package browser

import (
	"image"
	"testing"
)

func TestGridThreeNamedAreasLayOutInTrackOrder(t *testing.T) {
	doc := styledForLayout(t, `<body style="margin:0"><div id="grid" style="display:grid;width:160px;font:10px monospace;grid-template-columns:min-content minmax(0,auto) min-content;grid-template-areas:'leading text trailing';column-gap:8px;align-items:center"><span id="text" style="grid-area:text;white-space:nowrap">middle</span><span id="trailing" style="grid-area:trailing">i</span><span id="leading" style="grid-area:leading">WIDE</span></div></body>`)
	layout, err := LayoutWithViewport(doc, image.Rect(0, 0, 160, 80))
	if err != nil {
		t.Fatal(err)
	}
	boxes := boxesByID(layout.Root, "grid", "leading", "text", "trailing")
	grid, leading, text, trailing := boxes["grid"], boxes["leading"], boxes["text"], boxes["trailing"]
	if grid == nil || leading == nil || text == nil || trailing == nil {
		t.Fatalf("missing grid boxes: %+v", boxes)
	}
	if !(leading.Rect.Min.X < text.Rect.Min.X && text.Rect.Min.X < trailing.Rect.Min.X) {
		t.Fatalf("named areas are not in track order: leading=%v text=%v trailing=%v",
			leading.Rect, text.Rect, trailing.Rect)
	}
	if leading.Rect.Max.X > text.Rect.Min.X || text.Rect.Max.X > trailing.Rect.Min.X {
		t.Fatalf("grid tracks overlap: leading=%v text=%v trailing=%v",
			leading.Rect, text.Rect, trailing.Rect)
	}
	if leading.Rect.Min.Y != text.Rect.Min.Y || text.Rect.Min.Y != trailing.Rect.Min.Y {
		t.Fatalf("centered one-row items have inconsistent y positions: leading=%v text=%v trailing=%v",
			leading.Rect, text.Rect, trailing.Rect)
	}
}

func TestGridIntrinsicTracksFollowNamedAreasNotDOMOrder(t *testing.T) {
	layoutFor := func(children string) map[string]*Box {
		t.Helper()
		doc := styledForLayout(t, `<body style="margin:0"><div id="grid" style="display:grid;width:160px;font:10px monospace;grid-template-columns:min-content minmax(0,auto) min-content;grid-template-areas:'leading text trailing';column-gap:8px">`+children+`</div></body>`)
		layout, err := LayoutWithViewport(doc, image.Rect(0, 0, 200, 80))
		if err != nil {
			t.Fatal(err)
		}
		return boxesByID(layout.Root, "leading", "text", "trailing")
	}
	forward := layoutFor(`<span id="leading" style="grid-area:leading">WIDE</span><span id="text" style="grid-area:text">middle</span><span id="trailing" style="grid-area:trailing">i</span>`)
	reordered := layoutFor(`<span id="text" style="grid-area:text">middle</span><span id="trailing" style="grid-area:trailing">i</span><span id="leading" style="grid-area:leading">WIDE</span>`)
	for _, id := range []string{"leading", "text", "trailing"} {
		if forward[id] == nil || reordered[id] == nil {
			t.Fatalf("missing %s box: forward=%+v reordered=%+v", id, forward, reordered)
		}
		if forward[id].Rect != reordered[id].Rect {
			t.Errorf("%s geometry changes when DOM order changes: forward=%v reordered=%v",
				id, forward[id].Rect, reordered[id].Rect)
		}
	}
}

func TestGridUnsupportedExtraChildFallsBackWithoutDroppingContent(t *testing.T) {
	doc := styledForLayout(t, `<body style="margin:0"><div id="grid" style="display:grid;width:160px;grid-template-columns:min-content minmax(0,auto) min-content;grid-template-areas:'leading text trailing';column-gap:8px"><span style="grid-area:leading">L</span><span style="grid-area:text">M</span><span style="grid-area:trailing">T</span><div id="extra">EXTRA</div></div></body>`)
	layout, err := LayoutWithViewport(doc, image.Rect(0, 0, 200, 80))
	if err != nil {
		t.Fatal(err)
	}
	boxes := boxesByID(layout.Root, "extra")
	if boxes["extra"].Rect.Empty() {
		t.Errorf("unsupported extra child has empty geometry: %v", boxes["extra"].Rect)
	}
}

func TestGridSubsetDoesNotClaimBroadDisplaySupport(t *testing.T) {
	if supportsConditionMatches("(display: grid)") {
		t.Fatal("narrow Grid subset must not claim broad display:grid support")
	}
	if !supportsConditionMatches(`(grid-template-areas: "leading text trailing")`) {
		t.Fatal("named three-area template should be reported as supported")
	}
	if supportsConditionMatches(`(grid-template-areas: "leading text")`) {
		t.Fatal("two-column template is outside the narrow subset")
	}
}

// @supports must only report grid-template-areas shapes that layoutGrid
// actually places; anything that would fall back to normal flow is false.
func TestGridTemplateAreasSupportsMatchesLayout(t *testing.T) {
	cases := []struct {
		areas string
		want  bool
	}{
		{`"leading text trailing"`, true},
		{`'a b c'`, true},
		{`"leading . trailing"`, false},
		{`". text trailing"`, false},
		{`"a a b"`, false},
		{`"a b A"`, false},
		{`"leading text text"`, false},
		{`"a b c" "d e f"`, false},
		{`"a b c d"`, false},
		{`leading text trailing`, false},
	}
	for _, tc := range cases {
		cond := "(grid-template-areas: " + tc.areas + ")"
		if got := supportsConditionMatches(cond); got != tc.want {
			t.Errorf("supports %s = %v, want %v", cond, got, tc.want)
		}
		style := ComputedStyle{"display": "grid", "grid-template-areas": tc.areas,
			"grid-template-columns": "min-content minmax(0,auto) min-content"}
		if got := isGridContainer(&StyledNode{Style: style}); got != tc.want {
			t.Errorf("isGridContainer(%s) = %v, want %v (must agree with @supports)", tc.areas, got, tc.want)
		}
	}
}
