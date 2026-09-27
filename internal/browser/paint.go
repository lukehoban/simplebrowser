package browser

import (
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"io"
	"math"
	"sort"
	"strconv"
	"strings"

	xdraw "golang.org/x/image/draw"
	"golang.org/x/image/font"
	"golang.org/x/image/math/fixed"
)

// paint draws each stacking context in the simplified CSS 2.1 Appendix E
// order: the context's own background, negative z-index layers, in-flow
// content in tree order (a descendant's background covers its parent's but
// never its own text), then z-index auto/0 and positive layers. Each call owns
// its canvas and font faces.
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
	p := &painter{
		canvas:                   canvas,
		document:                 layout.Document,
		faces:                    faces,
		options:                  options,
		canvasRoot:               canvasRoot,
		canvasBody:               canvasBody,
		bodyBackgroundPropagated: bodyBackgroundPropagated,
		treeOrder:                documentOrder(layout.Document.Document.Root),
	}
	if layout.Root != nil {
		// The root is an anonymous viewport box, not an extra CSS element; it
		// establishes the root stacking context.
		p.paintStackingContext(layout.Root, false)
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
	ascent, _ := m.lineMetrics()
	baseline := run.Rect.Min.Y + ascent
	// SubImage constrains glyph masks to the run and the viewport; long
	// unbreakable words cannot paint across neighboring boxes.
	drawer := font.Drawer{Dst: dst.SubImage(clip).(draw.Image), Src: image.NewUniform(ink),
		Face: m.face, Dot: fixed.Point26_6{X: run.PenX, Y: fixed.I(baseline)}}
	m.eachTextSegment(run.Text, func(face font.Face, text string) {
		drawer.Face = face
		drawer.DrawString(text)
	})
	if decorated(run, styles, "underline") {
		fill(dst, image.Rect(run.Rect.Min.X, baseline+1, run.Rect.Max.X, baseline+2).Intersect(clip), ink)
	}
	if decorated(run, styles, "line-through") {
		y := baseline - m.face.Metrics().Ascent.Ceil()/3
		fill(dst, image.Rect(run.Rect.Min.X, y, run.Rect.Max.X, y+1).Intersect(clip), ink)
	}
}

type painter struct {
	canvas                   *image.RGBA
	document                 StyledDocument
	faces                    *faceSet
	options                  renderOptions
	canvasRoot, canvasBody   *Node
	bodyBackgroundPropagated bool
	treeOrder                map[*Node]int
}

// stackingLayer is a positioned box painted as a unit by the stacking context
// that owns it. context is false for z-index:auto boxes, whose positioned
// descendants belong to the enclosing context instead (CSS 2.1 §9.9.1).
type stackingLayer struct {
	box     *Box
	z       int
	context bool
	order   int
}

func documentOrder(root *Node) map[*Node]int {
	order := make(map[*Node]int)
	var walk func(*Node)
	walk = func(n *Node) {
		if n == nil {
			return
		}
		order[n] = len(order)
		for _, child := range n.Children {
			walk(child)
		}
	}
	walk(root)
	return order
}

// stacking reports whether box is positioned and, if so, its z-index. Only
// positioned boxes with an integer z-index form a stacking context; z-index
// on non-positioned boxes is ignored.
func (p *painter) stacking(box *Box) (positioned bool, z int, context bool) {
	if box == nil || box.Node == nil || box.Node.Type != ElementNode {
		return false, 0, false
	}
	style := p.document.Styles[box.Node]
	switch strings.ToLower(strings.TrimSpace(style["position"])) {
	case "relative", "absolute", "fixed", "sticky":
	default:
		return false, 0, false
	}
	value := strings.TrimSpace(style["z-index"])
	if value == "" || strings.EqualFold(value, "auto") {
		return true, 0, false
	}
	z, err := strconv.Atoi(value)
	if err != nil {
		return true, 0, false
	}
	return true, z, true
}

// paintStackingContext paints ctx and every box it owns. includeSelf is false
// for the anonymous viewport box, which has no background of its own.
func (p *painter) paintStackingContext(ctx *Box, includeSelf bool) {
	if includeSelf {
		p.paintOwn(ctx)
	}
	var layers []stackingLayer
	p.collectLayers(ctx, &layers)
	sort.SliceStable(layers, func(i, j int) bool {
		if layers[i].z != layers[j].z {
			return layers[i].z < layers[j].z
		}
		return layers[i].order < layers[j].order
	})
	i := 0
	for ; i < len(layers) && layers[i].z < 0; i++ {
		p.paintStackingContext(layers[i].box, true)
	}
	p.paintFlow(ctx.Children)
	for ; i < len(layers); i++ {
		if layers[i].context {
			p.paintStackingContext(layers[i].box, true)
		} else {
			p.paintOwn(layers[i].box)
			p.paintFlow(layers[i].box.Children)
		}
	}
}

// collectLayers gathers the positioned descendants owned by the stacking
// context containing box, without entering nested stacking contexts.
func (p *painter) collectLayers(box *Box, layers *[]stackingLayer) {
	for _, child := range box.Children {
		positioned, z, context := p.stacking(child)
		if positioned {
			order, ok := p.treeOrder[child.Node]
			if !ok {
				order = len(p.treeOrder) + len(*layers)
			}
			*layers = append(*layers, stackingLayer{box: child, z: z, context: context, order: order})
			if context {
				continue
			}
		}
		p.collectLayers(child, layers)
	}
}

