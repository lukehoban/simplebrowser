package browser

import (
	"image"
	"image/color"
	"testing"
)

func TestNoBreakPositionedIntrinsicWidths(t *testing.T) {
	faces := newFaceSet()
	defer faces.close()
	style := ComputedStyle{"font-size": "20px", "font-family": "monospace"}
	m := faces.metrics(style)
	for _, tc := range []struct {
		name, text, longest, normalized string
	}{
		{"NBSP", "a\u00a0b", "a\u00a0b", "a\u00a0b"},
		{"collapsible space", "a b", "a", "a b"},
		{"mixed whitespace", "a\u00a0b \t c", "a\u00a0b", "a\u00a0b c"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			n := &StyledNode{Node: &Node{Type: TextNode, Data: tc.text}, Style: style}
			minimum, maximum := positionedIntrinsicWidths(n, faces, 48)
			if want := m.width(tc.longest); minimum != want {
				t.Errorf("min-content = %d, want %d", minimum, want)
			}
			if want := m.width(tc.normalized); maximum != want {
				t.Errorf("max-content = %d, want %d", maximum, want)
			}
		})
	}
}

func TestNoBreakPositionedShrinkToFitGeometryAndPixels(t *testing.T) {
	// A 48px containing block fits the NBSP word, but not the following
	// collapsible-space-separated word. This exercises the auto-width path
	// and the painted position of its right edge.
	markup := `<body style="margin:0;font:20px monospace"><div style="position:relative;width:48px;height:90px;background:#eee">` +
		`<div id="abs" style="position:absolute;left:0;top:0;background:#f99">a&nbsp;b c</div></div></body>`
	boxes := percentHeightLayout(t, markup, "abs")
	if got, want := boxes["abs"].Content.Dx(), 48; got != want {
		t.Fatalf("shrink-to-fit content width = %d, want %d", got, want)
	}
	runs := make([]TextRun, 0)
	var walk func(*Box)
	walk = func(b *Box) {
		runs = append(runs, b.Text...)
		for _, child := range b.Children {
			walk(child)
		}
	}
	walk(boxes["abs"])
	var nbspY, cY int
	for _, run := range runs {
		switch run.Text {
		case "a\u00a0b":
			nbspY = run.Rect.Min.Y
		case "c":
			cY = run.Rect.Min.Y
		}
	}
	if cY <= nbspY {
		t.Errorf("NBSP word should stay together before wrapped c: runs = %+v", runs)
	}
	img := painted(t, markup, image.Rect(0, 0, 100, 100))
	pixel(t, img, 47, 5, color.RGBA{255, 153, 153, 255})
	pixel(t, img, 48, 5, color.RGBA{255, 255, 255, 255})
	pixel(t, img, 2, 50, color.RGBA{238, 238, 238, 255})
}
