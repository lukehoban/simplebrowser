package browser

import (
	"math"
	"strconv"
	"strings"
)

// style computes the cascade and inheritance without making layout decisions.
func style(document Document, fetcher *Fetcher) (StyledDocument, error) {
	sheets, inline, err := ExtractStyles(document, fetcher)
	if err != nil {
		return StyledDocument{}, err
	}
	// External CSS image URLs are relative to the stylesheet, not the page.
	for i := range sheets {
		base := sheets[i].URL
		if base == "" {
			base = document.BaseURL
		}
		for j := range sheets[i].Rules {
			for k := range sheets[i].Rules[j].Declarations {
				d := &sheets[i].Rules[j].Declarations[k]
				if d.Property == "background" || d.Property == "background-image" {
					d.Value = resolveBackgroundURL(d.Value, base)
				}
			}
		}
	}
	ua := UserAgentStylesheet()
	styles := make(map[*Node]ComputedStyle)
	rootFontSize := 16.0
	rootElementSeen := false
	var makeTree func(*Node, ComputedStyle) *StyledNode
	makeTree = func(n *Node, parent ComputedStyle) *StyledNode {
		if n.Type != ElementNode {
			result := &StyledNode{Node: n, Style: parent}
			for _, child := range n.Children {
				result.Children = append(result.Children, makeTree(child, parent))
			}
			return result
		}
		isRootElement := !rootElementSeen
		computed := cascade(n, parent, rootFontSize, isRootElement, ua, sheets, inline[n])
		if !rootElementSeen {
			rootElementSeen = true
			rootFontSize = computedFontSize(computed)
		}
		styles[n] = computed
		result := &StyledNode{Node: n, Style: computed}
		for _, child := range n.Children {
			result.Children = append(result.Children, makeTree(child, computed))
		}
		return result
	}
	root := makeTree(document.Root, nil)
	images, backgrounds := fetchImages(document, root, fetcher)
	return StyledDocument{Document: document, UserAgent: ua, Stylesheets: sheets,
		InlineStyles: inline, StyleRoot: root, Styles: styles,
		Images: images, BackgroundImages: backgrounds}, nil
}

type winningDeclaration struct {
	d                 Declaration
	important, inline bool
	origin, order     int
	spec              [3]int
}

func cascade(n *Node, parent ComputedStyle, rootFontSize float64, isRootElement bool, ua Stylesheet, sheets []Stylesheet,
	inline []Declaration) ComputedStyle {
	values := ComputedStyle{"display": "inline", "color": "black", "font-family": "serif",
		"font-size": "16px", "font-style": "normal", "font-weight": "normal",
		"line-height": "normal", "text-align": "start"}
	// border-spacing is inherited (CSS 2.1 §17.6.1); the UA table rule sets 2px.
	for _, p := range []string{"border-spacing", "color", "font-family", "font-size",
		"font-style", "font-weight", "line-height", "text-align"} {
		if parent != nil {
			values[p] = parent[p]
		}
	}
	winners := map[string]winningDeclaration{}
	order := 0
	add := func(d Declaration, spec [3]int, origin int, isInline bool) {
		for _, expanded := range expandDeclaration(d) {
			order++
			candidate := winningDeclaration{expanded, d.Important, isInline, origin, order, spec}
			old, ok := winners[expanded.Property]
			if ok && !beats(candidate, old) {
				continue
			}
			winners[expanded.Property] = candidate
		}
	}
	applySheet := func(sheet Stylesheet, origin int) {
		for _, rule := range sheet.Rules {
			for _, selector := range rule.Selectors {
				if matchesSelector(n, selector) {
					spec := specificity(selector)
					for _, d := range rule.Declarations {
						add(d, spec, origin, false)
					}
				}
			}
		}
	}
	applySheet(ua, 0)
	addPresentational(n, add)
	for _, sheet := range sheets {
		applySheet(sheet, 1)
	}
	for _, d := range inline {
		add(d, [3]int{1, 0, 0}, 1, true)
	}
	for property, winner := range winners {
		value := winner.d.Value
		if strings.EqualFold(value, "inherit") {
			if parent != nil {
				value = parent[property]
			} else {
				continue
			}
		}
		values[property] = value
	}
	parentFontSize := 16.0
	if parent != nil {
		parentFontSize = computedFontSize(parent)
	}
	values["font-size"] = formatPixels(resolveFontSize(values["font-size"], parentFontSize, rootFontSize))
	if isRootElement {
		rootFontSize = computedFontSize(values)
	}
	resolveFontRelativeValues(values, rootFontSize)
	return values
}

