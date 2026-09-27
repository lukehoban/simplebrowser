package browser

import (
	"fmt"
	"image/color"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
	"unicode/utf8"
)

// CSS structures preserve source order for the future cascade pass (#7).
type Stylesheet struct {
	Rules []CSSRule
	URL   string
}

type CSSRule struct {
	Selectors    []Selector
	Declarations []Declaration
}

// A selector is stored left-to-right. The first part has no combinator;
// later parts use " " (descendant) or ">" (child).
type Selector struct{ Parts []SelectorPart }
type SelectorPart struct {
	Combinator string
	Tag        string
	ID         string
	Classes    []string
	Attributes []AttributeSelector
	// PseudoClasses holds lower-cased pseudo-class names such as "link".
	// Unsupported functions keep their name with a trailing "(";
	// pseudo-elements are stored with a leading ":".
	// Only supported names can match; see matchesPseudoClass.
	PseudoClasses []string
	// Each :not() contributes one unforgiving selector list. Its specificity
	// is the maximum argument specificity, not an extra pseudo-class unit.
	Negations [][]Selector
}

// AttributeSelector is the deliberately bounded attribute-selector grammar
// supported by the renderer: [name] and [name=value]. Other operators and
// modifiers are rejected with the containing rule rather than approximated.
type AttributeSelector struct {
	Name     string
	Value    string
	HasValue bool
}

type Declaration struct {
	Property  string
	Value     string // Original value, trimmed; shorthand interpretation belongs to #7.
	Values    []CSSValue
	Important bool
}

// CSSValue represents an individual value component. Unrecognized functions
// remain raw rather than being discarded (e.g. font families and gradients).
type CSSValue struct {
	Kind   string // keyword, string, number, length, percentage, color, url, function, delimiter
	Text   string
	Number float64
	Unit   string
	Color  color.RGBA
}

type cssScanner struct {
	s string
	i int
}

func cssSpace(b byte) bool { return isSpace(b) }
func cssIdent(b byte) bool {
	return isLetter(b) || b >= '0' && b <= '9' || b == '_' || b == '-'
}
func (p *cssScanner) skip() bool {
	start := p.i
	for p.i < len(p.s) {
		if cssSpace(p.s[p.i]) {
			p.i++
		} else if strings.HasPrefix(p.s[p.i:], "/*") {
			end := strings.Index(p.s[p.i+2:], "*/")
			if end < 0 {
				p.i = len(p.s)
			} else {
				p.i += end + 4
			}
		} else {
			break
		}
	}
	return p.i > start
}
func (p *cssScanner) ident() string {
	start := p.i
	for p.i < len(p.s) && cssIdent(p.s[p.i]) {
		p.i++
	}
	return p.s[start:p.i]
}

// readUntil respects strings, comments and parentheses. It returns the first
// top-level delimiter; malformed quotes consume the rest rather than spinning.
func (p *cssScanner) readUntil(delims string) (string, byte) {
	start := p.i
	var quote byte
	depth := 0
	for p.i < len(p.s) {
		c := p.s[p.i]
		if quote != 0 {
			if c == '\\' && p.i+1 < len(p.s) {
				p.i += 2
				continue
			}
			if c == quote {
				quote = 0
			}
		} else if strings.HasPrefix(p.s[p.i:], "/*") {
			end := strings.Index(p.s[p.i+2:], "*/")
			if end < 0 {
				p.i = len(p.s)
				break
			}
			p.i += end + 4
			continue
		} else if c == '"' || c == '\'' {
			quote = c
		} else if c == '(' {
			depth++
		} else if c == ')' && depth > 0 {
			depth--
		} else if depth == 0 && strings.IndexByte(delims, c) >= 0 {
			text := p.s[start:p.i]
			p.i++
			return text, c
		}
		p.i++
	}
	return p.s[start:p.i], 0
}

