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
	anonymous     bool
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
	var textRun []*StyledNode
	flushText := func() {
		if len(textRun) == 0 {
			return
		}
		visible := false
		for _, text := range textRun {
			if !collapsibleWhitespaceOnly(text.Node.Data) {
				visible = true
				break
			}
		}
		if visible {
			// An anonymous flex item inherits text properties, but not the
			// container's flex, dimensions, background or box edges. Keep the
			// original text nodes for inline layout and painting.
			style := ComputedStyle{"display": "block"}
			for property := range inheritedCSSProperties {
				if value, ok := parent.Style[property]; ok {
					style[property] = value
				}
			}
			nodes = append(nodes, &StyledNode{
				Node:  &Node{Type: ElementNode, Name: "div"},
				Style: style, Children: textRun,
			})
		}
		textRun = nil
	}
	for _, child := range parent.Children {
		if childFlowKind(child) == flowSkip {
			continue
		}
		if child.Node.Type == TextNode {
			textRun = append(textRun, child)
			continue
		}
		flushText()
		if positioned(child) {
			outOfFlow = append(outOfFlow, child)
			continue
		}
		if child.Node.Type == ElementNode {
			nodes = append(nodes, child)
		}
	}
	flushText()
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
			if hasBasis {
				main = basis
			} else if h, ok := specifiedHeight(child, containerHeight, heightDefinite); ok {
				main = float64(h)
			} else if strings.EqualFold(child.Node.Name, "img") {
				_, h := imageDimensions(child, faces.images[child.Node], width)
				main = float64(h)
			} else {
				// Auto-sized text (including anonymous items) has a natural
				// line height on the column's main axis.
				natural, _ := layoutBlock(child, 0, 0, width, faces, cb)
				main = float64(natural.Content.Dy())
			}
		} else {
			if hasBasis {
				main = basis
			} else if value := strings.TrimSpace(child.Style["width"]); value != "" && !strings.EqualFold(value, "auto") {
				main = px(value, float64(width), 0)
			} else if strings.EqualFold(child.Node.Name, "img") {
				w, _ := imageDimensions(child, faces.images[child.Node], width)
				main = float64(w)
			} else {
				_, preferred := contentIntrinsicWidths(child, faces)
				main = float64(preferred)
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
			explicitCross: flexHasCrossSize(child, column), anonymous: child.Node.Parent == nil})
	}

	// An auto-height column grows to its contents; on a definite axis freeze
	// items at their min/max constraints and redistribute the remaining space.
	if !column || heightDefinite {
		resolveFlexLengths(items, float64(availableMain)-total, column, availableMain)
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
		items[i].box = layoutFlexItem(used, x, itemY, width, faces, cb)
		items[i].box.Anonymous = items[i].anonymous
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
			items[i].box = layoutFlexItem(used, x, y+items[i].margin[0], width, faces, cb)
			items[i].box.Anonymous = items[i].anonymous
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
		mainPos := cursor
		if reverse {
			outer := item.box.Rect.Dx() + item.margin[1] + item.margin[3]
			if column {
				outer = item.box.Rect.Dy() + item.margin[0] + item.margin[2]
			}
			mainPos = mainSize - cursor - outer
		}
		wantX, wantY := x+mainPos+item.margin[3], y+crossOffset+item.margin[0]
		if column {
			wantX, wantY = x+crossOffset+item.margin[3], y+mainPos+item.margin[0]
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

func layoutFlexItem(n *StyledNode, x, y, width int, faces *faceSet, cb containingBlock) *Box {
	if strings.EqualFold(n.Node.Name, "img") {
		box, _ := layoutReplacedBlock(n, x, y, width, faces)
		return box
	}
	box, _ := layoutBlock(n, x, y, width, faces, cb)
	return box
}

// Resolve flexible lengths with min/max freezing. Free space is measured
// against the hypothetical base sizes (including outer edges); each frozen
// item is removed from the factor sum before the next distribution pass.
// Percentage min/max sizes resolve against the definite main size (the
// container height for columns, its width for rows).
func resolveFlexLengths(items []flexItem, free float64, column bool, mainSize int) {
	property := "width"
	if column {
		property = "height"
	}
	base := make([]float64, len(items))
	frozen := make([]bool, len(items))
	minimum := make([]float64, len(items))
	maximum := make([]float64, len(items))
	for i := range items {
		base[i] = items[i].main
		maximum[i] = math.Inf(1)
		if v := strings.TrimSpace(items[i].node.Style["min-"+property]); v != "" && v != "auto" {
			minimum[i] = math.Max(0, px(v, float64(mainSize), 0))
		}
		if v := strings.TrimSpace(items[i].node.Style["max-"+property]); v != "" && v != "none" {
			maximum[i] = math.Max(0, px(v, float64(mainSize), 0))
		}
		if maximum[i] < minimum[i] {
			maximum[i] = minimum[i]
		}
	}
	growing := free > 0
	for pass := 0; pass <= len(items); pass++ {
		remaining := free
		sum := 0.0
		for i := range items {
			if frozen[i] {
				remaining -= items[i].main - base[i]
			} else if growing {
				sum += items[i].grow
			} else {
				sum += items[i].shrink * base[i]
			}
		}
		violated := false
		for i := range items {
			if frozen[i] {
				continue
			}
			factor := items[i].grow
			if !growing {
				factor = items[i].shrink * base[i]
			}
			target := base[i]
			if sum > 0 {
				target += remaining * factor / sum
			}
			clamped := math.Max(minimum[i], math.Min(maximum[i], math.Max(0, target)))
			items[i].main = clamped
			if clamped != target {
				frozen[i] = true
				violated = true
			}
		}
		if !violated {
			break
		}
	}
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