func computedFontSize(style ComputedStyle) float64 {
	if style == nil {
		return 16
	}
	v := classifyValue(style["font-size"])
	if (v.Kind == "length" || v.Kind == "number") && v.Number >= 0 {
		return v.Number
	}
	return 16
}

func formatPixels(value float64) string {
	return strconv.FormatFloat(value, 'f', -1, 64) + "px"
}

// resolveFontSize turns the specified font-size into the computed pixel value
// inherited by descendants. Relative font sizes use the parent's computed
// size, except rem, which uses the document element's computed size.
func resolveFontSize(value string, parentSize, rootSize float64) float64 {
	value = strings.ToLower(strings.TrimSpace(value))
	var size float64
	switch value {
	case "xx-small":
		size = 9.6
	case "x-small":
		size = 12
	case "small":
		size = 14.222222222222221
	case "medium", "":
		size = 16
	case "large":
		size = 19.2
	case "x-large":
		size = 24
	case "xx-large":
		size = 32
	case "xxx-large":
		size = 48
	case "smaller":
		size = parentSize * 5 / 6
	case "larger":
		size = parentSize * 6 / 5
	default:
		v := classifyValue(value)
		switch v.Kind {
		case "percentage":
			size = parentSize * v.Number / 100
		case "length", "number":
			switch v.Unit {
			case "em":
				size = parentSize * v.Number
			case "rem":
				size = rootSize * v.Number
			default:
				size = px(value, parentSize, math.NaN())
			}
		}
	}
	if size < 0 || size > 512 || math.IsNaN(size) || math.IsInf(size, 0) {
		return parentSize
	}
	return size
}

// CSS computed values resolve font-relative lengths against this element's
// font size. Doing this once in style computation keeps every layout path
// (blocks, tables, images, and borders) consistent.
func resolveFontRelativeValues(values ComputedStyle, rootSize float64) {
	fontSize := computedFontSize(values)
	for property, text := range values {
		if property == "font-size" {
			continue
		}
		v := classifyValue(strings.TrimSpace(text))
		if v.Kind == "length" {
			switch v.Unit {
			case "em":
				values[property] = formatPixels(v.Number * fontSize)
			case "rem":
				values[property] = formatPixels(v.Number * rootSize)
			}
		}
		if property == "line-height" && v.Kind == "percentage" {
			values[property] = formatPixels(v.Number * fontSize / 100)
		}
	}
}

func beats(a, b winningDeclaration) bool {
	// Important author declarations outrank all normal declarations. Inline is
	// only a specificity tie breaker, as it is in the author origin.
	if a.important != b.important {
		return a.important
	}
	if a.origin != b.origin {
		return a.origin > b.origin
	}
	for i := range a.spec {
		if a.spec[i] != b.spec[i] {
			return a.spec[i] > b.spec[i]
		}
	}
	if a.inline != b.inline {
		return a.inline
	}
	return a.order > b.order
}

func specificity(s Selector) [3]int {
	var result [3]int
	for _, p := range s.Parts {
		if p.ID != "" {
			result[0]++
		}
		result[1] += len(p.Classes)
		for _, pseudo := range p.PseudoClasses {
			if strings.HasPrefix(pseudo, ":") {
				result[2]++ // pseudo-elements count like type selectors
			} else {
				result[1]++
			}
		}
		if p.Tag != "" && p.Tag != "*" {
			result[2]++
		}
	}
	return result
}

func matchesSelector(n *Node, s Selector) bool {
	if len(s.Parts) == 0 {
		return false
	}
	var match func(*Node, int) bool
	match = func(node *Node, index int) bool {
		if node == nil || !matchesPart(node, s.Parts[index]) {
			return false
		}
		if index == 0 {
			return true
		}
		if s.Parts[index].Combinator == ">" {
			return match(node.Parent, index-1)
		}
		for parent := node.Parent; parent != nil; parent = parent.Parent {
			if match(parent, index-1) {
				return true
			}
		}
		return false
	}
	return match(n, len(s.Parts)-1)
}

