package browser

import (
	"strconv"
	"strings"
)

// supportsConditionMatches evaluates an @supports prelude (CSS Conditional 3).
// Support is a static property of this engine, so the condition is answered at
// parse time. Anything this bounded grammar does not understand — selector(),
// other functions, mixed and/or without parentheses, malformed input — is
// false, and so is any declaration the engine cannot actually render.
func supportsConditionMatches(condition string) bool {
	p := supportsParser{s: strings.TrimSpace(stripComments(condition))}
	result, ok := p.condition(0)
	p.skipSpace()
	return ok && p.i == len(p.s) && result
}

// Bound nesting so hostile input cannot recurse without limit.
const maxSupportsDepth = 32

type supportsParser struct {
	s string
	i int
}

func (p *supportsParser) skipSpace() {
	for p.i < len(p.s) && cssSpace(p.s[p.i]) {
		p.i++
	}
}

// keyword consumes a case-insensitive "not"/"and"/"or" that is followed by
// whitespace. Per the grammar, "not(" is a function token (general-enclosed),
// not the not keyword.
func (p *supportsParser) keyword(word string) bool {
	end := p.i + len(word)
	if end >= len(p.s) || !strings.EqualFold(p.s[p.i:end], word) || !cssSpace(p.s[end]) {
		return false
	}
	p.i = end
	p.skipSpace()
	return true
}

// condition parses: not <in-parens> | <in-parens> [and <in-parens>]* |
// <in-parens> [or <in-parens>]*. The ok result reports syntactic validity.
func (p *supportsParser) condition(depth int) (bool, bool) {
	if depth > maxSupportsDepth {
		return false, false
	}
	p.skipSpace()
	if p.keyword("not") {
		v, ok := p.inParens(depth + 1)
		return !v, ok
	}
	result, ok := p.inParens(depth + 1)
	if !ok {
		return false, false
	}
	combiner := ""
	for {
		p.skipSpace()
		start := p.i
		word := ""
		if p.keyword("and") {
			word = "and"
		} else if p.keyword("or") {
			word = "or"
		} else {
			p.i = start
			return result, true
		}
		if combiner != "" && combiner != word {
			return false, false
		}
		combiner = word
		v, ok := p.inParens(depth + 1)
		if !ok {
			return false, false
		}
		if word == "and" {
			result = result && v
		} else {
			result = result || v
		}
	}
}

// inParens parses ( <condition> ) | ( <declaration> ) | <general-enclosed>.
// General-enclosed forms are syntactically valid but evaluate to false.
func (p *supportsParser) inParens(depth int) (bool, bool) {
	p.skipSpace()
	if p.i >= len(p.s) {
		return false, false
	}
	if p.s[p.i] != '(' {
		// A function such as selector(...) or font-tech(...).
		start := p.i
		for p.i < len(p.s) && cssIdent(p.s[p.i]) {
			p.i++
		}
		if p.i == start || p.i >= len(p.s) || p.s[p.i] != '(' {
			return false, false
		}
		if _, ok := p.block(); !ok {
			return false, false
		}
		return false, true
	}
	inner, ok := p.block()
	if !ok {
		return false, false
	}
	nested := supportsParser{s: inner}
	if v, ok := nested.condition(depth + 1); ok {
		nested.skipSpace()
		if nested.i == len(nested.s) {
			return v, true
		}
	}
	if property, value, ok := supportsDeclaration(inner); ok {
		return featureSupported(property, value), true
	}
	return false, true // general-enclosed: ( <any-value> )
}

// block consumes a balanced parenthesized block starting at '(' and returns
// its contents. Strings are skipped so a quoted ')' does not close the block.
func (p *supportsParser) block() (string, bool) {
	if p.i >= len(p.s) || p.s[p.i] != '(' {
		return "", false
	}
	start := p.i + 1
	depth := 0
	var quote byte
	for ; p.i < len(p.s); p.i++ {
		c := p.s[p.i]
		if quote != 0 {
			if c == '\\' {
				p.i++
			} else if c == quote {
				quote = 0
			}
			continue
		}
		switch c {
		case '"', '\'':
			quote = c
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				inner := p.s[start:p.i]
				p.i++
				return inner, true
			}
		}
	}
	return "", false
}

