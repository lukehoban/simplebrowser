package browser

import (
	"math"
	"strconv"
	"strings"
)

// cssMathValue represents either a unitless number or a length containing a
// pixel constant and a percentage fraction. Keeping the percentage separate
// lets used-value callers apply their own property-specific basis.
type cssMathValue struct {
	px, percent float64
	number      bool
}

type cssMathParser struct {
	s       string
	i       int
	depth   int
	convert func(float64, string) (cssMathValue, bool)
}

const maxCSSMathDepth = 64

func calcLengthProperty(property string) bool {
	switch property {
	case "width", "height", "min-width", "max-width", "min-height", "max-height",
		"top", "right", "bottom", "left", "margin", "margin-top", "margin-right",
		"margin-bottom", "margin-left", "padding", "padding-top", "padding-right",
		"padding-bottom", "padding-left", "flex-basis", "gap", "row-gap", "column-gap",
		"border-width", "border-top-width", "border-right-width", "border-bottom-width",
		"border-left-width":
		return true
	}
	return false
}

// calcPercentAllowed reports whether the property's length grammar accepts
// percentages; border widths do not, so calc() there must not either.
func calcPercentAllowed(property string) bool {
	return !strings.HasPrefix(property, "border-")
}

func parseCSSMath(value string, convert func(float64, string) (cssMathValue, bool)) (cssMathValue, bool) {
	value = strings.TrimSpace(value)
	if len(value) < 6 || !strings.EqualFold(value[:5], "calc(") || value[len(value)-1] != ')' {
		return cssMathValue{}, false
	}
	p := cssMathParser{s: value[5 : len(value)-1], convert: convert}
	v, ok := p.sum()
	p.space()
	return v, ok && p.i == len(p.s) && finite(v.px) && finite(v.percent)
}

func (p *cssMathParser) space() {
	for p.i < len(p.s) && cssSpace(p.s[p.i]) {
		p.i++
	}
}

func (p *cssMathParser) sum() (cssMathValue, bool) {
	left, ok := p.product()
	if !ok {
		return cssMathValue{}, false
	}
	for {
		before := p.i
		p.space()
		spacedBefore := p.i > before
		if p.i >= len(p.s) || p.s[p.i] != '+' && p.s[p.i] != '-' {
			return left, true
		}
		op := p.s[p.i]
		p.i++
		after := p.i
		p.space()
		if !spacedBefore || p.i == after {
			return cssMathValue{}, false // CSS requires whitespace around binary +/-
		}
		right, ok := p.product()
		if !ok || left.number != right.number {
			return cssMathValue{}, false
		}
		sign := 1.0
		if op == '-' {
			sign = -1
		}
		left.px += sign * right.px
		left.percent += sign * right.percent
	}
}

func (p *cssMathParser) product() (cssMathValue, bool) {
	left, ok := p.unary()
	if !ok {
		return cssMathValue{}, false
	}
	for {
		beforeSpace := p.i
		p.space()
		if p.i >= len(p.s) || p.s[p.i] != '*' && p.s[p.i] != '/' {
			p.i = beforeSpace
			return left, true
		}
		op := p.s[p.i]
		p.i++
		p.space()
		right, ok := p.unary()
		if !ok {
			return cssMathValue{}, false
		}
		if op == '*' {
			if !left.number && !right.number {
				return cssMathValue{}, false
			}
			if left.number {
				scalar := left.px
				left = cssMathValue{px: right.px * scalar, percent: right.percent * scalar, number: right.number}
			} else {
				scalar := right.px
				left.px *= scalar
				left.percent *= scalar
			}
		} else {
			if !right.number || right.px == 0 {
				return cssMathValue{}, false
			}
			left.px /= right.px
			left.percent /= right.px
		}
	}
}

func (p *cssMathParser) unary() (cssMathValue, bool) {
	p.space()
	if p.i < len(p.s) && (p.s[p.i] == '+' || p.s[p.i] == '-') {
		if p.depth >= maxCSSMathDepth {
			return cssMathValue{}, false
		}
		sign := 1.0
		if p.s[p.i] == '-' {
			sign = -1
		}
		p.i++
		p.depth++
		v, ok := p.unary()
		p.depth--
		v.px *= sign
		v.percent *= sign
		return v, ok
	}
	if p.i < len(p.s) && p.s[p.i] == '(' {
		if p.depth >= maxCSSMathDepth {
			return cssMathValue{}, false
		}
		p.i++
		p.depth++
		v, ok := p.sum()
		p.depth--
		p.space()
		if !ok || p.i >= len(p.s) || p.s[p.i] != ')' {
			return cssMathValue{}, false
		}
		p.i++
		return v, true
	}
	return p.atom()
}

