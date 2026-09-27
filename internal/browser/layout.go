package browser

import (
	"image"
	"math"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/image/font"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/font/sfnt"
	"golang.org/x/image/math/fixed"
	"golang.org/x/text/cases"
	"golang.org/x/text/language"
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
// or malformed SVG; the rectangle is still reserved so painting can draw a
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
	// PenX retains the 26.6 CSS-pixel text origin. Rect stays pixel-aligned
	// for clipping and diagnostics; the font drawer uses PenX so separately
	// laid-out inline/table fragments keep the same glyph phase as one run.
	PenX fixed.Int26_6
}

type metrics struct {
	face            font.Face
	smallCapsFace   font.Face
	size            float64
	lineHeightValue string
	language        language.Tag
	// fallback supplies per-glyph fallback faces (same size, weight and
	// style) for runes the selected face cannot draw. Nil disables fallback.
	fallback *glyphFallback
}

func (m metrics) width(s string) int {
	return int((m.advance(s) + 63) / 64)
}

func (m metrics) advance(s string) fixed.Int26_6 {
	if m.face == nil {
		return fixed.I(utf8.RuneCountInString(s) * int(m.size*0.55))
	}
	if m.smallCapsFace == nil && m.fallback == nil {
		return font.MeasureString(m.face, s)
	}
	var total fixed.Int26_6
	m.eachTextSegment(s, func(face font.Face, text string) {
		total += font.MeasureString(face, text)
	})
	return total
}

// Synthetic small caps use uppercase glyphs at 80% of the selected face's
// size. Preserve the original text in the DOM and layout runs; both measurement
// and painting walk the same contiguous face segments (including kerning).
//
// Lowercase runs are mapped with Unicode full (SpecialCasing) uppercase rules
// for the inherited document language, so one source rune may expand to
// several glyphs (ß → SS, ﬁ → FI, ŉ → ʼN). Mapping a whole contiguous
// lowercase segment rather than rune-by-rune keeps context-sensitive rules
// deterministic.
//
// Each case segment is further split into per-glyph fallback runs (see
// glyphFallback), after any small-caps mapping so expansions such as ŉ → ʼN
// also fall back.
func (m metrics) eachTextSegment(s string, visit func(font.Face, string)) {
	if m.fallback != nil {
		inner := visit
		visit = func(face font.Face, text string) {
			m.fallback.eachRun(face, face == m.smallCapsFace, text, inner)
		}
	}
	if m.smallCapsFace == nil {
		visit(m.face, s)
		return
	}
	start := 0
	small := false
	flush := func(end int) {
		if end <= start {
			return
		}
		face, text := m.face, s[start:end]
		if small {
			face, text = m.smallCapsFace, smallCapsUpper(text, m.language)
		}
		visit(face, text)
		start = end
	}
	for i, r := range s {
		if unicode.IsMark(r) {
			continue
		}
		if next := unicode.IsLower(r); next != small {
			flush(i)
			small = next
		}
	}
	flush(len(s))
}

// smallCapsUpper applies full uppercase mapping for the specified language. A
// fresh Caser is used per call because cases.Caser is stateful and not safe
// for concurrent renders.
func smallCapsUpper(s string, lang language.Tag) string {
	if lang == (language.Tag{}) {
		lang = language.Und
	}
	return cases.Upper(lang).String(s)
}

func (m metrics) lineHeight() int {
	value := strings.ToLower(strings.TrimSpace(m.lineHeightValue))
	if value == "" || value == "normal" {
		return int(math.Ceil(m.size * 1.2))
	}
	v := classifyValue(value)
	var height float64
	switch v.Kind {
	case "number":
		height = m.size * v.Number
	case "percentage":
		height = m.size * v.Number / 100
	case "length":
		height = px(value, m.size, m.size*1.2)
	default:
		height = m.size * 1.2
	}
	if height < 0 || height > 4096 || math.IsNaN(height) || math.IsInf(height, 0) {
		height = m.size * 1.2
	}
	return int(math.Ceil(height))
}

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
	// CSS centers glyph metrics in the specified line-height. Leading may be
	// negative when line-height is smaller than the font's em box.
	leading := height - ascent - descent
	ascent += leading / 2
	descent += leading - leading/2
	ascent = max(0, ascent)
	descent = max(0, descent)
	return ascent, descent
}

