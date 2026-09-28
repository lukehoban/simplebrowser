package browser

import (
	"image"
	"math"
	"regexp"
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
				if d.Property == "background" || d.Property == "background-image" ||
					canonicalMaskProperty(d.Property) == "mask" || canonicalMaskProperty(d.Property) == "mask-image" {
					d.Value = resolveBackgroundURL(d.Value, base)
				} else if strings.HasPrefix(d.Property, "--") {
					// A custom property retains the URL's declaration base when
					// substituted on another element or into a fallback. Resolve
					// each url() before the token stream enters inheritance.
					d.Value = resolveCustomPropertyURLs(d.Value, base)
				}
			}
		}
	}
	styled := computeStyles(StyledDocument{Document: document, UserAgent: UserAgentStylesheet(),
		Stylesheets: sheets, InlineStyles: inline}, image.Pt(placeholderWidth, placeholderHeight))
	styled.Images, styled.BackgroundImages, styled.MaskImages = fetchImages(document, styled.StyleRoot, fetcher)
	return styled, nil
}

// computeStyles rebuilds computed values from the loaded cascade without
// fetching resources or mutating the original tree. A new viewport can change
// inherited font sizes and hence em/rem/ex/ch throughout the document.
func computeStyles(document StyledDocument, viewport image.Point) StyledDocument {
	// Conditional layer declarations establish order only while their
	// conditions match. Recompute against this viewport on a copy because a
	// later layout pass can restyle the same loaded document at another size.
	document.Stylesheets = cloneStylesheetsForLayerOrder(document.Stylesheets)
	assignLayerOrder(document.Stylesheets, viewport)
	styleLayerOrder := layerOrders(document.Stylesheets, viewport)
	styles := make(map[*Node]ComputedStyle)
	pseudoNodes := document.pseudoNodes
	if pseudoNodes == nil {
		pseudoNodes = map[pseudoKey]*Node{}
	}
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
			result := &StyledNode{Node: n, Style: textStyle, StyleLayerOrder: styleLayerOrder}
			for _, child := range n.Children {
				result.Children = append(result.Children, makeTree(child, textStyle))
			}
			return result
		}
		isRootElement := !rootElementSeen
		stylePriority := map[string]winningDeclaration{}
		computed := cascade(n, parent, rootFontSize, isRootElement, document.UserAgent,
			document.Stylesheets, document.InlineStyles[n], viewport, "", stylePriority)
		if !rootElementSeen {
			rootElementSeen = true
			rootFontSize = computedFontSize(computed)
		}
		styles[n] = computed
		result := &StyledNode{Node: n, Style: computed, StylePriority: exportedStylePriorities(stylePriority),
			StyleLayerOrder: styleLayerOrder}
		// A generated box is the originating element's first or last child
		// (CSS Content 3 §2), inheriting from it like a real child element.
		if before := pseudoStyledNode(n, "before", computed, rootFontSize, document, viewport, pseudoNodes); before != nil {
			styles[before.Node] = before.Style
			result.Children = append(result.Children, before)
		}
		for _, child := range n.Children {
			result.Children = append(result.Children, makeTree(child, computed))
		}
		if after := pseudoStyledNode(n, "after", computed, rootFontSize, document, viewport, pseudoNodes); after != nil {
			styles[after.Node] = after.Style
			result.Children = append(result.Children, after)
		}
		return result
	}
	document.StyleRoot = makeTree(document.Document.Root, nil)
	document.Styles = styles
	document.pseudoNodes = pseudoNodes
	document.styleViewport = viewport
	return document
}

type winningDeclaration struct {
	d                         Declaration
	important, inline         bool
	presentational            bool
	validateAfterSubstitution bool
	origin, order             int
	layer                     int
	spec                      [3]int
}

