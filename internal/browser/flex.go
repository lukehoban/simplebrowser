package browser

import (
	"math"
	"strconv"
	"strings"
)

// This is the deliberately small flex formatting context used by the pinned
// GitHub and Moon fixtures. It supports rows and columns, flexible main sizes,
// gaps, the common main/cross-axis alignment values, and bounded multi-line
// wrapping (flex-wrap:wrap and wrap-reverse with per-line flexing and
// align-content). Ordering, align-self and baseline synthesis remain outside
// this renderer's flex scope; see docs/flexbox.md.
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
	extra         int // margins, borders and padding on the main axis
	explicitCross bool
}

// flexLine is one line of items plus its resolved cross size and position.
type flexLine struct {
	items      []flexItem
	cross, pos int
}

// flexChildren shares anonymous text item creation between layout and
// intrinsic sizing, so floated text-only flex containers measure their runs.
func flexChildren(parent *StyledNode) (nodes, outOfFlow []*StyledNode) {
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
	return nodes, outOfFlow
}

func layoutFlex(parent *StyledNode, x, y, width, containerHeight int, heightDefinite bool, faces *faceSet, cb containingBlock) ([]*Box, int) {
	direction := strings.ToLower(strings.TrimSpace(parent.Style["flex-direction"]))
	column := strings.HasPrefix(direction, "column")
	reverse := strings.HasSuffix(direction, "-reverse")
	wrapMode := strings.ToLower(strings.TrimSpace(parent.Style["flex-wrap"]))
	wrap := wrapMode == "wrap" || wrapMode == "wrap-reverse"
	wrapReverse := wrapMode == "wrap-reverse"
	nodes, outOfFlow := flexChildren(parent)
	gap := flexGap(parent.Style, column, width)
	crossGap := flexGap(parent.Style, !column, width)
	// An auto-height column has no definite main size: it grows to its
	// contents, never flexes and never wraps.
	mainDefinite := !column || heightDefinite
	availableMain := width
	if column && heightDefinite {
		availableMain = containerHeight
	}

	items := make([]flexItem, 0, len(nodes))
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
		extra := margin[1] + margin[3] + inner[1] + inner[3]
		if column {
			extra = margin[0] + margin[2] + inner[0] + inner[2]
		}
		items = append(items, flexItem{node: child, margin: margin, main: main, grow: grow, shrink: shrink,
			extra: extra, explicitCross: flexHasCrossSize(child, column), anonymous: child.Node.Parent == nil})
	}

	// Collect items into lines. A single-line container keeps every item on
	// one line; a wrapping container with a definite main size breaks before
	// an item whose hypothetical (min/max-clamped) outer size would overflow.
	var lines []flexLine
	if wrap && mainDefinite {
		used := 0.0
		for _, item := range items {
			lo, hi := flexMinMax(item, column, availableMain)
			outer := math.Max(lo, math.Min(hi, item.main)) + float64(item.extra)
			if n := len(lines); n > 0 && len(lines[n-1].items) > 0 && used+float64(gap)+outer <= float64(availableMain)+0.001 {
				lines[n-1].items = append(lines[n-1].items, item)
				used += float64(gap) + outer
				continue
			}
			lines = append(lines, flexLine{items: []flexItem{item}})
			used = outer
		}
	}
	if len(lines) == 0 {
		lines = []flexLine{{items: items}}
	}

	// Resolve flexible lengths per line, then lay out at the origin so each
	// line's cross size is known before alignment.
	for li := range lines {
		line := &lines[li]
		if mainDefinite {
			total := float64(max(0, len(line.items)-1) * gap)
			for _, item := range line.items {
				total += item.main + float64(item.extra)
			}
			resolveFlexLengths(line.items, float64(availableMain)-total, column, availableMain)
		}
		for i := range line.items {
			item := &line.items[i]
			style := cloneStyle(item.node.Style)
			if column {
				style["height"] = formatFlexPixels(item.main)
			} else {
				style["width"] = formatFlexPixels(item.main)
			}
			used := &StyledNode{Node: item.node.Node, Style: style, Children: item.node.Children}
			itemWidth := width
			if column && !item.explicitCross {
				minWidth, maxWidth := intrinsicWidths(item.node, faces)
				itemWidth = min(max(minWidth, width), maxWidth)
			}
			item.box = layoutFlexItem(used, x, y+item.margin[0], itemWidth, faces, cb)
			item.box.Anonymous = item.anonymous
			line.cross = max(line.cross, flexOuterCross(*item, column))
		}
	}

	// The container's cross size is definite for columns (the width) and for
	// rows with a definite height; otherwise it is the sum of the lines.
	crossDefinite := column || heightDefinite
	containerCross := width
	if !column {
		containerCross = containerHeight
	}
	if !wrap {
		if crossDefinite {
			lines[0].cross = containerCross
		}
	} else {
		sum := max(0, len(lines)-1) * crossGap
		for _, line := range lines {
			sum += line.cross
		}
		free := 0
		if crossDefinite {
			free = containerCross - sum
		}
		content := strings.ToLower(strings.TrimSpace(parent.Style["align-content"]))
		offset, between := 0, crossGap
		if content == "" || content == "normal" || content == "stretch" {
			if free > 0 {
				for i := range lines {
					lines[i].cross += free*(i+1)/len(lines) - free*i/len(lines)
				}
			}
		} else {
			offset, between = flexJustification(content, free, len(lines), crossGap)
		}
		cursor := offset
		for i := range lines {
			lines[i].pos = cursor
			cursor += lines[i].cross + between
		}
		if !crossDefinite {
			containerCross = sum
		}
	}
	if !crossDefinite && !wrap {
		containerCross = lines[0].cross
	}

	align := strings.ToLower(strings.TrimSpace(parent.Style["align-items"]))
	if align == "" || align == "normal" {
		align = "stretch"
	}
	mainSize := width
	boxes := make([]*Box, 0, len(items))
	for _, line := range lines {
		linePos := line.pos
		if wrapReverse {
			linePos = containerCross - line.pos - line.cross
		}
		// Stretch auto cross sizes to the line, then align along the main axis.
		for i := range line.items {
			item := &line.items[i]
			if align != "stretch" || item.explicitCross {
				continue
			}
			style := cloneStyle(item.node.Style)
			inner := inlineInnerEdges(item.node, width)
			if column {
				style["width"] = formatFlexPixels(float64(max(0, line.cross-item.margin[1]-item.margin[3]-inner[1]-inner[3])))
				style["height"] = formatFlexPixels(item.main)
			} else {
				style["width"] = formatFlexPixels(item.main)
				style["height"] = formatFlexPixels(float64(max(0, line.cross-item.margin[0]-item.margin[2]-inner[0]-inner[2])))
			}
			used := &StyledNode{Node: item.node.Node, Style: style, Children: item.node.Children}
			item.box = layoutFlexItem(used, x, y+item.margin[0], width, faces, cb)
			item.box.Anonymous = item.anonymous
		}

		occupied := max(0, len(line.items)-1) * gap
		for _, item := range line.items {
			occupied += flexOuterMain(item, column)
		}
		if column {
			mainSize = occupied
			if heightDefinite {
				mainSize = containerHeight
			}
		}
		offset, between := flexJustification(parent.Style["justify-content"], mainSize-occupied, len(line.items), gap)
		cursor := offset
		for _, item := range line.items {
			outerCross := flexOuterCross(item, column)
			crossOffset := flexCrossOffset(align, line.cross-outerCross)
			if wrapReverse {
				// wrap-reverse swaps cross-start and cross-end.
				crossOffset = max(0, line.cross-outerCross) - crossOffset
			}
			crossOffset += linePos
			outerMain := flexOuterMain(item, column)
			mainPos := cursor
			if reverse {
				mainPos = mainSize - cursor - outerMain
			}
			wantX, wantY := x+mainPos+item.margin[3], y+crossOffset+item.margin[0]
			if column {
				wantX, wantY = x+crossOffset+item.margin[3], y+mainPos+item.margin[0]
			}
			translatePositionedBox(item.box, wantX-item.box.Rect.Min.X, wantY-item.box.Rect.Min.Y)
			boxes = append(boxes, item.box)
			cursor += outerMain + between
		}
	}
	for _, child := range outOfFlow {
		boxes = append(boxes, layoutPositioned(child, x, y, width, cb, faces))
	}
	if column {
		return boxes, y + mainSize
	}
	return boxes, y + containerCross
}