func stripComments(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); {
		if strings.HasPrefix(s[i:], "/*") {
			end := strings.Index(s[i+2:], "*/")
			if end < 0 {
				break
			}
			b.WriteByte(' ')
			i += end + 4
		} else if s[i] == '"' || s[i] == '\'' {
			q := s[i]
			b.WriteByte(s[i])
			i++
			for i < len(s) {
				b.WriteByte(s[i])
				if s[i] == '\\' && i+1 < len(s) {
					i++
					b.WriteByte(s[i])
				} else if s[i] == q {
					i++
					break
				}
				i++
			}
		} else {
			b.WriteByte(s[i])
			i++
		}
	}
	return b.String()
}

// ParseCSS parses supported style rules; malformed and unsupported rules are
// skipped, so a broken rule does not suppress later valid rules.
func ParseCSS(input string) Stylesheet {
	p := cssScanner{s: input}
	var sheet Stylesheet
	for p.i < len(p.s) {
		p.skip()
		if p.i >= len(p.s) {
			break
		}
		prelude, delim := p.readUntil("{;}")
		if delim != '{' {
			continue
		}
		body, _ := p.readUntil("}")
		selectors, ok := parseSelectorGroup(stripComments(prelude))
		if ok {
			sheet.Rules = append(sheet.Rules, CSSRule{Selectors: selectors, Declarations: ParseDeclarations(body)})
		}
	}
	return sheet
}

func parseSelectorGroup(s string) ([]Selector, bool) {
	return parseSelectorGroupDepth(s, 0)
}

// Bound recursive :not() parsing (and thus matching/specificity recursion).
const maxNegationDepth = 16

func parseSelectorGroupDepth(s string, depth int) ([]Selector, bool) {
	if depth > maxNegationDepth {
		return nil, false
	}
	var result []Selector
	for _, group := range splitSelectorList(s) {
		p := cssScanner{s: group}
		var selector Selector
		for {
			space := p.skip()
			if p.i >= len(p.s) {
				break
			}
			part := SelectorPart{}
			if len(selector.Parts) > 0 {
				if p.s[p.i] == '>' {
					part.Combinator = ">"
					p.i++
					p.skip()
				} else if space {
					part.Combinator = " "
				} else {
					return nil, false
				}
			}
			if p.i >= len(p.s) {
				return nil, false
			}
			start := p.i
			if p.s[p.i] == '*' {
				part.Tag = "*"
				p.i++
			} else if isLetter(p.s[p.i]) {
				part.Tag = strings.ToLower(p.ident())
			}
			for p.i < len(p.s) && (p.s[p.i] == '.' || p.s[p.i] == '#' || p.s[p.i] == ':' || p.s[p.i] == '[') {
				kind := p.s[p.i]
				p.i++
				if kind == '[' {
					attribute, ok := parseAttributeSelector(&p)
					if !ok {
						return nil, false
					}
					part.Attributes = append(part.Attributes, attribute)
					continue
				}
				if kind == ':' {
					pseudo, args, ok := parsePseudo(&p)
					if !ok {
						return nil, false
					}
					if pseudo == "not(" {
						negated, valid := parseSelectorGroupDepth(args, depth+1)
						if !valid || !validNegationArguments(negated) {
							return nil, false
						}
						part.Negations = append(part.Negations, negated)
						continue
					}
					part.PseudoClasses = append(part.PseudoClasses, pseudo)
					continue
				}
				name := p.ident()
				if name == "" {
					return nil, false
				}
				if kind == '#' {
					if part.ID != "" {
						return nil, false
					}
					part.ID = name
				} else {
					part.Classes = append(part.Classes, name)
				}
			}
			if p.i == start {
				return nil, false
			}
			selector.Parts = append(selector.Parts, part)
		}
		if len(selector.Parts) == 0 {
			return nil, false
		}
		result = append(result, selector)
	}
	return result, len(result) > 0
}

