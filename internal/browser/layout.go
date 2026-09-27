package browser

import (
	"image"
	"math"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/image/font"
	"golang.org/x/image/font/gofont/gobold"
	"golang.org/x/image/font/gofont/goregular"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/math/fixed"
)

// Box is a laid out element. Coordinates are in viewport pixels and include
// the border box. Text is kept as runs so the painter does not need to walk
// the DOM again.
type Box struct {
	Node     *Node
	Rect     image.Rectangle
	Content  image.Rectangle
	Children []*Box
	Text     []TextRun
	Images   []ImageBox
	// BorderWidths overrides the widths from the node's computed style when
	// table border collapsing allocates a shared edge to another box.
	BorderWidths *[4]int // top, right, bottom, left
}

// ImageBox exposes a decoded replaced image and its used rectangle to the
// painting stage without requiring the painter to resolve resources again.
// Image is nil when the resource failed to load or is an unsupported format
// such as SVG; the rectangle is still reserved so painting can draw a
// placeholder.
type ImageBox struct {
	Image image.Image
	Rect  image.Rectangle
}

type TextRun struct {
	Node  *Node
	Style ComputedStyle
	Text  string
	Rect  image.Rectangle
}

type metrics struct {
	face font.Face
	size float64
}

func (m metrics) width(s string) int {
	return int((m.advance(s) + 63) / 64)
}

func (m metrics) advance(s string) fixed.Int26_6 {
	if m.face == nil {
		return fixed.I(utf8.RuneCountInString(s) * int(m.size*0.55))
	}
	return font.MeasureString(m.face, s)
}

func (m metrics) lineHeight() int { return int(math.Ceil(m.size * 1.2)) }

// lineMetrics describes the portion of a text line box above and below its
// baseline.  Keeping these separately lets text with different font sizes and
// replaced elements share a baseline instead of all being pinned to the
// line's top edge.
func (m metrics) lineMetrics() (ascent, descent int) {
	height := m.lineHeight()
	if m.face != nil {
		fm := m.face.Metrics()
		ascent = int(math.Ceil(float64(fm.Ascent) / 64))
		descent = int(math.Ceil(float64(fm.Descent) / 64))
	} else {
		ascent = int(math.Ceil(m.size * .8))
		descent = int(math.Ceil(m.size * .2))
	}
	// Font metrics describe glyphs, while CSS's normal line-height has a
	// little leading. Split that leading around the baseline.
	if leading := height - ascent - descent; leading > 0 {
		ascent += (leading + 1) / 2
		descent += leading / 2
	}
	return ascent, descent
}

// Each layout owns its font faces. opentype faces cache glyph data internally
// and must not be shared across concurrent renders.
type faceSet struct {
	regular *opentype.Font
	bold    *opentype.Font
	faces   map[faceKey]font.Face
	// images holds the render-scoped decoded resources keyed by DOM node, so
	// layout never fetches during measurement.
	images map[*Node]image.Image
}

type faceKey struct {
	size float64
	bold bool
}

func newFaceSet() *faceSet {
	regular, _ := opentype.Parse(goregular.TTF)
	bold, _ := opentype.Parse(gobold.TTF)
	return &faceSet{regular: regular, bold: bold, faces: make(map[faceKey]font.Face)}
}

func (f *faceSet) close() {
	for _, face := range f.faces {
		if closer, ok := face.(interface{ Close() error }); ok {
			_ = closer.Close()
		}
	}
}

func (f *faceSet) metrics(style ComputedStyle) metrics {
	size := fontSize(style["font-size"])
	key := faceKey{size: size, bold: isBold(style["font-weight"])}
	face, ok := f.faces[key]
	if !ok {
		fontData := f.regular
		if key.bold {
			fontData = f.bold
		}
		if fontData != nil {
			face, _ = opentype.NewFace(fontData, &opentype.FaceOptions{Size: size, DPI: 72, Hinting: font.HintingNone})
			f.faces[key] = face
		}
	}
	return metrics{face: face, size: size}
}