// paintFlow paints non-positioned boxes in the CSS block-background, float,
// and inline-content phases. Keeping these phases separate matters when
// overflowing inline content overlaps a later block background.
func (p *painter) paintFlow(boxes []*Box) {
	p.paintFlowBackgrounds(boxes)
	p.paintFlowFloats(boxes)
	p.paintFlowContent(boxes)
}

func (p *painter) paintFlowBackgrounds(boxes []*Box) {
	for _, box := range boxes {
		if positioned, _, _ := p.stacking(box); positioned {
			continue
		}
		if p.isFloat(box) {
			continue
		}
		p.paintBackground(box)
		p.paintFlowBackgrounds(box.Children)
	}
}

func (p *painter) paintFlowFloats(boxes []*Box) {
	for _, box := range boxes {
		if positioned, _, _ := p.stacking(box); positioned {
			continue
		}
		if p.isFloat(box) {
			p.paintOwn(box)
			p.paintFlow(box.Children)
			continue
		}
		p.paintFlowFloats(box.Children)
	}
}

func (p *painter) paintFlowContent(boxes []*Box) {
	for _, box := range boxes {
		if positioned, _, _ := p.stacking(box); positioned {
			continue
		}
		if p.isFloat(box) {
			continue
		}
		p.paintContent(box)
		p.paintFlowContent(box.Children)
	}
}

func (p *painter) isFloat(box *Box) bool {
	if box == nil || box.Node == nil {
		return false
	}
	value := strings.ToLower(strings.TrimSpace(p.document.Styles[box.Node]["float"]))
	return value == "left" || value == "right"
}

// paintBackground draws a box's background and borders, but not its content
// or child boxes.
func (p *painter) paintBackground(box *Box) {
	if box == nil {
		return
	}
	style := p.document.Styles[box.Node]
	if box.Node != nil && box.Node.Type == ElementNode && style != nil {
		if box.BorderOnly {
			if box.BorderWidths != nil {
				drawBordersWithWidths(p.canvas, box.Rect, style, *box.BorderWidths)
			}
			return
		}
		propagated := box.Node == p.canvasRoot || (p.bodyBackgroundPropagated && box.Node == p.canvasBody)
		if !propagated {
			if c, ok := backgroundColor(style); ok {
				fill(p.canvas, box.Rect, c)
			}
		}
		layers := p.document.BackgroundImages[box.Node]
		for i := len(layers) - 1; i >= 0; i-- {
			src := layers[i]
			if src == nil {
				values := backgroundLayers(style["background-image"])
				if i < len(values) {
					area := box.Rect
					area.Min.X += borderWidth(style, "left")
					area.Min.Y += borderWidth(style, "top")
					area.Max.X -= borderWidth(style, "right")
					area.Max.Y -= borderWidth(style, "bottom")
					if gradient := parseGradient(values[i], area.Dx(), area.Dy()); gradient != nil {
						src = gradient
					}
				}
			}
			drawBackgroundImage(p.canvas, box, src, backgroundLayerStyle(style, i))
		}
		if box.BorderWidths != nil {
			drawBordersWithWidths(p.canvas, box.Rect, style, *box.BorderWidths)
		} else {
			drawBorders(p.canvas, box.Rect, style)
		}
	}
}

// paintContent draws a box's text and replaced content, but not its child
// boxes.
func (p *painter) paintContent(box *Box) {
	if box == nil {
		return
	}
	for _, fragment := range box.InlineBackgrounds {
		p.paintInlineBackground(fragment)
	}
	for _, run := range box.Text {
		drawText(p.canvas, run, p.document.Styles, p.faces)
	}
	for _, picture := range box.Images {
		if p.options.debugImageBoxes {
			drawImageBoxOutline(p.canvas, picture)
		} else {
			drawImageBox(p.canvas, picture)
		}
	}
}

// paintInlineBackground paints the background layers for one line fragment.
// Inline borders and padding are not represented by the current inline layout
// model; unlike paintBackground, this deliberately does not invent borders
// around the text rectangle.
func (p *painter) paintInlineBackground(fragment InlineBackground) {
	style := p.document.Styles[fragment.Node]
	if style == nil || fragment.Rect.Empty() {
		return
	}
	if c, ok := backgroundColor(style); ok {
		fill(p.canvas, fragment.Rect, c)
	}
	layers := p.document.BackgroundImages[fragment.Node]
	for i := len(layers) - 1; i >= 0; i-- {
		src := layers[i]
		if src == nil {
			values := backgroundLayers(style["background-image"])
			if i < len(values) {
				if gradient := parseGradient(values[i], fragment.Rect.Dx(), fragment.Rect.Dy()); gradient != nil {
					src = gradient
				}
			}
		}
		drawBackgroundImage(p.canvas, &Box{Node: fragment.Node, Rect: fragment.Rect},
			src, backgroundLayerStyle(style, i))
	}
}

// paintOwn draws a box as a unit for positioned stacking layers and floats.
func (p *painter) paintOwn(box *Box) {
	p.paintBackground(box)
	p.paintContent(box)
}
