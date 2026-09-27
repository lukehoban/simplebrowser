package browser

import (
	"image"
	"strings"
	"unicode/utf8"
)

// Generated content (CSS Content 3 §2). A `::before` or `::after` selector
// whose `content` computes to a string sequence creates an element-like box
// that is the originating element's first or last child. The box takes part in
// the ordinary style, layout and paint paths, so backgrounds, borders and
// masks apply to it without further special cases.
//
// The supported `content` grammar is deliberately narrow: `none`, `normal` and
// sequences of strings. `url()`, `attr()`, counters and quotes generate no box
// at all, which can only paint less than a full implementation, never more.

// pseudoBeforeName and pseudoAfterName are the synthetic element names used
// for generated boxes. They cannot collide with an HTML tag name.
const (
	pseudoBeforeName = "::before"
	pseudoAfterName  = "::after"
)

// pseudoKey identifies one generated box of one originating element. Node
// identity is stable across restyles so fetched images stay addressable.
type pseudoKey struct {
	origin *Node
	which  string // "before" or "after"
}

// legacyPseudoElements are the one-colon spellings that CSS 2.1 defined and
// the parser therefore stores without a leading ':'.
var legacyPseudoElements = map[string]bool{
	"before": true, "after": true, "first-line": true, "first-letter": true,
}

// isPseudoElementName reports whether a parsed pseudo name is a pseudo-element
// rather than a pseudo-class. The parser keeps the '::' spelling as a leading
// ':'; the four CSS 2.1 names are also pseudo-elements when written with one.
func isPseudoElementName(pseudo string) bool {
	return strings.HasPrefix(pseudo, ":") || legacyPseudoElements[pseudo]
}

// selectorPseudoElement returns which generated box a selector targets and
// whether the selector is supported at all. Unsupported pseudo-elements (and
// pseudo-elements in a non-final compound) report ok=false, so such selectors
// match neither elements nor generated boxes.
func selectorPseudoElement(s Selector) (which string, ok bool) {
	for i, part := range s.Parts {
		for _, pseudo := range part.PseudoClasses {
			if !isPseudoElementName(pseudo) {
				continue
			}
			if which != "" || i != len(s.Parts)-1 {
				return "", false
			}
			switch strings.TrimPrefix(pseudo, ":") {
			case "before":
				which = "before"
			case "after":
				which = "after"
			default:
				return "", false
			}
		}
	}
	return which, true
}

// selectorWithoutPseudoElement returns a copy of s with the trailing
// pseudo-element removed, so the remainder can be matched against the
// originating element by the ordinary selector matcher.
func selectorWithoutPseudoElement(s Selector) Selector {
	parts := make([]SelectorPart, len(s.Parts))
	copy(parts, s.Parts)
	last := &parts[len(parts)-1]
	var kept []string
	for _, pseudo := range last.PseudoClasses {
		if !isPseudoElementName(pseudo) {
			kept = append(kept, pseudo)
		}
	}
	last.PseudoClasses = kept
	return Selector{Parts: parts}
}

// generatedContent evaluates a computed `content` value. It returns the text
// of the generated box and whether a box is generated at all. An empty string
// still generates a box: that is how icon-only pseudo-elements are written.
func generatedContent(value string) (string, bool) {
	value = strings.TrimSpace(value)
	if value == "" || value == invalidVariable {
		return "", false
	}
	switch strings.ToLower(value) {
	case "none", "normal":
		return "", false
	}
	values := parseValues(value)
	if len(values) == 0 {
		return "", false
	}
	var text strings.Builder
	for _, v := range values {
		if v.Kind != "string" {
			// url(), attr(), counters, quotes and images are out of scope and
			// suppress the box entirely rather than painting something wrong.
			return "", false
		}
		decoded, ok := decodeCSSString(v.Text)
		if !ok {
			return "", false
		}
		text.WriteString(decoded)
	}
	return text.String(), true
}

