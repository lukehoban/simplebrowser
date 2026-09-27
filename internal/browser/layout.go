package browser

import (
	"image"
	"math"
	"strings"
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
	Node *Node
	Text string
	Rect image.Rectangle
}

type metrics struct {
	face font.Face
	size float64
	line int
}

var regularFace = func() font.Face {
	f, err := opentype.Parse(goregular.TTF)
	if err != nil {
		return nil
	}
	face, err := opentype.NewFace(f, &opentype.FaceOptions{Size: 16, DPI: 72, Hinting: font.HintingNone})
	if err != nil {
		return nil
	}
	return face
}()

func (m metrics) width(s string) int {
	if m.face == nil {
		return utf8.RuneCountInString(s) * int(m.size*0.55)
	}
	return int((font.MeasureString(m.face, s) + 63) / 64)
}

func (m metrics) lineHeight() int {
	if m.line > 0 {
		return m.line
	}
	return int(math.Ceil(m.size * 1.2))
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
	root := &Box{Node: document.Document.Root, Rect: viewport, Content: viewport}
	if document.StyleRoot != nil {
		root.Children, _ = layoutChildren(document.StyleRoot, viewport.Min.X, viewport.Min.Y, viewport.Dx())
	}
	return Layout{Document: document, Viewport: viewport, Root: root}, nil
}

func layoutChildren(parent *StyledNode, x, y, width int) ([]*Box, int) {
	var boxes []*Box
	cursor := y
	var inline []*StyledNode
	flush := func() {
		if len(inline) == 0 {
			return
		}
		height := 0
		for _, n := range inline {
			b, h := layoutInline(n, x, cursor+height, width)
			if b != nil {
				boxes = append(boxes, b)
			}
			height += h
		}
		cursor += height
		inline = nil
	}
	for _, child := range parent.Children {
		if child.Node.Type == TextNode || !displayBlock(child) {
			if child.Node.Type != TextNode && strings.EqualFold(child.Style["display"], "none") {
				continue
			}
			inline = append(inline, child)
			continue
		}
		flush()
		b, h := layoutBlock(child, x, cursor, width)
		boxes = append(boxes, b)
		cursor += h
	}
	flush()
	return boxes, cursor - y
}

func layoutBlock(n *StyledNode, x, y, width int) (*Box, int) {
	margin := boxEdges(n, "margin", float64(width))
	padding := boxEdges(n, "padding", float64(width))
	border := boxEdges(n, "border-width", float64(width))
	outerWidth := int(math.Max(0, float64(width-margin[1]-margin[3])))
	contentWidth := outerWidth - padding[1] - padding[3] - border[1] - border[3]
	if w := n.Style["width"]; w != "" && w != "auto" {
		contentWidth = int(math.Max(0, px(w, float64(width), float64(contentWidth))))
	}
	contentX := x + margin[3] + border[3] + padding[3]
	contentY := y + margin[0] + border[0] + padding[0]
	children, childHeight := layoutChildren(n, contentX, contentY, contentWidth)
	height := childHeight
	if h := n.Style["height"]; h != "" && h != "auto" {
		height = int(math.Max(0, px(h, float64(childHeight), float64(height))))
	}
	content := image.Rect(contentX, contentY, contentX+contentWidth, contentY+height)
	rect := image.Rect(x+margin[3], y+margin[0], x+margin[3]+outerWidth, content.Max.Y+padding[2]+border[2])
	return &Box{Node: n.Node, Rect: rect, Content: content, Children: children}, rect.Dy() + margin[0] + margin[2]
}

func layoutInline(n *StyledNode, x, y, width int) (*Box, int) {
	size := px(n.Style["font-size"], 16, 16)
	if size <= 0 {
		size = 16
	}
	m := metrics{face: regularFace, size: size, line: int(math.Ceil(size * 1.2))}
	if size != 16 && regularFace != nil {
		// A separate face avoids changing the shared embedded face.
		if f, err := opentype.Parse(goregular.TTF); err == nil {
			if face, err := opentype.NewFace(f, &opentype.FaceOptions{Size: size, DPI: 72, Hinting: font.HintingNone}); err == nil {
				m.face = face
			}
		}
	}
	text := collectText(n)
	if text == "" {
		return nil, 0
	}
	lineHeight := m.lineHeight()
	line, lineWidth, lines := "", 0, 0
	var runs []TextRun
	flush := func() {
		if strings.TrimSpace(line) != "" {
			runs = append(runs, TextRun{Node: n.Node, Text: strings.TrimSpace(line),
				Rect: image.Rect(x, y+lines*lineHeight, x+lineWidth, y+(lines+1)*lineHeight)})
			lines++
		}
		line, lineWidth = "", 0
	}
	for _, word := range strings.Fields(strings.ReplaceAll(text, "\u00a0", " ")) {
		w := m.width(word)
		space := 0
		if line != "" {
			space = m.width(" ")
		}
		if line != "" && lineWidth+space+w > width {
			flush()
			space = 0
		}
		line += strings.Repeat(" ", btoi(space > 0)) + word
		lineWidth += space + w
	}
	flush()
	if lines == 0 {
		return nil, 0
	}
	rect := image.Rect(x, y, x+width, y+lines*lineHeight)
	return &Box{Node: n.Node, Rect: rect, Content: rect, Text: runs}, lines * lineHeight
}

func btoi(v bool) int {
	if v {
		return 1
	}
	return 0
}

func collectText(n *StyledNode) string {
	if n.Node.Type == TextNode {
		return n.Node.Data
	}
	var b strings.Builder
	for _, c := range n.Children {
		if c.Node.Type == ElementNode && strings.EqualFold(c.Style["display"], "none") {
			continue
		}
		b.WriteString(collectText(c))
		b.WriteByte(' ')
	}
	return b.String()
}