func fontSize(value string) float64 {
	var size float64
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "xx-small":
		size = 9
	case "x-small":
		size = 10
	case "small":
		size = 13
	case "medium", "":
		size = 16
	case "large":
		size = 18
	case "x-large":
		size = 24
	case "xx-large":
		size = 32
	default:
		size = px(value, 16, 16)
	}
	if size <= 0 || size > 512 || math.IsNaN(size) || math.IsInf(size, 0) {
		return 16
	}
	return size
}

func isBold(weight string) bool {
	switch strings.ToLower(strings.TrimSpace(weight)) {
	case "bold", "bolder":
		return true
	}
	return px(weight, 0, 400) >= 600
}

func px(value string, basis, fallback float64) float64 {
	v := classifyValue(strings.TrimSpace(value))
	switch v.Kind {
	case "length", "number":
		switch v.Unit {
		case "em":
			return v.Number * 16
		case "rem":
			return v.Number * 16
		case "pt":
			return v.Number * 96 / 72
		case "pc":
			return v.Number * 16
		case "in":
			return v.Number * 96
		case "cm":
			return v.Number * 96 / 2.54
		case "mm":
			return v.Number * 96 / 25.4
		case "q":
			return v.Number * 96 / 101.6
		default:
			return v.Number
		}
	case "percentage":
		return basis * v.Number / 100
	}
	return fallback
}

func displayBlock(n *StyledNode) bool {
	if n == nil || n.Node == nil {
		return false
	}
	if strings.EqualFold(n.Style["display"], "none") {
		return false
	}
	switch n.Node.Name {
	case "html", "body", "address", "article", "aside", "blockquote", "div", "dl",
		"dt", "dd", "fieldset", "figcaption", "figure", "footer", "form", "h1",
		"h2", "h3", "h4", "h5", "h6", "header", "hr", "li", "main", "nav",
		"ol", "p", "pre", "section", "table", "tbody", "td", "tfoot", "th",
		"thead", "tr", "ul":
		return true
	}
	return strings.EqualFold(n.Style["display"], "block") || strings.EqualFold(n.Style["display"], "list-item")
}

func boxEdges(n *StyledNode, name string, basis float64) [4]int {
	var e [4]int
	for i, side := range []string{"top", "right", "bottom", "left"} {
		if name == "border-width" {
			e[i] = borderWidth(n.Style, side)
		} else {
			e[i] = int(math.Max(0, math.Round(px(n.Style[name+"-"+side], basis, 0))))
		}
	}
	return e
}

// LayoutWithViewport lays out a styled document without painting it.
func LayoutWithViewport(document StyledDocument, viewport image.Rectangle) (Layout, error) {
	if viewport.Dx() <= 0 || viewport.Dy() <= 0 {
		viewport = image.Rect(0, 0, placeholderWidth, placeholderHeight)
	}
	faces := newFaceSet()
	defer faces.close()
	faces.images = document.Images
	root := &Box{Node: document.Document.Root, Rect: viewport, Content: viewport}
	if document.StyleRoot != nil {
		root.Children, _ = layoutChildren(document.StyleRoot, viewport.Min.X, viewport.Min.Y, viewport.Dx(), faces,
			containingBlock{x: viewport.Min.X, y: viewport.Min.Y, width: viewport.Dx(), height: viewport.Dy(), viewport: viewport})
	}
	return Layout{Document: document, Viewport: viewport, Root: root}, nil
}

// flowKind classifies how a child participates in its parent's block flow.
type flowKind int

const (
	flowSkip     flowKind = iota // display:none
	flowInline                   // text or inline-level content
	flowReplaced                 // block-level img
	flowTable                    // table formatting context
	flowBlock                    // block container
)

func childFlowKind(child *StyledNode) flowKind {
	if child.Node.Type == ElementNode && strings.EqualFold(child.Style["display"], "none") {
		return flowSkip
	}
	if child.Node.Type == ElementNode && strings.EqualFold(child.Node.Name, "img") && displayBlock(child) {
		// A block-level replaced element still needs an image box, and it
		// has no children to lay out.
		return flowReplaced
	}
	if child.Node.Type == ElementNode && isTableNode(child) {
		return flowTable
	}
	if child.Node.Type != TextNode && containsTable(child) {
		// An inline element wrapping a table is treated as block level so
		// the table keeps its own formatting context instead of being
		// flattened into the inline flow.
		return flowBlock
	}
	if child.Node.Type == TextNode || !displayBlock(child) {
		return flowInline
	}
	return flowBlock
}

