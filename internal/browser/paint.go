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
	sideStyle := style["border-style-"+side]
	if sideStyle == "" {
		sideStyle = style["border-"+side+"-style"]
	}
	hasVisibleStyle := false
	if sideStyle != "" {
		switch strings.ToLower(strings.TrimSpace(sideStyle)) {
		case "none", "hidden":
			return 0
		case "dotted", "dashed", "solid", "double", "groove", "ridge", "inset", "outset":
			hasVisibleStyle = true
		default:
			return 0
		}
	} else {
		for _, token := range parseValues(shorthand) {
			switch strings.ToLower(token.Text) {
			case "none", "hidden":
				return 0
			case "dotted", "dashed", "solid", "double", "groove", "ridge", "inset", "outset":
				hasVisibleStyle = true
			}
		}
	}
	if !hasVisibleStyle {
		// border-style is non-inherited and its initial value is none, so a
		// specified width without a visible style has no used border width.
		return 0
	}

	value := style["border-width-"+side]
	if value == "" {
		value = style["border-"+side+"-width"]
	}
	if value == "" {
		for _, token := range parseValues(shorthand) {
			if isBorderWidthKeyword(token.Text) ||
				token.Kind == "length" || token.Kind == "number" {
				value = token.Text
				break
			}
		}
	}
	if value == "" {
		// CSS 2.1's initial border-width is medium: 3px for visible styles,
		// while none/hidden have a used width of zero (checked above).
		for _, token := range parseValues(shorthand) {
			part := strings.ToLower(token.Text)
			if token.Kind == "keyword" && part != "currentcolor" &&
				!isBorderStyleKeyword(part) && !isBorderWidthKeyword(part) {
				// Do not turn an invalid shorthand such as
				// "solid wide" into a valid medium-width border.
				return 0
			}
		}
		return 3
	}
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "thin":
		return 1
	case "medium":
		return 3
	case "thick":
		return 5
	}
	n := px(value, 0, 0)
	if math.IsNaN(n) || math.IsInf(n, 0) || n < 0 {
		return 0
	}
	return int(math.Min(4096, math.Round(n)))
}

func isBorderStyleKeyword(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "none", "hidden", "dotted", "dashed", "solid", "double", "groove", "ridge", "inset", "outset":
		return true
	}
	return false
}

func isBorderWidthKeyword(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "thin", "medium", "thick":
		return true
	}
	return false
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
	drawBordersWithColors(dst, rect, style, widths, nil)
}

func drawBordersWithColors(dst *image.RGBA, rect image.Rectangle, style ComputedStyle, widths [4]int, colors *[4]color.RGBA) {
	if rect.Empty() {
		return
	}
	top := min(rect.Dy(), widths[0])
	right := min(rect.Dx(), widths[1])
	bottom := min(rect.Dy(), widths[2])
	left := min(rect.Dx(), widths[3])
	borderColors := [4]color.RGBA{
		borderColor(style, "top"), borderColor(style, "right"),
		borderColor(style, "bottom"), borderColor(style, "left"),
	}
	if colors != nil {
		borderColors = *colors
	}
	fill(dst, image.Rect(rect.Min.X, rect.Min.Y, rect.Max.X, rect.Min.Y+top), borderColors[0])
	fill(dst, image.Rect(rect.Min.X, rect.Max.Y-bottom, rect.Max.X, rect.Max.Y), borderColors[2])
	fill(dst, image.Rect(rect.Min.X, rect.Min.Y+top, rect.Min.X+left, rect.Max.Y-bottom), borderColors[3])
	fill(dst, image.Rect(rect.Max.X-right, rect.Min.Y+top, rect.Max.X, rect.Max.Y-bottom), borderColors[1])
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
	clip    image.Rectangle
}

// withClip constrains all drawing paths (including text, images and background
// layers) without changing their original layout coordinates or image scaling.
func (p *painter) withClip(rect image.Rectangle, paint func()) {
	canvas := p.canvas
	p.canvas = canvas.SubImage(rect.Intersect(canvas.Bounds())).(*image.RGBA)
	defer func() { p.canvas = canvas }()
	paint()
}

func (p *painter) overflowClip(box *Box) (image.Rectangle, bool) {
	if box == nil || box.Node == nil || box.Anonymous {
		return image.Rectangle{}, false
	}
	style := p.document.Styles[box.Node]
	value := strings.ToLower(strings.TrimSpace(style["overflow"]))
	if value == "" || value == "visible" {
		return image.Rectangle{}, false
	}
	rect := box.Rect
	widths := [4]int{borderWidth(style, "top"), borderWidth(style, "right"),
		borderWidth(style, "bottom"), borderWidth(style, "left")}
	if box.BorderWidths != nil {
		widths = *box.BorderWidths
	}
	return image.Rect(rect.Min.X+widths[3], rect.Min.Y+widths[0],
		rect.Max.X-widths[1], rect.Max.Y-widths[2]), true
}

