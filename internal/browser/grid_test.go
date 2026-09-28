package browser

import (
	"image"
	"testing"
)

func TestGridThreeNamedAreasLayOutInTrackOrder(t *testing.T) {
	doc := styledForLayout(t, `<body style="margin:0"><div id="grid" style="display:grid;width:120px;grid-template-columns:min-content minmax(0,auto) min-content;grid-template-areas:'leading text trailing';column-gap:8px;align-items:center"><span id="text" style="grid-area:text;white-space:nowrap">main</span><span id="trailing" style="grid-area:trailing">›</span><span id="leading" style="grid-area:leading">◆</span></div></body>`)
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
