package browser

import (
	"math"
	"strconv"
	"strings"
)

// This is the deliberately small flex formatting context used by the pinned
// GitHub and Moon fixtures. It supports a single row or column, flexible main
// sizes, gaps and the common main/cross-axis alignment values. Wrapping,
// ordering and baseline synthesis remain outside this renderer's flex scope.
func isFlexContainer(n *StyledNode) bool {
	if n == nil {
		return false
	}
	switch strings.ToLower(strings.TrimSpace(n.Style["display"])) {
	case "flex", "inline-flex":
		return true
	}
	return false
}

type flexItem struct {
	node          *StyledNode
	box           *Box
	margin        [4]int
	main, grow    float64
	shrink        float64
	explicitCross bool
}

func layoutFlex(parent *StyledNode, x, y, width, containerHeight int, heightDefinite bool, faces *faceSet, cb containingBlock) ([]*Box, int) {
	column := strings.HasPrefix(strings.ToLower(strings.TrimSpace(parent.Style["flex-direction"])), "column")
	reverse := strings.HasSuffix(strings.ToLower(strings.TrimSpace(parent.Style["flex-direction"])), "reverse")
	var nodes []*StyledNode
	var outOfFlow []*StyledNode
	for _, child := range parent.Children {
		if childFlowKind(child) == flowSkip || (child.Node.Type == TextNode && strings.TrimSpace(child.Node.Data) == "") {
			continue
		}
		if positioned(child) {
			outOfFlow = append(outOfFlow, child)
			continue
		}
		// The fixtures contain element flex items. Keeping non-whitespace text
		// in one anonymous item would require a synthetic styled node.
		if child.Node.Type == ElementNode {
			nodes = append(nodes, child)
		}
	}
	if reverse {
		for left, right := 0, len(nodes)-1; left < right; left, right = left+1, right-1 {
			nodes[left], nodes[right] = nodes[right], nodes[left]
		}
	}
	gap := flexGap(parent.Style, column, width)
	availableMain := width
	if column && heightDefinite {
		availableMain = containerHeight
	}

	items := make([]flexItem, 0, len(nodes))
	total := float64(max(0, len(nodes)-1) * gap)
	for _, child := range nodes {
		margin := boxEdges(child, "margin", float64(width))
		grow, shrink, basis, hasBasis := flexFactors(child.Style, availableMain)
		main := 0.0
		if column {
			if h, ok := specifiedHeight(child, containerHeight, heightDefinite); ok {
				main = float64(h)
			} else if hasBasis {
				main = basis
			}
		} else {
			if hasBasis {
				main = basis
			} else if value := strings.TrimSpace(child.Style["width"]); value != "" && !strings.EqualFold(value, "auto") {
				main = px(value, float64(width), 0)
			} else {
				_, preferred := contentIntrinsicWidths(child, faces)
				main = float64(preferred)
			}
			if minWidth := strings.TrimSpace(child.Style["min-width"]); minWidth != "" && !strings.EqualFold(minWidth, "auto") {
				main = math.Max(main, px(minWidth, float64(width), 0))
			}
		}
		main = math.Max(0, main)
		inner := inlineInnerEdges(child, width)
		extras := margin[1] + margin[3] + inner[1] + inner[3]
		if column {
			extras = margin[0] + margin[2] + inner[0] + inner[2]
		}
		total += main + float64(extras)
		items = append(items, flexItem{node: child, margin: margin, main: main, grow: grow, shrink: shrink,
			explicitCross: flexHasCrossSize(child, column)})
	}

	// An auto-height column grows to its contents; a definite main axis
	// distributes positive or negative free space by grow/shrink factors.
	if !column || heightDefinite {
		free := float64(availableMain) - total
		if free > 0 {
			var sum float64
			for _, item := range items {
				sum += item.grow
			}
			if sum > 0 {
				for i := range items {
					items[i].main += free * items[i].grow / sum
				}
			}
		} else if free < 0 {
			var sum float64
			for _, item := range items {
				sum += item.shrink * item.main
			}
			if sum > 0 {
				for i := range items {
					items[i].main = math.Max(0, items[i].main+free*(items[i].shrink*items[i].main)/sum)
				}
			}
		}
	}

	// Lay out at the origin first so the cross size is known before alignment.
	cross := 0
	for i := range items {
		style := cloneStyle(items[i].node.Style)
		if column {
			style["height"] = formatFlexPixels(items[i].main)
		} else {
			style["width"] = formatFlexPixels(items[i].main)
		}
		used := &StyledNode{Node: items[i].node.Node, Style: style, Children: items[i].node.Children}
		itemY := y + items[i].margin[0]
		items[i].box, _ = layoutBlock(used, x, itemY, width, faces, cb)
		itemCross := items[i].box.Rect.Dy() + items[i].margin[0] + items[i].margin[2]
		if column {
			itemCross = items[i].box.Rect.Dx() + items[i].margin[1] + items[i].margin[3]
		}
		cross = max(cross, itemCross)
	}
	if !column && heightDefinite {
		cross = containerHeight
	}
	if column {
		cross = width
	}

	// Stretch auto cross sizes, then align and place along the main axis.
	align := strings.ToLower(strings.TrimSpace(parent.Style["align-items"]))
	if align == "" || align == "normal" {
		align = "stretch"
	}
	for i := range items {
		if align == "stretch" && !items[i].explicitCross {
			style := cloneStyle(items[i].node.Style)
			if column {
				inner := inlineInnerEdges(items[i].node, width)
				style["width"] = formatFlexPixels(float64(max(0, cross-items[i].margin[1]-items[i].margin[3]-inner[1]-inner[3])))
				style["height"] = formatFlexPixels(items[i].main)
			} else {
				inner := inlineInnerEdges(items[i].node, width)
				style["width"] = formatFlexPixels(items[i].main)
				style["height"] = formatFlexPixels(float64(max(0, cross-items[i].margin[0]-items[i].margin[2]-inner[0]-inner[2])))
			}
			used := &StyledNode{Node: items[i].node.Node, Style: style, Children: items[i].node.Children}
			items[i].box, _ = layoutBlock(used, x, y+items[i].margin[0], width, faces, cb)
		}
	}

	occupied := max(0, len(items)-1) * gap
	for _, item := range items {
		if column {
			occupied += item.box.Rect.Dy() + item.margin[0] + item.margin[2]
		} else {
			occupied += item.box.Rect.Dx() + item.margin[1] + item.margin[3]
		}
	}
	mainSize := width
	if column {
		mainSize = occupied
		if heightDefinite {
			mainSize = containerHeight
		}
	}
	offset, between := flexJustification(parent.Style["justify-content"], mainSize-occupied, len(items), gap)
	cursor := offset
	boxes := make([]*Box, 0, len(items))
	for _, item := range items {
		itemCross := item.box.Rect.Dy() + item.margin[0] + item.margin[2]
		if column {
			itemCross = item.box.Rect.Dx() + item.margin[1] + item.margin[3]
		}
		crossOffset := flexCrossOffset(align, cross-itemCross)
		wantX, wantY := x+cursor+item.margin[3], y+crossOffset+item.margin[0]
		if column {
			wantX, wantY = x+crossOffset+item.margin[3], y+cursor+item.margin[0]
		}
		translatePositionedBox(item.box, wantX-item.box.Rect.Min.X, wantY-item.box.Rect.Min.Y)
		boxes = append(boxes, item.box)
		if column {
			cursor += item.box.Rect.Dy() + item.margin[0] + item.margin[2] + between
		} else {
			cursor += item.box.Rect.Dx() + item.margin[1] + item.margin[3] + between
		}
	}
	for _, child := range outOfFlow {
		boxes = append(boxes, layoutPositioned(child, x, y, width, cb, faces))
	}
	if column {
		return boxes, y + mainSize
	}
	return boxes, y + cross
}