func (p *cssMathParser) atom() (cssMathValue, bool) {
	start := p.i
	if p.i < len(p.s) && (p.s[p.i] == '.' || p.s[p.i] >= '0' && p.s[p.i] <= '9') {
		for p.i < len(p.s) && p.s[p.i] >= '0' && p.s[p.i] <= '9' {
			p.i++
		}
		if p.i < len(p.s) && p.s[p.i] == '.' {
			p.i++
			for p.i < len(p.s) && p.s[p.i] >= '0' && p.s[p.i] <= '9' {
				p.i++
			}
		}
		if p.i < len(p.s) && (p.s[p.i] == 'e' || p.s[p.i] == 'E') {
			exponent := p.i
			p.i++
			if p.i < len(p.s) && (p.s[p.i] == '+' || p.s[p.i] == '-') {
				p.i++
			}
			digits := p.i
			for p.i < len(p.s) && p.s[p.i] >= '0' && p.s[p.i] <= '9' {
				p.i++
			}
			if digits == p.i {
				p.i = exponent
			}
		}
		n, err := strconv.ParseFloat(p.s[start:p.i], 64)
		if err != nil || !finite(n) {
			return cssMathValue{}, false
		}
		unitStart := p.i
		if p.i < len(p.s) && p.s[p.i] == '%' {
			p.i++
		} else {
			for p.i < len(p.s) && (p.s[p.i] >= 'a' && p.s[p.i] <= 'z' || p.s[p.i] >= 'A' && p.s[p.i] <= 'Z') {
				p.i++
			}
		}
		unit := strings.ToLower(p.s[unitStart:p.i])
		if unit == "" {
			return cssMathValue{px: n, number: true}, true
		}
		return p.convert(n, unit)
	}
	return cssMathValue{}, false
}

func finite(n float64) bool { return !math.IsNaN(n) && !math.IsInf(n, 0) }

func normalizeCalcValues(values ComputedStyle, viewportWidth, viewportHeight int, rootSize float64) {
	fontSize := computedFontSize(values)
	ratios := ratiosFor(values)
	for property, text := range values {
		if strings.HasPrefix(property, "--") || !calcLengthProperty(property) {
			continue
		}
		values[property] = replaceCalcFunctions(text, func(expression string) (string, bool) {
			v, ok := parseCSSMath("calc("+expression+")", func(n float64, unit string) (cssMathValue, bool) {
				switch unit {
				case "%":
					return cssMathValue{percent: n / 100}, true
				case "px":
					return cssMathValue{px: n}, true
				case "em":
					return cssMathValue{px: n * fontSize}, true
				case "rem":
					return cssMathValue{px: n * rootSize}, true
				case "ex":
					return cssMathValue{px: n * fontSize * ratios.ex}, true
				case "ch":
					return cssMathValue{px: n * fontSize * ratios.ch}, true
				case "vw":
					return cssMathValue{px: n * float64(viewportWidth) / 100}, true
				case "vh":
					return cssMathValue{px: n * float64(viewportHeight) / 100}, true
				}
				return cssMathValue{}, false
			})
			if !ok || v.number {
				return "", false
			}
			return serializeCSSMath(v), true
		})
	}
}

func serializeCSSMath(v cssMathValue) string {
	if v.percent == 0 {
		return strconv.FormatFloat(v.px, 'f', -1, 64) + "px"
	}
	if v.px == 0 {
		return strconv.FormatFloat(v.percent*100, 'f', -1, 64) + "%"
	}
	var parts []string
	if v.percent != 0 {
		parts = append(parts, strconv.FormatFloat(v.percent*100, 'f', -1, 64)+"%")
	}
	if v.px != 0 || len(parts) == 0 {
		parts = append(parts, strconv.FormatFloat(v.px, 'f', -1, 64)+"px")
	}
	if len(parts) == 1 {
		return "calc(" + parts[0] + ")"
	}
	if v.px < 0 {
		return "calc(" + parts[0] + " - " + strconv.FormatFloat(-v.px, 'f', -1, 64) + "px)"
	}
	return "calc(" + parts[0] + " + " + strconv.FormatFloat(v.px, 'f', -1, 64) + "px)"
}

