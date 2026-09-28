package browser

import (
	"image"
	"sort"
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

func layerCount(sheets []Stylesheet, viewport image.Point) int {
	seen := map[string]bool{}
	for _, sheet := range sheets {
		for _, declaration := range sheet.LayerDeclarations {
			if declaration.Media == "" || mediaQueryMatches(declaration.Media, viewport) {
				seen[declaration.Name] = true
			}
		}
	}
	return len(seen)
}

// layerOrders exposes the same document-wide layer ranks used by the host
// cascade to renderers that perform a second cascade over a subtree. Every
// declared layer is included, even one without rules, so that the subtree
// can merge its own declarations into the complete host registry.
func layerOrders(sheets []Stylesheet, viewport image.Point) map[string]int {
	orders, unlayered := layerRanks(sheets, viewport)
	orders[""] = unlayered
	return orders
}

func joinLayerName(parent, child string) string {
	if parent == "" {
		return child
	}
	return parent + "." + child
}

func (s *Stylesheet) declareLayer(name, media string) {
	for i := 0; i <= len(name); i++ {
		if i < len(name) && name[i] != '.' {
			continue
		}
		prefix := name[:i]
		s.LayerDeclarations = append(s.LayerDeclarations, LayerDeclaration{Name: prefix, Media: media})
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

// assignLayerOrder merges layer trees in stylesheet order. Later sibling
// layers have higher normal precedence. A parent layer's own declarations
// occupy its implicit final sublayer, after its explicitly nested layers.
func assignLayerOrder(sheets []Stylesheet, viewport image.Point) {
	rank, unlayered := layerRanks(sheets, viewport)
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

// layerRanks returns the rank of every layer declared (under matching media)
// by sheets, taken in order, and the rank of unlayered declarations.
func layerRanks(sheets []Stylesheet, viewport image.Point) (map[string]int, int) {
	children := map[string][]string{"": nil}
	known := map[string]bool{}
	for _, sheet := range sheets {
		for _, declaration := range sheet.LayerDeclarations {
			if declaration.Media != "" && !mediaQueryMatches(declaration.Media, viewport) {
				continue
			}
			name := declaration.Name
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
			visit(name)
			rank[name] = next
			next++
		}
	}
	visit("")
	return rank, next
}

// hostLayerRegistry rebuilds a declaration sequence equivalent to a document
// layer order produced by layerOrders. Declaring names in rank (post-order)
// sequence preserves every sibling's relative order, so re-merging it with
// later sheets reproduces the host tree before appending their new layers.
func hostLayerRegistry(orders map[string]int) Stylesheet {
	names := make([]string, 0, len(orders))
	for name := range orders {
		if name != "" {
			names = append(names, name)
		}
	}
	sort.Slice(names, func(i, j int) bool {
		if orders[names[i]] != orders[names[j]] {
			return orders[names[i]] < orders[names[j]]
		}
		return names[i] < names[j]
	})
	var sheet Stylesheet
	for _, name := range names {
		sheet.LayerDeclarations = append(sheet.LayerDeclarations, LayerDeclaration{Name: name})
	}
	return sheet
}

func cloneStylesheetsForLayerOrder(sheets []Stylesheet) []Stylesheet {
	cloned := append([]Stylesheet(nil), sheets...)
	for i := range cloned {
		cloned[i].Rules = append([]CSSRule(nil), sheets[i].Rules...)
	}
	return cloned
}
