package browser

import "strings"

// This is intentionally a small, explicit Grid formatting context.  It
// covers the three named areas used by the current GitHub controls, rather
// than treating every unknown grid declaration as supported.  Keeping this
// separate from flex also makes the @supports registry honest.
func isGridContainer(n *StyledNode) bool {
	return n != nil && strings.EqualFold(strings.TrimSpace(n.Style["display"]), "grid") &&
		gridTemplateSupported(n.Style)
}

func gridTemplateSupported(style ComputedStyle) bool {
	columns := cssWhitespaceFields(style["grid-template-columns"])
	if len(columns) != 3 || columns[0] != "min-content" ||
		columns[1] != "minmax(0,auto)" || columns[2] != "min-content" {
		return false
	}
	_, ok := parseGridTemplateAreas(style["grid-template-areas"])
	return ok
}

// parseGridTemplateAreas accepts only the single-row, three-unique-area
// shape that layoutGrid places. Dot (null) cells, repeated or spanning names,
// and multiple rows would fall back to normal flow, so they are rejected here
// and by @supports, which shares this validator.
func parseGridTemplateAreas(value string) ([]string, bool) {
	v := trimCSSWhitespace(value)
	if len(v) < 2 || (v[0] != '"' && v[0] != '\'') || v[len(v)-1] != v[0] {
		return nil, false
	}
	names := cssWhitespaceFields(v[1 : len(v)-1])
	if len(names) != 3 {
		return nil, false
	}
	seen := make(map[string]bool, len(names))
	for _, name := range names {
		if !cssIdentString(name) || seen[name] {
			return nil, false
		}
		seen[name] = true
	}
	return names, true
}

func isCSSWhitespace(b byte) bool {
	switch b {
	case ' ', '\t', '\n', '\r', '\f':
		return true
	default:
		return false
	}
}

func trimCSSWhitespace(value string) string {
	start, end := 0, len(value)
	for start < end && isCSSWhitespace(value[start]) {
		start++
	}
	for end > start && isCSSWhitespace(value[end-1]) {
		end--
	}
	return value[start:end]
}

// cssWhitespaceFields splits CSS component values without treating other
// Unicode spaces as separators. In particular, NBSP is valid identifier
// content and must remain part of the surrounding name.
func cssWhitespaceFields(value string) []string {
	var fields []string
	for i := 0; i < len(value); {
		for i < len(value) && isCSSWhitespace(value[i]) {
			i++
		}
		start := i
		for i < len(value) && !isCSSWhitespace(value[i]) {
			i++
		}
		if start < i {
			fields = append(fields, value[start:i])
		}
	}
	return fields
}

func gridGap(style ComputedStyle, width int) int {
	value := strings.TrimSpace(style["column-gap"])
	if value == "" {
		value = strings.TrimSpace(style["gap"])
	}
	return max(0, int(px(value, float64(width), 0)))
}

func layoutGrid(parent *StyledNode, x, y, width int, faces *faceSet, cb containingBlock) ([]*Box, int) {
	names, ok := parseGridTemplateAreas(parent.Style["grid-template-areas"])
	if !ok {
		return layoutGridFlowFallback(parent, x, y, width, faces, cb)
	}
	gap := gridGap(parent.Style, width)
	items, outOfFlow := flexChildren(parent)
	columns := make(map[string]int, len(names))
	for i, name := range names {
		if name == "." {
			return layoutGridFlowFallback(parent, x, y, width, faces, cb)
		}
		if _, exists := columns[name]; exists {
			return layoutGridFlowFallback(parent, x, y, width, faces, cb)
		}
		columns[name] = i
	}
	// This subset only lays out one item in each named area. Falling back for
	// every other shape preserves content until placement is implemented;
	// truncating children here would silently lose page content.
	if len(names) != 3 || len(items) != len(names) || len(outOfFlow) != 0 {
		return layoutGridFlowFallback(parent, x, y, width, faces, cb)
	}
	itemColumns := make([]int, len(items))
	seen := make(map[string]bool, len(items))
	for i, item := range items {
		area := trimCSSWhitespace(item.Style["grid-area"])
		column, ok := columns[area]
		if !ok || seen[area] {
			return layoutGridFlowFallback(parent, x, y, width, faces, cb)
		}
		seen[area] = true
		itemColumns[i] = column
	}
	if len(seen) != len(names) {
		return layoutGridFlowFallback(parent, x, y, width, faces, cb)
	}
	tracks := [3]int{}
	for i, item := range items {
		minimum, _ := intrinsicWidths(item, faces)
		column := itemColumns[i]
		if column == 0 || column == 2 {
			tracks[column] = max(tracks[column], minimum)
		}
	}
	remaining := max(0, width-gap*2-tracks[0]-tracks[2])
	tracks[1] = remaining
	positions := [3]int{x, x + tracks[0] + gap, x + tracks[0] + gap + tracks[1] + gap}

	boxes := make([]*Box, 0, len(items))
	bottom := y
	for i, item := range items {
		column := itemColumns[i]
		box, _ := layoutBlock(asGridItem(item), positions[column], y, tracks[column], faces, cb)
		boxes = append(boxes, box)
		bottom = max(bottom, box.Rect.Max.Y)
	}
	height := bottom - y
	if strings.EqualFold(strings.TrimSpace(parent.Style["align-items"]), "center") {
		for _, box := range boxes {
			dy := (height - box.Rect.Dy()) / 2
			if dy > 0 {
				translateBox(box, 0, dy)
			}
		}
	}
	return boxes, y + height
}

func layoutGridFlowFallback(parent *StyledNode, x, y, width int, faces *faceSet, cb containingBlock) ([]*Box, int) {
	boxes, bottom, _ := layoutFlow(parent, x, y, width, faces, false, false, cb)
	return boxes, bottom
}

func asGridItem(n *StyledNode) *StyledNode {
	if n == nil {
		return n
	}
	clone := *n
	// Grid items establish an independent formatting context for this subset.
	clone.flexItem = true
	return &clone
}