func replaceCalcFunctions(text string, replace func(string) (string, bool)) string {
	lower := strings.ToLower(text)
	var out strings.Builder
	for i := 0; i < len(text); {
		start := strings.Index(lower[i:], "calc(")
		if start < 0 {
			out.WriteString(text[i:])
			break
		}
		start += i
		out.WriteString(text[i:start])
		depth, end := 1, start+5
		for end < len(text) && depth > 0 {
			if text[end] == '(' {
				depth++
			} else if text[end] == ')' {
				depth--
			}
			end++
		}
		if depth != 0 {
			out.WriteString(text[start:])
			break
		}
		inner := text[start+5 : end-1]
		if value, ok := replace(inner); ok {
			out.WriteString(value)
		} else {
			out.WriteString(text[start:end])
		}
		i = end
	}
	return out.String()
}

func evaluateComputedCalc(value string, basis float64) (float64, bool) {
	v, ok := parseCSSMath(value, func(n float64, unit string) (cssMathValue, bool) {
		switch unit {
		case "px":
			return cssMathValue{px: n}, true
		case "%":
			return cssMathValue{px: basis * n / 100}, true
		}
		return cssMathValue{}, false
	})
	if !ok || v.number || v.percent != 0 {
		return 0, false
	}
	return v.px, true
}

func validCalcDeclaration(property, value string) bool {
	if !calcLengthProperty(property) {
		return true
	}
	if !strings.Contains(strings.ToLower(value), "calc(") {
		return true
	}
	// The box shorthands accept one to four whitespace-separated components.
	// Split only at top level so spaces inside calc() remain part of its
	// expression, and validate each component using the shorthand's grammar.
	if property == "margin" || property == "padding" || property == "border-width" {
		parts, ok := splitCSSComponents(value)
		if !ok || len(parts) < 1 || len(parts) > 4 {
			return false
		}
		for _, part := range parts {
			if strings.Contains(strings.ToLower(part), "calc(") {
				if !validSingleCalc(property, part) {
					return false
				}
				continue
			}
			switch property {
			case "margin":
				if !supportsOr(keywordValidator("auto"), lengthOrPercentage)(part) {
					return false
				}
			case "padding", "border-width":
				if !nonNegativeLength(part) {
					return false
				}
			}
		}
		return true
	}
	return validSingleCalc(property, strings.TrimSpace(value))
}

func validSingleCalc(property, value string) bool {
	value = strings.TrimSpace(value)
	if !strings.HasPrefix(strings.ToLower(value), "calc(") {
		return false
	}
	v, ok := parseCSSMath(value, func(n float64, unit string) (cssMathValue, bool) {
		switch unit {
		case "%", "px", "em", "rem", "ex", "ch", "vw", "vh":
			if unit == "%" {
				if !calcPercentAllowed(property) {
					return cssMathValue{}, false
				}
				return cssMathValue{percent: n / 100}, true
			}
			return cssMathValue{px: n}, true
		}
		return cssMathValue{}, false
	})
	return ok && !v.number
}

// splitCSSComponents splits CSS shorthand tokens at whitespace outside
// parenthesized functions. Quoted text is kept together as well, so malformed
// tokens are rejected by the property's validator rather than mis-expanded.
func splitCSSComponents(value string) ([]string, bool) {
	var parts []string
	start, depth := -1, 0
	var quote byte
	for i := 0; i < len(value); i++ {
		c := value[i]
		if quote != 0 {
			if c == '\\' {
				i++
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
			if depth < 0 {
				return nil, false
			}
		}
		if cssSpace(c) && depth == 0 && quote == 0 {
			if start >= 0 {
				parts = append(parts, value[start:i])
				start = -1
			}
			continue
		}
		if start < 0 {
			start = i
		}
	}
	if depth != 0 || quote != 0 {
		return nil, false
	}
	if start >= 0 {
		parts = append(parts, value[start:])
	}
	return parts, true
}

// computedCalcHasPercentage reports whether a normalized calc expression
// still depends on its percentage basis.
func computedCalcHasPercentage(value string) bool {
	v, ok := parseCSSMath(value, func(n float64, unit string) (cssMathValue, bool) {
		switch unit {
		case "%":
			return cssMathValue{percent: n / 100}, true
		case "px":
			return cssMathValue{px: n}, true
		}
		return cssMathValue{}, false
	})
	return ok && v.percent != 0
}
