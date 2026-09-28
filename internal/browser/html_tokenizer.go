package browser

import (
	"html"
	"strings"
)

// TokenType identifies lexical HTML tokens.
type TokenType uint8

const (
	TextToken TokenType = iota
	StartTagToken
	EndTagToken
	CommentToken
	DoctypeToken
	EOFToken
)

// Attribute retains whether an attribute was written without a value.
type Attribute struct {
	Name    string
	Value   string
	Boolean bool
}

// Token is one unit of the HTML input. Tag and attribute names are lowercase.
type Token struct {
	Type        TokenType
	Name        string
	Data        string
	Attributes  []Attribute
	SelfClosing bool
}

// Tokenizer lexes a complete HTML string. Next always makes progress until EOF.
// It is intentionally a practical HTML subset, not a full WHATWG tokenizer.
type Tokenizer struct {
	input string
	pos   int
	raw   string
	xhtml bool
	// foreignDepth tracks the SVG/MathML foreign-content scope in HTML
	// mode. CDATA sections are text only in that scope; outside it, the
	// existing HTML declaration recovery remains unchanged.
	foreignDepth int
}

func NewTokenizer(input string) *Tokenizer { return &Tokenizer{input: input} }

// NewXHTMLTokenizer enables the small set of XML lexical rules needed by
// XHTML resources. It intentionally remains a browser-oriented subset rather
// than a general-purpose XML tokenizer.
func NewXHTMLTokenizer(input string) *Tokenizer {
	return &Tokenizer{input: input, xhtml: true}
}

func (t *Tokenizer) Next() Token {
	if t.pos >= len(t.input) {
		return Token{Type: EOFToken}
	}
	s := t.input[t.pos:]
	if t.raw != "" {
		close := "</" + t.raw
		for offset := 0; offset < len(s); {
			i := strings.Index(strings.ToLower(s[offset:]), close)
			if i < 0 {
				if token, ok := t.nextRawCDATA(s, len(s)); ok {
					return token
				}
				t.pos = len(t.input)
				return Token{Type: TextToken, Data: s}
			}
			i += offset
			end := i + len(close)
			if end == len(s) || isSpace(s[end]) || s[end] == '>' || s[end] == '/' {
				if i > 0 {
					if token, ok := t.nextRawCDATA(s, i); ok {
						return token
					}
					t.pos += i
					return Token{Type: TextToken, Data: s[:i]}
				}
				t.raw = ""
				break
			}
			offset = end
		}
	}
	s = t.input[t.pos:]
	if (t.xhtml || t.foreignDepth > 0) && strings.HasPrefix(s, "<![CDATA[") {
		return t.cdataToken(s)
	}
	if s[0] != '<' {
		i := strings.IndexByte(s, '<')
		if i < 0 {
			i = len(s)
		}
		t.pos += i
		return Token{Type: TextToken, Data: decodeReferences(s[:i], false)}
	}
	if strings.HasPrefix(s, "<!--") {
		end := strings.Index(s[4:], "-->")
		if end < 0 {
			t.pos = len(t.input)
			return Token{Type: CommentToken, Data: s[4:]}
		}
		t.pos += end + 7
		return Token{Type: CommentToken, Data: s[4 : end+4]}
	}
	if len(s) >= 9 && strings.EqualFold(s[:9], "<!doctype") && (len(s) == 9 || isSpace(s[9]) || s[9] == '>') {
		end := strings.IndexByte(s, '>')
		if end < 0 {
			end = len(s)
		}
		data := strings.TrimSpace(s[9:end])
		t.pos += end
		if t.pos < len(t.input) {
			t.pos++
		}
		return Token{Type: DoctypeToken, Data: data}
	}
	if strings.HasPrefix(s, "<!") || strings.HasPrefix(s, "<?") {
		end := strings.IndexByte(s, '>')
		if end < 0 {
			end = len(s)
		}
		data := s[2:end]
		t.pos += end
		if t.pos < len(t.input) {
			t.pos++
		}
		return Token{Type: CommentToken, Data: data}
	}
	endTag := strings.HasPrefix(s, "</")
	i := 1
	if endTag {
		i++
	}
	start := i
	for i < len(s) && isName(s[i]) {
		i++
	}
	if i == start || !isLetter(s[start]) {
		t.pos++
		return Token{Type: TextToken, Data: "<"}
	}
	token := Token{Type: StartTagToken, Name: strings.ToLower(s[start:i])}
	if endTag {
		token.Type = EndTagToken
		for i < len(s) && s[i] != '>' {
			i++
		}
		if i < len(s) {
			i++
		}
		t.pos += i
		if isForeignRoot(token.Name) && t.foreignDepth > 0 {
			t.foreignDepth--
		}
		return token
	}
	seen := map[string]bool{}
	for i < len(s) {
		for i < len(s) && isSpace(s[i]) {
			i++
		}
		if i == len(s) {
			break
		}
		if s[i] == '>' {
			i++
			break
		}
		if s[i] == '/' && i+1 < len(s) && s[i+1] == '>' {
			token.SelfClosing = true
			i += 2
			break
		}
		start = i
		for i < len(s) && !isSpace(s[i]) && s[i] != '=' && s[i] != '>' && s[i] != '/' {
			i++
		}
		if start == i {
			i++ // malformed character: recover rather than loop forever
			continue
		}
		attr := Attribute{Name: strings.ToLower(s[start:i]), Boolean: true}
		for i < len(s) && isSpace(s[i]) {
			i++
		}
		if i < len(s) && s[i] == '=' {
			attr.Boolean = false
			i++
			for i < len(s) && isSpace(s[i]) {
				i++
			}
			if i < len(s) && (s[i] == '"' || s[i] == '\'') {
				quote := s[i]
				i++
				start = i
				for i < len(s) && s[i] != quote {
					i++
				}
				attr.Value = decodeReferences(s[start:i], true)
				if i < len(s) {
					i++
				}
			} else {
				start = i
				for i < len(s) && !isSpace(s[i]) && s[i] != '>' {
					i++
				}
				attr.Value = decodeReferences(s[start:i], true)
			}
		}
		if !seen[attr.Name] {
			token.Attributes = append(token.Attributes, attr)
			seen[attr.Name] = true
		}
	}
	t.pos += i
	if isForeignRoot(token.Name) && !token.SelfClosing {
		t.foreignDepth++
	}
	if token.Name == "script" || token.Name == "style" {
		if !token.SelfClosing {
			t.raw = token.Name
		}
	}
	return token
}