// A block descendant of an inline element participates in the enclosing
// block formatting context, not in its inline line box. Split the inline
// wrapper into before/after fragments, retaining the original styled leaves
// (and hence their inherited font/color) on either side of the block.
func splitInlineBlocks(children []*StyledNode) []*StyledNode {
	var result []*StyledNode
	var hasBlock func(*StyledNode) bool
	hasBlock = func(n *StyledNode) bool {
		if childFlowKind(n) == flowSkip {
			return false
		}
		for _, c := range n.Children {
			if childFlowKind(c) != flowInline || hasBlock(c) {
				return true
			}
		}
		return false
	}
	var appendChild func(*StyledNode)
	appendChild = func(n *StyledNode) {
		if childFlowKind(n) == flowSkip {
			return
		}
		if childFlowKind(n) != flowInline || !hasBlock(n) {
			result = append(result, n)
			return
		}
		for _, c := range n.Children {
			appendChild(c)
		}
	}
	for _, child := range children {
		appendChild(child)
	}
	return result
}

// emptyInline reports whether inline content would produce no line box:
// whitespace-only text and elements containing nothing else. Such content
// does not separate adjoining vertical margins.
func emptyInline(n *StyledNode) bool {
	if n.Node.Type == TextNode {
		return strings.TrimFunc(n.Node.Data, func(r rune) bool { return unicode.IsSpace(r) && r != '\u00a0' }) == ""
	}
	if n.Node.Type != ElementNode {
		return true
	}
	if strings.EqualFold(n.Style["display"], "none") {
		return true
	}
	switch strings.ToLower(n.Node.Name) {
	case "img", "br":
		return false
	}
	for _, c := range n.Children {
		if !emptyInline(c) {
			return false
		}
	}
	return true
}

// collapsedMargin accumulates adjoining vertical margins. Per CSS 2.1 §8.3.1
// the result is the largest positive margin plus the most negative one.
type collapsedMargin struct{ pos, neg int }

func (m collapsedMargin) add(v int) collapsedMargin {
	m.pos, m.neg = max(m.pos, v), min(m.neg, v)
	return m
}

func (m collapsedMargin) join(o collapsedMargin) collapsedMargin {
	return collapsedMargin{pos: max(m.pos, o.pos), neg: min(m.neg, o.neg)}
}

func (m collapsedMargin) value() int { return m.pos + m.neg }

// verticalMargin returns a signed top or bottom margin; unlike horizontal
// margins, negative vertical margins take part in collapsing.
func verticalMargin(n *StyledNode, side string, basis int) int {
	return int(math.Round(px(n.Style["margin-"+side], float64(basis), 0)))
}

// establishesContext reports whether a block box starts a new block
// formatting context, whose margins never collapse with its children.
func establishesContext(n *StyledNode) bool {
	if n.Node == nil || n.Node.Parent == nil || n.Node.Type != ElementNode ||
		strings.EqualFold(n.Node.Name, "html") {
		return true
	}
	if o := strings.ToLower(strings.TrimSpace(n.Style["overflow"])); o != "" && o != "visible" {
		return true
	}
	if f := strings.ToLower(strings.TrimSpace(n.Style["float"])); f != "" && f != "none" {
		return true
	}
	switch strings.ToLower(strings.TrimSpace(n.Style["position"])) {
	case "absolute", "fixed":
		return true
	}
	switch strings.ToLower(strings.TrimSpace(n.Style["display"])) {
	case "flow-root", "inline-block", "flex", "grid", "table-cell":
		return true
	}
	return false
}

// collapsesThroughTop reports whether a block's top margin adjoins its first
// in-flow child's top margin (no top border or padding separates them).
func collapsesThroughTop(n *StyledNode, width int) bool {
	return !establishesContext(n) && borderWidth(n.Style, "top") == 0 &&
		boxEdges(n, "padding", float64(width))[0] == 0
}