func flexFactors(style ComputedStyle, basisSize int) (grow, shrink, basis float64, hasBasis bool) {
	grow, shrink = 0, 1
	if shorthand := strings.Fields(strings.TrimSpace(style["flex"])); len(shorthand) > 0 {
		if strings.EqualFold(shorthand[0], "none") {
			return 0, 0, 0, false
		}
		if value, err := strconv.ParseFloat(shorthand[0], 64); err == nil {
			grow = math.Max(0, value)
			basis, hasBasis = 0, true
			if len(shorthand) > 1 {
				if value, err = strconv.ParseFloat(shorthand[1], 64); err == nil {
					shrink = math.Max(0, value)
				} else {
					basis, hasBasis = flexBasis(shorthand[1], basisSize)
				}
			}
			if len(shorthand) > 2 {
				basis, hasBasis = flexBasis(shorthand[2], basisSize)
			}
		}
	}
	if value, err := strconv.ParseFloat(strings.TrimSpace(style["flex-grow"]), 64); err == nil {
		grow = math.Max(0, value)
	}
	if value, err := strconv.ParseFloat(strings.TrimSpace(style["flex-shrink"]), 64); err == nil {
		shrink = math.Max(0, value)
	}
	if value := strings.TrimSpace(style["flex-basis"]); value != "" && !strings.EqualFold(value, "auto") {
		basis, hasBasis = flexBasis(value, basisSize)
	}
	return
}

func flexBasis(value string, basisSize int) (float64, bool) {
	if strings.EqualFold(value, "0") || strings.EqualFold(value, "0%") {
		return 0, true
	}
	v := classifyValue(value)
	if v.Kind == "length" || v.Kind == "number" || v.Kind == "percentage" {
		return math.Max(0, px(value, float64(basisSize), 0)), true
	}
	return 0, false
}

func flexGap(style ComputedStyle, column bool, basis int) int {
	property := "column-gap"
	if column {
		property = "row-gap"
	}
	value := strings.TrimSpace(style[property])
	if value == "" || strings.EqualFold(value, "normal") {
		fields := strings.Fields(style["gap"])
		if len(fields) > 0 {
			value = fields[0]
			if !column && len(fields) > 1 {
				value = fields[1]
			}
		}
	}
	return max(0, int(math.Round(px(value, float64(basis), 0))))
}

func flexHasCrossSize(n *StyledNode, column bool) bool {
	property := "height"
	if column {
		property = "width"
	}
	value := strings.TrimSpace(n.Style[property])
	return value != "" && !strings.EqualFold(value, "auto")
}

func flexJustification(value string, free, count, gap int) (offset, between int) {
	free = max(0, free)
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "end", "flex-end":
		return free, gap
	case "center":
		return free / 2, gap
	case "space-between":
		if count > 1 {
			return 0, gap + free/(count-1)
		}
	case "space-around":
		if count > 0 {
			unit := free / count
			return unit / 2, gap + unit
		}
	case "space-evenly":
		if count > 0 {
			unit := free / (count + 1)
			return unit, gap + unit
		}
	}
	return 0, gap
}

func flexCrossOffset(align string, free int) int {
	free = max(0, free)
	switch align {
	case "end", "flex-end":
		return free
	case "center":
		return free / 2
	}
	return 0
}

func formatFlexPixels(value float64) string {
	return strconv.FormatFloat(math.Max(0, value), 'f', 3, 64) + "px"
}
