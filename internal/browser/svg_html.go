package browser

import (
	"bytes"
	"encoding/xml"
	"image"
	"image/color"
	"sort"
	"strconv"
	"strings"
)

const inlineSVGHostStyleAttribute = "data-simplebrowser-host-css"
const inlineSVGHostPriorityAttribute = "data-simplebrowser-host-css-priority"

var inlineSVGHostStyleProperties = []string{
	"color",
	"fill", "fill-rule", "fill-opacity",
	"stroke", "stroke-opacity", "stroke-width",
	"stroke-linecap", "stroke-linejoin", "stroke-miterlimit",
	"stroke-dasharray", "stroke-dashoffset",
	"font-size", "font-family", "font-style", "font-weight",
	"opacity", "stop-color", "stop-opacity",
	"d", "x", "y", "width", "height", "rx", "ry", "cx", "cy", "r",
}

var inlineSVGHostInheritedProperties = []string{
	"fill", "fill-rule", "fill-opacity",
	"stroke", "stroke-opacity", "stroke-width",
	"stroke-linecap", "stroke-linejoin", "stroke-miterlimit",
	"stroke-dasharray", "stroke-dashoffset",
	"font-size", "font-family", "font-style", "font-weight",
	"stop-color", "stop-opacity",
}

// inlineSVGImage bridges an HTML SVG subtree to the existing bounded SVG
// decoder. HTML's tokenizer lowercases element and attribute names, so restore
// the case-sensitive SVG names understood by that decoder while serializing.
func inlineSVGImage(n *StyledNode) image.Image {
	if n == nil || n.Node == nil {
		return nil
	}
	hasXLinkHref := false
	var source bytes.Buffer
	elements := 0
	rootEnd := 0
	rootHasXLinkNamespace := false
	var writeNode func(*Node, *StyledNode, int) bool
	writeNode = func(node *Node, styled *StyledNode, depth int) bool {
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
			if strings.EqualFold(attr.Name, inlineSVGHostStyleAttribute) {
				continue
			}
			attrName := svgHTMLAttributeName(attr.Name)
			if strings.EqualFold(attrName, "xlink:href") {
				hasXLinkHref = true
			}
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
		var hostCSS, hostPriorityCSS string
		if styled != nil {
			hostCSS, hostPriorityCSS = inlineSVGHostStyle(styled.Style, styled.StylePriority)
		}
		for _, bridge := range []struct{ name, value string }{
			{inlineSVGHostStyleAttribute, hostCSS},
			{inlineSVGHostPriorityAttribute, hostPriorityCSS},
		} {
			if bridge.value == "" {
				continue
			}
			escaped := strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", "\"", "&quot;").Replace(bridge.value)
			if source.Len()+len(bridge.name)+len(escaped)+4 > maxSVGBytes {
				return false
			}
			source.WriteByte(' ')
			source.WriteString(bridge.name)
			source.WriteString("=\"")
			source.WriteString(escaped)
			source.WriteByte('"')
		}
		if depth == 0 {
			rootEnd = source.Len()
			rootHasXLinkNamespace = hasXLinkNamespace
		}
		source.WriteByte('>')
		if source.Len() > maxSVGBytes {
			return false
		}
		// Resolve styles only for each source child as serialization reaches
		// it. This keeps style collection behind the same depth and element
		// limits as output instead of walking the whole styled subtree up
		// front. The two trees have source children in the same order; the
		// styled tree can additionally contain generated pseudo-elements.
		styledIndex := 0
		for _, child := range node.Children {
			if depth+1 > 64 || child != nil && child.Type == ElementNode && elements >= maxSVGElements {
				return false
			}
			var styledChild *StyledNode
			if styled != nil {
				for styledIndex < len(styled.Children) {
					candidate := styled.Children[styledIndex]
					if candidate != nil && candidate.Node == child {
						styledIndex++
						styledChild = candidate
						break
					}
					// Skip generated children, but leave a different source
					// child for the next match. The latter also tolerates a
					// source-only child added after styling.
					if candidate == nil || candidate.Node == nil ||
						candidate.Node.Name == pseudoBeforeName || candidate.Node.Name == pseudoAfterName {
						styledIndex++
						continue
					}
					break
				}
			}
			if !writeNode(child, styledChild, depth+1) {
				return false
			}
		}
		source.WriteString("</")
		source.WriteString(name)
		source.WriteByte('>')
		return source.Len() <= maxSVGBytes
	}
	if !writeNode(n.Node, n, 0) {
		return nil
	}
	if hasXLinkHref && !rootHasXLinkNamespace {
		const declaration = ` xmlns:xlink="http://www.w3.org/1999/xlink"`
		if source.Len()+len(declaration) > maxSVGBytes {
			return nil
		}
		serialized := source.Bytes()
		withNamespace := make([]byte, 0, len(serialized)+len(declaration))
		withNamespace = append(withNamespace, serialized[:rootEnd]...)
		withNamespace = append(withNamespace, declaration...)
		withNamespace = append(withNamespace, serialized[rootEnd:]...)
		source.Reset()
		source.Write(withNamespace)
	}
	rgba, ok := parseColor(n.Style["color"])
	if !ok {
		rgba = color.RGBA{A: 255}
	}
	inherited := color.NRGBA{R: rgba.R, G: rgba.G, B: rgba.B, A: rgba.A}
	img, err := decodeSVGWithHostStyles(source.Bytes(), inherited, true,
		inlineSVGHostInheritedStyle(n.Style, n.StylePriority), n.StyleLayerOrder,
		inlineSVGHostInheritedCustomProperties(n.Style, n.StylePriority))
	if err != nil {
		return nil
	}
	return img
}

