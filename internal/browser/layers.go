package browser

import (
	"strconv"
	"strings"
	"sync/atomic"
)

// Layer names are kept separately from selector parsing so that layer
// ordering statements can affect rules in later (or earlier) stylesheets.
var anonymousLayerID atomic.Uint64

func layerPrelude(prelude string) (string, bool) {
	if len(prelude) < len("@layer") || !strings.EqualFold(prelude[:len("@layer")], "@layer") {
		return "", false
	}
	rest := prelude[len("@layer"):]
	if rest != "" && !cssSpace(rest[0]) {
		return "", false
	}
	return strings.TrimSpace(rest), true
}

func layerBlockName(prelude string) (string, bool) {
	name, ok := layerPrelude(prelude)
	if !ok {
		return "", false
	}
	if name == "" {
		return "\x00anonymous-" + strconv.FormatUint(anonymousLayerID.Add(1), 10), true
	}
	if !validLayerName(name) {
		return "", false
	}
	return name, true
}

func layerStatementNames(prelude string) ([]string, bool) {
	namesText, ok := layerPrelude(prelude)
	if !ok || namesText == "" {
		return nil, false
	}
	parts := strings.Split(namesText, ",")
	names := make([]string, 0, len(parts))
	for _, part := range parts {
		name := strings.TrimSpace(part)
		if !validLayerName(name) {
			return nil, false
		}
		names = append(names, name)
	}
	return names, true
}

// This renderer's CSS identifier scanner is ASCII-only; apply the same
// deliberately bounded identifier grammar to layer names rather than
// accepting malformed names as if they were valid layer declarations.
func validLayerName(name string) bool {
	if name == "" {
		return false
	}
	for _, part := range strings.Split(name, ".") {
		if part == "" || part == "-" || (part[0] >= '0' && part[0] <= '9') ||
			(part[0] == '-' && len(part) > 1 && part[1] >= '0' && part[1] <= '9') {
			return false
		}
		for i := 0; i < len(part); i++ {
			if c := part[i]; !cssIdent(c) {
				return false
			}
		}
	}
	return true
}

func layerCount(sheets []Stylesheet) int {
	seen := map[string]bool{}
	for _, sheet := range sheets {
		for _, name := range sheet.Layers {
			seen[name] = true
		}
	}
	return len(seen)
}

func joinLayerName(parent, child string) string {
	if parent == "" {
		return child
	}
	return parent + "." + child
}

func (s *Stylesheet) declareLayer(name string) {
	for i := 0; i <= len(name); i++ {
		if i < len(name) && name[i] != '.' {
			continue
		}
		prefix := name[:i]
		found := false
		for _, existing := range s.Layers {
			if existing == prefix {
				found = true
				break
			}
		}
		if !found {
			s.Layers = append(s.Layers, prefix)
		}
	}
}

// assignLayerOrder merges layer trees in stylesheet order. The preorder
// rank gives later sibling layers higher normal precedence; a layer's own
// declarations precede its nested layers, as required by the cascade.
func assignLayerOrder(sheets []Stylesheet) {
	children := map[string][]string{"": nil}
	known := map[string]bool{}
	for _, sheet := range sheets {
		for _, name := range sheet.Layers {
			if known[name] {
				continue
			}
			known[name] = true
			parent := ""
			if dot := strings.LastIndexByte(name, '.'); dot >= 0 {
				parent = name[:dot]
			}
			children[parent] = append(children[parent], name)
			if _, ok := children[name]; !ok {
				children[name] = nil
			}
		}
	}
	rank := make(map[string]int, len(known))
	next := 0
	var visit func(string)
	visit = func(parent string) {
		for _, name := range children[parent] {
			rank[name] = next
			next++
			visit(name)
		}
	}
	visit("")
	unlayered := next
	for i := range sheets {
		for j := range sheets[i].Rules {
			rule := &sheets[i].Rules[j]
			if rule.Layer == "" {
				rule.LayerOrder = unlayered
			} else if value, ok := rank[rule.Layer]; ok {
				rule.LayerOrder = value
			}
		}
	}
}