// supportsDeclaration splits "property: value [!important]".
func supportsDeclaration(s string) (string, string, bool) {
	colon := strings.IndexByte(s, ':')
	if colon < 0 {
		return "", "", false
	}
	property := strings.ToLower(strings.TrimSpace(s[:colon]))
	value := strings.TrimSpace(s[colon+1:])
	if !validProperty(property) {
		return "", "", false
	}
	if bang := strings.LastIndexByte(value, '!'); bang >= 0 &&
		strings.EqualFold(strings.TrimSpace(value[bang+1:]), "important") {
		value = strings.TrimSpace(value[:bang])
	}
	return property, value, true
}

// supportValidators is the engine's honest feature registry: a property is
// listed only when the cascade, layout or painting consumes it, and only
// values this renderer implements are accepted. It is intentionally
// conservative; extend it when a feature lands. Unlisted properties such as
// mask-mode or mask-composite, and values such as display:grid or math
// functions like min()/round() (outside mask-size), are unsupported. A single calc() length is accepted for the
// properties listed by calcLengthProperty (see docs/css-calc.md).
var supportValidators = map[string]func(string) bool{
	"display": keywordValidator("none", "block", "inline", "inline-block", "flex", "inline-flex", "list-item", "flow-root",
		"table", "inline-table", "table-row", "table-cell", "table-row-group", "table-header-group",
		"table-footer-group", "table-column", "table-column-group", "table-caption"),
	"flex":                  flexValue,
	"flex-grow":             nonNegativeNumber,
	"flex-shrink":           nonNegativeNumber,
	"flex-basis":            supportsOr(keywordValidator("auto"), nonNegativeLength),
	"flex-direction":        keywordValidator("row", "row-reverse", "column", "column-reverse"),
	"flex-wrap":             keywordValidator("nowrap", "wrap", "wrap-reverse"),
	"align-content":         keywordValidator("normal", "stretch", "start", "end", "flex-start", "flex-end", "center", "space-between", "space-around", "space-evenly"),
	"gap":                   oneOrTwo(nonNegativeLength),
	"row-gap":               supportsOr(keywordValidator("normal"), nonNegativeLength),
	"column-gap":            supportsOr(keywordValidator("normal"), nonNegativeLength),
	"grid-template-columns": keywordValidator("min-content minmax(0,auto) min-content"),
	"grid-template-areas":   gridAreasValue,
	"grid-area":             gridAreaValue,
	"justify-content":       keywordValidator("normal", "start", "end", "flex-start", "flex-end", "center", "space-between", "space-around", "space-evenly"),
	"align-items":           keywordValidator("normal", "stretch", "start", "end", "flex-start", "flex-end", "center"),
	"align-self":            keywordValidator("auto", "stretch", "start", "end", "flex-start", "flex-end", "center", "self-start", "self-end"),
	"position":              keywordValidator("static", "relative", "absolute", "fixed"),
	"float":                 keywordValidator("none", "left", "right"),
	"overflow":              keywordValidator("visible", "hidden", "clip"),
	"visibility":            keywordValidator("visible", "hidden", "collapse"),
	"text-align":            keywordValidator("left", "right", "center", "start", "end"),
	"white-space":           keywordValidator("normal", "nowrap"),
	"vertical-align":        supportsOr(keywordValidator("baseline", "top", "bottom", "middle", "text-top", "text-bottom"), lengthOrPercentage),
	"text-decoration":       keywordValidator("none", "underline", "line-through"),
	"text-decoration-line":  keywordValidator("none", "underline", "line-through"),
	"border-collapse":       keywordValidator("collapse", "separate"),
	"table-layout":          keywordValidator("auto", "fixed"),
	"caption-side":          keywordValidator("top", "bottom"),
	"font-style":            keywordValidator("normal", "italic", "oblique"),
	"font-variant":          keywordValidator("normal", "small-caps"),
	"font-weight":           supportsOr(keywordValidator("normal"), validFontWeight),
	"font-family":           validFontFamilyValue,
	"font-size":             validFontSize,
	"line-height":           validLineHeight,
	"color":                 colorValue,
	"background-color":      colorValue,
	"background-image":      supportsOr(keywordValidator("none"), urlValue),
	"background-repeat":     keywordValidator("repeat", "no-repeat", "repeat-x", "repeat-y"),
	"width":                 supportsOr(keywordValidator("auto"), nonNegativeLength),
	"height":                supportsOr(keywordValidator("auto"), nonNegativeLength),
	"top":                   supportsOr(keywordValidator("auto"), lengthOrPercentage),
	"right":                 supportsOr(keywordValidator("auto"), lengthOrPercentage),
	"bottom":                supportsOr(keywordValidator("auto"), lengthOrPercentage),
	"left":                  supportsOr(keywordValidator("auto"), lengthOrPercentage),
	"z-index":               supportsOr(keywordValidator("auto"), integerValue),
	"margin":                boxShorthand(supportsOr(keywordValidator("auto"), lengthOrPercentage)),
	"padding":               boxShorthand(nonNegativeLength),
	"border-width":          boxShorthand(nonNegativeLength),
	// Group opacity, painted by opacity.go for HTML and svg.go for SVG.
	"opacity": validOpacity,
	// SVG presentation properties that svg.go resolves from the cascade.
	"fill":            supportsOr(keywordValidator("none"), colorValue),
	"stroke":          supportsOr(keywordValidator("none"), colorValue),
	"stroke-width":    nonNegativeLength,
	"fill-rule":       keywordValidator("nonzero", "evenodd"),
	"stroke-linecap":  keywordValidator("butt", "round", "square"),
	"stroke-linejoin": keywordValidator("miter", "round", "bevel"),
	"fill-opacity":    unitNumber,
	"stroke-opacity":  unitNumber,
	// CSS Masking subset painted by mask.go. -webkit-mask* aliases are
	// canonicalized before lookup.
	"mask":          validMask,
	"mask-image":    layerList(supportsOr(keywordValidator("none"), urlValue, gradientValue)),
	"mask-repeat":   layerList(keywordValidator("repeat", "no-repeat", "repeat-x", "repeat-y")),
	"mask-position": layerList(positionValue),
	"mask-size":     layerList(maskSizeValue),
}