func matchesPart(n *Node, p SelectorPart) bool {
	if p.Tag != "" && p.Tag != "*" && n.Name != p.Tag {
		return false
	}
	if p.ID != "" {
		id, ok := n.Attribute("id")
		if !ok || id.Value != p.ID {
			return false
		}
	}
	class, _ := n.Attribute("class")
	classes := map[string]bool{}
	for _, c := range strings.Fields(class.Value) {
		classes[c] = true
	}
	for _, c := range p.Classes {
		if !classes[c] {
			return false
		}
	}
	for _, pseudo := range p.PseudoClasses {
		if !matchesPseudoClass(n, pseudo) {
			return false
		}
	}
	return true
}

// matchesPseudoClass supports the static link pseudo-classes. Every link is
// treated as unvisited, and there is no user interaction, so :visited and
// dynamic pseudo-classes (:hover, :active, :focus, ...) never match. Unknown
// pseudo-classes and pseudo-elements also never match, so an unsupported
// selector can only style fewer elements, never more.
func matchesPseudoClass(n *Node, pseudo string) bool {
	switch pseudo {
	case "link", "any-link":
		if n.Name != "a" && n.Name != "area" {
			return false
		}
		_, ok := n.Attribute("href")
		return ok
	}
	return false
}

func expandDeclaration(d Declaration) []Declaration {
	if d.Property == "background" {
		return expandBackground(d)
	}
	if d.Property == "border" {
		result := make([]Declaration, 0, 4)
		for _, side := range []string{"top", "right", "bottom", "left"} {
			result = append(result, Declaration{Property: "border-" + side, Value: d.Value, Values: d.Values, Important: d.Important})
		}
		return result
	}
	if d.Property != "margin" && d.Property != "padding" && d.Property != "border-width" &&
		d.Property != "border-color" && d.Property != "border-style" {
		return []Declaration{d}
	}
	parts := strings.Fields(d.Value)
	if len(parts) < 1 || len(parts) > 4 {
		return []Declaration{d}
	}
	if len(parts) == 1 {
		parts = []string{parts[0], parts[0], parts[0], parts[0]}
	}
	if len(parts) == 2 {
		parts = []string{parts[0], parts[1], parts[0], parts[1]}
	}
	if len(parts) == 3 {
		parts = []string{parts[0], parts[1], parts[2], parts[1]}
	}
	names := []string{d.Property + "-top", d.Property + "-right", d.Property + "-bottom", d.Property + "-left"}
	result := make([]Declaration, 4)
	for i := range result {
		result[i] = Declaration{Property: names[i], Value: parts[i], Important: d.Important}
	}
	return result
}

func addPresentational(n *Node, add func(Declaration, [3]int, int, bool)) {
	for _, a := range n.Attributes {
		switch strings.ToLower(a.Name) {
		case "bgcolor":
			add(Declaration{Property: "background-color", Value: a.Value}, [3]int{}, 1, false)
		case "width":
			add(Declaration{Property: "width", Value: cssDimension(a.Value)}, [3]int{}, 1, false)
		case "height":
			add(Declaration{Property: "height", Value: cssDimension(a.Value)}, [3]int{}, 1, false)
		case "align":
			add(Declaration{Property: "text-align", Value: strings.ToLower(a.Value)}, [3]int{}, 1, false)
			if n.Name == "table" && strings.EqualFold(strings.TrimSpace(a.Value), "center") {
				add(Declaration{Property: "margin-left", Value: "auto"}, [3]int{}, 1, false)
				add(Declaration{Property: "margin-right", Value: "auto"}, [3]int{}, 1, false)
			}
		case "cellpadding":
			add(Declaration{Property: "padding", Value: cssDimension(a.Value)}, [3]int{}, 1, false)
		case "cellspacing":
			add(Declaration{Property: "border-spacing", Value: cssDimension(a.Value)}, [3]int{}, 1, false)
		case "color":
			if n.Name == "font" {
				add(Declaration{Property: "color", Value: a.Value}, [3]int{}, 1, false)
			}
		case "size":
			if n.Name == "font" {
				add(Declaration{Property: "font-size", Value: fontSizeAttribute(a.Value)}, [3]int{}, 1, false)
			}
		}
	}
	if n.Name == "center" {
		add(Declaration{Property: "text-align", Value: "center"}, [3]int{}, 1, false)
	}
}

func cssDimension(s string) string {
	if _, err := strconv.ParseFloat(s, 64); err == nil {
		return s + "px"
	}
	return s
}
func fontSizeAttribute(s string) string {
	if n, err := strconv.Atoi(s); err == nil {
		return strconv.Itoa(n) + "px"
	}
	return s
}