// cascade computes one element's values, or those of one of its generated
// boxes when pseudo is "before" or "after". A generated box has no inline
// style attribute and no presentational attributes of its own.
func cascade(n *Node, parent ComputedStyle, rootFontSize float64, isRootElement bool, ua Stylesheet, sheets []Stylesheet,
	inline []Declaration, viewport image.Point, pseudo string, priorityOutput ...map[string]winningDeclaration) ComputedStyle {
	values := ComputedStyle{"display": "inline", "color": "black", "font-family": "serif",
		"font-size": "16px", "font-style": "normal", "font-variant": "normal", "font-weight": "normal",
		"lang": language.Und.String(), "line-height": "normal", "text-align": "start", "visibility": "visible",
		"white-space": "normal"}
	// border-spacing is inherited (CSS 2.1 §17.6.1); the UA table rule sets 2px.
	// visibility is inherited (CSS 2.1 §11.2), so a hidden subtree stays hidden
	// unless a descendant sets visibility:visible. white-space is inherited
	// (CSS Text 3 §3), so nowrap reaches text inside nested inline elements.
	for _, p := range []string{"border-spacing", "color", "font-family", "font-size",
		"font-style", "font-variant", "font-weight", "lang", "line-height", "text-align", "visibility",
		"white-space"} {
		if parent != nil {
			values[p] = parent[p]
		}
	}
	for _, p := range inlineSVGHostInheritedProperties {
		if parent != nil {
			if value, ok := parent[p]; ok {
				values[p] = value
			}
		}
	}
	for p, v := range parent {
		if strings.HasPrefix(p, "--") {
			values[p] = v
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
	var candidates []winningDeclaration
	unlayeredOrder := layerCount(sheets, viewport)
	add := func(d Declaration, spec [3]int, origin int, isInline bool, layer int, presentational bool) {
		order++
		candidates = append(candidates, winningDeclaration{
			d: d, important: d.Important, inline: isInline, origin: origin, order: order,
			layer: layer, presentational: presentational, spec: spec,
		})
	}
	consider := func(candidate winningDeclaration) {
		candidate.d.Property = canonicalMaskProperty(candidate.d.Property)
		candidate.d.Property = canonicalLogicalSizeProperty(candidate.d.Property)
		// Box shorthands are a single declaration: an invalid component must
		// not apply its valid siblings to the cascade.
		switch candidate.d.Property {
		case "margin", "padding", "border-width", "border-radius":
			if cssWideKeyword(candidate.d.Value) == "" && candidate.d.Value != invalidVariable &&
				!validSubstitutedDeclaration(candidate.d.Property, candidate.d.Value) {
				if !candidate.validateAfterSubstitution {
					return
				}
				candidate.d.Value = invalidVariable
			}
		}
		if candidate.validateAfterSubstitution && candidate.d.Property == "background" &&
			candidate.d.Value != invalidVariable && cssWideKeyword(candidate.d.Value) == "" &&
			!validBackground(candidate.d.Value) {
			candidate.d.Value = invalidVariable
		}
		if candidate.validateAfterSubstitution && candidate.d.Property == "mask" &&
			candidate.d.Value != invalidVariable && cssWideKeyword(candidate.d.Value) == "" &&
			!validMask(candidate.d.Value) {
			candidate.d.Value = invalidVariable
		}
		expandedDeclarations := []Declaration(nil)
		if keyword := cssWideKeyword(candidate.d.Value); keyword != "" {
			expandedDeclarations = expandCSSWideDeclaration(candidate.d, keyword)
		} else {
			expandedDeclarations = expandDeclaration(candidate.d)
		}
		// A substituted invalid font shorthand is still the cascade winner.
		// expandFont rejects malformed shorthands by returning no longhands;
		// represent that winner as invalid for every longhand instead.
		if candidate.validateAfterSubstitution && candidate.d.Property == "font" &&
			len(expandedDeclarations) == 0 {
			expandedDeclarations = invalidLonghands(candidate.d,
				"font-size", "font-family", "font-style", "font-variant", "font-weight", "line-height")
		}
		for _, expanded := range expandedDeclarations {
			invalid := expanded.Value == invalidVariable
			if !invalid && strings.HasPrefix(expanded.Property, "border-") && strings.HasSuffix(expanded.Property, "-radius") &&
				cssWideKeyword(expanded.Value) == "" && !validCornerRadius(expanded.Value) {
				if candidate.validateAfterSubstitution {
					expanded.Value = invalidVariable
					invalid = true
				} else {
					continue
				}
			}
			if !invalid && !validCalcDeclaration(expanded.Property, expanded.Value) {
				if candidate.validateAfterSubstitution {
					expanded.Value = invalidVariable
					invalid = true
				} else {
					// A malformed calc() is invalid at parse time, so it does
					// not displace a lower-priority valid declaration.
					continue
				}
			}
			if candidate.validateAfterSubstitution && !invalid &&
				!validSubstitutedDeclaration(expanded.Property, expanded.Value) {
				expanded.Value = invalidVariable
				invalid = true
			}
			if !candidate.validateAfterSubstitution && expanded.Property == "color" && !strings.EqualFold(expanded.Value, "inherit") {
				// Invalid color tokens must not win the cascade and then paint
				// black; leave the lower-priority declaration or inherited value.
				if _, ok := parseColor(strings.ToLower(expanded.Value)); !ok && !invalid {
					continue
				}
			}
			// The element language is HTML/XML metadata, not a CSS property.
			if expanded.Property == "lang" {
				continue
			}
			// An unrecognized visibility keyword is dropped at parse time, so
			// the inherited or lower-priority value still applies.
			if !candidate.validateAfterSubstitution && expanded.Property == "visibility" && !invalid &&
				cssWideKeyword(expanded.Value) == "" && !supportValidators["visibility"](strings.TrimSpace(expanded.Value)) {
				continue
			}
			if !candidate.validateAfterSubstitution && expanded.Property == "font-variant" && !invalid {
				switch strings.ToLower(strings.TrimSpace(expanded.Value)) {
				case "normal", "small-caps", "inherit":
				default:
					continue
				}
			}
			candidate.d = expanded
			old, ok := winners[expanded.Property]
			if ok && !beats(candidate, old) {
				continue
			}
			winners[expanded.Property] = candidate
		}
	}
	applySheet := func(sheet Stylesheet, origin int) {
		for _, rule := range sheet.Rules {
			if rule.Media != "" && !mediaQueryMatches(rule.Media, viewport) {
				continue
			}
			for _, selector := range rule.Selectors {
				which, supported := selectorPseudoElement(selector)
				if !supported || which != pseudo {
					continue
				}
				// The pseudo-element still counts toward specificity, so take
				// it before stripping it for matching (Selectors 4 §17).
				spec := specificity(selector)
				if which != "" {
					selector = selectorWithoutPseudoElement(selector)
				}
				if matchesSelector(n, selector) {
					for _, d := range rule.Declarations {
						layer := rule.LayerOrder
						if rule.Layer == "" && origin == 0 {
							layer = 0
						}
						add(d, spec, origin, false, layer, false)
					}
				}
			}
		}
	}
	applySheet(ua, 0)
	if pseudo == "" {
		// Presentational hints and the style attribute belong to the element,
		// not to its generated boxes.
		addPresentational(n, func(d Declaration, spec [3]int, origin int, inline bool) {
			add(d, spec, origin, inline, -1, true)
		})
	}
	for _, sheet := range sheets {
		applySheet(sheet, 1)
	}
	if pseudo == "" {
		for _, d := range inline {
			add(d, [3]int{1, 0, 0}, 1, true, unlayeredOrder, false)
		}
	}
	// Custom properties cascade first, but retain their raw token streams:
	// a child may replace a variable referenced by an inherited declaration.
	for _, candidate := range candidates {
		if strings.HasPrefix(candidate.d.Property, "--") {
			consider(candidate)
		}
	}
	if len(priorityOutput) > 0 && priorityOutput[0] != nil {
		for property, winner := range winners {
			if winner.d.Value != invalidVariable {
				priorityOutput[0][property] = winner
			}
		}
	}
	for p, winner := range winners {
		switch winner.d.Value {
		case "inherit", "unset":
			// The inherited computed token stream was copied above.
		case "initial":
			values[p] = invalidVariable
		default:
			values[p] = winner.d.Value
		}
	}
	// Custom properties inherit their computed (already substituted) values,
	// not their unresolved declarations. Compute them before ordinary values.
	rawCustom := cloneStyle(values)
	for p, v := range values {
		if strings.HasPrefix(p, "--") {
			resolved, ok := substituteVars(v, rawCustom, map[string]bool{p: true})
			if !ok {
				values[p] = invalidVariable
			} else {
				values[p] = resolved
			}
		}
	}
	clear(winners)
	for _, candidate := range candidates {
		if strings.HasPrefix(candidate.d.Property, "--") {
			continue
		}
		if containsVarFunction(candidate.d.Value) {
			candidate.validateAfterSubstitution = true
			resolved, ok := substituteVars(candidate.d.Value, values, nil)
			if ok && strings.TrimSpace(resolved) != "" {
				candidate.d.Value = strings.TrimSpace(resolved)
				candidate.d.Values = parseValues(candidate.d.Value)
			} else {
				candidate.d.Value = invalidVariable
			}
		}
		consider(candidate)
	}
	for property, winner := range winners {
		value := winner.d.Value
		if value == invalidVariable {
			// Invalid at computed-value time behaves as unset. The inherited
			// value or compact initial representation is already in values.
			continue
		}
		keyword := cssWideKeyword(value)
		switch keyword {
		case "inherit":
			inheritComputedValue(values, parent, property)
		case "initial":
			setInitialComputedValue(values, property)
		case "unset", "revert", "revert-layer":
			// Revert keywords are not implemented; treat them as invalid at
			// computed-value time rather than exposing the keyword. Invalid
			// substituted values have the same unset behavior.
			unsetComputedValue(values, parent, property)
		default:
			values[property] = value
		}
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
	normalizeCalcValues(values, viewport.X, viewport.Y, rootFontSize)
	if len(priorityOutput) > 0 && priorityOutput[0] != nil {
		for property, winner := range winners {
			if winner.d.Value != invalidVariable {
				priorityOutput[0][property] = winner
			}
		}
	}
	return values
}

func exportedStylePriorities(winners map[string]winningDeclaration) map[string]StylePriority {
	result := make(map[string]StylePriority)
	add := func(property string, winner winningDeclaration) {
		result[property] = StylePriority{
			Important: winner.important, Inline: winner.inline, Specificity: winner.spec,
			Layer: winner.layer, Order: winner.order,
		}
	}
	for _, property := range inlineSVGHostStyleProperties {
		if winner, ok := winners[property]; ok {
			add(property, winner)
		}
	}
	for property, winner := range winners {
		if strings.HasPrefix(property, "--") {
			add(property, winner)
		}
	}
	if len(result) == 0 {
		return nil
	}
	return result
}

// The current layout engine only supports horizontal writing modes, where
// the logical size properties map to their physical counterparts.
func canonicalLogicalSizeProperty(property string) string {
	switch property {
	case "inline-size":
		return "width"
	case "block-size":
		return "height"
	default:
		return property
	}
}

// validSubstitutedDeclaration applies the same grammar used by @supports to
// values whose var() references have been resolved. Unsupported properties
// remain outside this renderer's validation surface, as they do in @supports.
func validSubstitutedDeclaration(property, value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "inherit", "initial", "unset":
		return true
	}
	if calcLengthProperty(property) && containsMathFunction(value) {
		return validCalcDeclaration(property, value)
	}
	if validate, ok := supportValidators[property]; ok {
		return validate(strings.TrimSpace(value))
	}
	return true
}

func cssWideKeyword(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "inherit", "initial", "unset", "revert", "revert-layer":
		return strings.ToLower(strings.TrimSpace(value))
	}
	return ""
}

// expandCSSWideDeclaration preserves a CSS-wide keyword until it reaches each
// longhand. Shorthand parsers normally reject or consume these as ordinary
// tokens, but CSS-wide keywords apply atomically to every shorthand component.
func expandCSSWideDeclaration(d Declaration, keyword string) []Declaration {
	var properties []string
	switch d.Property {
	case "font":
		properties = []string{"font-style", "font-variant", "font-weight", "font-size", "line-height", "font-family"}
	case "background":
		properties = []string{"background-color", "background-image", "background-repeat", "background-position", "background-size"}
	case "mask":
		properties = maskLonghands
	case "margin", "padding", "border-width", "border-color", "border-style":
		properties = []string{d.Property + "-top", d.Property + "-right", d.Property + "-bottom", d.Property + "-left"}
	case "border":
		properties = []string{"border-top", "border-right", "border-bottom", "border-left"}
	case "border-radius":
		for _, corner := range radiusCorners {
			properties = append(properties, "border-"+corner+"-radius")
		}
	default:
		properties = []string{d.Property}
	}
	if keyword == "revert" || keyword == "revert-layer" {
		return invalidLonghands(d, properties...)
	}
	result := make([]Declaration, 0, len(properties))
	for _, property := range properties {
		result = append(result, Declaration{Property: property, Value: keyword, Important: d.Important})
	}
	return result
}

var inheritedCSSProperties = map[string]bool{
	"border-spacing": true, "color": true, "font-family": true, "font-size": true,
	"font-style": true, "font-variant": true, "font-weight": true, "line-height": true,
	"text-align": true, "visibility": true, "white-space": true,
	"fill": true, "fill-rule": true, "fill-opacity": true,
	"stroke": true, "stroke-opacity": true, "stroke-width": true,
	"stroke-linecap": true, "stroke-linejoin": true, "stroke-miterlimit": true,
	"stroke-dasharray": true, "stroke-dashoffset": true,
	"stop-color": true, "stop-opacity": true,
}

var initialComputedValues = map[string]string{
	"display": "inline", "color": "black", "font-family": "serif", "font-size": "16px",
	"font-style": "normal", "font-variant": "normal", "font-weight": "normal",
	"line-height": "normal", "text-align": "start", "visibility": "visible", "white-space": "normal",
	"text-overflow": "clip", "background-color": "transparent",
	"background-image": "none", "background-repeat": "repeat", "background-position": "0% 0%",
	"background-size": "auto", "mask-image": "none", "mask-repeat": "repeat", "mask-position": "0% 0%",
	"mask-size": "auto",
}

func setInitialComputedValue(values ComputedStyle, property string) {
	if value, ok := initialComputedValues[property]; ok {
		values[property] = value
	} else {
		delete(values, property)
	}
}

func inheritComputedValue(values ComputedStyle, parent ComputedStyle, property string) {
	if parent != nil {
		if value, ok := parent[property]; ok {
			values[property] = value
			return
		}
	}
	setInitialComputedValue(values, property)
}

func unsetComputedValue(values ComputedStyle, parent ComputedStyle, property string) {
	if inheritedCSSProperties[property] {
		inheritComputedValue(values, parent, property)
		return
	}
	setInitialComputedValue(values, property)
}

func validFontFamilyValue(value string) bool {
	tokens, ok := tokenizeFont(value)
	return ok && validFontFamily(tokens)
}

// mediaQueryMatches deliberately implements the fixed rendering environment:
// layout is a screen viewport, color is light, and motion is unrestricted.
func mediaQueryMatches(query string, viewport image.Point) bool {
	for _, alternative := range splitMediaList(query) {
		if mediaQueryAlternativeMatches(strings.TrimSpace(alternative), viewport) {
			return true
		}
	}
	return false
}

func splitMediaList(s string) []string {
	var result []string
	start, depth := 0, 0
	for i, r := range s {
		switch r {
		case '(':
			depth++
		case ')':
			if depth > 0 {
				depth--
			}
		case ',':
			if depth == 0 {
				result = append(result, s[start:i])
				start = i + 1
			}
		}
	}
	return append(result, s[start:])
}

func mediaQueryAlternativeMatches(query string, viewport image.Point) bool {
	query = strings.Join(strings.Fields(strings.ToLower(query)), " ")
	negated := strings.HasPrefix(query, "not ")
	if negated {
		query = strings.TrimSpace(query[4:])
	}
	if strings.HasPrefix(query, "only ") {
		query = strings.TrimSpace(query[5:])
	}
	parts := splitMediaAnd(query)
	if len(parts) == 0 {
		return false
	}
	mediaType := strings.TrimSpace(parts[0])
	if mediaType == "screen" || mediaType == "all" {
		parts = parts[1:]
	} else if mediaType == "print" {
		return negated // The renderer is never a print medium.
	} else if !strings.HasPrefix(mediaType, "(") {
		return false
	}
	matches := true
	for _, part := range parts {
		// Trim exactly one level of parentheses: a feature value may contain
		// its own, as in (max-width: calc(1120px - 1px)).
		part = strings.TrimSpace(part)
		if strings.HasPrefix(part, "(") && strings.HasSuffix(part, ")") {
			part = strings.TrimSpace(part[1 : len(part)-1])
		}
		colon := strings.IndexByte(part, ':')
		if colon < 0 {
			// Media Queries Level 4 range syntax permits no spaces around
			// the operator, e.g. (width<=1011.98px).
			operator := strings.IndexAny(part, "<>=")
			if operator < 0 || operator+1 >= len(part) ||
				(part[operator:operator+2] != "<=" && part[operator:operator+2] != ">=") {
				matches = false
				break
			}
			name := strings.TrimSpace(part[:operator])
			value := strings.TrimSpace(part[operator+2:])
			length, ok := mediaRangeLength(value)
			dimension := viewport.X
			if name == "height" {
				dimension = viewport.Y
			} else if name != "width" {
				ok = false
			}
			if !ok || (part[operator:operator+2] == "<=" && float64(dimension) > length) ||
				(part[operator:operator+2] == ">=" && float64(dimension) < length) {
				matches = false
			}
			continue
		}
		name, value := strings.TrimSpace(part[:colon]), strings.TrimSpace(part[colon+1:])
		switch name {
		case "min-width", "max-width":
			width, ok := mediaLength(value)
			if !ok || (name == "min-width" && float64(viewport.X) < width) ||
				(name == "max-width" && float64(viewport.X) > width) {
				matches = false
			}
		case "prefers-color-scheme":
			matches = matches && value == "light"
		case "prefers-reduced-motion":
			matches = matches && value == "no-preference"
		default:
			matches = false
		}
	}
	if negated {
		return !matches
	}
	return matches
}

// Validate the entire comparison operand before using mediaLength's px/calc
// evaluator; that evaluator alone can accept adjacent lengths without an
// operator, which must not accidentally activate a malformed media rule.
func mediaRangeLength(value string) (float64, bool) {
	if !mediaRangeValuePattern.MatchString(value) {
		return 0, false
	}
	return mediaLength(value)
}

var mediaRangeValuePattern = regexp.MustCompile(`(?i)^(?:[0-9]*\.?[0-9]+px|calc\(\s*[0-9]*\.?[0-9]+px(?:\s*[+-]\s*[0-9]*\.?[0-9]+px)*\s*\))$`)

func splitMediaAnd(s string) []string {
	var result []string
	start, depth := 0, 0
	for i := 0; i < len(s); {
		switch s[i] {
		case '(':
			depth++
		case ')':
			if depth > 0 {
				depth--
			}
		}
		if depth == 0 && i+5 <= len(s) && strings.EqualFold(s[i:i+5], " and ") {
			result = append(result, s[start:i])
			i += 5
			start = i
			continue
		}
		i++
	}
	return append(result, s[start:])
}

func mediaLength(value string) (float64, bool) {
	value = strings.TrimSpace(strings.ToLower(value))
	if strings.HasPrefix(value, "calc(") && strings.HasSuffix(value, ")") {
		value = strings.TrimSpace(value[5 : len(value)-1])
	}
	total := 0.0
	sign := 1.0
	for {
		value = strings.TrimSpace(value)
		if value == "" {
			return total, true
		}
		if value[0] == '+' || value[0] == '-' {
			if value[0] == '-' {
				sign = -1
			} else {
				sign = 1
			}
			value = value[1:]
			continue
		}
		end := 0
		for end < len(value) && (value[end] == '.' || value[end] >= '0' && value[end] <= '9') {
			end++
		}
		if end == 0 || !strings.HasPrefix(strings.TrimSpace(value[end:]), "px") {
			return 0, false
		}
		n, err := strconv.ParseFloat(value[:end], 64)
		if err != nil {
			return 0, false
		}
		total += sign * n
		value = strings.TrimSpace(value[end+2:])
		sign = 1
	}
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
		if strings.HasPrefix(property, "--") {
			continue
		}
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
		if property == "font-size" || strings.HasPrefix(property, "--") {
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
	case "border-spacing", "background-position", "background-size", "mask-position", "mask-size", "border",
		"border-top", "border-right", "border-bottom", "border-left",
		"border-top-left-radius", "border-top-right-radius",
		"border-bottom-right-radius", "border-bottom-left-radius":
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
	// Importance and origin come first. Within an origin, inline declarations
	// outrank every selector specificity, not just a single ID selector.
	if a.important != b.important {
		return a.important
	}
	if a.origin != b.origin {
		if a.important {
			// Important declarations reverse origin precedence: UA important
			// rules override author important rules.
			return a.origin < b.origin
		}
		return a.origin > b.origin
	}
	// Presentational hints have zero specificity and precede every author
	// stylesheet declaration. They are never important declarations.
	if a.presentational != b.presentational {
		return !a.presentational
	}
	// Element-attached declarations outrank stylesheet declarations at the
	// same origin and importance, before layer order and selector specificity.
	if a.inline != b.inline {
		return a.inline
	}
	// Layers are considered before specificity. Normal declarations prefer
	// later layers; important declarations reverse layer order, including
	// making unlayered important rules weaker than layered important rules.
	if a.layer != b.layer {
		if a.important {
			return a.layer < b.layer
		}
		return a.layer > b.layer
	}
	for i := range a.spec {
		if a.spec[i] != b.spec[i] {
			return a.spec[i] > b.spec[i]
		}
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
		result[1] += len(p.Attributes)
		for _, pseudo := range p.PseudoClasses {
			if isPseudoElementName(pseudo) {
				// Pseudo-elements, including the legacy one-colon :before,
				// :after, :first-line and :first-letter, count like type
				// selectors.
				result[2]++
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
		if node == nil || node.Type != ElementNode || !matchesPart(node, s.Parts[index]) {
			return false
		}
		if index == 0 {
			return true
		}
		switch s.Parts[index].Combinator {
		case ">":
			return match(node.Parent, index-1)
		case "+", "~":
			if node.Parent == nil {
				return false
			}
			for i := len(node.Parent.Children) - 1; i >= 0; i-- {
				if node.Parent.Children[i] != node {
					continue
				}
				for j := i - 1; j >= 0; j-- {
					sibling := node.Parent.Children[j]
					if sibling.Type != ElementNode {
						continue
					}
					if match(sibling, index-1) {
						return true
					}
					if s.Parts[index].Combinator == "+" {
						return false
					}
				}
				return false
			}
			return false
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
	for _, selector := range p.Attributes {
		attr, ok := n.Attribute(selector.Name)
		if !ok || selector.HasValue && attr.Value != selector.Value {
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

// matchesPseudoClass supports first-child, last-child, and static link
// pseudo-classes. Every link is treated as unvisited, and there is no user
// interaction, so :visited and dynamic pseudo-classes (:hover, :active,
// :focus, ...) never match. Unknown pseudo-classes and pseudo-elements also
// never match, so an unsupported selector can only style fewer elements,
// never more.
func matchesPseudoClass(n *Node, pseudo string) bool {
	switch pseudo {
	case "root":
		return n.Parent == nil || n.Parent.Type != ElementNode
	case "first-child":
		if n.Type != ElementNode {
			return false
		}
		if n.Parent == nil {
			return true // Selectors 4 does not require a parent.
		}
		for _, sibling := range n.Parent.Children {
			if sibling.Type == ElementNode {
				return sibling == n
			}
		}
		return false
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
	if d.Value == invalidVariable {
		switch d.Property {
		case "border-radius":
			return invalidLonghands(d, "border-top-left-radius", "border-top-right-radius", "border-bottom-right-radius", "border-bottom-left-radius")
		case "margin", "padding":
			return invalidLonghands(d, d.Property+"-top", d.Property+"-right", d.Property+"-bottom", d.Property+"-left")
		case "border-width":
			return invalidLonghands(d, "border-top-width", "border-right-width", "border-bottom-width", "border-left-width")
		case "background":
			return invalidLonghands(d, "background-color", "background-image", "background-repeat", "background-position", "background-size")
		case "mask":
			return invalidLonghands(d, maskLonghands...)
		case "font":
			return invalidLonghands(d, "font-size", "font-family", "font-style", "font-variant", "font-weight", "line-height")
		}
	}
	if d.Property == "background" {
		return expandBackground(d)
	}
	if d.Property == "border-radius" {
		return expandRadius(d)
	}
	if d.Property == "mask" {
		return expandMask(d)
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
	parts, ok := splitCSSComponents(d.Value)
	if !ok {
		return nil
	}
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
	if d.Property == "border-width" {
		names = []string{"border-top-width", "border-right-width", "border-bottom-width", "border-left-width"}
	}
	result := make([]Declaration, 4)
	for i := range result {
		result[i] = Declaration{Property: names[i], Value: parts[i], Important: d.Important}
	}
	return result
}

func invalidLonghands(d Declaration, names ...string) []Declaration {
	result := make([]Declaration, 0, len(names))
	for _, name := range names {
		result = append(result, Declaration{Property: name, Value: invalidVariable, Important: d.Important})
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