func flexOuterMain(item flexItem, column bool) int {
	if column {
		return item.box.Rect.Dy() + item.margin[0] + item.margin[2]
	}
	return item.box.Rect.Dx() + item.margin[1] + item.margin[3]
}

func flexOuterCross(item flexItem, column bool) int {
	if column {
		return item.box.Rect.Dx() + item.margin[1] + item.margin[3]
	}
	return item.box.Rect.Dy() + item.margin[0] + item.margin[2]
}

// flexMinMax returns an item's main-axis min/max constraints. Percentages
// resolve against the definite main size (the container height for columns,
// its width for rows).
func flexMinMax(item flexItem, column bool, mainSize int) (minimum, maximum float64) {
	property := "width"
	if column {
		property = "height"
	}
	maximum = math.Inf(1)
	if v := strings.TrimSpace(item.node.Style["min-"+property]); v != "" && v != "auto" {
		minimum = math.Max(0, px(v, float64(mainSize), 0))
	}
	if v := strings.TrimSpace(item.node.Style["max-"+property]); v != "" && v != "none" {
		maximum = math.Max(0, px(v, float64(mainSize), 0))
	}
	if maximum < minimum {
		maximum = minimum
	}
	return
}

// flexIntrinsicWidths measures a flex container's content. A row places every
// item on one hypothetical line for its max-content width; its min-content
// width is the widest item when wrapping, else the sum of the items' minimums.
// A column's widths are those of its widest item.
func flexIntrinsicWidths(n *StyledNode, faces *faceSet) (int, int) {
	direction := strings.ToLower(strings.TrimSpace(n.Style["flex-direction"]))
	column := strings.HasPrefix(direction, "column")
	wrapMode := strings.ToLower(strings.TrimSpace(n.Style["flex-wrap"]))
	wrap := wrapMode == "wrap" || wrapMode == "wrap-reverse"
	gap := flexGap(n.Style, false, 0)
	minWidth, maxWidth, count := 0, 0, 0
	children, _ := flexChildren(n)
	for _, child := range children {
		childMin, childMax := intrinsicWidths(child, faces)
		switch {
		case column:
			minWidth, maxWidth = max(minWidth, childMin), max(maxWidth, childMax)
		case wrap:
			minWidth = max(minWidth, childMin)
			maxWidth += childMax
		default:
			minWidth += childMin
			maxWidth += childMax
		}
		count++
	}
	if !column && count > 1 {
		maxWidth += (count - 1) * gap
		if !wrap {
			minWidth += (count - 1) * gap
		}
	}
	return minWidth, max(minWidth, maxWidth)
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
	base := make([]float64, len(items))
	frozen := make([]bool, len(items))
	minimum := make([]float64, len(items))
	maximum := make([]float64, len(items))
	for i := range items {
		base[i] = items[i].main
		minimum[i], maximum[i] = flexMinMax(items[i], column, mainSize)
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