func parseAttributeSelector(p *cssScanner) (AttributeSelector, bool) {
	p.skip()
	name := strings.ToLower(p.ident())
	if name == "" {
		return AttributeSelector{}, false
	}
	p.skip()
	if p.i >= len(p.s) {
		return AttributeSelector{}, false
	}
	if p.s[p.i] == ']' {
		p.i++
		return AttributeSelector{Name: name}, true
	}
	// Reject every operator other than exact equality. In particular, do not
	// accidentally treat ~=, |=, ^=, $=, or *= as equality.
	if p.s[p.i] != '=' {
		return AttributeSelector{}, false
	}
	p.i++
	p.skip()
	if p.i >= len(p.s) {
		return AttributeSelector{}, false
	}
	var value string
	if p.s[p.i] == '"' || p.s[p.i] == '\'' {
		quote := p.s[p.i]
		p.i++
		var b strings.Builder
		closed := false
		for p.i < len(p.s) {
			if p.s[p.i] == quote {
				p.i++
				closed = true
				break
			}
			if p.s[p.i] == '\\' {
				p.i++
				if p.i >= len(p.s) {
					return AttributeSelector{}, false
				}
			}
			b.WriteByte(p.s[p.i])
			p.i++
		}
		if !closed {
			return AttributeSelector{}, false
		}
		value = b.String()
	} else {
		value = p.ident()
		if value == "" {
			return AttributeSelector{}, false
		}
	}
	p.skip()
	if p.i >= len(p.s) || p.s[p.i] != ']' {
		return AttributeSelector{}, false
	}
	p.i++
	return AttributeSelector{Name: name, Value: value, HasValue: true}, true
}

// Unknown pseudo-classes and pseudo-elements normally never match. Inside
// negation that would turn unsupported syntax into a match-all selector;
// reject the whole rule instead. Dynamic states are known, but always false
// in this static renderer, just as they are outside :not().
func validNegationArguments(selectors []Selector) bool {
	for _, selector := range selectors {
		for _, part := range selector.Parts {
			for _, pseudo := range part.PseudoClasses {
				switch pseudo {
				case "link", "any-link", "visited", "hover", "active", "focus", "last-child":
				default:
					return false
				}
			}
		}
	}
	return true
}