// validContentDeclaration reports whether a declared `content` value matches
// the property's grammar closely enough to be kept. Declarations that do not,
// such as bad or unterminated strings, lengths or unknown keywords, are
// invalid and must be dropped during parsing instead of overriding an
// earlier valid declaration in the cascade. Valid values the renderer does
// not support yet (url(), attr(), counters, quote keywords) are kept; they
// still suppress the generated box in generatedContent. A value containing
// var() can only be checked after substitution, so it is kept here.
func validContentDeclaration(value string) bool {
	value = strings.TrimSpace(value)
	if value == "" {
		return false
	}
	if strings.Contains(strings.ToLower(value), "var(") {
		return true
	}
	switch strings.ToLower(value) {
	case "none", "normal", "inherit", "initial", "unset", "revert", "revert-layer":
		return true
	}
	values := parseValues(value)
	if len(values) == 0 {
		return false
	}
	for _, v := range values {
		switch v.Kind {
		case "string":
			if _, ok := decodeCSSString(v.Text); !ok {
				return false
			}
		case "url", "function":
			// Images, attr(), counter(s)() and similar functions.
		default:
			switch strings.ToLower(v.Text) {
			case "open-quote", "close-quote", "no-open-quote", "no-close-quote", "/":
			default:
				return false
			}
		}
	}
	return true
}

// decodeCSSString unquotes a CSS string token and resolves its escapes,
// including hex escapes and escaped newlines (line continuations). It reports
// ok=false for a bad or unterminated string: one whose closing quote is
// missing or escaped (`"x\"`), or that contains an unescaped newline (CSS
// Syntax 3 §4.3.5), since such a declaration is invalid.
func decodeCSSString(token string) (string, bool) {
	if len(token) < 2 {
		return "", false
	}
	quote := token[0]
	if quote != '"' && quote != '\'' {
		return "", false
	}
	var b strings.Builder
	for i := 1; i < len(token); {
		c := token[i]
		switch {
		case c == quote:
			// Only a closing quote that ends the token terminates the string.
			return b.String(), i == len(token)-1
		case c == '\n' || c == '\r' || c == '\f':
			return "", false // Unescaped newline: a bad string.
		case c != '\\':
			r, size := utf8.DecodeRuneInString(token[i:])
			b.WriteRune(r)
			i += size
		case i+1 >= len(token):
			return "", false // Escape at end of input: unterminated.
		case token[i+1] == '\n' || token[i+1] == '\f':
			i += 2 // Escaped newline: a line continuation adds nothing.
		case token[i+1] == '\r':
			i += 2
			if i < len(token) && token[i] == '\n' {
				i++
			}
		default:
			r, end, _ := colorEscape(token, i)
			b.WriteRune(r)
			i = end
		}
	}
	return "", false // No closing quote.
}

// replacedOrVoidElements never have generated children: their rendering is
// defined by the element itself, so ::before/::after generate no box (CSS
// Content 3 §2.1). Keeping them out also avoids handing replaced layout
// children it does not expect.
var replacedOrVoidElements = map[string]bool{
	"img": true, "br": true, "hr": true, "input": true, "textarea": true, "select": true,
	"iframe": true, "embed": true, "object": true, "canvas": true, "video": true, "audio": true,
}

// generatesPseudoElements reports whether an element can have generated boxes.
func generatesPseudoElements(origin *Node, originStyle ComputedStyle) bool {
	if origin == nil || origin.Type != ElementNode {
		return false
	}
	if replacedOrVoidElements[strings.ToLower(origin.Name)] {
		return false
	}
	// A display:none element generates no boxes at all, including generated
	// ones, whatever the pseudo-element's own display says.
	return !strings.EqualFold(strings.TrimSpace(originStyle["display"]), "none")
}

// pseudoStyledNode computes one generated box for an originating element and
// returns nil when `content` generates none. The DOM-like nodes it needs are
// cached in nodes, keyed by originating element, so repeated restyles (for
// example after a viewport change) reuse the same pointers.
func pseudoStyledNode(origin *Node, which string, originStyle ComputedStyle, rootFontSize float64,
	document StyledDocument, viewport image.Point, nodes map[pseudoKey]*Node) *StyledNode {
	if !generatesPseudoElements(origin, originStyle) {
		return nil
	}
	computed := cascade(origin, originStyle, rootFontSize, false, document.UserAgent,
		document.Stylesheets, nil, viewport, which)
	text, ok := generatedContent(computed["content"])
	if !ok {
		return nil
	}
	if strings.EqualFold(strings.TrimSpace(computed["display"]), "none") {
		return nil
	}
	key := pseudoKey{origin: origin, which: which}
	node := nodes[key]
	if node == nil {
		name := pseudoBeforeName
		if which == "after" {
			name = pseudoAfterName
		}
		node = &Node{Type: ElementNode, Name: name, Parent: origin}
		node.Children = []*Node{{Type: TextNode, Parent: node}}
		nodes[key] = node
	}
	node.Children[0].Data = text
	textStyle := cloneStyle(computed)
	delete(textStyle, "position")
	return &StyledNode{Node: node, Style: computed,
		Children: []*StyledNode{{Node: node.Children[0], Style: textStyle}}}
}