func init() {
	for _, side := range []string{"top", "right", "bottom", "left"} {
		supportValidators["margin-"+side] = supportsOr(keywordValidator("auto"), lengthOrPercentage)
		supportValidators["padding-"+side] = nonNegativeLength
	}
}

// featureSupported answers one @supports (property: value) test.
func featureSupported(property, value string) bool {
	property = canonicalMaskProperty(property)
	valid, ok := supportValidators[property]
	if !ok || value == "" {
		return false
	}
	switch strings.ToLower(value) {
	case "inherit":
		return true // resolved generically by the cascade
	}
	if calcLengthProperty(property) && strings.Contains(strings.ToLower(value), "calc(") {
		return validCalcDeclaration(property, value)
	}
	return valid(strings.TrimSpace(value))
}

func keywordValidator(words ...string) func(string) bool {
	return func(v string) bool {
		for _, w := range words {
			if strings.EqualFold(v, w) {
				return true
			}
		}
		return false
	}
}

func supportsOr(validators ...func(string) bool) func(string) bool {
	return func(v string) bool {
		for _, valid := range validators {
			if valid(v) {
				return true
			}
		}
		return false
	}
}

// Math functions are evaluated by the computed-value pipeline for the
// supported ordinary length properties. Other function-valued lengths remain
// outside this compact validator.
func lengthOrPercentage(v string) bool {
	c := classifyValue(v)
	return c.Kind == "length" || c.Kind == "percentage" || c.Kind == "number" && c.Number == 0
}

func nonNegativeLength(v string) bool {
	return lengthOrPercentage(v) && classifyValue(v).Number >= 0
}

func colorValue(v string) bool {
	if strings.EqualFold(v, "currentcolor") {
		return true
	}
	_, ok := parseColor(strings.ToLower(v))
	return ok
}

func urlValue(v string) bool { return classifyValue(v).Kind == "url" }

func integerValue(v string) bool {
	_, err := strconv.Atoi(v)
	return err == nil
}

