package browser

import (
	"image"
	"math"
	"strconv"
	"strings"

	"golang.org/x/text/language"
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
	styled := computeStyles(StyledDocument{Document: document, UserAgent: UserAgentStylesheet(),
		Stylesheets: sheets, InlineStyles: inline}, image.Pt(placeholderWidth, placeholderHeight))
	styled.Images, styled.BackgroundImages = fetchImages(document, styled.StyleRoot, fetcher)
	return styled, nil
}

// computeStyles rebuilds computed values from the loaded cascade without
// fetching resources or mutating the original tree. A new viewport can change
// inherited font sizes and hence em/rem/ex/ch throughout the document.
func computeStyles(document StyledDocument, viewport image.Point) StyledDocument {
	styles := make(map[*Node]ComputedStyle)
	rootFontSize := 16.0
	rootElementSeen := false
	var makeTree func(*Node, ComputedStyle) *StyledNode
	makeTree = func(n *Node, parent ComputedStyle) *StyledNode {
		if n.Type != ElementNode {
			// Text needs its parent's font and color, but position is not
			// inherited. In particular, a text leaf is never itself an
			// absolutely positioned box.
			var textStyle ComputedStyle
			if parent != nil {
				textStyle = cloneStyle(parent)
				delete(textStyle, "position")
			}
			result := &StyledNode{Node: n, Style: textStyle}
			for _, child := range n.Children {
				result.Children = append(result.Children, makeTree(child, textStyle))
			}
			return result
		}
		isRootElement := !rootElementSeen
		computed := cascade(n, parent, rootFontSize, isRootElement, document.UserAgent,
			document.Stylesheets, document.InlineStyles[n], viewport)
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
	document.StyleRoot = makeTree(document.Document.Root, nil)
	document.Styles = styles
	document.styleViewport = viewport
	return document
}

type winningDeclaration struct {
	d                 Declaration
	important, inline bool
	origin, order     int
	spec              [3]int
}

func cascade(n *Node, parent ComputedStyle, rootFontSize float64, isRootElement bool, ua Stylesheet, sheets []Stylesheet,
	inline []Declaration, viewport image.Point) ComputedStyle {
	values := ComputedStyle{"display": "inline", "color": "black", "font-family": "serif",
		"font-size": "16px", "font-style": "normal", "font-variant": "normal", "font-weight": "normal",
		"lang": language.Und.String(), "line-height": "normal", "text-align": "start"}
	// border-spacing is inherited (CSS 2.1 §17.6.1); the UA table rule sets 2px.
	for _, p := range []string{"border-spacing", "color", "font-family", "font-size",
		"font-style", "font-variant", "font-weight", "lang", "line-height", "text-align"} {
		if parent != nil {
			values[p] = parent[p]
		}
	}
	// The document language is metadata inherited from HTML, not a CSS
	// declaration. Invalid or empty tags explicitly reset it to Unicode root.
	if attr, ok := n.Attribute("lang"); ok {
		values["lang"] = normalizedLanguage(attr.Value)
	} else if attr, ok := n.Attribute("xml:lang"); ok {
		values["lang"] = normalizedLanguage(attr.Value)
	}
	winners := map[string]winningDeclaration{}
	order := 0
	add := func(d Declaration, spec [3]int, origin int, isInline bool) {
		for _, expanded := range expandDeclaration(d) {
			if expanded.Property == "color" && !strings.EqualFold(expanded.Value, "inherit") {
				// Invalid color tokens must not win the cascade and then paint
				// black; leave the lower-priority declaration or inherited value.
				if _, ok := parseColor(strings.ToLower(expanded.Value)); !ok {
					continue
				}
			}
			// The element language is HTML/XML metadata, not a CSS property.
			if expanded.Property == "lang" {
				continue
			}
			if expanded.Property == "font-variant" {
				switch strings.ToLower(strings.TrimSpace(expanded.Value)) {
				case "normal", "small-caps", "inherit":
				default:
					continue
				}
			}
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
	// Resolve viewport units before font-size, so font-relative lengths and
	// inherited values use the viewport-dependent computed font size.
	resolveViewportRelativeValues(values, viewport)
	// ex and ch in font-size use the parent's font, like em (CSS Values 4 §6.1).
	values["font-size"] = formatPixels(resolveFontSize(values["font-size"], parentFontSize, rootFontSize, ratiosFor(parent)))
	if isRootElement {
		rootFontSize = computedFontSize(values)
	}
	resolveFontRelativeValues(values, rootFontSize)
	return values
}

func normalizedLanguage(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return language.Und.String()
	}
	tag, err := language.Parse(value)
	if err != nil {
		return language.Und.String()
	}
	return tag.String()
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

// Viewport-percentage lengths use the layout viewport, never a containing
// block or the document's content height. This static renderer has no browser
// chrome or changing visual viewport, so small, large, and dynamic variants
// deliberately share that viewport. Keeping the mapping here makes it
// straightforward to split when the renderer gains those viewport concepts.
// Keep fractions until used-value rounding in layout, just as for
// font-relative computed lengths.
func resolveViewportRelativeValues(values ComputedStyle, viewport image.Point) {
	for property, text := range values {
		values[property] = resolveLengthTokens(property, text, func(v CSSValue) (string, bool) {
			basis, ok := viewportLengthBasis(v.Unit, viewport)
			if !ok {
				return "", false
			}
			return formatPixels(v.Number * float64(basis) / 100), true
		})
	}
}

func viewportLengthBasis(unit string, viewport image.Point) (int, bool) {
	switch unit {
	case "vw", "svw", "lvw", "dvw":
		return viewport.X, true
	case "vh", "svh", "lvh", "dvh":
		return viewport.Y, true
	case "vmin", "svmin", "lvmin", "dvmin":
		return min(viewport.X, viewport.Y), true
	case "vmax", "svmax", "lvmax", "dvmax":
		return max(viewport.X, viewport.Y), true
	default:
		return 0, false
	}
}

// resolveFontSize turns the specified font-size into the computed pixel value
// inherited by descendants. Relative font sizes use the parent's computed
// size, except rem, which uses the document element's computed size. ex and ch
// use parentRatios, the metrics of the parent's selected face.
func resolveFontSize(value string, parentSize, rootSize float64, parentRatios fontRatios) float64 {
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
			case "ex":
				size = parentSize * parentRatios.ex * v.Number
			case "ch":
				size = parentSize * parentRatios.ch * v.Number
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
// font size (and, for ex/ch, this element's selected face). Doing this once
// in style computation keeps every layout path (blocks, tables, images, and
// borders) consistent.
func resolveFontRelativeValues(values ComputedStyle, rootSize float64) {
	fontSize := computedFontSize(values)
	ratios := ratiosFor(values)
	for property, text := range values {
		if property == "font-size" {
			continue
		}
		values[property] = resolveLengthTokens(property, text, func(v CSSValue) (string, bool) {
			switch v.Unit {
			case "em":
				return formatPixels(v.Number * fontSize), true
			case "rem":
				return formatPixels(v.Number * rootSize), true
			case "ex":
				return formatPixels(v.Number * fontSize * ratios.ex), true
			case "ch":
				return formatPixels(v.Number * fontSize * ratios.ch), true
			}
			if property == "line-height" && v.Kind == "percentage" {
				return formatPixels(v.Number * fontSize / 100), true
			}
			return "", false
		})
	}
}

// Only properties whose compound grammar actually contains lengths are scanned.
// Other properties retain their existing single-value behavior (notably URLs
// and CSS functions, whose internals must not be rewritten as free lengths).
func compoundLengthProperty(property string) bool {
	switch property {
	case "border-spacing", "background-position", "background-size", "border",
		"border-top", "border-right", "border-bottom", "border-left":
		return true
	}
	return false
}

// resolveLengthTokens preserves all separators and opaque quoted/function
// tokens, replacing only complete top-level CSS length tokens. This also
// handles comma-separated background layers and unexpanded border shorthands.
func resolveLengthTokens(property, text string, resolve func(CSSValue) (string, bool)) string {
	single := strings.TrimSpace(text)
	if !compoundLengthProperty(property) {
		v := classifyValue(single)
		if replacement, ok := resolve(v); ok && (v.Kind == "length" || v.Kind == "percentage") {
			return replacement
		}
		return text
	}
	var result strings.Builder
	for i := 0; i < len(text); {
		if cssSpace(text[i]) || text[i] == ',' || text[i] == '/' {
			result.WriteByte(text[i])
			i++
			continue
		}
		start := i
		depth := 0
		var quote byte
		for i < len(text) {
			c := text[i]
			if quote != 0 {
				if c == '\\' && i+1 < len(text) {
					i += 2
					continue
				}
				if c == quote {
					quote = 0
				}
			} else {
				switch c {
				case '\'', '"':
					quote = c
				case '(':
					depth++
				case ')':
					if depth > 0 {
						depth--
					}
				}
				if depth == 0 && (cssSpace(c) || c == ',' || c == '/') {
					break
				}
			}
			i++
		}
		token := text[start:i]
		v := classifyValue(token)
		if v.Kind == "length" || v.Kind == "percentage" {
			if replacement, ok := resolve(v); ok {
				result.WriteString(replacement)
				continue
			}
		}
		result.WriteString(token)
	}
	return result.String()
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
		for _, negation := range p.Negations {
			var maximum [3]int
			for _, argument := range negation {
				spec := specificity(argument)
				for i := range spec {
					if spec[i] > maximum[i] {
						maximum = spec
						break
					}
					if spec[i] < maximum[i] {
						break
					}
				}
			}
			for i := range result {
				result[i] += maximum[i]
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
	if n == nil || n.Type != ElementNode {
		return false
	}
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
	for _, negation := range p.Negations {
		for _, argument := range negation {
			if matchesSelector(n, argument) {
				return false
			}
		}
	}
	return true
}

// matchesPseudoClass supports last-child and static link pseudo-classes. Every link is
// treated as unvisited, and there is no user interaction, so :visited and
// dynamic pseudo-classes (:hover, :active, :focus, ...) never match. Unknown
// pseudo-classes and pseudo-elements also never match, so an unsupported
// selector can only style fewer elements, never more.
func matchesPseudoClass(n *Node, pseudo string) bool {
	switch pseudo {
	case "last-child":
		if n.Type != ElementNode {
			return false
		}
		if n.Parent == nil {
			return true // Selectors 4 does not require a parent.
		}
		for i := len(n.Parent.Children) - 1; i >= 0; i-- {
			if sibling := n.Parent.Children[i]; sibling.Type == ElementNode {
				return sibling == n
			}
		}
		return false
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
	if d.Property == "font" {
		return expandFont(d)
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

type fontToken struct {
	text   string
	quoted bool
}

// systemFontSizes maps CSS system fonts to fixed sizes. All use the embedded
// Go Sans face, never an OS-dependent UI font. These sizes are renderer choices,
// not claims about a particular platform's system font metrics.
var systemFontSizes = map[string]string{
	"caption": "14px", "icon": "12px", "menu": "14px",
	"message-box": "14px", "small-caption": "12px", "status-bar": "12px",
}

// expandFont parses the CSS 2 font shorthand. The font-stretch component is
// deliberately unsupported; rejecting the whole declaration prevents a
// malformed shorthand from partially changing style.
func expandFont(d Declaration) []Declaration {
	if size, ok := systemFontSizes[strings.ToLower(strings.TrimSpace(d.Value))]; ok {
		return fontDeclarations(d, "normal", "normal", "normal", size, "normal", "sans-serif")
	}
	tokens, ok := tokenizeFont(d.Value)
	if !ok || len(tokens) < 2 {
		return nil
	}

	style, variant, weight := "normal", "normal", "normal"
	seenStyle, seenVariant, seenWeight, optionalCount := false, false, false, 0
	sizeIndex := -1
	for i, token := range tokens {
		lower := strings.ToLower(token.text)
		if !token.quoted && validFontSize(lower) {
			sizeIndex = i
			break
		}
		if token.quoted || token.text == "/" || token.text == "," {
			return nil
		}
		optionalCount++
		if optionalCount > 3 {
			return nil
		}
		switch {
		case lower == "italic" || lower == "oblique":
			if seenStyle {
				return nil
			}
			style, seenStyle = lower, true
		case lower == "small-caps":
			if seenVariant {
				return nil
			}
			variant, seenVariant = lower, true
		case validFontWeight(lower):
			if seenWeight {
				return nil
			}
			weight, seenWeight = lower, true
		case lower == "normal":
			// "normal" is shared by style, variant, and weight. Its exact
			// assignment is immaterial because all three initial values match.
		default:
			return nil
		}
	}
	if sizeIndex < 0 {
		return nil
	}

	size := tokens[sizeIndex].text
	index := sizeIndex + 1
	lineHeight := "normal"
	if index < len(tokens) && tokens[index].text == "/" {
		index++
		if index >= len(tokens) || tokens[index].quoted || !validLineHeight(tokens[index].text) {
			return nil
		}
		lineHeight = tokens[index].text
		index++
	}
	if index >= len(tokens) || !validFontFamily(tokens[index:]) {
		return nil
	}
	family := joinFontFamily(tokens[index:])

	return fontDeclarations(d, style, variant, weight, size, lineHeight, family)
}

func fontDeclarations(d Declaration, style, variant, weight, size, lineHeight, family string) []Declaration {
	property := func(name, value string) Declaration {
		return Declaration{Property: name, Value: value, Values: parseValues(value), Important: d.Important}
	}
	return []Declaration{
		property("font-style", style),
		property("font-variant", variant),
		property("font-weight", weight),
		property("font-size", size),
		property("line-height", lineHeight),
		property("font-family", family),
	}
}

func tokenizeFont(value string) ([]fontToken, bool) {
	var tokens []fontToken
	for i := 0; i < len(value); {
		if cssSpace(value[i]) {
			i++
			continue
		}
		if value[i] == '/' || value[i] == ',' {
			tokens = append(tokens, fontToken{text: value[i : i+1]})
			i++
			continue
		}
		start := i
		if value[i] == '"' || value[i] == '\'' {
			quote := value[i]
			i++
			closed := false
			for i < len(value) {
				if value[i] == '\\' && i+1 < len(value) {
					i += 2
				} else if value[i] == quote {
					i++
					closed = true
					break
				} else {
					i++
				}
			}
			if !closed {
				return nil, false
			}
			tokens = append(tokens, fontToken{text: value[start:i], quoted: true})
			continue
		}
		for i < len(value) && !cssSpace(value[i]) && value[i] != '/' && value[i] != ',' {
			i++
		}
		tokens = append(tokens, fontToken{text: value[start:i]})
	}
	return tokens, len(tokens) > 0
}

func validFontSize(value string) bool {
	switch strings.ToLower(value) {
	case "xx-small", "x-small", "small", "medium", "large", "x-large", "xx-large",
		"xxx-large", "smaller", "larger":
		return true
	}
	v := classifyValue(value)
	return (v.Kind == "length" || v.Kind == "percentage" || v.Kind == "number" && v.Number == 0) && v.Number >= 0
}

func validLineHeight(value string) bool {
	if strings.EqualFold(value, "normal") {
		return true
	}
	v := classifyValue(value)
	return (v.Kind == "length" || v.Kind == "percentage" || v.Kind == "number") && v.Number >= 0
}

func validFontWeight(value string) bool {
	switch value {
	case "bold", "bolder", "lighter":
		return true
	}
	n, err := strconv.Atoi(value)
	return err == nil && n >= 100 && n <= 900 && n%100 == 0
}

func validFontFamily(tokens []fontToken) bool {
	expectName := true
	words := 0
	quotedGroup := false
	for _, token := range tokens {
		if token.text == "/" {
			return false
		}
		if token.text == "," {
			if expectName || words == 0 {
				return false
			}
			expectName, words, quotedGroup = true, 0, false
			continue
		}
		if token.quoted {
			if !expectName || len(token.text) <= 2 {
				return false
			}
			expectName, words, quotedGroup = false, 1, true
			continue
		}
		if !validFontFamilyIdentifier(token.text) || quotedGroup {
			return false
		}
		expectName = false
		words++
	}
	return !expectName && words > 0
}

func validFontFamilyIdentifier(value string) bool {
	if value == "" || strings.ContainsAny(value, `"'()!`) {
		return false
	}
	if !(isLetter(value[0]) || value[0] == '_' ||
		value[0] == '-' && len(value) > 1 && !(value[1] >= '0' && value[1] <= '9')) {
		return false
	}
	for i := range value {
		if !cssIdent(value[i]) {
			return false
		}
	}
	switch strings.ToLower(value) {
	case "inherit", "initial", "unset", "revert", "revert-layer":
		return false
	}
	return true
}

func joinFontFamily(tokens []fontToken) string {
	var result strings.Builder
	for i, token := range tokens {
		if token.text == "," {
			result.WriteString(", ")
		} else {
			if i > 0 && tokens[i-1].text != "," {
				result.WriteByte(' ')
			}
			result.WriteString(token.text)
		}
	}
	return result.String()
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
