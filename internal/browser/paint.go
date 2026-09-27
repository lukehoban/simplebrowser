package browser

import (
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"io"
	"math"
	"strings"

	xdraw "golang.org/x/image/draw"
	"golang.org/x/image/font"
	"golang.org/x/image/math/fixed"
)

// paint draws in tree order, so a descendant's background covers its parent's
// background but never its own text. Each call owns its canvas and font faces.
func paint(layout Layout, output io.Writer, options renderOptions) error {
	viewport := layout.Viewport
	if viewport.Empty() {
		viewport = image.Rect(0, 0, placeholderWidth, placeholderHeight)
	}
	canvas := image.NewRGBA(viewport)
	fill(canvas, viewport, color.RGBA{255, 255, 255, 255})
	canvasRoot, canvasBody := canvasBackground(layout.Document)
	bodyBackgroundPropagated := false
	if c, ok := backgroundColor(layout.Document.Styles[canvasRoot]); ok {
		fill(canvas, viewport, c)
	} else if c, ok := backgroundColor(layout.Document.Styles[canvasBody]); ok {
		fill(canvas, viewport, c)
		bodyBackgroundPropagated = true
	}
	faces := newFaceSet()
	defer faces.close()
	var visit func(*Box)
	visit = func(box *Box) {
		if box == nil {
			return
		}
		style := layout.Document.Styles[box.Node]
		if box.Node != nil && box.Node.Type == ElementNode && style != nil {
			propagated := box.Node == canvasRoot || (bodyBackgroundPropagated && box.Node == canvasBody)
			if !propagated {
				if c, ok := backgroundColor(style); ok {
					fill(canvas, box.Rect, c)
				}
			}
			drawBackgroundImage(canvas, box, layout.Document.BackgroundImages[box.Node], style)
			if box.BorderWidths != nil {
				drawBordersWithWidths(canvas, box.Rect, style, *box.BorderWidths)
			} else {
				drawBorders(canvas, box.Rect, style)
			}
		}
		for _, run := range box.Text {
			drawText(canvas, run, layout.Document.Styles, faces)
		}
		for _, picture := range box.Images {
			if options.debugImageBoxes {
				drawImageBoxOutline(canvas, picture)
			} else {
				drawImageBox(canvas, picture)
			}
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

// canvasBackground finds the explicit html root and its direct body child.
// The DOM is intentionally not repaired with implicit html/body elements, so
// fragments and incomplete documents simply retain the white canvas default.
func canvasBackground(document StyledDocument) (*Node, *Node) {
	if document.Document.Root == nil {
		return nil, nil
	}
	var root, body *Node
	for _, child := range document.Document.Root.Children {
		if child.Type == ElementNode && strings.EqualFold(child.Name, "html") {
			root = child
			break
		}
	}
	if root == nil {
		return nil, nil
	}
	for _, child := range root.Children {
		if child.Type == ElementNode && strings.EqualFold(child.Name, "body") {
			body = child
			break
		}
	}
	return root, body
}

func backgroundColor(style ComputedStyle) (color.RGBA, bool) {
	if style == nil {
		return color.RGBA{}, false
	}
	value := style["background-color"]
	if value == "" {
		value = style["background"]
	}
	c, ok := parseColor(strings.ToLower(strings.TrimSpace(value)))
	return c, ok && c.A != 0
}

// Scale against the full layout rectangle, not the clipped rectangle: cropping
// an image at the viewport edge must not stretch its visible portion.
func drawImageBox(dst *image.RGBA, picture ImageBox) {
	rect := picture.Rect
	clip := rect.Intersect(dst.Bounds())
	if rect.Empty() || clip.Empty() {
		return
	}
	if svg, ok := picture.Image.(*svgImage); ok {
		// Vector images render at the used size rather than being resampled.
		if raster := svg.rasterize(rect.Dx(), rect.Dy()); raster != nil {
			draw.Draw(dst, clip, raster, clip.Min.Sub(rect.Min), draw.Over)
			return
		}
	}
	if picture.Image != nil && !picture.Image.Bounds().Empty() {
		xdraw.ApproxBiLinear.Scale(dst.SubImage(clip).(draw.Image), rect,
			picture.Image, picture.Image.Bounds(), draw.Over, nil)
		return
	}
	fill(dst, rect, color.RGBA{R: 245, G: 245, B: 245, A: 255})
	gray := color.RGBA{R: 160, G: 160, B: 160, A: 255}
	fill(dst, image.Rect(rect.Min.X, rect.Min.Y, rect.Max.X, rect.Min.Y+1), gray)
	fill(dst, image.Rect(rect.Min.X, rect.Max.Y-1, rect.Max.X, rect.Max.Y), gray)
	fill(dst, image.Rect(rect.Min.X, rect.Min.Y, rect.Min.X+1, rect.Max.Y), gray)
	fill(dst, image.Rect(rect.Max.X-1, rect.Min.Y, rect.Max.X, rect.Max.Y), gray)
}

// drawImageBoxOutline marks a laid-out replaced box: magenta when the image
// decoded, gray when it is a placeholder (missing, broken, or unsupported).
func drawImageBoxOutline(dst *image.RGBA, picture ImageBox) {
	outline := color.RGBA{R: 160, G: 160, B: 160, A: 255}
	if picture.Image != nil {
		outline = color.RGBA{R: 230, G: 0, B: 200, A: 255}
	}
	rect := picture.Rect.Canon()
	if rect.Dx() == 0 {
		rect.Max.X = rect.Min.X + 1
	}
	if rect.Dy() == 0 {
		rect.Max.Y = rect.Min.Y + 1
	}
	fill(dst, image.Rect(rect.Min.X, rect.Min.Y, rect.Max.X, rect.Min.Y+1), outline)
	fill(dst, image.Rect(rect.Min.X, rect.Max.Y-1, rect.Max.X, rect.Max.Y), outline)
	fill(dst, image.Rect(rect.Min.X, rect.Min.Y, rect.Min.X+1, rect.Max.Y), outline)
	fill(dst, image.Rect(rect.Max.X-1, rect.Min.Y, rect.Max.X, rect.Max.Y), outline)
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
	drawBordersWithWidths(dst, rect, style, [4]int{
		borderWidth(style, "top"),
		borderWidth(style, "right"),
		borderWidth(style, "bottom"),
		borderWidth(style, "left"),
	})
}

func drawBordersWithWidths(dst *image.RGBA, rect image.Rectangle, style ComputedStyle, widths [4]int) {
	if rect.Empty() {
		return
	}
	top := min(rect.Dy(), widths[0])
	right := min(rect.Dx(), widths[1])
	bottom := min(rect.Dy(), widths[2])
	left := min(rect.Dx(), widths[3])
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
