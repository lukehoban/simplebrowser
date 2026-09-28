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
	// functions enables nested calc(), min(), max() and clamp() inside an
	// expression. parseCSSMath (used by SVG) leaves it off.
	functions bool
}

const maxCSSMathDepth = 64

func calcLengthProperty(property string) bool {
	switch property {
	case "width", "height", "min-width", "max-width", "min-height", "max-height",
		"inline-size", "block-size",
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

// calcNonNegativeProperty reports properties whose length values are
// constrained to be non-negative. Math expressions are range-clamped after
// substitution; a percentage-dependent expression may need to wait until its
// layout basis is known.
func calcNonNegativeProperty(property string) bool {
	switch property {
	case "width", "height", "min-width", "max-width", "min-height", "max-height",
		"inline-size", "block-size", "padding", "padding-top", "padding-right",
		"padding-bottom", "padding-left", "flex-basis", "gap", "row-gap",
		"column-gap", "border-width", "border-top-width", "border-right-width",
		"border-bottom-width", "border-left-width":
		return true
	}
	return false
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
	if p.functions {
		if v, ok, matched := p.function(); matched {
			return v, ok
		}
	}
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

// function parses a calc(), min(), max(), clamp(), round(), abs() or sign().
// matched reports whether the input started with one of these names, so atom
// can fall through to numbers otherwise. Comparisons and rounding need a
// common basis. Mixed percentage/pixel operations that need a comparison are
// rejected here; normalizeCalcValues keeps such expressions until layout,
// where percentages are resolved to pixels.
func (p *cssMathParser) function() (v cssMathValue, ok, matched bool) {
	name := ""
	for _, candidate := range []string{"calc(", "min(", "max(", "clamp(", "round(", "abs(", "sign("} {
		if len(p.s)-p.i >= len(candidate) && strings.EqualFold(p.s[p.i:p.i+len(candidate)], candidate) {
			name = candidate[:len(candidate)-1]
			break
		}
	}
	if name == "" {
		return cssMathValue{}, false, false
	}
	if p.depth >= maxCSSMathDepth {
		return cssMathValue{}, false, true
	}
	p.i += len(name) + 1
	p.depth++
	defer func() { p.depth-- }()
	strategy := "nearest"
	if name == "round" {
		start := p.i
		p.space()
		strategyStart := p.i
		for p.i < len(p.s) && (p.s[p.i] >= 'a' && p.s[p.i] <= 'z' || p.s[p.i] >= 'A' && p.s[p.i] <= 'Z' || p.s[p.i] == '-') {
			p.i++
		}
		candidate := strings.ToLower(p.s[strategyStart:p.i])
		p.space()
		if p.i < len(p.s) && p.s[p.i] == ',' && (candidate == "nearest" || candidate == "up" || candidate == "down" || candidate == "to-zero") {
			strategy = candidate
			p.i++
		} else {
			p.i = start
		}
	}
	var args []cssMathValue
	var noneArgs []bool
	for {
		p.space()
		isNone := false
		if name == "clamp" && (len(args) == 0 || len(args) == 2) &&
			len(p.s)-p.i >= 4 && strings.EqualFold(p.s[p.i:p.i+4], "none") &&
			(p.i+4 == len(p.s) || cssSpace(p.s[p.i+4]) || p.s[p.i+4] == ',' || p.s[p.i+4] == ')') {
			p.i += 4
			isNone = true
		}
		arg, ok := cssMathValue{}, true
		if !isNone {
			arg, ok = p.sum()
		}
		p.space()
		if !ok || p.i >= len(p.s) {
			return cssMathValue{}, false, true
		}
		args = append(args, arg)
		noneArgs = append(noneArgs, isNone)
		if p.s[p.i] == ')' {
			p.i++
			break
		}
		if p.s[p.i] != ',' || name == "calc" || (name == "clamp" || name == "round" || name == "abs" || name == "sign") && len(args) == 3 {
			return cssMathValue{}, false, true
		}
		p.i++
	}
	if name == "calc" {
		return args[0], true, true
	}
	if name == "abs" || name == "sign" {
		if len(args) != 1 || noneArgs[0] {
			return cssMathValue{}, false, true
		}
		v := args[0]
		if !(v.px >= 0 && v.percent >= 0 || v.px <= 0 && v.percent <= 0) {
			// Opposing pixel and percentage terms can change sign with the
			// eventual layout basis; leave the expression for used-value evaluation.
			return cssMathValue{}, false, true
		}
		if name == "sign" {
			sign := 0.0
			if v.px > 0 || v.percent > 0 {
				sign = 1
			} else if v.px < 0 || v.percent < 0 {
				sign = -1
			}
			return cssMathValue{px: sign, number: true}, true, true
		}
		if v.px < 0 || v.percent < 0 {
			v.px, v.percent = -v.px, -v.percent
		}
		return v, true, true
	}
	if name == "round" {
		if len(args) != 2 || noneArgs[0] || noneArgs[1] ||
			args[0].number != args[1].number ||
			args[0].px != 0 && args[0].percent != 0 ||
			args[1].px != 0 && args[1].percent != 0 {
			return cssMathValue{}, false, true
		}
		value, step := args[0], args[1]
		if value.percent != 0 && step.px != 0 || value.px != 0 && step.percent != 0 {
			// A percentage step and a pixel value have a common length type,
			// but their ratio depends on the layout basis.
			return cssMathValue{}, false, true
		}
		component := func(v cssMathValue) float64 {
			if v.percent != 0 {
				return v.percent
			}
			return v.px
		}
		stepValue := math.Abs(component(step))
		if stepValue == 0 {
			return cssMathValue{}, false, true
		}
		q := component(value) / stepValue
		var multiple float64
		switch strategy {
		case "nearest":
			multiple = math.Floor(q + 0.5)
		case "up":
			multiple = math.Ceil(q)
		case "down":
			multiple = math.Floor(q)
		case "to-zero":
			multiple = math.Trunc(q)
		default:
			return cssMathValue{}, false, true
		}
		result := multiple * stepValue
		if value.percent != 0 {
			return cssMathValue{percent: result}, true, true
		}
		return cssMathValue{px: result, number: value.number}, true, true
	}
	if name == "clamp" && len(args) != 3 {
		return cssMathValue{}, false, true
	}
	if name == "clamp" && noneArgs[1] {
		return cssMathValue{}, false, true
	}
	first := -1
	for i, isNone := range noneArgs {
		if !isNone {
			first = i
			break
		}
	}
	if first < 0 {
		return args[1], true, true
	}
	usePercent := args[first].px == 0 && args[first].percent != 0
	for i, arg := range args {
		if noneArgs[i] {
			continue
		}
		if arg.number != args[first].number {
			return cssMathValue{}, false, true
		}
		if usePercent && arg.px != 0 || !usePercent && arg.percent != 0 {
			return cssMathValue{}, false, true
		}
	}
	key := func(v cssMathValue) float64 {
		if usePercent {
			return v.percent
		}
		return v.px
	}
	pick := func(a, b cssMathValue, smaller bool) cssMathValue {
		if (key(b) < key(a)) == smaller && key(b) != key(a) {
			return b
		}
		return a
	}
	switch name {
	case "clamp":
		// clamp(MIN, VAL, MAX) is max(MIN, min(VAL, MAX)); MIN wins a conflict.
		if noneArgs[0] && noneArgs[2] {
			return args[1], true, true
		}
		if noneArgs[0] {
			return pick(args[1], args[2], true), true, true
		}
		if noneArgs[2] {
			return pick(args[0], args[1], false), true, true
		}
		return pick(args[0], pick(args[1], args[2], true), false), true, true
	default:
		result := args[0]
		for _, arg := range args[1:] {
			result = pick(result, arg, name == "min")
		}
		return result, true, true
	}
}

// mathFunctionNames are the CSS math functions accepted in ordinary length
// declarations.
var mathFunctionNames = []string{"calc(", "min(", "max(", "clamp(", "round(", "abs(", "sign("}

// mathFunctionAt reports the length of the math function name (including
// the opening parenthesis) starting at text[i], or 0. The name must not be
// the tail of a longer identifier such as minmax(.
func mathFunctionAt(text string, i int) int {
	if i > 0 {
		c := text[i-1]
		if c == '-' || c == '_' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' {
			return 0
		}
	}
	for _, name := range mathFunctionNames {
		if len(text)-i >= len(name) && strings.EqualFold(text[i:i+len(name)], name) {
			return len(name)
		}
	}
	return 0
}

func containsMathFunction(text string) bool {
	for i := range text {
		if mathFunctionAt(text, i) > 0 {
			return true
		}
	}
	return false
}

func startsWithMathFunction(text string) bool {
	return mathFunctionAt(strings.TrimSpace(text), 0) > 0
}

// rewriteMathUnits converts every non-percentage dimension in a math
// expression to pixels, leaving numbers and percentages untouched, so the
// expression can be kept as a computed value and finished at layout time.
func rewriteMathUnits(text string, convert func(float64, string) (cssMathValue, bool)) (string, bool) {
	var out strings.Builder
	for i := 0; i < len(text); {
		c := text[i]
		startsNumber := c >= '0' && c <= '9' || c == '.' && i+1 < len(text) && text[i+1] >= '0' && text[i+1] <= '9'
		if startsNumber && i > 0 {
			prev := text[i-1]
			if prev == '_' || prev >= 'a' && prev <= 'z' || prev >= 'A' && prev <= 'Z' || prev >= '0' && prev <= '9' || prev == '.' {
				startsNumber = false
			}
		}
		if !startsNumber {
			out.WriteByte(c)
			i++
			continue
		}
		start := i
		for i < len(text) && text[i] >= '0' && text[i] <= '9' {
			i++
		}
		if i < len(text) && text[i] == '.' {
			i++
			for i < len(text) && text[i] >= '0' && text[i] <= '9' {
				i++
			}
		}
		if i < len(text) && (text[i] == 'e' || text[i] == 'E') {
			exponent := i
			i++
			if i < len(text) && (text[i] == '+' || text[i] == '-') {
				i++
			}
			digits := i
			for i < len(text) && text[i] >= '0' && text[i] <= '9' {
				i++
			}
			if digits == i {
				i = exponent
			}
		}
		numberEnd := i
		if i < len(text) && text[i] == '%' {
			i++
		} else {
			for i < len(text) && (text[i] >= 'a' && text[i] <= 'z' || text[i] >= 'A' && text[i] <= 'Z') {
				i++
			}
		}
		unit := strings.ToLower(text[numberEnd:i])
		if unit == "" || unit == "%" || unit == "px" {
			out.WriteString(text[start:i])
			continue
		}
		n, err := strconv.ParseFloat(text[start:numberEnd], 64)
		if err != nil {
			return "", false
		}
		v, ok := convert(n, unit)
		if !ok || v.number || v.percent != 0 || !finite(v.px) {
			return "", false
		}
		out.WriteString(strconv.FormatFloat(v.px, 'f', -1, 64) + "px")
	}
	return out.String(), true
}

// parseCSSLengthMathShape type-checks a pixel/percentage math expression
// without a percentage basis by treating 1% as 1px. The value is only
// meaningful as a validity check.
func parseCSSLengthMathShape(value string) (cssMathValue, bool) {
	return parseCSSMathFunction(value, func(n float64, unit string) (cssMathValue, bool) {
		switch unit {
		case "px", "%":
			return cssMathValue{px: n}, true
		}
		return cssMathValue{}, false
	})
}

func finite(n float64) bool { return !math.IsNaN(n) && !math.IsInf(n, 0) }

func normalizeCalcValues(values ComputedStyle, viewportWidth, viewportHeight int, rootSize float64) {
	fontSize := computedFontSize(values)
	ratios := ratiosFor(values)
	convert := func(n float64, unit string) (cssMathValue, bool) {
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
	}
	for property, text := range values {
		if property == "mask-size" || property == "mask-position" ||
			property == "background-size" || property == "background-position" {
			values[property] = normalizeMathComponents(text, convert)
			continue
		}
		if strings.HasPrefix(property, "--") || !calcLengthProperty(property) {
			continue
		}
		values[property] = replaceCalcFunctions(text, func(expression string) (string, bool) {
			rewritten, ok := rewriteMathUnits(expression, convert)
			if !ok {
				return "", false
			}
			if v, ok := parseCSSMathFunction(rewritten, convert); ok && !v.number {
				if calcNonNegativeProperty(property) {
					switch {
					case v.px < 0 && v.percent <= 0, v.px <= 0 && v.percent < 0:
						// For a non-negative percentage basis these terms
						// can never produce a positive used value.
						return "0px", true
					case v.px < 0 && v.percent > 0, v.px > 0 && v.percent < 0:
						// The sign depends on the property's percentage
						// basis. Defer range clamping until layout.
						return "calc(max(0px, " + serializeCSSMath(v) + "))", true
					}
				}
				return serializeCSSMath(v), true
			}
			// A comparison between percentages and pixels cannot be
			// resolved without the layout basis; keep it as a calc()
			// wrapper so used-value callers evaluate it.
			if v, ok := parseCSSLengthMathShape(rewritten); !ok || v.number {
				return "", false
			}
			if calcNonNegativeProperty(property) && strings.Contains(rewritten, "%") {
				// Mixed percentage/length comparisons cannot be evaluated
				// until layout. Keep the expression deferred, but ensure its
				// eventual used value is clamped to the property's range.
				return "calc(max(0px, " + rewritten + "))", true
			}
			if strings.HasPrefix(strings.ToLower(rewritten), "calc(") {
				return rewritten, true
			}
			return "calc(" + rewritten + ")", true
		})
	}
}

// parseCSSMathFunction evaluates one top-level calc(), min(), max() or clamp(),
// including nested math functions.
func parseCSSMathFunction(value string, convert func(float64, string) (cssMathValue, bool)) (cssMathValue, bool) {
	p := cssMathParser{s: strings.TrimSpace(value), convert: convert, functions: true}
	v, ok, matched := p.function()
	return v, ok && matched && p.i == len(p.s) && finite(v.px) && finite(v.percent)
}

// normalizeMathComponents replaces each comma- or space-separated math
// function in a compound value (e.g. mask-size layers) with its computed
// length. Unevaluable components are left unchanged for the used-value
// parser to reject.
func normalizeMathComponents(text string, convert func(float64, string) (cssMathValue, bool)) string {
	if !strings.Contains(text, "(") {
		return text
	}
	layers := backgroundLayers(text)
	for i, layer := range layers {
		parts, ok := splitCSSComponents(layer)
		if !ok {
			continue
		}
		for j, part := range parts {
			if !strings.Contains(part, "(") {
				continue
			}
			if v, ok := parseCSSMathFunction(part, convert); ok && !v.number {
				parts[j] = serializeCSSMath(v)
			}
		}
		layers[i] = strings.Join(parts, " ")
	}
	return strings.Join(layers, ", ")
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

// replaceCalcFunctions calls replace with each top-level math function in
// text (the full function, including its name) and substitutes the result.
func replaceCalcFunctions(text string, replace func(string) (string, bool)) string {
	var out strings.Builder
	for i := 0; i < len(text); {
		start, nameLen := -1, 0
		for j := i; j < len(text); j++ {
			if n := mathFunctionAt(text, j); n > 0 {
				start, nameLen = j, n
				break
			}
		}
		if start < 0 {
			out.WriteString(text[i:])
			break
		}
		out.WriteString(text[i:start])
		depth, end := 1, start+nameLen
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
		if value, ok := replace(text[start:end]); ok {
			out.WriteString(value)
		} else {
			out.WriteString(text[start:end])
		}
		i = end
	}
	return out.String()
}

func evaluateComputedCalc(value string, basis float64) (float64, bool) {
	v, ok := parseCSSMathFunction(value, func(n float64, unit string) (cssMathValue, bool) {
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
	if !containsMathFunction(value) {
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
			if containsMathFunction(part) {
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
	if !startsWithMathFunction(value) {
		return false
	}
	// Percentages are type-checked as lengths (1% as 1px) so comparisons
	// such as min(50%, 300px) validate without a layout basis.
	v, ok := parseCSSMathFunction(value, func(n float64, unit string) (cssMathValue, bool) {
		switch unit {
		case "%", "px", "em", "rem", "ex", "ch", "vw", "vh":
			if unit == "%" && !calcPercentAllowed(property) {
				return cssMathValue{}, false
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
	v, ok := parseCSSMathFunction(value, func(n float64, unit string) (cssMathValue, bool) {
		switch unit {
		case "%":
			return cssMathValue{percent: n / 100}, true
		case "px":
			return cssMathValue{px: n}, true
		}
		return cssMathValue{}, false
	})
	if ok {
		return v.percent != 0
	}
	// Mixed percentage/pixel comparisons only parse once percentages have a
	// basis; any such expression depends on its percentage basis.
	_, ok = parseCSSLengthMathShape(value)
	return ok && strings.Contains(value, "%")
}
