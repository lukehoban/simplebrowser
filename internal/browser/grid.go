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
	columns := strings.Fields(strings.TrimSpace(style["grid-template-columns"]))
	if len(columns) != 3 || columns[0] != "min-content" ||
		columns[1] != "minmax(0,auto)" || columns[2] != "min-content" {
		return false
	}
	areas := strings.TrimSpace(style["grid-template-areas"])
	if len(areas) < 2 || (areas[0] != '"' && areas[0] != '\'') || areas[len(areas)-1] != areas[0] {
		return false
	}
	return len(strings.Fields(areas[1:len(areas)-1])) == 3
}

func gridAreaNames(style ComputedStyle) []string {
	area := strings.TrimSpace(style["grid-template-areas"])
	if len(area) < 2 {
		return nil
	}
	return strings.Fields(area[1 : len(area)-1])
}

func gridGap(style ComputedStyle, width int) int {
	value := strings.TrimSpace(style["column-gap"])
	if value == "" {
		value = strings.TrimSpace(style["gap"])
	}
	return max(0, int(px(value, float64(width), 0)))
}

func layoutGrid(parent *StyledNode, x, y, width int, faces *faceSet, cb containingBlock) ([]*Box, int) {
	names := gridAreaNames(parent.Style)
	gap := gridGap(parent.Style, width)
	items, _ := flexChildren(parent)
	if len(items) > len(names) {
		items = items[:len(names)]
	}
	tracks := [3]int{}
	for i, item := range items {
		minimum, _ := intrinsicWidths(item, faces)
		if i < 3 {
			tracks[i] = minimum
		}
	}
	remaining := max(0, width-gap*2-tracks[0]-tracks[2])
	tracks[1] = remaining
	positions := [3]int{x, x + tracks[0] + gap, x + tracks[0] + gap + tracks[1] + gap}

	boxes := make([]*Box, 0, len(items))
	bottom := y
	for i, item := range items {
		column := i
		if area := strings.TrimSpace(item.Style["grid-area"]); area != "" {
			for j, name := range names {
				if strings.EqualFold(area, name) {
					column = j
					break
				}
			}
		}
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

func asGridItem(n *StyledNode) *StyledNode {
	if n == nil {
		return n
	}
	clone := *n
	// Grid items establish an independent formatting context for this subset.
	clone.flexItem = true
	return &clone
}