// nextRawCDATA splits raw text at a CDATA opener so the next call can consume
// it without exposing the delimiters to the style or script content.
func (t *Tokenizer) nextRawCDATA(s string, before int) (Token, bool) {
	if !t.xhtml && t.foreignDepth == 0 {
		return Token{}, false
	}

	i := strings.Index(s[:before], "<![CDATA[")
	if i < 0 {
		return Token{}, false
	}
	if i > 0 {
		t.pos += i
		return Token{Type: TextToken, Data: s[:i]}, true
	}
	return t.cdataToken(s), true
}

func (t *Tokenizer) cdataToken(s string) Token {
	const opener = "<![CDATA["
	content := s[len(opener):]
	end := strings.Index(content, "]]>")
	if end < 0 {
		t.pos = len(t.input)
		return Token{Type: TextToken, Data: content}
	}
	t.pos += len(opener) + end + len("]]>")
	return Token{Type: TextToken, Data: content[:end]}
}

func isForeignRoot(name string) bool {
	return name == "svg" || name == "math"
}

func isSpace(b byte) bool  { return b == ' ' || b == '\t' || b == '\n' || b == '\r' || b == '\f' }
func isLetter(b byte) bool { return b >= 'a' && b <= 'z' || b >= 'A' && b <= 'Z' }
func isName(b byte) bool {
	return isLetter(b) || b >= '0' && b <= '9' || b == ':' || b == '-' || b == '_'
}

// The standard library's entity table covers the full HTML named reference
// set. Scan one reference at a time so ambiguous semicolonless references in
// attributes (e.g. &copy=) stay literal.
func decodeReferences(s string, attribute bool) string {
	if !attribute {
		return html.UnescapeString(s)
	}
	var out strings.Builder
	for len(s) > 0 {
		amp := strings.IndexByte(s, '&')
		if amp < 0 {
			out.WriteString(s)
			break
		}
		out.WriteString(s[:amp])
		s = s[amp:]
		found := false
		max := 1
		for max < len(s) && max < 40 && (isLetter(s[max]) || s[max] >= '0' && s[max] <= '9' || s[max] == '#' || s[max] == 'x' || s[max] == 'X') {
			max++
		}
		if max < len(s) && s[max] == ';' {
			max++
		}
		for n := max; n > 1; n-- {
			part := s[:n]
			decoded := html.UnescapeString(part)
			if decoded == part {
				continue
			}
			if part[len(part)-1] != ';' && n > 2 && html.UnescapeString(part[:n-1]) != part[:n-1] &&
				decoded == html.UnescapeString(part[:n-1])+part[n-1:] {
				continue
			}
			if attribute && part[len(part)-1] != ';' && n < len(s) && (isLetter(s[n]) || s[n] >= '0' && s[n] <= '9' || s[n] == '=') {
				continue
			}
			out.WriteString(decoded)
			s = s[n:]
			found = true
			break
		}
		if !found {
			out.WriteByte('&')
			s = s[1:]
		}
	}
	return out.String()
}
