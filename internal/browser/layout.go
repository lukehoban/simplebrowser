package browser

import (
	"image"
	"math"
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
		root.Children, _ = layoutChildren(document.StyleRoot, viewport.Min.X, viewport.Min.Y, viewport.Dx(), faces)
	}
	return Layout{Document: document, Viewport: viewport, Root: root}, nil
}

func layoutChildren(parent *StyledNode, x, y, width int, faces *faceSet) ([]*Box, int) {
	var boxes []*Box
	cursor := y
	var inline []*StyledNode
	flush := func() {
		if len(inline) == 0 {
			return
		}
		if b, h := layoutInline(parent.Node, inline, x, cursor, width, faces); b != nil {
			boxes = append(boxes, b)
			cursor += h
		}
		inline = nil
	}
	for _, child := range parent.Children {
		if child.Node.Type == ElementNode && strings.EqualFold(child.Style["display"], "none") {
			continue
		}
		if child.Node.Type == ElementNode && strings.EqualFold(child.Node.Name, "img") &&
			displayBlock(child) {
			// A block-level replaced element still needs an image box, and it
			// has no children to lay out.
			flush()
			b, h := layoutReplacedBlock(child, x, cursor, width, faces)
			boxes = append(boxes, b)
			cursor += h
			continue
		}
		if child.Node.Type == ElementNode && isTableNode(child) {
			flush()
			b, h := layoutTable(child, x, cursor, width, parent.Style["text-align"], faces)
			boxes = append(boxes, b)
			cursor += h
			continue
		}
		if child.Node.Type != TextNode && containsTable(child) {
			// An inline element wrapping a table is treated as block level so
			// the table keeps its own formatting context instead of being
			// flattened into the inline flow.
			flush()
			b, h := layoutBlock(child, x, cursor, width, faces)
			boxes = append(boxes, b)
			cursor += h
			continue
		}
		if child.Node.Type == TextNode || !displayBlock(child) {
			inline = append(inline, child)
			continue
		}
		flush()
		b, h := layoutBlock(child, x, cursor, width, faces)
		boxes = append(boxes, b)
		cursor += h
	}
	flush()
	return boxes, cursor - y
}

func layoutBlock(n *StyledNode, x, y, width int, faces *faceSet) (*Box, int) {
	margin := boxEdges(n, "margin", float64(width))
	padding := boxEdges(n, "padding", float64(width))
	border := boxEdges(n, "border-width", float64(width))
	edges := padding[1] + padding[3] + border[1] + border[3]
	contentWidth := width - margin[1] - margin[3] - edges
	if w := n.Style["width"]; w != "" && w != "auto" {
		contentWidth = int(px(w, float64(width), float64(contentWidth)))
	}
	contentWidth = max(0, contentWidth)
	outerWidth := contentWidth + edges
	contentX := x + margin[3] + border[3] + padding[3]
	contentY := y + margin[0] + border[0] + padding[0]
	children, childHeight := layoutChildren(n, contentX, contentY, contentWidth, faces)
	height := childHeight
	if h := n.Style["height"]; h != "" && h != "auto" {
		height = int(math.Max(0, px(h, float64(childHeight), float64(height))))
	}
	content := image.Rect(contentX, contentY, contentX+contentWidth, contentY+height)
	rect := image.Rect(x+margin[3], y+margin[0], x+margin[3]+outerWidth, content.Max.Y+padding[2]+border[2])
	return &Box{Node: n.Node, Rect: rect, Content: content, Children: children}, rect.Dy() + margin[0] + margin[2]
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

func layoutInline(parent *Node, nodes []*StyledNode, x, y, width int, faces *faceSet) (*Box, int) {
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