// collapsesThroughBottom reports whether a block's bottom margin adjoins its
// last in-flow child's bottom margin.
func collapsesThroughBottom(n *StyledNode, width int) bool {
	h := strings.TrimSpace(n.Style["height"])
	return !establishesContext(n) && borderWidth(n.Style, "bottom") == 0 &&
		boxEdges(n, "padding", float64(width))[2] == 0 && (h == "" || strings.EqualFold(h, "auto"))
}

// blockContentWidth mirrors layoutBlock's content width so that margin
// percentages of descendants resolve against the same basis.
func blockContentWidth(n *StyledNode, width int) int {
	margin := boxEdges(n, "margin", float64(width))
	padding := boxEdges(n, "padding", float64(width))
	border := boxEdges(n, "border-width", float64(width))
	contentWidth := width - margin[1] - margin[3] - padding[1] - padding[3] - border[1] - border[3]
	if w := n.Style["width"]; w != "" && w != "auto" {
		contentWidth = int(px(w, float64(width), float64(contentWidth)))
	}
	return max(0, contentWidth)
}

// topMargin returns the collapsed top margin of a block-level child,
// including the top margins of first children it collapses through.
func topMargin(n *StyledNode, kind flowKind, width int) collapsedMargin {
	m := collapsedMargin{}.add(verticalMargin(n, "top", width))
	if kind != flowBlock || !collapsesThroughTop(n, width) {
		return m
	}
	inner := blockContentWidth(n, width)
	for _, child := range n.Children {
		switch k := childFlowKind(child); k {
		case flowSkip:
			continue
		case flowInline:
			if emptyInline(child) {
				continue
			}
			return m
		default:
			return m.join(topMargin(child, k, inner))
		}
	}
	return m
}

type containingBlock struct {
	x, y, width, height int
	viewport            image.Rectangle
}

func positioned(n *StyledNode) bool {
	switch strings.ToLower(strings.TrimSpace(n.Style["position"])) {
	case "absolute", "fixed":
		return true
	}
	return false
}

func layoutChildren(parent *StyledNode, x, y, width int, faces *faceSet, cb containingBlock) ([]*Box, int) {
	boxes, bottom, trailing := layoutFlow(parent, x, y, width, faces, false, false, cb)
	return boxes, bottom + trailing.value() - y
}

// layoutFlow lays out a block container's children in normal flow starting
// at y and returns the bottom of the last in-flow box. Adjoining vertical
// margins between siblings collapse. When absorbTop is set, the first
// child's top margin has already been applied by the caller (it collapsed
// through the parent). When keepTrailing is set, the last child's bottom
// margin is returned instead of added, so the parent can collapse it.
func layoutFlow(parent *StyledNode, x, y, width int, faces *faceSet, absorbTop, keepTrailing bool, cb containingBlock) ([]*Box, int, collapsedMargin) {
	var boxes []*Box
	var positionedBoxes []*Box
	cursor := y
	pending := collapsedMargin{}
	first := true
	var inline []*StyledNode
	flush := func() {
		if len(inline) == 0 {
			return
		}
		if b, h := layoutInline(parent.Node, parent.Style, inline, x, cursor+pending.value(), width, faces); b != nil {
			cursor += pending.value()
			pending = collapsedMargin{}
			boxes = append(boxes, b)
			cursor += h
		}
		inline = nil
	}
	for _, child := range splitInlineBlocks(parent.Children) {
		kind := childFlowKind(child)
		switch kind {
		case flowSkip:
			continue
		}
		if positioned(child) {
			// Positioned boxes retain a static position at their place in the
			// source, but are appended after all normal-flow siblings for paint.
			flush()
			staticY := cursor + pending.value()
			positionedBoxes = append(positionedBoxes,
				layoutPositioned(child, x, staticY, width, cb, faces))
			continue
		}
		switch kind {
		case flowInline:
			inline = append(inline, child)
			if !emptyInline(child) {
				first = false
			}
			continue
		}
		flush()
		top := cursor
		if !(first && absorbTop) {
			top += pending.join(topMargin(child, kind, width)).value()
		}
		first = false
		var b *Box
		var bottom collapsedMargin
		switch kind {
		case flowReplaced:
			// Positive margins are applied inside; offset so the border box
			// starts at top.
			b, _ = layoutReplacedBlock(child, x, top-boxEdges(child, "margin", float64(width))[0], width, faces)
			bottom = bottom.add(verticalMargin(child, "bottom", width))
		case flowTable:
			b, _ = layoutTable(child, x, top-boxEdges(child, "margin", float64(width))[0], width, parent.Style["text-align"], faces)
			bottom = bottom.add(verticalMargin(child, "bottom", width))
		default:
			b, bottom = layoutBlock(child, x, top, width, faces, cb)
		}
		boxes = append(boxes, b)
		cursor = b.Rect.Max.Y
		pending = bottom
	}
	flush()
	boxes = append(boxes, positionedBoxes...)
	if keepTrailing {
		return boxes, cursor, pending
	}
	return boxes, cursor + pending.value(), collapsedMargin{}
}