// Each layout owns its font faces. opentype faces cache glyph data internally
// and must not be shared across concurrent renders.
type faceSet struct {
	fonts map[fontVariant]*opentype.Font
	faces map[faceKey]font.Face
	// images holds the render-scoped decoded resources keyed by DOM node, so
	// layout never fetches during measurement.
	images map[*Node]image.Image
	// coverage caches per-font glyph presence for per-glyph fallback.
	coverage map[coverageKey]bool
	glyphBuf sfnt.Buffer
}

type faceKey struct {
	size   float64
	family string
	bold   bool
	italic bool
}

func newFaceSet() *faceSet {
	fonts := make(map[fontVariant]*opentype.Font, len(fontSources))
	for variant, data := range fontSources {
		if parsed, err := opentype.Parse(data); err == nil {
			fonts[variant] = parsed
		}
	}
	return &faceSet{fonts: fonts, faces: make(map[faceKey]font.Face)}
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
	variant := styleFontVariant(style)
	key := faceKey{size: size, family: variant.family, bold: variant.bold, italic: variant.italic}
	face := f.face(key, variant)
	lang, err := language.Parse(style["lang"])
	if err != nil {
		lang = language.Und
	}
	m := metrics{face: face, size: size, lineHeightValue: style["line-height"], language: lang}
	if strings.EqualFold(strings.TrimSpace(style["font-variant"]), "small-caps") {
		small := key
		small.size = size * .8
		m.smallCapsFace = f.face(small, variant)
	}
	m.fallback = f.glyphFallback(key, variant)
	return m
}

func (f *faceSet) face(key faceKey, variant fontVariant) font.Face {
	if face, ok := f.faces[key]; ok {
		return face
	}
	fontData := f.fonts[variant]
	if fontData == nil {
		fontData = f.fonts[fontVariant{family: "sans"}]
	}
	if fontData == nil {
		return nil
	}
	face, _ := opentype.NewFace(fontData, &opentype.FaceOptions{Size: key.size, DPI: 72, Hinting: font.HintingNone})
	f.faces[key] = face
	return face
}