func unitNumber(v string) bool {
	n, err := strconv.ParseFloat(v, 64)
	return err == nil && n >= 0 && n <= 1
}

func nonNegativeNumber(v string) bool {
	n, err := strconv.ParseFloat(v, 64)
	return err == nil && n >= 0
}

func flexValue(v string) bool {
	if strings.EqualFold(strings.TrimSpace(v), "none") {
		return true
	}
	parts := strings.Fields(v)
	if len(parts) < 1 || len(parts) > 3 || !nonNegativeNumber(parts[0]) {
		return false
	}
	if len(parts) >= 2 && !nonNegativeNumber(parts[1]) && !nonNegativeLength(parts[1]) {
		return false
	}
	return len(parts) < 3 || nonNegativeLength(parts[2])
}

func oneOrTwo(valid func(string) bool) func(string) bool {
	return func(v string) bool {
		parts := strings.Fields(v)
		if len(parts) < 1 || len(parts) > 2 {
			return false
		}

		for _, part := range parts {
			if !valid(part) {
				return false
			}
		}

		return true
	}
}

func gridAreasValue(v string) bool {
	v = strings.TrimSpace(v)
	if len(v) < 2 || (v[0] != '"' && v[0] != '\'') || v[len(v)-1] != v[0] {
		return false
	}
	parts := strings.Fields(v[1 : len(v)-1])
	if len(parts) != 3 {
		return false
	}
	for _, part := range parts {
		if part != "." && !cssIdentString(part) {
			return false
		}
	}
	return true
}

func gridAreaValue(v string) bool {
	return cssIdentString(strings.TrimSpace(v))
}

func cssIdentString(v string) bool {
	if v == "" {
		return false
	}
	for i, r := range v {
		if !(r == '-' || r == '_' || r >= 'a' && r <= 'z' ||
			r >= 'A' && r <= 'Z' || i > 0 && r >= '0' && r <= '9') {
			return false
		}
	}
	return true
}

func boxShorthand(valid func(string) bool) func(string) bool {
	return func(v string) bool {
		parts := strings.Fields(v)
		if len(parts) < 1 || len(parts) > 4 {
			return false
		}
		for _, part := range parts {
			if !valid(part) {
				return false
			}
		}
		return true
	}
}

// layerList applies valid to each comma-separated layer of a list-valued
// property such as mask-image or mask-size.
func layerList(valid func(string) bool) func(string) bool {
	return func(v string) bool {
		for _, layer := range backgroundLayers(v) {
			if layer == "" || !valid(layer) {
				return false
			}
		}
		return true
	}
}

func gradientValue(v string) bool {
	c := classifyValue(v)
	return c.Kind == "function" && gradientFunction(c.Text) != ""
}

// positionValue accepts the one- and two-component <position> forms that
// backgroundPositionAxes resolves.
func positionValue(v string) bool {
	parts, ok := splitCSSComponents(v)
	if !ok || len(parts) < 1 || len(parts) > 2 {
		return false
	}
	for _, part := range parts {
		if !keywordValidator("left", "right", "top", "bottom", "center")(part) &&
			!lengthOrPercentage(part) && !backgroundMathComponent(classifyValue(part)) {
			return false
		}
	}
	return true
}

// maskSizeValue accepts cover/contain or one or two auto/length/percentage
// components, where a component may be a calc()/min()/max() expression.
func maskSizeValue(v string) bool {
	if keywordValidator("cover", "contain")(v) {
		return true
	}
	parts, ok := splitCSSComponents(v)
	if !ok || len(parts) < 1 || len(parts) > 2 {
		return false
	}
	for _, part := range parts {
		if strings.Contains(part, "(") {
			m, ok := parseCSSMathFunction(part, func(n float64, unit string) (cssMathValue, bool) {
				switch unit {
				case "%":
					return cssMathValue{percent: n / 100}, true
				case "px", "em", "rem", "ex", "ch", "vw", "vh":
					return cssMathValue{px: n}, true
				}
				return cssMathValue{}, false
			})
			if !ok || m.number {
				return false
			}
			continue
		}
		if !keywordValidator("auto")(part) && !nonNegativeLength(part) {
			return false
		}
	}
	return true
}