// layoutBlock lays out a block box whose border box starts at y; the caller
// has already resolved its (collapsed) top margin. It returns the box and its
// bottom margin, which may include a collapsed last-child margin.
func layoutBlock(n *StyledNode, x, y, width int, faces *faceSet, cb containingBlock) (*Box, collapsedMargin) {
	margin := boxEdges(n, "margin", float64(width))
	padding := boxEdges(n, "padding", float64(width))
	border := boxEdges(n, "border-width", float64(width))
	edges := padding[1] + padding[3] + border[1] + border[3]
	contentWidth := blockContentWidth(n, width)
	outerWidth := contentWidth + edges
	contentX := x + margin[3] + border[3] + padding[3]
	contentY := y + border[0] + padding[0]
	collapseBottom := collapsesThroughBottom(n, width)
	childCB := cb
	if strings.EqualFold(strings.TrimSpace(n.Style["position"]), "relative") || positioned(n) {
		childCB = containingBlock{
			x:        x + margin[3] + border[3],
			y:        y + border[0],
			width:    max(0, outerWidth-border[1]-border[3]),
			height:   cb.height,
			viewport: cb.viewport,
		}
		if h := strings.TrimSpace(n.Style["height"]); h != "" && !strings.EqualFold(h, "auto") {
			childCB.height = int(math.Max(0, px(h, float64(width), float64(cb.height)))) +
				padding[0] + padding[2]
		}
	}
	children, childBottom, trailing := layoutFlow(n, contentX, contentY, contentWidth, faces,
		collapsesThroughTop(n, width), collapseBottom, childCB)
	childHeight := childBottom - contentY
	height := childHeight
	if h := n.Style["height"]; h != "" && h != "auto" {
		height = int(math.Max(0, px(h, float64(childHeight), float64(height))))
	}

	content := image.Rect(contentX, contentY, contentX+contentWidth, contentY+height)
	rect := image.Rect(x+margin[3], y, x+margin[3]+outerWidth, content.Max.Y+padding[2]+border[2])
	bottom := collapsedMargin{}.add(verticalMargin(n, "bottom", width))
	if collapseBottom {
		bottom = bottom.join(trailing)
	}
	return &Box{Node: n.Node, Rect: rect, Content: content, Children: children}, bottom
}

