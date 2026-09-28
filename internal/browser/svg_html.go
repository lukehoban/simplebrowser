package browser

import (
	"bytes"
	"encoding/xml"
	"image"
	"image/color"
	"strings"
)

// inlineSVGImage bridges an HTML SVG subtree to the existing bounded SVG
// decoder. HTML's tokenizer lowercases element and attribute names, so restore
// the case-sensitive SVG names understood by that decoder while serializing.
func inlineSVGImage(n *StyledNode) image.Image {
	if n == nil || n.Node == nil {
		return nil
	}
	hasXLinkHref := false
	var findXLinkHref func(*Node)
	findXLinkHref = func(node *Node) {
		if node == nil || hasXLinkHref {
			return
		}
		for _, attr := range node.Attributes {
			if strings.EqualFold(attr.Name, "xlink:href") {
				hasXLinkHref = true
				return
			}
		}
		for _, child := range node.Children {
			findXLinkHref(child)
		}
	}
	findXLinkHref(n.Node)

	var source bytes.Buffer
	elements := 0
	var writeNode func(*Node, int) bool
	writeNode = func(node *Node, depth int) bool {
		if node == nil || depth > 64 {
			return false
		}
		if node.Type == TextNode {
			var escaped bytes.Buffer
			_ = xml.EscapeText(&escaped, []byte(node.Data))
			if source.Len()+escaped.Len() > maxSVGBytes {
				return false
			}
			source.Write(escaped.Bytes())
			return true
		}
		if node.Type != ElementNode {
			return true
		}
		elements++
		if elements > maxSVGElements {
			return false
		}
		name := svgHTMLName(node.Name)
		source.WriteByte('<')
		source.WriteString(name)
		hasXLinkNamespace := false
		for _, attr := range node.Attributes {
			attrName := svgHTMLAttributeName(attr.Name)
			if strings.EqualFold(attrName, "xmlns:xlink") {
				hasXLinkNamespace = true
			}
			escaped := strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", "\"", "&quot;").Replace(attr.Value)
			if source.Len()+len(attrName)+len(escaped)+4 > maxSVGBytes {
				return false
			}
			source.WriteByte(' ')
			source.WriteString(attrName)
			source.WriteString("=\"")
			source.WriteString(escaped)
			source.WriteByte('"')
		}
		if depth == 0 && hasXLinkHref && !hasXLinkNamespace {
			const declaration = ` xmlns:xlink="http://www.w3.org/1999/xlink"`
			if source.Len()+len(declaration) > maxSVGBytes {
				return false
			}
			source.WriteString(declaration)
		}
		source.WriteByte('>')
		if source.Len() > maxSVGBytes {
			return false
		}
		for _, child := range node.Children {
			if !writeNode(child, depth+1) {
				return false
			}
		}
		source.WriteString("</")
		source.WriteString(name)
		source.WriteByte('>')
		return source.Len() <= maxSVGBytes
	}
	if !writeNode(n.Node, 0) {
		return nil
	}
	rgba, ok := parseColor(n.Style["color"])
	if !ok {
		rgba = color.RGBA{A: 255}
	}
	inherited := color.NRGBA{R: rgba.R, G: rgba.G, B: rgba.B, A: rgba.A}
	img, err := decodeSVGWithInheritedColor(source.Bytes(), inherited)
	if err != nil {
		return nil
	}
	return img
}

func svgHTMLName(name string) string {
	switch strings.ToLower(name) {
	case "lineargradient":
		return "linearGradient"
	case "radialgradient":
		return "radialGradient"
	case "clippath":
		return "clipPath"
	case "textpath":
		return "textPath"
	default:
		return name
	}
}

func svgHTMLAttributeName(name string) string {
	switch strings.ToLower(name) {
	case "viewbox":
		return "viewBox"
	case "preserveaspectratio":
		return "preserveAspectRatio"
	case "gradientunits":
		return "gradientUnits"
	case "gradienttransform":
		return "gradientTransform"
	case "patternunits":
		return "patternUnits"
	case "patterncontentunits":
		return "patternContentUnits"
	case "patterntransform":
		return "patternTransform"
	case "clippathunits":
		return "clipPathUnits"
	case "markerwidth":
		return "markerWidth"
	case "markerheight":
		return "markerHeight"
	case "refx":
		return "refX"
	case "refy":
		return "refY"
	case "textlength":
		return "textLength"
	case "lengthadjust":
		return "lengthAdjust"
	case "startoffset":
		return "startOffset"
	case "spreadmethod":
		return "spreadMethod"
	default:
		return name
	}
}