// clip: rect() uses offsets from the border box of an absolutely positioned
// element. Comma and whitespace separated CSS 2.1 forms are both accepted.
func (p *painter) legacyClip(box *Box) (image.Rectangle, bool) {
	if box == nil || box.Node == nil || box.Anonymous {
		return image.Rectangle{}, false
	}
	style := p.document.Styles[box.Node]
	if !strings.EqualFold(strings.TrimSpace(style["position"]), "absolute") {
		return image.Rectangle{}, false
	}
	value := strings.TrimSpace(style["clip"])
	if len(value) < 6 || !strings.EqualFold(value[:5], "rect(") || value[len(value)-1] != ')' {
		return image.Rectangle{}, false
	}
	parts := strings.Fields(strings.ReplaceAll(value[5:len(value)-1], ",", " "))
	if len(parts) != 4 {
		return image.Rectangle{}, false
	}
	edges := [4]int{0, box.Rect.Dx(), box.Rect.Dy(), 0}
	for i, part := range parts {
		if strings.EqualFold(part, "auto") {
			continue
		}
		n := px(part, 0, math.NaN())
		if math.IsNaN(n) || math.IsInf(n, 0) {
			return image.Rectangle{}, false
		}
		edges[i] = int(math.Round(n))
	}
	return image.Rect(box.Rect.Min.X+edges[3], box.Rect.Min.Y+edges[0],
		box.Rect.Min.X+edges[1], box.Rect.Min.Y+edges[2]), true
}