// inlineSVGHostCustomProperties carries the already-computed HTML custom
// property token streams across the inline SVG boundary. Keep this snapshot
// bounded: it is input to the SVG variable resolver, not serialized markup.
func inlineSVGHostCustomProperties(style ComputedStyle) ComputedStyle {
	const maxProperties = 256
	result := make(ComputedStyle)
	total := 0
	var properties []string
	for property := range style {
		if strings.HasPrefix(property, "--") {
			properties = append(properties, property)
		}
	}
	sort.Strings(properties)
	for _, property := range properties {
		value := style[property]
		if !strings.HasPrefix(property, "--") || !validProperty(property) ||
			value == invalidVariable || len(value) > maxSVGBytes-total {
			continue
		}
		result[property] = value
		total += len(value)
		if len(result) >= maxProperties {
			break
		}
	}
	if len(result) == 0 {
		return nil
	}
	return result
}

// Only inherited HTML custom values seed the SVG root. Direct declarations
// must instead enter the SVG author cascade with their host priority.
func inlineSVGHostInheritedCustomProperties(style ComputedStyle, priorities map[string]StylePriority) ComputedStyle {
	custom := inlineSVGHostCustomProperties(style)
	for property := range priorities {
		delete(custom, property)
	}
	return custom
}

func inlineSVGHostInheritedStyle(style ComputedStyle, priorities map[string]StylePriority) map[string]string {
	inherited := make(map[string]string)
	for _, property := range inlineSVGHostInheritedProperties {
		value := strings.TrimSpace(style[property])
		// A direct declaration on the SVG root is carried separately with its
		// cascade priority; only values inherited through the host DOM seed the
		// SVG root's inherited frame.
		if value != "" && value != invalidVariable {
			if _, direct := priorities[property]; !direct {
				inherited[property] = value
			}
		}
	}
	if len(inherited) == 0 {
		return nil
	}
	return inherited
}

// inlineSVGHostStyle bridges the HTML cascade's final declarations for the
// presentation and geometry properties the bounded SVG decoder understands.
// They remain stylesheet declarations in that decoder: SVG inline declarations
// retain inline priority, and presentation attributes remain lower priority.
func inlineSVGHostStyle(style ComputedStyle, priorities map[string]StylePriority) (string, string) {
	var declarations []string
	var encodedPriorities []string
	add := func(property string) {
		value := style[property]
		if !strings.HasPrefix(property, "--") {
			value = strings.TrimSpace(value)
		}
		priority, hasPriority := priorities[property]
		if value == "" || value == invalidVariable || !hasPriority {
			return
		}
		declarations = append(declarations, property+":"+value)
		flag := func(v bool) string {
			if v {
				return "1"
			}
			return "0"
		}
		spec := priority.Specificity
		encodedPriorities = append(encodedPriorities, property+":"+strings.Join([]string{
			flag(priority.Important), flag(priority.Inline),
			strconv.Itoa(spec[0]), strconv.Itoa(spec[1]), strconv.Itoa(spec[2]),
			strconv.Itoa(priority.Layer), strconv.Itoa(priority.Order),
		}, ","))
	}
	for _, property := range inlineSVGHostStyleProperties {
		add(property)
	}
	// The HTML engine has already computed these streams, but a direct
	// declaration on an SVG node must still compete with SVG-local rules.
	// Sort names to make the bounded serialization deterministic.
	var custom []string
	for property := range priorities {
		if strings.HasPrefix(property, "--") && validProperty(property) {
			custom = append(custom, property)
		}
	}
	sort.Strings(custom)
	bounded := inlineSVGHostCustomProperties(style)
	for _, property := range custom {
		if _, ok := bounded[property]; ok {
			add(property)
		}
	}
	return strings.Join(declarations, ";"), strings.Join(encodedPriorities, ";")
}

func parseInlineSVGHostPriorities(value string) map[string]StylePriority {
	priorities := make(map[string]StylePriority)
	for _, entry := range strings.Split(value, ";") {
		property, encoded, ok := strings.Cut(entry, ":")
		if !ok {
			continue
		}
		fields := strings.Split(encoded, ",")
		if len(fields) != 7 {
			continue
		}
		nums := [7]int{}
		valid := true
		for i, field := range fields {
			number, err := strconv.Atoi(field)
			if err != nil || number < 0 || number > maxSVGBytes {
				valid = false
				break
			}
			nums[i] = number
		}
		if !valid || (nums[0] > 1 || nums[1] > 1) {
			continue
		}
		priorities[property] = StylePriority{
			Important: nums[0] != 0, Inline: nums[1] != 0,
			Specificity: [3]int{nums[2], nums[3], nums[4]}, Layer: nums[5], Order: nums[6],
		}
	}
	return priorities
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
