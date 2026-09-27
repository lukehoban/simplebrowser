package browser

import (
	"image"
	"image/color"
	"testing"
)

// An auto-width inline-block shrinks to fit the space left on its line
// (CSS 2.1 §10.3.5) instead of measuring against the whole line and wrapping
// below text that already occupies part of it (#219).
func TestAutoInlineBlockShrinksToRemainingLineWidth(t *testing.T) {
	const markup = `<body style="margin:0"><div style="width:200px;line-height:20px">leading text ` +
		`<span style="display:inline-block;border:1px solid blue;background:grey">a fairly long phrase</span>` +
		`</div></body>`
	viewport := image.Rect(0, 0, 240, 120)
	layout, err := LayoutWithViewport(styledForLayout(t, markup), viewport)
	if err != nil {
		t.Fatal(err)
	}
	spans := collectBoxes(layout.Root, "span")
	if len(spans) != 1 {
		t.Fatalf("span boxes = %d, want 1", len(spans))
	}
	box := spans[0]
	var leading TextRun
	for _, div := range collectBoxes(layout.Root, "div") {
		for _, child := range div.Children {
			for _, run := range child.Text {
				if run.Text == "leading text " || run.Text == "leading text" {
					leading = run
				}
			}
		}
	}
	if leading.Text == "" {
		t.Fatal("leading text run not found")
	}
	if box.Rect.Min.X < leading.Rect.Max.X-1 || box.Rect.Max.X > 200 {
		t.Fatalf("inline-block %v should sit after %v within the 200px line", box.Rect, leading.Rect)
	}
	if box.Rect.Min.Y > leading.Rect.Min.Y {
		t.Fatalf("inline-block %v wrapped below leading text %v", box.Rect, leading.Rect)
	}
	var inner []TextRun
	for _, child := range box.Children {
		inner = append(inner, child.Text...)
	}
	if len(inner) < 2 {
		t.Fatalf("inline-block content %v should wrap inside the narrowed box", inner)
	}
	// The leading text shares the inline-block's last line baseline.
	if last := inner[len(inner)-1]; last.Rect.Min.Y != leading.Rect.Min.Y {
		t.Errorf("baseline mismatch: last inner %v vs leading %v", last.Rect, leading.Rect)
	}
	img := painted(t, markup, viewport)
	blue := color.RGBA{0, 0, 255, 255}
	pixel(t, img, box.Rect.Min.X, box.Rect.Min.Y, blue)
	pixel(t, img, box.Rect.Max.X-1, box.Rect.Max.Y-1, blue)
	pixel(t, img, box.Content.Max.X-1, box.Content.Max.Y-1, grey)
}

// When even the minimum content width does not fit the remaining space, the
// inline-block still wraps and keeps its full-line shrink-to-fit width.
func TestAutoInlineBlockWrapsWhenMinimumDoesNotFit(t *testing.T) {
	const markup = `<body style="margin:0"><div style="width:200px;line-height:20px">leading text ` +
		`<span style="display:inline-block;border:1px solid blue">unbreakabletokenthatislong</span>` +
		`</div></body>`
	rects := spanRects(t, markup)
	if len(rects) != 1 {
		t.Fatalf("span boxes = %v, want 1", rects)
	}
	if rects[0].Min.X != 0 || rects[0].Min.Y < 20 {
		t.Fatalf("inline-block %v should wrap to the start of the second line", rects[0])
	}
}

// Percentage margins keep resolving against the containing block even when
// the box is narrowed to the remaining line width.
func TestAutoInlineBlockRemainingWidthKeepsPercentMargins(t *testing.T) {
	const markup = `<body style="margin:0"><div style="width:200px;line-height:20px">lead ` +
		`<span style="display:inline-block;margin-left:10%;border:1px solid blue">one two three four five six</span>` +
		`</div></body>`
	rects := spanRects(t, markup)
	if len(rects) != 1 {
		t.Fatalf("span boxes = %v, want 1", rects)
	}
	if rects[0].Min.Y != 0 || rects[0].Max.X > 200 {
		t.Fatalf("inline-block %v should stay on the first line within 200px", rects[0])
	}
}
