package browser

import (
	"image"
	"math"
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/image/font"
	"golang.org/x/image/font/gofont/goregular"
	"golang.org/x/image/font/opentype"
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
	if m.face == nil {
		return utf8.RuneCountInString(s) * int(m.size*0.55)
	}
	return int((font.MeasureString(m.face, s) + 63) / 64)
}

func (m metrics) lineHeight() int { return int(math.Ceil(m.size * 1.2)) }

// Each layout owns its font faces. opentype faces cache glyph data internally
// and must not be shared across concurrent renders.
type faceSet struct {
	font  *opentype.Font
	faces map[float64]font.Face
}

func newFaceSet() *faceSet {
	f, _ := opentype.Parse(goregular.TTF)
	return &faceSet{font: f, faces: make(map[float64]font.Face)}
}

func (f *faceSet) close() {
	for _, face := range f.faces {
		if closer, ok := face.(interface{ Close() error }); ok {
			_ = closer.Close()
		}
	}
}

func (f *faceSet) metrics(style ComputedStyle) metrics {
	size := px(style["font-size"], 16, 16)
	if size <= 0 {
		size = 16
	}
	face, ok := f.faces[size]
	if !ok && f.font != nil {
		face, _ = opentype.NewFace(f.font, &opentype.FaceOptions{Size: size, DPI: 72, Hinting: font.HintingNone})
		f.faces[size] = face
	}
	return metrics{face: face, size: size}
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
		e[i] = int(math.Max(0, math.Round(px(n.Style[name+"-"+side], basis, 0))))
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
		if child.Node.Type == ElementNode && isTableNode(child) {
			flush()
			b, h := layoutTable(child, x, cursor, width, faces)
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

type inlinePart struct {
	node  *Node
	style ComputedStyle
	text  string
	br    bool
}

// Inline descendants are flattened in document order, without manufacturing
// whitespace between element boundaries. Text ownership survives flattening.
func inlineParts(nodes []*StyledNode) []inlinePart {
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

type inlineLine struct {
	parts  []inlinePart
	width  int
	height int
}

func layoutInline(parent *Node, nodes []*StyledNode, x, y, width int, faces *faceSet) (*Box, int) {
	width = max(0, width)
	var lines []inlineLine
	line := inlineLine{}
	var word []inlinePart
	var space *inlinePart
	forced := false
	add := func(p inlinePart) {
		m := faces.metrics(p.style)
		line.parts = append(line.parts, p)
		line.width += m.width(p.text)
		line.height = max(line.height, m.lineHeight())
	}
	flushWord := func() {
		if len(word) == 0 {
			return
		}
		wordWidth := 0
		for _, p := range word {
			wordWidth += faces.metrics(p.style).width(p.text)
		}
		gap := 0
		if space != nil && len(line.parts) != 0 {
			gap = faces.metrics(space.style).width(" ")
		}
		if len(line.parts) != 0 && line.width+gap+wordWidth > width {
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
	for _, part := range inlineParts(nodes) {
		if part.br {
			flushWord()
			if line.height == 0 {
				line.height = faces.metrics(part.style).lineHeight()
			}
			lines = append(lines, line)
			line = inlineLine{}
			space = nil
			forced = true
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
		if line.height == 0 {
			line.height = faces.metrics(nodes[0].Style).lineHeight()
		}
		lines = append(lines, line)
	}
	if len(lines) == 0 {
		return nil, 0
	}
	box := &Box{Node: parent}
	cursor := y
	for _, l := range lines {
		xpos := x
		for _, p := range l.parts {
			w := faces.metrics(p.style).width(p.text)
			if len(box.Text) != 0 {
				last := &box.Text[len(box.Text)-1]
				if last.Node == p.node && last.Rect.Min.Y == cursor {
					last.Text += p.text
					last.Rect.Max.X += w
					xpos += w
					continue
				}
			}
			box.Text = append(box.Text, TextRun{Node: p.node, Style: p.style, Text: p.text,
				Rect: image.Rect(xpos, cursor, xpos+w, cursor+l.height)})
			xpos += w
		}
		cursor += l.height
	}
	box.Rect = image.Rect(x, y, x+width, cursor)
	box.Content = box.Rect
	return box, cursor - y
}