func (p *painter) paintChildren(box *Box, paint func()) {
	if clip, ok := p.overflowClip(box); ok {
		p.withClip(clip, paint)
	} else {
		paint()
	}
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
// on non-positioned boxes is ignored. A masked element, or one with opacity
// below 1, also forms a stacking context and, when not positioned, paints in
// the z-index 0 layer (CSS Masking §6, CSS Color 4 §11.2 and CSS 2.1
// Appendix E), so that it can be composited as a unit.
func (p *painter) stacking(box *Box) (positioned bool, z int, context bool) {
	if box == nil || box.Node == nil || box.Node.Type != ElementNode {
		return false, 0, false
	}
	style := p.document.Styles[box.Node]
	// Masks and opacity below 1 both composite the element as a group, which
	// requires a stacking context.
	masked := !box.Anonymous && (hasMask(style) || elementOpacity(style) < 1)
	switch strings.ToLower(strings.TrimSpace(style["position"])) {
	case "relative", "absolute", "fixed", "sticky":
	default:
		if masked {
			return true, 0, true
		}
		return false, 0, false
	}
	value := strings.TrimSpace(style["z-index"])
	if value == "" || strings.EqualFold(value, "auto") {
		return true, 0, masked
	}
	z, err := strconv.Atoi(value)
	if err != nil {
		return true, 0, masked
	}
	return true, z, true
}

// paintStackingContext paints ctx and every box it owns. includeSelf is false
// for the anonymous viewport box, which has no background of its own.
func (p *painter) paintStackingContext(ctx *Box, includeSelf bool) {
	paint := func() { p.paintStackingContextContents(ctx, includeSelf) }
	if includeSelf && ctx.Node != nil && !ctx.Anonymous {
		style := p.document.Styles[ctx.Node]
		if hasMask(style) {
			contents := paint
			paint = func() { p.paintMasked(ctx, contents) }
		}
		if opacity := elementOpacity(style); opacity < 1 {
			// Opacity applies to the element's rendering after masking.
			grouped := paint
			paint = func() { p.paintWithOpacity(opacity, grouped) }
		}
	}
	if clip, ok := p.legacyClip(ctx); ok {
		p.withClip(clip, paint)
		return
	}
	paint()
}

func (p *painter) paintStackingContextContents(ctx *Box, includeSelf bool) {
	if includeSelf {
		p.paintBackground(ctx)
		p.paintChildren(ctx, func() { p.paintContent(ctx) })
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
		layer := layers[i]
		p.withClip(layer.clip, func() { p.paintStackingContext(layer.box, true) })
	}
	p.paintChildren(ctx, func() { p.paintFlow(ctx.Children) })
	for ; i < len(layers); i++ {
		layer := layers[i]
		p.withClip(layer.clip, func() {
			if layer.context {
				p.paintStackingContext(layer.box, true)
			} else {
				p.paintPositionedAuto(layer.box)
			}
		})
	}
}

func (p *painter) paintPositionedAuto(box *Box) {
	paint := func() {
		p.paintBackground(box)
		p.paintChildren(box, func() {
			p.paintContent(box)
			p.paintFlow(box.Children)
		})
	}
	if clip, ok := p.legacyClip(box); ok {
		p.withClip(clip, paint)
	} else {
		paint()
	}
}

// collectLayers gathers the positioned descendants owned by the stacking
// context containing box, without entering nested stacking contexts.
func (p *painter) collectLayers(box *Box, layers *[]stackingLayer) {
	clip := p.canvas.Bounds()
	if rect, ok := p.overflowClip(box); ok {
		clip = clip.Intersect(rect)
	}
	if rect, ok := p.legacyClip(box); ok {
		clip = clip.Intersect(rect)
	}
	for _, child := range box.Children {
		positioned, z, context := p.stacking(child)
		if positioned {
			order, ok := p.treeOrder[child.Node]
			if !ok {
				order = len(p.treeOrder) + len(*layers)
			}
			*layers = append(*layers, stackingLayer{box: child, z: z, context: context, order: order, clip: clip})
			if context {
				continue
			}
		}
		p.withClip(clip, func() { p.collectLayers(child, layers) })
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
		if p.isFloat(box) || box.AtomicInline {
			continue
		}
		p.paintBackground(box)
		p.paintChildren(box, func() { p.paintFlowBackgrounds(box.Children) })
	}
}

func (p *painter) paintFlowFloats(boxes []*Box) {
	for _, box := range boxes {
		if positioned, _, _ := p.stacking(box); positioned {
			continue
		}
		if p.isFloat(box) {
			p.paintBackground(box)
			p.paintChildren(box, func() {
				p.paintContent(box)
				p.paintFlow(box.Children)
			})
			continue
		}
		p.paintChildren(box, func() { p.paintFlowFloats(box.Children) })
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
		if box.AtomicInline {
			p.paintBackground(box)
			p.paintChildren(box, func() {
				p.paintContent(box)
				p.paintFlow(box.Children)
			})
			continue
		}
		p.paintChildren(box, func() {
			p.paintContent(box)
			p.paintFlowContent(box.Children)
		})
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
	if box == nil || box.Anonymous {
		return
	}
	style := p.document.Styles[box.Node]
	if box.Node != nil && box.Node.Type == ElementNode && style != nil {
		if visibilityHidden(style) {
			return
		}
		if box.BorderOnly {
			if box.BorderWidths != nil {
				r := usedRadii(style, box.Rect)
				if box.BorderColors != nil {
					drawBordersWithColors(p.canvas, box.Rect, style, *box.BorderWidths, box.BorderColors)
				} else if hasRadius(r) {
					paintRoundedBox(p.canvas, box.Rect, style, *box.BorderWidths, r, func(*image.RGBA) {})
				} else {
					drawBordersWithWidths(p.canvas, box.Rect, style, *box.BorderWidths)
				}
			}
			return
		}
		propagated := box.Node == p.canvasRoot || (p.bodyBackgroundPropagated && box.Node == p.canvasBody)
		widths := [4]int{borderWidth(style, "top"), borderWidth(style, "right"),
			borderWidth(style, "bottom"), borderWidth(style, "left")}
		if box.BorderWidths != nil {
			widths = *box.BorderWidths
		}
		r := usedRadii(style, box.Rect)
		canvas := p.canvas
		rounded := hasRadius(r)
		if rounded {
			p.canvas = image.NewRGBA(box.Rect.Intersect(canvas.Bounds()))
		}
		backgroundLayer := p.canvas
		func() {
			if rounded {
				defer func() { p.canvas = canvas }()
			}
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
		}()
		if rounded {
			// Background layers were drawn to the temporary box canvas; the
			// rounded compositor receives that canvas as its source.
			paintRoundedBox(canvas, box.Rect, style, widths, r, func(dst *image.RGBA) {
				draw.Draw(dst, dst.Bounds(), backgroundLayer, dst.Bounds().Min, draw.Src)
			})
		} else {
			drawBordersWithWidths(p.canvas, box.Rect, style, widths)
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
		if visibilityHidden(run.Style) {
			continue
		}
		drawText(p.canvas, run, p.document.Styles, p.faces)
	}
	for _, picture := range box.Images {
		if picture.Node != nil && visibilityHidden(p.document.Styles[picture.Node]) {
			continue
		}
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
	if style == nil || fragment.Rect.Empty() || visibilityHidden(style) {
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

// visibilityHidden reports whether a box's own background, borders, text and
// replaced content are invisible (CSS 2.1 §11.2). Layout is unaffected, and
// painting still visits descendants because visibility:visible can override
// the inherited value. collapse only differs from hidden for table rows and
// columns, which this renderer does not collapse yet.
func visibilityHidden(style ComputedStyle) bool {
	switch strings.ToLower(strings.TrimSpace(style["visibility"])) {
	case "hidden", "collapse":
		return true
	}
	return false
}

// paintOwn draws a box as a unit for positioned stacking layers and floats.
func (p *painter) paintOwn(box *Box) {
	p.paintBackground(box)
	p.paintContent(box)
}
