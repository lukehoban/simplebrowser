package browser

import (
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"io"
	"math"
	"strings"

	"golang.org/x/image/font"
	"golang.org/x/image/math/fixed"
)

// paint draws in tree order, so a descendant's background covers its parent's
// background but never its own text. Each call owns its canvas and font faces.
func paint(layout Layout, output io.Writer) error {
	viewport := layout.Viewport
	if viewport.Empty() {
		viewport = image.Rect(0, 0, placeholderWidth, placeholderHeight)
	}
	canvas := image.NewRGBA(viewport)
	fill(canvas, viewport, color.RGBA{255, 255, 255, 255})
	faces := newFaceSet()
	defer faces.close()
	var visit func(*Box)
	visit = func(box *Box) {
		if box == nil {
			return
		}
		style := layout.Document.Styles[box.Node]
		if box.Node != nil && box.Node.Type == ElementNode && style != nil {
			bg := style["background-color"]
			if bg == "" {
				bg = style["background"]
			}
			if c, ok := parseColor(strings.ToLower(strings.TrimSpace(bg))); ok {
				fill(canvas, box.Rect, c)
			}
			drawBorders(canvas, box.Rect, style)
		}
		for _, run := range box.Text {
			drawText(canvas, run, layout.Document.Styles, faces)
		}
		for _, child := range box.Children {
			visit(child)
		}
	}
	if layout.Root != nil {
		// The root is an anonymous viewport box, not an extra CSS element.
		for _, child := range layout.Root.Children {
			visit(child)
		}
	}
	return png.Encode(output, canvas)
}

func fill(dst *image.RGBA, rect image.Rectangle, c color.RGBA) {
	rect = rect.Intersect(dst.Bounds())
	if rect.Empty() || c.A == 0 {
		return
	}
	draw.Draw(dst, rect, &image.Uniform{C: c}, image.Point{}, draw.Over)
}

func borderWidth(style ComputedStyle, side string) int {
	shorthand := style["border-"+side]
	value := style["border-width-"+side]
	if value == "" {
		value = style["border-"+side+"-width"]
	}
	if value == "" && (strings.EqualFold(style["border-"+side+"-style"], "none") ||
		strings.EqualFold(style["border-"+side+"-style"], "hidden")) {
		return 0
	}
	for _, part := range strings.Fields(strings.ToLower(shorthand)) {
		if value == "" && (part == "none" || part == "hidden") {
			return 0
		}
	}
	if value == "" {
		for _, part := range strings.Fields(shorthand) {
			if classifyValue(part).Kind == "length" || classifyValue(part).Kind == "number" {
				value = part
				break
			}
		}
	}
	if value == "" {
		return 0
	}
	if value == "thin" {
		return 1
	}
	if value == "medium" {
		return 3
	}
	if value == "thick" {
		return 5
	}
	n := px(value, 0, 0)
	if math.IsNaN(n) || math.IsInf(n, 0) || n < 0 {
		return 0
	}
	return int(math.Min(4096, math.Round(n)))
}

func borderColor(style ComputedStyle, side string) color.RGBA {
	value := style["border-color-"+side]
	if value == "" {
		value = style["border-"+side+"-color"]
	}
	if value == "" {
		for _, part := range strings.Fields(style["border-"+side]) {
			if _, ok := parseColor(strings.ToLower(part)); ok {
				value = part
			}
		}
	}
	if value == "" || strings.EqualFold(value, "currentcolor") {
		value = style["color"]
	}
	if c, ok := parseColor(strings.ToLower(value)); ok {
		return c
	}
	return color.RGBA{A: 255}
}

func drawBorders(dst *image.RGBA, rect image.Rectangle, style ComputedStyle) {
	if rect.Empty() {
		return
	}
	top := min(rect.Dy(), borderWidth(style, "top"))
	right := min(rect.Dx(), borderWidth(style, "right"))
	bottom := min(rect.Dy(), borderWidth(style, "bottom"))
	left := min(rect.Dx(), borderWidth(style, "left"))
	fill(dst, image.Rect(rect.Min.X, rect.Min.Y, rect.Max.X, rect.Min.Y+top), borderColor(style, "top"))
	fill(dst, image.Rect(rect.Min.X, rect.Max.Y-bottom, rect.Max.X, rect.Max.Y), borderColor(style, "bottom"))
	fill(dst, image.Rect(rect.Min.X, rect.Min.Y+top, rect.Min.X+left, rect.Max.Y-bottom), borderColor(style, "left"))
	fill(dst, image.Rect(rect.Max.X-right, rect.Min.Y+top, rect.Max.X, rect.Max.Y-bottom), borderColor(style, "right"))
}

func decorated(run TextRun, styles map[*Node]ComputedStyle, keyword string) bool {
	for n := run.Node; n != nil; n = n.Parent {
		if strings.Contains(styles[n]["text-decoration"], keyword) ||
			strings.Contains(styles[n]["text-decoration-line"], keyword) {
			return true
		}
	}
	return strings.Contains(run.Style["text-decoration"], keyword) ||
		strings.Contains(run.Style["text-decoration-line"], keyword)
}

func drawText(dst *image.RGBA, run TextRun, styles map[*Node]ComputedStyle, faces *faceSet) {
	if run.Text == "" || run.Rect.Empty() {
		return
	}
	m := faces.metrics(run.Style)
	if m.face == nil {
		return
	}
	ink, ok := parseColor(strings.ToLower(strings.TrimSpace(run.Style["color"])))
	if !ok {
		ink = color.RGBA{A: 255}
	}
	clip := run.Rect.Intersect(dst.Bounds())
	if clip.Empty() {
		return
	}
	baseline := run.Rect.Min.Y + (run.Rect.Dy()-m.lineHeight())/2 + m.face.Metrics().Ascent.Ceil()
	// SubImage constrains glyph masks to the run and the viewport; long
	// unbreakable words cannot paint across neighboring boxes.
	drawer := font.Drawer{Dst: dst.SubImage(clip).(draw.Image), Src: image.NewUniform(ink),
		Face: m.face, Dot: fixed.P(run.Rect.Min.X, baseline)}
	drawer.DrawString(run.Text)
	if decorated(run, styles, "underline") {
		fill(dst, image.Rect(run.Rect.Min.X, baseline+1, run.Rect.Max.X, baseline+2).Intersect(clip), ink)
	}
	if decorated(run, styles, "line-through") {
		y := baseline - m.face.Metrics().Ascent.Ceil()/3
		fill(dst, image.Rect(run.Rect.Min.X, y, run.Rect.Max.X, y+1).Intersect(clip), ink)
	}
}