// layoutPositioned lays out an absolute/fixed box without changing the
// normal-flow cursor. Auto offsets use the position where the box occurred
// in source order; fixed boxes always use the viewport containing block.
func layoutPositioned(n *StyledNode, staticX, staticY, _ int, cb containingBlock, faces *faceSet) *Box {
	if strings.EqualFold(strings.TrimSpace(n.Style["position"]), "fixed") {
		cb = containingBlock{x: cb.viewport.Min.X, y: cb.viewport.Min.Y, width: cb.viewport.Dx(),
			height: cb.viewport.Dy(), viewport: cb.viewport}
	}
	width := cb.width
	cssWidth := strings.TrimSpace(n.Style["width"])
	if cssWidth == "" || strings.EqualFold(cssWidth, "auto") {
		minWidth, maxWidth := positionedIntrinsicWidths(n, faces)
		edges := boxEdges(n, "margin", float64(width))
		padding := boxEdges(n, "padding", float64(width))
		border := boxEdges(n, "border-width", float64(width))
		available := max(0, width-edges[1]-edges[3]-padding[1]-padding[3]-border[1]-border[3])
		used := min(maxWidth, available)
		used = max(minWidth, used)
		used = min(used, available)
		style := cloneStyle(n.Style)
		style["width"] = strconv.Itoa(used) + "px"
		n = &StyledNode{Node: n.Node, Style: style, Children: n.Children}
	}
	positionX, positionY := staticX, staticY
	left, top := strings.TrimSpace(n.Style["left"]), strings.TrimSpace(n.Style["top"])
	if left != "" && !strings.EqualFold(left, "auto") {
		positionX = cb.x + int(math.Round(px(left, float64(width), 0)))
	}
	if top != "" && !strings.EqualFold(top, "auto") {
		positionY = cb.y + int(math.Round(px(top, float64(width), 0)))
	}
	box, _ := layoutBlock(n, positionX, positionY, width, faces, cb)
	right, bottom := strings.TrimSpace(n.Style["right"]), strings.TrimSpace(n.Style["bottom"])
	if right != "" && !strings.EqualFold(right, "auto") && (left == "" || strings.EqualFold(left, "auto")) {
		offset := int(math.Round(px(right, float64(width), 0)))
		box = translatePositionedBox(box, cb.x+width-offset-box.Rect.Max.X, 0)
	}
	if bottom != "" && !strings.EqualFold(bottom, "auto") && (top == "" || strings.EqualFold(top, "auto")) {
		offset := int(math.Round(px(bottom, float64(width), 0)))
		box = translatePositionedBox(box, 0, cb.y+cb.height-offset-box.Rect.Max.Y)
	}
	return box
}

func cloneStyle(style ComputedStyle) ComputedStyle {
	out := make(ComputedStyle, len(style))
	for key, value := range style {
		out[key] = value
	}
	return out
}

func positionedIntrinsicWidths(n *StyledNode, faces *faceSet) (minimum, maximum int) {
	if n.Node.Type == TextNode {
		text := strings.Join(strings.Fields(n.Node.Data), " ")
		m := faces.metrics(n.Style)
		maximum = m.width(text)
		for _, word := range strings.Fields(text) {
			minimum = max(minimum, m.width(word))
		}
		return minimum, maximum
	}
	for _, child := range n.Children {
		childMin, childMax := positionedIntrinsicWidths(child, faces)
		minimum = max(minimum, childMin)
		maximum += childMax
	}
	return minimum, maximum
}

func translatePositionedBox(box *Box, dx, dy int) *Box {
	box.Rect = box.Rect.Add(image.Pt(dx, dy))
	box.Content = box.Content.Add(image.Pt(dx, dy))
	for i := range box.Text {
		box.Text[i].Rect = box.Text[i].Rect.Add(image.Pt(dx, dy))
	}
	for i := range box.Images {
		box.Images[i].Rect = box.Images[i].Rect.Add(image.Pt(dx, dy))
	}
	for _, child := range box.Children {
		translatePositionedBox(child, dx, dy)
	}
	return box
}

// layoutReplacedBlock lays out a block-level img, honouring margins, borders
// and padding while sizing the replaced content from intrinsic or CSS
// dimensions.
func layoutReplacedBlock(n *StyledNode, x, y, width int, faces *faceSet) (*Box, int) {
	margin := boxEdges(n, "margin", float64(width))
	padding := boxEdges(n, "padding", float64(width))
	border := boxEdges(n, "border-width", float64(width))
	picture := faces.images[n.Node]
	contentWidth, contentHeight := imageDimensions(n, picture, width)
	contentX := x + margin[3] + border[3] + padding[3]
	contentY := y + margin[0] + border[0] + padding[0]
	content := image.Rect(contentX, contentY, contentX+contentWidth, contentY+contentHeight)
	rect := image.Rect(x+margin[3], y+margin[0],
		content.Max.X+padding[1]+border[1], content.Max.Y+padding[2]+border[2])
	box := &Box{Node: n.Node, Rect: rect, Content: content,
		Images: []ImageBox{{Image: picture, Rect: content}}}
	return box, rect.Dy() + margin[0] + margin[2]
}

type inlinePart struct {
	node       *Node
	style      ComputedStyle
	text       string
	br         bool
	image      image.Image
	imageW     int
	imageH     int
	imageEdges [4]int // margin, border, and padding around replaced content
	isImage    bool
}