// mappedFontFamily picks the first supported family in a CSS family list.
// Verdana and Geneva (the Hacker News stack) map to bundled DejaVu Sans, whose
// Bitstream Vera design has Verdana-like wide metrics. x/image ships Go Sans
// and Go Mono but no serif face, so serif/Times intentionally use the Go Sans
// fallback, as do Arial/Helvetica and the generic sans-serif family.
func mappedFontFamily(value string) string {
	for _, family := range strings.Split(value, ",") {
		family = strings.ToLower(strings.Trim(strings.TrimSpace(family), `"'`))
		switch family {
		case "courier", "courier new", "monospace":
			return "mono"
		case "verdana", "geneva", "dejavu sans":
			return "verdana"
		case "arial", "helvetica", "sans-serif",
			"times", "times new roman", "serif":
			return "sans"
		}
	}
	return "sans"
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
	if size < 0 || size > 512 || math.IsNaN(size) || math.IsInf(size, 0) {
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
		case "ex", "ch":
			// Computed styles resolve these from font metrics; this is only
			// the 0.5em fallback for unresolved values.
			return v.Number * 8
		case "vw", "vh", "vmin", "vmax",
			"svw", "svh", "svmin", "svmax",
			"lvw", "lvh", "lvmin", "lvmax",
			"dvw", "dvh", "dvmin", "dvmax":
			// These need the viewport at computed-style time. Do not
			// mistake unresolved lengths (e.g. in compound values) for px.
			return fallback
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

// LayoutWithViewport lays out a styled document without painting it. If the
// viewport size changes, computed styles are rebuilt from the loaded cascade,
// without refetching resources or modifying the input document.
func LayoutWithViewport(document StyledDocument, viewport image.Rectangle) (Layout, error) {
	if viewport.Dx() <= 0 || viewport.Dy() <= 0 {
		viewport = image.Rect(0, 0, placeholderWidth, placeholderHeight)
	}
	if document.styleViewport != (image.Point{}) && document.styleViewport != viewport.Size() {
		document = computeStyles(document, viewport.Size())
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
	// Comments, doctypes, and other non-rendered DOM nodes never generate
	// boxes, even when their computed style map inherited a block display.
	if child == nil || child.Node == nil ||
		(child.Node.Type != ElementNode && child.Node.Type != TextNode) {
		return flowSkip
	}
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
	if child.Node.Type == ElementNode && isAtomicInline(child) {
		// An empty inline-block is inline-level even on an element whose tag
		// otherwise defaults to block (for example a div).
		return flowInline
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
	if isAtomicInline(n) {
		// An empty inline-block is still an atomic inline box: it generates a
		// line box (and paints its background and borders) even with no
		// content of its own.
		return false
	}
	for _, c := range n.Children {
		if !emptyInline(c) {
			return false
		}
	}
	return true
}

// isAtomicInline reports whether an element is laid out as a single opaque
// unit on a line. Only inline-blocks with no rendered content qualify today;
// inline-blocks with content are still flattened into the surrounding inline
// flow, so their own width, height and borders are ignored.
func isAtomicInline(n *StyledNode) bool {
	if n == nil || n.Node == nil || n.Node.Type != ElementNode {
		return false
	}
	if strings.EqualFold(n.Node.Name, "img") || strings.EqualFold(n.Node.Name, "br") {
		return false
	}
	if !strings.EqualFold(strings.TrimSpace(n.Style["display"]), "inline-block") {
		return false
	}
	for _, c := range n.Children {
		if !emptyInline(c) {
			return false
		}
	}
	return true
}

// atomicInlineSize resolves the used content size of an atomic inline box.
// An empty inline-block shrinks to fit, so an auto width or height is zero;
// percentage heights have no definite basis here and also resolve to zero.
func atomicInlineSize(n *StyledNode, width int) (int, int) {
	size := func(property string, basis int) int {
		value := strings.TrimSpace(n.Style[property])
		if value == "" || strings.EqualFold(value, "auto") {
			return 0
		}
		v := px(value, float64(basis), 0)
		if math.IsNaN(v) || math.IsInf(v, 0) || v < 0 {
			return 0
		}
		return int(math.Round(v))
	}
	return size("width", width), size("height", 0)
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
	inlinePenX          fixed.Int26_6
	hasInlinePenX       bool
}

func positioned(n *StyledNode) bool {
	if n == nil || n.Node == nil || n.Node.Type != ElementNode {
		return false
	}
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
		// Comments and surrounding collapsible whitespace do not create a
		// line box. In particular, they must not move the static position of
		// a following absolutely positioned box.
		visible := false
		for _, child := range inline {
			if !emptyInline(child) {
				visible = true
				break
			}
		}
		if !visible {
			inline = nil
			return
		}
		inlineX := fixed.I(x)
		if cb.hasInlinePenX {
			inlineX = cb.inlinePenX
		}
		if b, h := layoutInlineAt(parent.Node, parent.Style, inline, x, cursor+pending.value(), width, inlineX, faces); b != nil {
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
		minWidth, maxWidth := positionedIntrinsicWidths(n, faces, width)
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
	left := strings.TrimSpace(n.Style["left"])
	top := strings.TrimSpace(n.Style["top"])
	bottom := strings.TrimSpace(n.Style["bottom"])
	if left != "" && !strings.EqualFold(left, "auto") {
		positionX = cb.x + int(math.Round(px(left, float64(width), 0)))
	}
	if top != "" && !strings.EqualFold(top, "auto") {
		positionY = cb.y + int(math.Round(px(top, float64(width), 0)))
	}
	box, _ := layoutBlock(n, positionX, positionY, width, faces, cb)
	cssHeight := strings.TrimSpace(n.Style["height"])
	marginTopAuto := strings.EqualFold(strings.TrimSpace(n.Style["margin-top"]), "auto")
	marginBottomAuto := strings.EqualFold(strings.TrimSpace(n.Style["margin-bottom"]), "auto")
	// With definite top, bottom, and height, auto vertical margins absorb
	// the remaining constraint space. layoutBlock represents the border box
	// only, so move it by the used top margin after resolving both margins.
	if top != "" && !strings.EqualFold(top, "auto") &&
		bottom != "" && !strings.EqualFold(bottom, "auto") &&
		cssHeight != "" && !strings.EqualFold(cssHeight, "auto") &&
		(marginTopAuto || marginBottomAuto) {
		margins := boxEdges(n, "margin", float64(width))
		topMargin, bottomMargin := margins[0], margins[2]
		if marginTopAuto {
			topMargin = 0
		}
		if marginBottomAuto {
			bottomMargin = 0
		}
		topOffset := int(math.Round(px(top, float64(width), 0)))
		bottomOffset := int(math.Round(px(bottom, float64(width), 0)))
		remaining := cb.height - topOffset - bottomOffset - box.Rect.Dy() - topMargin - bottomMargin
		switch {
		case marginTopAuto && marginBottomAuto:
			if remaining >= 0 {
				topMargin = remaining / 2
				bottomMargin = remaining - topMargin
			} else {
				// CSS 2.1 resolves a negative equal split by setting the
				// top auto margin to zero and placing the deficit below.
				topMargin, bottomMargin = 0, remaining
			}
		case marginTopAuto:
			topMargin = remaining
		case marginBottomAuto:
			bottomMargin = remaining
		}
		if topMargin != 0 {
			box = translatePositionedBox(box, 0, topMargin)
		}
	}
	right := strings.TrimSpace(n.Style["right"])
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

func positionedIntrinsicWidths(n *StyledNode, faces *faceSet, containingWidth int) (minimum, maximum int) {
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
		childMin, childMax := positionedIntrinsicWidths(child, faces, containingWidth)
		if child.Node.Type == ElementNode {
			if width := strings.TrimSpace(child.Style["width"]); width != "" && !strings.EqualFold(width, "auto") {
				used := px(width, float64(containingWidth), 0)
				if !math.IsNaN(used) && !math.IsInf(used, 0) {
					childMin = max(0, int(math.Round(used)))
					childMax = childMin
				}
			}
			margin := boxEdges(child, "margin", float64(containingWidth))
			padding := boxEdges(child, "padding", float64(containingWidth))
			border := boxEdges(child, "border-width", float64(containingWidth))
			extras := margin[1] + margin[3] + padding[1] + padding[3] + border[1] + border[3]
			childMin += extras
			childMax += extras
		}
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
		box.Text[i].PenX += fixed.I(dx)
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
	innerEdges [4]int // border and padding alone, the inner part of imageEdges
	isImage    bool
	// isBox marks a non-replaced atomic inline box (an empty inline-block).
	// It occupies imageW by imageH of content, and paints as a child box
	// rather than as replaced content.
	isBox bool
}

// atomic reports whether a part is laid out as one unbreakable unit with an
// explicit box size rather than as shaped text.
func (p inlinePart) atomic() bool { return p.isImage || p.isBox }

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
			if isAtomicInline(n) {
				w, h := atomicInlineSize(n, width)
				parts = append(parts, inlinePart{node: n.Node, style: n.Style,
					imageW: w, imageH: h, imageEdges: inlineImageEdges(n, width),
					innerEdges: inlineInnerEdges(n, width), isBox: true})
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

// inlineInnerEdges returns the border and padding edges of an inline-level
// box, i.e. inlineImageEdges without the margins.
func inlineInnerEdges(n *StyledNode, width int) [4]int {
	padding := boxEdges(n, "padding", float64(width))
	border := boxEdges(n, "border-width", float64(width))
	var edges [4]int
	for i := range edges {
		edges[i] = padding[i] + border[i]
	}
	return edges
}

func (p inlinePart) outerWidth() int  { return p.imageW + p.imageEdges[1] + p.imageEdges[3] }
func (p inlinePart) outerHeight() int { return p.imageH + p.imageEdges[0] + p.imageEdges[2] }

// borderBox returns the part's border box given the top-left of its margin
// box, so backgrounds and borders paint inside the reserved margins.
func (p inlinePart) borderBox(outerX, outerY int) image.Rectangle {
	x := outerX + p.imageEdges[3] - p.innerEdges[3]
	y := outerY + p.imageEdges[0] - p.innerEdges[0]
	return image.Rect(x, y,
		x+p.imageW+p.innerEdges[1]+p.innerEdges[3],
		y+p.imageH+p.innerEdges[0]+p.innerEdges[2])
}

type inlineLine struct {
	parts   []inlinePart
	width   fixed.Int26_6
	ascent  int
	descent int
	height  int
}

func layoutInline(parent *Node, parentStyle ComputedStyle, nodes []*StyledNode, x, y, width int, faces *faceSet) (*Box, int) {
	return layoutInlineAt(parent, parentStyle, nodes, x, y, width, fixed.I(x), faces)
}

func layoutInlineAt(parent *Node, parentStyle ComputedStyle, nodes []*StyledNode, x, y, width int, startPenX fixed.Int26_6, faces *faceSet) (*Box, int) {
	width = max(0, width)
	var lines []inlineLine
	line := inlineLine{}
	var word []inlinePart
	var space *inlinePart
	forced := false
	add := func(p inlinePart) {
		if p.atomic() {
			line.parts = append(line.parts, p)
			line.width += fixed.I(p.outerWidth())
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
			if !p.atomic() {
				continue
			}
			h := p.outerHeight()
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
		if part.atomic() {
			flushWord()
			var gap fixed.Int26_6
			if space != nil && len(line.parts) != 0 {
				gap = faces.metrics(space.style).advance(" ")
			}
			if len(line.parts) != 0 && line.width+gap+fixed.I(part.outerWidth()) > fixed.I(width) {
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
		penX := fixed.I(xpos) + (startPenX - fixed.I(x))
		for _, p := range l.parts {
			if p.atomic() {
				outerHeight := p.outerHeight()
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
				contentX := penX.Round() + p.imageEdges[3]
				contentY := outerY + p.imageEdges[0]
				content := image.Rect(contentX, contentY, contentX+p.imageW, contentY+p.imageH)
				if p.isBox {
					// An atomic inline box paints like any other box: its
					// background and borders come from its own child box.
					border := p.borderBox(penX.Round(), outerY)
					box.Children = append(box.Children,
						&Box{Node: p.node, Rect: border, Content: content})
				} else {
					box.Images = append(box.Images, ImageBox{Image: p.image, Rect: content})
				}
				xpos = penX.Round() + p.outerWidth()
				penX = fixed.I(xpos)
				continue
			}
			advance := faces.metrics(p.style).advance(p.text)
			left := penX.Floor()
			right := (penX + advance).Ceil()
			textAscent, textDescent := faces.metrics(p.style).lineMetrics()
			textY := baseline - textAscent
			if len(box.Text) != 0 {
				last := &box.Text[len(box.Text)-1]
				if last.Node == p.node && last.Rect.Min.Y == textY {
					// Measure the merged run as one string: summing per-part
					// widths rounds up once per word and space, which made
					// runs (and their underlines) overshoot the drawn glyphs.
					last.Text += p.text
					last.Rect.Max.X = max(last.Rect.Max.X, right)
					penX += advance
					xpos = penX.Round()
					continue
				}
			}
			box.Text = append(box.Text, TextRun{Node: p.node, Style: p.style, Text: p.text,
				Rect: image.Rect(left, textY, right, textY+textAscent+textDescent), PenX: penX})
			penX += advance
			xpos = penX.Round()
		}
		cursor += l.height
	}
	box.Rect = image.Rect(x, y, x+width, cursor)
	box.Content = box.Rect
	return box, cursor - y
}
