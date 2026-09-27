package browser

import (
	"mime"
	"path/filepath"
	"strings"
)

// NodeType describes a DOM node. The document root contains the parsed
// fragment as children; implicit html/head/body wrappers are not synthesized.
type NodeType uint8

const (
	DocumentNode NodeType = iota
	ElementNode
	TextNode
	CommentNode
	DoctypeNode
)

// Node is a small DOM representation for later style and layout passes.
type Node struct {
	Type       NodeType
	Name       string
	Data       string
	Attributes []Attribute
	Parent     *Node
	Children   []*Node
}

// Attribute returns the first matching attribute (including boolean ones).
func (n *Node) Attribute(name string) (Attribute, bool) {
	for _, attr := range n.Attributes {
		if attr.Name == strings.ToLower(name) {
			return attr, true
		}
	}
	return Attribute{}, false
}

func (n *Node) append(child *Node) {
	child.Parent = n
	n.Children = append(n.Children, child)
}

// ParseHTML builds a tolerant DOM from an HTML fragment or document.
func ParseHTML(input string) *Node {
	return parseMarkup(NewTokenizer(input))
}

// ParseXHTML builds a DOM using the focused XHTML lexical mode. This mode
// recognizes XML CDATA sections while deliberately retaining the parser's
// tolerant recovery behavior; it is not a complete XML implementation.
func ParseXHTML(input string) *Node {
	return parseMarkup(NewXHTMLTokenizer(input))
}

func parseMarkup(t *Tokenizer) *Node {
	root := &Node{Type: DocumentNode}
	stack := []*Node{root}
	for {
		token := t.Next()
		if token.Type == EOFToken {
			break
		}
		if token.Type == EndTagToken {
			for i := len(stack) - 1; i > 0; i-- {
				if stack[i].Name == token.Name {
					stack = stack[:i]
					break
				}
				// Do not let a stray inline closing tag destroy an unrelated
				// table/list context. Matching an ancestor is still tolerated.
			}
			continue
		}
		if token.Type == StartTagToken {
			name := token.Name
			closeImplied(&stack, name)
			node := &Node{Type: ElementNode, Name: name, Attributes: token.Attributes}
			stack[len(stack)-1].append(node)
			if !token.SelfClosing && !voidElement(name) {
				stack = append(stack, node)
			}
			continue
		}
		node := &Node{Data: token.Data}
		switch token.Type {
		case TextToken:
			if node.Data == "" {
				continue
			}
			node.Type = TextNode
			parent := stack[len(stack)-1]
			if count := len(parent.Children); count > 0 && parent.Children[count-1].Type == TextNode {
				parent.Children[count-1].Data += node.Data
				continue
			}
		case CommentToken:
			node.Type = CommentNode
		case DoctypeToken:
			node.Type = DoctypeNode
		}
		stack[len(stack)-1].append(node)
	}
	return root
}

func parse(resource Resource) (Document, error) {
	base := resource.URL
	if base == "" {
		base = resource.Source // callers constructing resources directly
	}
	parseDocument := ParseHTML
	if isXHTMLResource(resource) {
		parseDocument = ParseXHTML
	}
	return Document{Resource: resource, Root: parseDocument(string(resource.Body)), BaseURL: base}, nil
}

func isXHTMLResource(resource Resource) bool {
	if mediaType, _, err := mime.ParseMediaType(resource.ContentType); err == nil &&
		strings.EqualFold(mediaType, "application/xhtml+xml") {
		return true
	}
	source := resource.URL
	if source == "" {
		source = resource.Source
	}
	return isLocalPath(source) && strings.EqualFold(filepath.Ext(localPath(source)), ".xht")
}

func voidElement(name string) bool {
	switch name {
	case "area", "base", "br", "col", "embed", "hr", "img", "input", "link", "meta", "param", "source", "track", "wbr":
		return true
	}
	return false
}

func closesParagraph(name string) bool {
	switch name {
	case "address", "article", "aside", "blockquote", "div", "dl", "fieldset", "footer", "form",
		"h1", "h2", "h3", "h4", "h5", "h6", "header", "hgroup", "hr", "main", "menu",
		"nav", "ol", "p", "pre", "section", "table", "ul":
		return true
	}
	return false
}

func closeImplied(stack *[]*Node, name string) {
	nodes := *stack
	// Paragraphs can contain inline descendants; a new block closes them.
	if closesParagraph(name) {
		for i := len(nodes) - 1; i > 0; i-- {
			if nodes[i].Name == "p" {
				nodes = nodes[:i]
				break
			}
		}
	}
	target := ""
	switch name {
	case "li", "dt", "dd":
		target = name
	case "tr":
		target = "tr"
	case "td", "th":
		target = "cell"
	}
	if target != "" {
		for i := len(nodes) - 1; i > 0; i-- {
			n := nodes[i].Name
			match := n == target || target == "cell" && (n == "td" || n == "th") ||
				(name == "dt" || name == "dd") && (n == "dt" || n == "dd")
			if match {
				nodes = nodes[:i]
				break
			}
			// Scope boundaries prevent closing a list item in a nested list,
			// or a table row/cell in a nested table.
			if n == "table" || (target == "li" && (n == "ul" || n == "ol")) ||
				(target == "cell" && n == "tr") || (target == "tr" && n == "tbody") ||
				((name == "dt" || name == "dd") && n == "dl") {
				break
			}
		}
	}
	*stack = nodes
}