// Inline descendants are flattened in document order, without manufacturing
// whitespace between element boundaries. Text ownership survives flattening.
func inlineParts(nodes []*StyledNode, faces *faceSet, width int) []inlinePart {
	var parts []inlinePart
	var visit func(*StyledNode)
	visit = func(n *StyledNode) {
		if n.Node.Type == ElementNode {
			if strings.EqualFold(n.Style["display"], "none") {
				return
			}
			if n.Node.Name == "br" {
				parts = append(parts, inlinePart{node: n.Node, style: n.Style, br: true})
				return
			}
			if strings.EqualFold(n.Node.Name, "img") {
				picture := faces.images[n.Node]
				w, h := imageDimensions(n, picture, width)
				parts = append(parts, inlinePart{node: n.Node, style: n.Style,
					image: picture, imageW: w, imageH: h,
					imageEdges: inlineImageEdges(n, width), isImage: true})
				return
			}
		}
		if n.Node.Type == TextNode {
			parts = append(parts, inlinePart{node: n.Node, style: n.Style, text: n.Node.Data})
		}
		for _, child := range n.Children {
			visit(child)
		}
	}
	for _, n := range nodes {
		visit(n)
	}
	return parts
}

func inlineImageEdges(n *StyledNode, width int) [4]int {
	margin := boxEdges(n, "margin", float64(width))
	padding := boxEdges(n, "padding", float64(width))
	border := boxEdges(n, "border-width", float64(width))
	var edges [4]int
	for i := range edges {
		edges[i] = margin[i] + padding[i] + border[i]
	}
	return edges
}

func (p inlinePart) outerImageWidth() int  { return p.imageW + p.imageEdges[1] + p.imageEdges[3] }
func (p inlinePart) outerImageHeight() int { return p.imageH + p.imageEdges[0] + p.imageEdges[2] }

type inlineLine struct {
	parts   []inlinePart
	width   fixed.Int26_6
	ascent  int
	descent int
	height  int
}