// splitSelectorList splits a selector list on top-level commas, so commas
// inside functional pseudo-classes such as :not(a, b) stay in one selector.
func splitSelectorList(s string) []string {
	var result []string
	depth, start := 0, 0
	for i := 0; i < len(s); i++ {
		switch s[i] {
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

// parsePseudo reads a pseudo-class or pseudo-element after its first ':'.
// Functional arguments are returned for :not() parsing. Other functions are
// kept as unsupported names and never match.
func parsePseudo(p *cssScanner) (string, string, bool) {
	prefix := ""
	if p.i < len(p.s) && p.s[p.i] == ':' {
		prefix = ":"
		p.i++
	}
	name := strings.ToLower(p.ident())
	if name == "" {
		return "", "", false
	}
	if p.i < len(p.s) && p.s[p.i] == '(' {
		start := p.i + 1
		depth := 0
		for ; p.i < len(p.s); p.i++ {
			if p.s[p.i] == '(' {
				depth++
			} else if p.s[p.i] == ')' {
				depth--
				if depth == 0 {
					args := p.s[start:p.i]
					p.i++
					return prefix + name + "(", args, true
				}
			}
		}
		return "", "", false
	}
	return prefix + name, "", true
}

// ParseDeclarations also handles inline style attributes.
func ParseDeclarations(input string) []Declaration {
	p := cssScanner{s: input}
	var result []Declaration
	for p.i < len(p.s) {
		p.skip()
		name, delim := p.readUntil(":;}")
		if delim == '}' {
			break
		}
		if delim != ':' {
			continue
		}
		value, end := p.readUntil(";}")
		name = strings.ToLower(strings.TrimSpace(stripComments(name)))
		value = strings.TrimSpace(stripComments(value))
		if name != "" && validProperty(name) && value != "" {
			important := false
			// !important is only meaningful at the end, outside strings/functions.
			v := cssScanner{s: value}
			lastBang := -1
			for v.i < len(v.s) {
				_, sep := v.readUntil("!")
				if sep == '!' {
					lastBang = v.i - 1
				}
			}
			if lastBang >= 0 && strings.EqualFold(strings.TrimSpace(value[lastBang+1:]), "important") {
				value = strings.TrimSpace(value[:lastBang])
				important = true
			}
			if value != "" {
				result = append(result, Declaration{Property: name, Value: value, Values: parseValues(value), Important: important})
			}
		}
		if end == '}' {
			break
		}
	}
	return result
}

func validProperty(s string) bool {
	if s == "" || !(isLetter(s[0]) || s[0] == '-') {
		return false
	}
	for i := 1; i < len(s); i++ {
		if !cssIdent(s[i]) {
			return false
		}
	}
	return true
}

func parseValues(s string) []CSSValue {
	var result []CSSValue
	for i := 0; i < len(s); {
		if cssSpace(s[i]) {
			i++
			continue
		}
		start := i
		if s[i] == '"' || s[i] == '\'' {
			q := s[i]
			i++
			for i < len(s) {
				if s[i] == '\\' && i+1 < len(s) {
					i += 2
				} else if s[i] == q {
					i++
					break
				} else {
					i++
				}
			}
			result = append(result, CSSValue{Kind: "string", Text: s[start:i]})
			continue
		}
		depth := 0
		var quote byte
		for i < len(s) {
			if quote != 0 {
				if s[i] == '\\' && i+1 < len(s) {
					i += 2
					continue
				}
				if s[i] == quote {
					quote = 0
				}
			} else if s[i] == '\\' {
				// An optional whitespace terminator belongs to a hex escape,
				// not to the boundary between values.
				if _, end, ok := colorEscape(s, i); ok {
					i = end
					continue
				}
			} else if s[i] == '"' || s[i] == '\'' {
				quote = s[i]
			} else if s[i] == '(' {
				depth++
			} else if s[i] == ')' && depth > 0 {
				depth--
			}
			if depth == 0 && (cssSpace(s[i]) || s[i] == ',' || s[i] == '/') {
				break
			}
			i++
		}
		if i == start {
			i++
		}
		result = append(result, classifyValue(s[start:i]))
	}
	return result
}

func classifyValue(s string) CSSValue {
	v := CSSValue{Kind: "keyword", Text: s}
	lower := strings.ToLower(s)
	if c, ok := parseColor(lower); ok {
		v.Kind, v.Color = "color", c
	} else if strings.HasPrefix(lower, "url(") && strings.HasSuffix(s, ")") {
		v.Kind = "url"
		v.Text = strings.Trim(strings.TrimSpace(s[4:len(s)-1]), "\"'")
	} else if strings.Contains(s, "(") {
		v.Kind = "function"
	} else {
		for _, unit := range []string{
			"svmin", "svmax", "lvmin", "lvmax", "dvmin", "dvmax",
			"vmin", "vmax", "svw", "svh", "lvw", "lvh", "dvw", "dvh",
			"vw", "vh", "rem", "px", "pt", "em", "ex", "ch",
			"in", "cm", "mm", "pc", "q", "%",
		} {
			if strings.HasSuffix(lower, unit) {
				if number, err := strconv.ParseFloat(s[:len(s)-len(unit)], 64); err == nil {
					v.Number, v.Unit = number, unit
					if unit == "%" {
						v.Kind = "percentage"
					} else {
						v.Kind = "length"
					}
					return v
				}
			}
		}
		if number, err := strconv.ParseFloat(s, 64); err == nil {
			v.Kind, v.Number = "number", number
		}
	}
	return v
}

var namedColors = map[string]color.RGBA{
	"black": {0, 0, 0, 255}, "white": {255, 255, 255, 255},
	"red": {255, 0, 0, 255}, "green": {0, 128, 0, 255},
	"blue": {0, 0, 255, 255}, "gray": {128, 128, 128, 255},
	"grey": {128, 128, 128, 255}, "silver": {192, 192, 192, 255},
	"orange": {255, 165, 0, 255}, "yellow": {255, 255, 0, 255},
	"lime": {0, 255, 0, 255},
	"navy": {0, 0, 128, 255}, "purple": {128, 0, 128, 255},
	"maroon": {128, 0, 0, 255}, "teal": {0, 128, 128, 255},
	"transparent": {0, 0, 0, 0},
}

// colorEscape consumes one CSS escape, including a hex escape's optional
// whitespace terminator. Invalid newlines/EOF are not escaped characters.
func colorEscape(s string, start int) (rune, int, bool) {
	i := start + 1
	if i >= len(s) || strings.ContainsRune("\n\r\f", rune(s[i])) {
		return 0, i, false
	}
	hexStart := i
	for i < len(s) && i-hexStart < 6 && strings.ContainsRune("0123456789abcdefABCDEF", rune(s[i])) {
		i++
	}
	if i > hexStart {
		n, _ := strconv.ParseUint(s[hexStart:i], 16, 32)
		r := rune(n)
		if r == 0 || !utf8.ValidRune(r) {
			r = utf8.RuneError
		}
		if i < len(s) && cssSpace(s[i]) {
			if s[i] == '\r' && i+1 < len(s) && s[i+1] == '\n' {
				i++
			}
			i++
		}
		return r, i, true
	}
	r, size := utf8.DecodeRuneInString(s[i:])
	return r, i + size, true
}

// colorKeyword decodes an identifier, never a quoted string or a hash token.
// Decode before case folding: \47 is G, even if the source was lowercased.
func colorKeyword(s string) (string, bool) {
	var b strings.Builder
	for i := 0; i < len(s); {
		if s[i] == '\\' {
			r, end, ok := colorEscape(s, i)
			if !ok {
				return "", false
			}
			b.WriteRune(r)
			i = end
		} else {
			if !cssIdent(s[i]) {
				return "", false
			}
			b.WriteByte(s[i])
			i++
		}
	}
	return strings.ToLower(b.String()), true
}

func parseColor(s string) (color.RGBA, bool) {
	keyword, _ := colorKeyword(s)
	if c, ok := namedColors[keyword]; ok {
		return c, true
	}
	if strings.HasPrefix(s, "#") {
		h := s[1:]
		if len(h) != 3 && len(h) != 4 && len(h) != 6 && len(h) != 8 {
			return color.RGBA{}, false
		}
		var parts [4]uint8
		parts[3] = 255
		step := len(h) / 3
		if len(h) == 4 || len(h) == 8 {
			step = len(h) / 4
		}
		for i := 0; i < len(h)/step; i++ {
			n, err := strconv.ParseUint(h[i*step:(i+1)*step], 16, 8)
			if err != nil {
				return color.RGBA{}, false
			}
			if step == 1 {
				n *= 17
			}
			parts[i] = uint8(n)
		}
		return color.RGBA{parts[0], parts[1], parts[2], parts[3]}, true
	}
	alpha := false
	switch {
	case strings.HasPrefix(s, "rgb(") && strings.HasSuffix(s, ")"):
		s = s[4 : len(s)-1]
	case strings.HasPrefix(s, "rgba(") && strings.HasSuffix(s, ")"):
		s = s[5 : len(s)-1]
		alpha = true
	default:
		return color.RGBA{}, false
	}
	parts := strings.Split(s, ",")
	if len(parts) != 3 && !(alpha && len(parts) == 4) || alpha && len(parts) != 4 {
		return color.RGBA{}, false
	}
	c := color.RGBA{A: 255}
	channels := []*uint8{&c.R, &c.G, &c.B}
	for i := 0; i < 3; i++ {
		t := strings.TrimSpace(parts[i])
		percent := strings.HasSuffix(t, "%")
		if percent {
			t = strings.TrimSuffix(t, "%")
		}
		n, err := strconv.ParseFloat(t, 64)
		if err != nil || n < 0 || (percent && n > 100) || (!percent && n > 255) {
			return color.RGBA{}, false
		}
		if percent {
			n = n * 255 / 100
		}
		*channels[i] = uint8(n + .5)
	}
	if alpha {
		n, err := strconv.ParseFloat(strings.TrimSpace(parts[3]), 64)
		if err != nil || n < 0 || n > 1 {
			return color.RGBA{}, false
		}
		c.A = uint8(n*255 + .5)
	}
	return c, true
}

// ExtractStyles collects author sheets in DOM order and inline declarations
// keyed by node. The UA sheet is returned separately for #7's origin ordering.
// External resources use the supplied fetcher, allowing deterministic tests.
func ExtractStyles(doc Document, fetcher *Fetcher) ([]Stylesheet, map[*Node][]Declaration, error) {
	if fetcher == nil {
		fetcher = &Fetcher{}
	}
	var sheets []Stylesheet
	inline := make(map[*Node][]Declaration)
	var walk func(*Node) error
	walk = func(n *Node) error {
		if n.Type == ElementNode {
			if attr, ok := n.Attribute("style"); ok {
				inline[n] = ParseDeclarations(attr.Value)
			}
			switch n.Name {
			case "style":
				var text strings.Builder
				for _, child := range n.Children {
					if child.Type == TextNode {
						text.WriteString(child.Data)
					}
				}
				sheets = append(sheets, ParseCSS(text.String()))
			case "link":
				rel, _ := n.Attribute("rel")
				href, _ := n.Attribute("href")
				for _, word := range strings.Fields(strings.ToLower(rel.Value)) {
					if word == "stylesheet" && href.Value != "" {
						resolved, err := ResolveCSSURL(doc.BaseURL, href.Value)
						if err != nil {
							return fmt.Errorf("stylesheet URL %q: %w", href.Value, err)
						}
						resource, err := fetcher.Fetch(resolved)
						if err != nil {
							return fmt.Errorf("fetch stylesheet %q: %w", resolved, err)
						}
						sheet := ParseCSS(string(resource.Body))
						sheet.URL = resource.URL
						sheets = append(sheets, sheet)
						break
					}
				}
			}
		}
		for _, child := range n.Children {
			if err := walk(child); err != nil {
				return err
			}
		}
		return nil
	}
	if doc.Root != nil {
		if err := walk(doc.Root); err != nil {
			return nil, nil, err
		}
	}
	return sheets, inline, nil
}

// ResolveCSSURL supports HTTP(S), file URLs and local filesystem bases.
func ResolveCSSURL(base, reference string) (string, error) {
	ref, err := url.Parse(strings.TrimSpace(reference))
	if err != nil {
		return "", err
	}
	if base == "" {
		return "", fmt.Errorf("missing document base URL")
	}
	if isLocalPath(base) && !strings.HasPrefix(strings.ToLower(base), "file:") {
		if ref.Scheme != "" || ref.Host != "" {
			return ref.String(), nil
		}
		if ref.RawQuery != "" || ref.Fragment != "" {
			return "", fmt.Errorf("local stylesheet URL cannot have query or fragment")
		}
		path := filepath.FromSlash(ref.Path)
		if filepath.IsAbs(path) {
			return filepath.Clean(path), nil
		}
		// Return a stable path even when the document was opened by a
		// relative name: background URLs are resolved again when fetched.
		return filepath.Abs(filepath.Join(filepath.Dir(base), path))
	}
	u, err := url.Parse(base)
	if err != nil {
		return "", err
	}
	return u.ResolveReference(ref).String(), nil
}

const uaCSS = `
html, body, div, p, pre, blockquote, ul, ol, li, table, tr, td, th,
header, footer, section, article, main, h1, h2, h3, h4, h5, h6 { display: block; }
head, meta, link, style, script, title { display: none; }
body { margin: 8px; }
a { color: blue; text-decoration: underline; }
b, strong, th { font-weight: bold; }
i, em { font-style: italic; }
h1 { font-size: 2em; margin: .67em 0; }
h2 { font-size: 1.5em; margin: .83em 0; }
p { margin: 1em 0; }
ul, ol { margin: 1em 0; padding-left: 40px; }
center { display: block; text-align: center; }
table { display: table; border-spacing: 2px; }
tr { display: table-row; }
td, th { display: table-cell; text-align: start; }
img { display: inline-block; }
`

// UserAgentStylesheet returns fresh rules so callers may modify them safely.
func UserAgentStylesheet() Stylesheet { return ParseCSS(uaCSS) }
