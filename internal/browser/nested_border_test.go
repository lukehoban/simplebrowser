package browser

import (
	"image"
	"testing"
)

func TestAbsoluteLengthUnits(t *testing.T) {
	for _, tc := range []struct {
		value string
		want  float64
	}{
		{"1in", 96}, {"2.54cm", 96}, {"25.4mm", 96}, {"6pc", 96},
		{"72pt", 96}, {"101.6q", 96}, {"1IN", 96}, {"0.5in", 48},
	} {
		if got := px(tc.value, 0, -1); got < tc.want-1e-9 || got > tc.want+1e-9 {
			t.Errorf("px(%q) = %v, want %v", tc.value, got, tc.want)
		}
		if kind := classifyValue(tc.value).Kind; kind != "length" {
			t.Errorf("classifyValue(%q).Kind = %q, want length", tc.value, kind)
		}
	}
}

// WPT block-formatting-contexts-005: a child with no margin or padding sits
// flush against its parent's left border, and both honour height: 1in.
func TestNestedBlockBordersFlush(t *testing.T) {
	rects := divRects(t, `<style>
		body { margin: 8px }
		div { height: 1in }
		#outer { border-left: solid 5px blue }
		div div { border-left: solid 5px orange }
	</style><body><div id="outer"><div></div></div></body>`)
	want := []image.Rectangle{
		image.Rect(8, 8, 392, 104),
		image.Rect(13, 8, 392, 104),
	}
	if len(rects) != len(want) {
		t.Fatalf("got %d divs %v, want %d", len(rects), rects, len(want))
	}
	for i := range want {
		if rects[i] != want[i] {
			t.Errorf("div %d rect = %v, want %v", i, rects[i], want[i])
		}
	}
}

func TestBorderWidthAbsoluteUnit(t *testing.T) {
	style := ComputedStyle{"border-left": "solid 0.0625in blue"}
	if got := borderWidth(style, "left"); got != 6 {
		t.Fatalf("borderWidth = %d, want 6", got)
	}
}