func layoutInline(parent *Node, parentStyle ComputedStyle, nodes []*StyledNode, x, y, width int, faces *faceSet) (*Box, int) {
	width = max(0, width)
	var lines []inlineLine
	line := inlineLine{}
	var word []inlinePart
	var space *inlinePart
	forced := false
	add := func(p inlinePart) {
		if p.isImage {
			line.parts = append(line.parts, p)
			line.width += fixed.I(p.outerImageWidth())
			return
		}
		m := faces.metrics(p.style)
		line.parts = append(line.parts, p)
		line.width += m.advance(p.text)
		ascent, descent := m.lineMetrics()
		line.ascent = max(line.ascent, ascent)
		line.descent = max(line.descent, descent)
	}
	finalize := func(line *inlineLine) {
		// A line containing only replaced elements still has a useful default
		// baseline from its parent. This also defines where top/bottom aligned
		// images sit when there is no text.
		if line.ascent+line.descent == 0 {
			line.ascent, line.descent = faces.metrics(nodes[0].Style).lineMetrics()
		}
		for _, p := range line.parts {
			if !p.isImage {
				continue
			}
			h := p.outerImageHeight()
			switch strings.ToLower(strings.TrimSpace(p.style["vertical-align"])) {
			case "middle":
				// The midpoint aligns with the baseline plus half the
				// parent font's x-height (approximated as half its em).
				xHalf := int(math.Ceil(faces.metrics(p.style).size / 4))
				line.ascent = max(line.ascent, (h+2*xHalf+1)/2)
				line.descent = max(line.descent, max(0, (h-2*xHalf+1)/2))
			case "top", "bottom":
				line.height = max(line.height, h)
			default: // baseline and unsupported values use the baseline.
				line.ascent = max(line.ascent, h)
			}
		}
		line.height = max(line.height, line.ascent+line.descent)
	}
	flushWord := func() {
		if len(word) == 0 {
			return
		}
		var wordWidth fixed.Int26_6
		for _, p := range word {
			wordWidth += faces.metrics(p.style).advance(p.text)
		}
		var gap fixed.Int26_6
		if space != nil && len(line.parts) != 0 {
			gap = faces.metrics(space.style).advance(" ")
		}
		if len(line.parts) != 0 && line.width+gap+wordWidth > fixed.I(width) {
			finalize(&line)
			lines = append(lines, line)
			line = inlineLine{}
		}
		if space != nil && len(line.parts) != 0 {
			add(*space)
		}
		for _, p := range word {
			add(p)
		}
		word = nil
		space = nil
		forced = false
	}
	for _, part := range inlineParts(nodes, faces, width) {
		if part.br {
			flushWord()
			if len(line.parts) == 0 {
				line.ascent, line.descent = faces.metrics(part.style).lineMetrics()
			}
			finalize(&line)
			lines = append(lines, line)
			line = inlineLine{}
			space = nil
			forced = true
			continue
		}
		if part.isImage {
			flushWord()
			var gap fixed.Int26_6
			if space != nil && len(line.parts) != 0 {
				gap = faces.metrics(space.style).advance(" ")
			}
			if len(line.parts) != 0 && line.width+gap+fixed.I(part.outerImageWidth()) > fixed.I(width) {
				finalize(&line)
				lines = append(lines, line)
				line = inlineLine{}
				gap = 0
			}
			if gap > 0 && space != nil {
				add(*space)
			}
			space = nil
			add(part)
			forced = false
			continue
		}
		for _, r := range part.text {
			if unicode.IsSpace(r) && r != '\u00a0' {
				flushWord()
				if space == nil {
					p := inlinePart{node: part.node, style: part.style, text: " "}
					space = &p
				}
				continue
			}
			if len(word) != 0 && word[len(word)-1].node == part.node {
				word[len(word)-1].text += string(r)
			} else {
				word = append(word, inlinePart{node: part.node, style: part.style, text: string(r)})
			}
		}
	}
	flushWord()
	if len(line.parts) != 0 || forced {
		finalize(&line)
		lines = append(lines, line)
	}
	if len(lines) == 0 {
		return nil, 0
	}
	box := &Box{Node: parent}
	cursor := y
	for _, l := range lines {
		xpos := x
		switch strings.ToLower(strings.TrimSpace(parentStyle["text-align"])) {
		case "right", "end":
			xpos += max(0, width-int((l.width+63)/64))
		case "center":
			xpos += max(0, (width-int((l.width+63)/64))/2)
		}
		baseline := cursor + l.ascent
		for _, p := range l.parts {
			if p.isImage {
				outerHeight := p.outerImageHeight()
				outerY := baseline - outerHeight
				switch strings.ToLower(strings.TrimSpace(p.style["vertical-align"])) {
				case "top":
					outerY = cursor
				case "bottom":
					outerY = cursor + l.height - outerHeight
				case "middle":
					xHalf := int(math.Ceil(faces.metrics(p.style).size / 4))
					outerY = baseline - xHalf - outerHeight/2
				}
				contentX := xpos + p.imageEdges[3]
				contentY := outerY + p.imageEdges[0]
				box.Images = append(box.Images, ImageBox{Image: p.image,
					Rect: image.Rect(contentX, contentY, contentX+p.imageW, contentY+p.imageH)})
				xpos += p.outerImageWidth()
				continue
			}
			w := faces.metrics(p.style).width(p.text)
			textAscent, textDescent := faces.metrics(p.style).lineMetrics()
			textY := baseline - textAscent
			if len(box.Text) != 0 {
				last := &box.Text[len(box.Text)-1]
				if last.Node == p.node && last.Rect.Min.Y == textY {
					// Measure the merged run as one string: summing per-part
					// widths rounds up once per word and space, which made
					// runs (and their underlines) overshoot the drawn glyphs.
					last.Text += p.text
					last.Rect.Max.X = last.Rect.Min.X + faces.metrics(p.style).width(last.Text)
					xpos = last.Rect.Max.X
					continue
				}
			}
			box.Text = append(box.Text, TextRun{Node: p.node, Style: p.style, Text: p.text,
				Rect: image.Rect(xpos, textY, xpos+w, textY+textAscent+textDescent)})
			xpos += w
		}
		cursor += l.height
	}
	box.Rect = image.Rect(x, y, x+width, cursor)
	box.Content = box.Rect
	return box, cursor - y
}
