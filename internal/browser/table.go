package browser

import (
	"image"
	"image/color"
	"math"
	"strings"
	"unicode"

	"golang.org/x/image/math/fixed"
)

// Table layout implements the separated-borders model that Hacker News relies
// on: a grid of rows and cells sized from intrinsic content widths, explicit
// CSS/HTML widths (including percentages), `colspan`, `rowspan`, and the
// `cellpadding`/`cellspacing` presentational attributes.
//
// Deliberate simplifications, all of which degrade gracefully:
//
//   - `cellspacing` is mapped by the cascade onto the table's `border-spacing`
//     and is applied around and between cells. Two-value `border-spacing`
//     uses distinct horizontal (column) and vertical (row) gaps.
//   - `cellpadding` is mapped onto the table's `padding-*`. Because the HTML
//     attribute describes cell padding rather than table padding, table
//     padding is used as the default padding of cells that do not declare
//     their own. The table box itself has no padding.
//   - Malformed markup is repaired with anonymous boxes: cells outside a row
//     get an anonymous row, and non-cell content inside a row (or directly
//     inside a table) gets an anonymous cell. Runs of misparented
//     table-internal boxes in block flow are wrapped in an anonymous table.
//   - Captions occupy the full width of the wrapper outside the grid border;
//     `caption-side: top` precedes the grid and `bottom` follows it.
//   - Cell content is top aligned; `valign` and vertical centering are not
//     implemented yet.
//   - `border-collapse: collapse` is partial: border-spacing is dropped,
//     outer cell borders contribute their trailing half-width when aligning
//     the anonymous wrapper with a caption, and row-group top/bottom borders
//     collapse (wider wins) into gaps between rows. A single unspanned row
//     resolves cell shared vertical edges; unspanned one-column rows also
//     resolve cell/row shared horizontal edges by border width.
//     Rows without cells are bridged by the outer halves of adjacent cell
//     borders. Multi-column segmentation and table-edge conflicts remain
//     unsupported.
//   - The first header group renders first and the first footer group last.
//   - `rowspan` is honored for geometry: a spanning cell covers its rows and
//     any extra height it needs is added to the last row it spans.

const (
	maxTableSpan = 1000
	maxTableCols = 4096
)

type tableCellBox struct {
	node    *StyledNode
	row     int
	col     int
	colspan int
	rowspan int

	minWidth   int
	maxWidth   int
	maxAdvance fixed.Int26_6 // exact no-wrap text advance when it is representable
	fixed      int           // explicit width in px, -1 when absent
	percent    float64       // explicit percentage width, -1 when absent
	caption    bool          // anonymous cell holding a table caption

	box    *Box
	height int // outer height required by the cell content
}

type tableRowBox struct {
	node   *StyledNode // nil for anonymous rows
	cells  []*tableCellBox
	group  *tableGroupBox
	height int
	y      int
	box    *Box
}

type tableGroupBox struct {
	node *StyledNode
	box  *Box
}

type tableGrid struct {
	rows                        []*tableRowBox
	topCaptions, bottomCaptions []*StyledNode
	groups                      []*tableGroupBox
	cols                        []*StyledNode // expanded <col>/<colgroup> definitions, in grid order
	colGroups                   []*StyledNode // owning <colgroup>, nil for direct columns
	columns                     int
	columnBorders               []*collapsedTableBorder
	rowBorders                  []*collapsedTableBorder
	rowBorderSegments           [][]*collapsedTableBorder
	outerBorders                [4]*collapsedTableBorder
	outerBorderSegments         [4][]*collapsedTableBorder
	hspacing                    int    // horizontal border-spacing between/around columns
	vspacing                    int    // vertical border-spacing between/around rows
	collapse                    bool   // border-collapse: collapse
	caption                     bool   // a direct caption participates in the anonymous wrapper
	padding                     [4]int // default cell padding contributed by cellpadding
}

type collapsedTableBorder struct {
	node  *StyledNode
	side  string
	style string
	width int
}

func tableBorderCandidate(node *StyledNode, side string) collapsedTableBorder {
	if node == nil {
		return collapsedTableBorder{side: side, style: "none"}
	}
	return collapsedTableBorder{
		node: node, side: side, style: borderStyle(node.Style, side),
		width: borderWidth(node.Style, side),
	}
}

func borderStyle(style ComputedStyle, side string) string {
	value := style["border-style-"+side]
	if value == "" {
		value = style["border-"+side+"-style"]
	}
	if value == "" {
		for _, token := range parseValues(style["border-"+side]) {
			if isBorderStyleKeyword(token.Text) {
				value = token.Text
				break
			}
		}
	}
	value = strings.ToLower(strings.TrimSpace(value))
	if !isBorderStyleKeyword(value) {
		return "none"
	}
	return value
}

func borderStyleRank(style string) int {
	switch style {
	case "double":
		return 8
	case "solid":
		return 7
	case "dashed":
		return 6
	case "dotted":
		return 5
	case "ridge":
		return 4
	case "outset":
		return 3
	case "groove":
		return 2
	case "inset":
		return 1
	}
	return 0
}

// winsCollapsedBorder implements CSS 2.1 §17.6.2.1. hidden suppresses every
// competing border, none is the weakest candidate, then width, style and
// table-part origin decide. Stable candidate order supplies the final
// top/left tie-break when every specified property is equal.
func winsCollapsedBorder(candidate, winner *collapsedTableBorder) bool {
	if winner == nil {
		return candidate.style != "none"
	}
	if candidate.style == "hidden" || winner.style == "hidden" {
		return candidate.style == "hidden" && winner.style != "hidden"
	}
	if candidate.style == "none" || candidate.width == 0 {
		return false
	}
	if winner.style == "none" || winner.width == 0 {
		return true
	}
	if candidate.width != winner.width {
		return candidate.width > winner.width
	}
	if borderStyleRank(candidate.style) != borderStyleRank(winner.style) {
		return borderStyleRank(candidate.style) > borderStyleRank(winner.style)
	}
	return borderOriginRank(candidate.node) > borderOriginRank(winner.node)
}

func resolveCollapsedBorder(candidates ...collapsedTableBorder) *collapsedTableBorder {
	var winner *collapsedTableBorder
	for i := range candidates {
		if winsCollapsedBorder(&candidates[i], winner) {
			winner = &candidates[i]
		}
	}
	if winner == nil {
		return nil
	}
	copy := *winner
	if copy.style == "hidden" {
		copy.width = 0
	}
	return &copy
}

func displayIs(n *StyledNode, values ...string) bool {
	if n == nil || n.Node == nil || n.Node.Type != ElementNode {
		return false
	}
	display := strings.TrimSpace(strings.ToLower(n.Style["display"]))
	for _, v := range values {
		if display == v {
			return true
		}
	}
	return false
}

func isTableNode(n *StyledNode) bool {
	if n == nil || n.Node == nil || n.Node.Type != ElementNode {
		return false
	}
	if strings.EqualFold(n.Style["display"], "none") {
		return false
	}
	return displayIs(n, "table", "inline-table") || strings.EqualFold(n.Node.Name, "table")
}

// isInlineTableNode reports whether a table box is inline-level and therefore
// participates in its parent's inline formatting context as an atomic inline
// box, rather than stacking as a block.
func isInlineTableNode(n *StyledNode) bool {
	return displayIs(n, "inline-table")
}

func isRowGroupNode(n *StyledNode) bool {
	if displayIs(n, "table-row-group", "table-header-group", "table-footer-group") {
		return true
	}
	switch strings.ToLower(nodeName(n)) {
	case "tbody", "thead", "tfoot":
		return !displayIs(n, "none")
	}
	return false
}

func isRowNode(n *StyledNode) bool {
	if displayIs(n, "table-row") {
		return true
	}
	return strings.EqualFold(nodeName(n), "tr") && !displayIs(n, "none")
}

func isCellNode(n *StyledNode) bool {
	if displayIs(n, "table-cell") {
		return true
	}
	switch strings.ToLower(nodeName(n)) {
	case "td", "th":
		return !displayIs(n, "none")
	}
	return false
}

func isColumnNode(n *StyledNode) bool {
	return displayIs(n, "table-column") || strings.EqualFold(nodeName(n), "col")
}

func isColumnGroupNode(n *StyledNode) bool {
	return displayIs(n, "table-column-group") || strings.EqualFold(nodeName(n), "colgroup")
}

// containsTable reports whether a subtree holds a visible table element.
func containsTable(n *StyledNode) bool {
	if n == nil || n.Node == nil || hiddenNode(n) {
		return false
	}
	if isInlineTableNode(n) {
		// An inline table is inline-level content: it does not force its
		// ancestors out of the inline formatting context.
		return false
	}
	if isTableNode(n) {
		return true
	}
	for _, child := range n.Children {
		if containsTable(child) {
			return true
		}
	}
	return false
}

func nodeName(n *StyledNode) string {
	if n == nil || n.Node == nil {
		return ""
	}
	return n.Node.Name
}

func hiddenNode(n *StyledNode) bool {
	return n != nil && n.Node != nil && n.Node.Type == ElementNode &&
		strings.EqualFold(n.Style["display"], "none")
}

// blankText reports whether a text node carries no visible characters.
func blankText(n *StyledNode) bool {
	if n == nil || n.Node == nil || n.Node.Type != TextNode {
		return false
	}
	return strings.TrimFunc(n.Node.Data, unicode.IsSpace) == ""
}

func spanAttribute(n *StyledNode, name string) int {
	if n == nil || n.Node == nil {
		return 1
	}
	attr, ok := n.Node.Attribute(name)
	if !ok {
		return 1
	}
	value := strings.TrimSpace(attr.Value)
	total := 0
	digits := 0
	for _, r := range value {
		if r < '0' || r > '9' {
			break
		}
		digits++
		total = total*10 + int(r-'0')
		if total > maxTableSpan {
			total = maxTableSpan
			break
		}
	}
	if digits == 0 || total < 1 {
		return 1
	}
	return total
}

// buildTableGrid collects rows and cells, repairing malformed markup with
// anonymous rows and cells so that every table produces usable geometry.
func buildTableGrid(table *StyledNode) *tableGrid {
	// The first component is horizontal spacing; an optional second component
	// is vertical spacing, otherwise the first applies to both axes
	// (CSS 2.1 §17.6.1).
	spacing := strings.Fields(table.Style["border-spacing"])
	grid := &tableGrid{}
	if len(spacing) > 0 {
		grid.hspacing = int(math.Max(0, math.Round(px(spacing[0], 0, 0))))
		grid.vspacing = grid.hspacing
		if len(spacing) > 1 {
			grid.vspacing = int(math.Max(0, math.Round(px(spacing[1], 0, 0))))
		}
	}
	if strings.EqualFold(strings.TrimSpace(table.Style["border-collapse"]), "collapse") {
		// In the collapsing border model border-spacing does not apply.
		grid.collapse, grid.hspacing, grid.vspacing = true, 0, 0
	}
	grid.padding = boxEdges(table, "padding", 0)

	// Column boxes describe the grid but never generate rows or cells. Expand
	// spans here so fixed layout can address declarations by column index.
	appendColumns := func(column, group *StyledNode) {
		for range spanAttribute(column, "span") {
			if len(grid.cols) >= maxTableCols {
				break
			}
			grid.cols = append(grid.cols, column)
			grid.colGroups = append(grid.colGroups, group)
		}
	}
	for _, child := range table.Children {
		switch {
		case hiddenNode(child):
		case isColumnNode(child):
			appendColumns(child, nil)
		case isColumnGroupNode(child):
			found := false
			for _, column := range child.Children {
				if hiddenNode(column) || !isColumnNode(column) {
					continue
				}
				found = true
				appendColumns(column, child)
			}
			if !found {
				appendColumns(child, child)
			}
		}
	}

	var pending []*StyledNode // cells or content awaiting an anonymous row
	flushPending := func() {
		if len(pending) == 0 {
			return
		}
		grid.addRow(nil, pending, nil)
		pending = nil
	}

	var walkGroup func(group *StyledNode, box *tableGroupBox)
	walkGroup = func(group *StyledNode, box *tableGroupBox) {
		var loose []*StyledNode
		flushLoose := func() {
			if len(loose) == 0 {
				return
			}
			grid.addRow(nil, loose, box)
			loose = nil
		}
		for _, child := range group.Children {
			switch {
			case hiddenNode(child) || blankText(child):
				continue
			case isRowNode(child):
				flushLoose()
				grid.addRow(child, child.Children, box)
			case isRowGroupNode(child):
				flushLoose()
				nested := &tableGroupBox{node: child}
				grid.groups = append(grid.groups, nested)
				walkGroup(child, nested)
			default:
				loose = append(loose, child)
			}
		}
		flushLoose()
	}

	for _, child := range orderedTableChildren(table.Children) {
		switch {
		case hiddenNode(child) || blankText(child) || isColumnNode(child) || isColumnGroupNode(child):
			continue
		case isTableCaptionNode(child):
			flushPending()
			grid.caption = true
			if captionSideBottom(child) {
				grid.bottomCaptions = append(grid.bottomCaptions, child)
			} else {
				grid.topCaptions = append(grid.topCaptions, child)
			}
		case isRowNode(child):
			flushPending()
			grid.addRow(child, child.Children, nil)
		case isRowGroupNode(child):
			flushPending()
			group := &tableGroupBox{node: child}
			grid.groups = append(grid.groups, group)
			walkGroup(child, group)
		default:
			pending = append(pending, child)
		}
	}
	flushPending()
	grid.assignColumns()
	grid.columns = max(grid.columns, len(grid.cols))
	return grid
}

func isTableCaptionNode(n *StyledNode) bool {
	return displayIs(n, "table-caption") || strings.EqualFold(nodeName(n), "caption")
}

// tableCaptionMinWidth returns the widest minimum intrinsic width of a
// visible direct caption. CSS 2.1 sizes the anonymous table box to at least
// this width, independently of the table grid's own width.
func tableCaptionMinWidth(n *StyledNode, faces *faceSet) int {
	width := 0
	if n == nil {
		return width
	}
	for _, child := range n.Children {
		if hiddenNode(child) || !isTableCaptionNode(child) {
			continue
		}
		minWidth, _ := intrinsicWidths(child, faces)
		width = max(width, minWidth)
	}
	return width
}

// anonymousCellKey marks the computed style of an anonymous grid cell. The
// space makes it impossible for a CSS declaration to set.
const anonymousCellKey = " anonymous-cell"

// isTableInternalNode reports whether a box is a proper table child or cell
// by its display value (CSS 2.1 §17.2.1). Floated and absolutely positioned
// boxes are blockified and so never table-internal.
func isTableInternalNode(n *StyledNode) bool {
	if !displayIs(n, "table-cell", "table-row", "table-row-group", "table-header-group",
		"table-footer-group", "table-caption", "table-column", "table-column-group") {
		return false
	}
	if f := strings.ToLower(strings.TrimSpace(n.Style["float"])); f != "" && f != "none" {
		return false
	}
	return !positioned(n)
}

// wrapAnonymousTables wraps each run of consecutive misparented
// table-internal children of a block container in an anonymous table box
// (CSS 2.1 §17.2.1 rules 2–3). Whitespace between table-internal siblings is
// dropped. A run holding only captions is left as ordinary blocks: the
// anonymous table would contribute nothing but the caption, and a caption
// is itself laid out as the child of an anonymous grid cell.
func wrapAnonymousTables(parent *StyledNode, children []*StyledNode) []*StyledNode {
	// An anonymous grid cell already holds its table's misparented children;
	// wrapping them again would recurse without end.
	if parent == nil || parent.Style[anonymousCellKey] != "" ||
		isTableNode(parent) || isRowNode(parent) || isRowGroupNode(parent) {
		return children
	}
	var result, run []*StyledNode
	var trailing []*StyledNode // whitespace held until the run continues
	flush := func() {
		wrap := false
		for _, n := range run {
			wrap = wrap || !isTableCaptionNode(n)
		}
		if wrap {
			result = append(result, anonymousTable(parent, run))
		} else {
			result = append(result, run...)
		}
		result = append(result, trailing...)
		run, trailing = nil, nil
	}
	for _, child := range children {
		switch {
		case isTableInternalNode(child):
			trailing = nil
			run = append(run, child)
		case len(run) > 0 && blankText(child):
			trailing = append(trailing, child)
		default:
			if len(run) > 0 {
				flush()
			}
			result = append(result, child)
		}
	}
	if len(run) > 0 {
		flush()
	}
	return result
}

// anonymousTable returns an anonymous display:table box for run, inheriting
// the parent's inherited properties.
func anonymousTable(parent *StyledNode, run []*StyledNode) *StyledNode {
	style := ComputedStyle{"display": "table"}
	for _, p := range []string{"border-spacing", "color", "font-family", "font-size",
		"font-style", "font-variant", "font-weight", "lang", "line-height", "text-align"} {
		if v, ok := parent.Style[p]; ok {
			style[p] = v
		}
	}
	node := &Node{Type: ElementNode, Parent: parent.nodeOrNil()}
	return &StyledNode{Node: node, Style: style, Children: run}
}

// captionSideBottom reports whether a caption is placed below the table grid.
func captionSideBottom(n *StyledNode) bool {
	return strings.EqualFold(strings.TrimSpace(n.Style["caption-side"]), "bottom")
}

// orderedTableChildren places captions in the table wrapper and orders row
// groups, regardless of source order: top captions precede the grid and
// bottom captions follow it (CSS 2.1 §17.4), while the first header group
// renders first and the first footer group last within the grid (§17.2).
func orderedTableChildren(children []*StyledNode) []*StyledNode {
	var top, grid, bottom []*StyledNode
	for _, child := range children {
		switch {
		case hiddenNode(child) || !isTableCaptionNode(child):
			grid = append(grid, child)
		case captionSideBottom(child):
			bottom = append(bottom, child)
		default:
			top = append(top, child)
		}
	}
	if len(top) == 0 && len(bottom) == 0 {
		return orderedTableGroups(children)
	}
	ordered := append(top, orderedTableGroups(grid)...)
	return append(ordered, bottom...)
}

// orderedTableGroups moves the first header group to the top and the first
// footer group to the bottom of the table grid.
func orderedTableGroups(children []*StyledNode) []*StyledNode {
	header, footer := -1, -1
	for i, child := range children {
		if hiddenNode(child) || !isRowGroupNode(child) {
			continue
		}
		if header < 0 && (displayIs(child, "table-header-group") ||
			(strings.EqualFold(nodeName(child), "thead") && !displayIs(child, "table-row-group", "table-footer-group"))) {
			header = i
		} else if footer < 0 && (displayIs(child, "table-footer-group") ||
			(strings.EqualFold(nodeName(child), "tfoot") && !displayIs(child, "table-row-group", "table-header-group"))) {
			footer = i
		}
	}
	if header < 0 && footer < 0 {
		return children
	}
	ordered := make([]*StyledNode, 0, len(children))
	if header >= 0 {
		ordered = append(ordered, children[header])
	}
	for i, child := range children {
		if i != header && i != footer {
			ordered = append(ordered, child)
		}
	}
	if footer >= 0 {
		ordered = append(ordered, children[footer])
	}
	return ordered
}

// groupBorder returns a row group's border width on one side, or zero for
// rows outside any group.
func groupBorder(g *tableGroupBox, side string) int {
	if g == nil || g.node == nil {
		return 0
	}
	return borderWidth(g.node.Style, side)
}

// collapsedGapBefore returns the space reserved above row i for collapsed
// row-group borders: where two groups meet, the wider border wins.
func (g *tableGrid) collapsedGapBefore(i int) int {
	if !g.collapse {
		return 0
	}
	if i > 0 && i < len(g.rowBorders) && g.rowBorders[i] != nil {
		return g.rowBorders[i].width
	}
	row := g.rows[i]
	if i > 0 && g.rows[i-1].group == row.group {
		return 0
	}
	gap := groupBorder(row.group, "top")
	if i > 0 {
		gap = max(gap, groupBorder(g.rows[i-1].group, "bottom"))
	}
	return gap
}

// resolveRowBorderSegments resolves every column of every internal horizontal
// grid edge independently. A single winner for a whole row is incorrect when
// a spanning cell or a neighbouring cell changes the conflict at a column
// boundary. rowBorders retains the widest segment for gap geometry; the
// segments are used by painting so each winner is painted exactly once.
func (g *tableGrid) resolveRowBorderSegments() {
	g.rowBorderSegments = make([][]*collapsedTableBorder, len(g.rows))
	g.rowBorders = make([]*collapsedTableBorder, len(g.rows))
	if !g.collapse || len(g.rows) < 2 || g.columns == 0 {
		return
	}
	complex := false
	for _, row := range g.rows {
		for _, cell := range row.cells {
			if cell.colspan != 1 || cell.rowspan != 1 {
				complex = true
				break
			}
		}
		if complex {
			break
		}
	}
	// Preserve the established cell-owned painting for the regular grid.
	// Segmentation is needed when spans make the adjacent cell grid
	// non-uniform; the regular grid has no paint-only edge fragments.
	if !complex {
		if g.columns == 1 {
			for boundary := 1; boundary < len(g.rows); boundary++ {
				above, below := g.rows[boundary-1], g.rows[boundary]
				if len(above.cells) != 1 || len(below.cells) != 1 ||
					above.cells[0].rowspan != 1 || below.cells[0].rowspan != 1 {
					continue
				}
				candidates := []collapsedTableBorder{
					tableBorderCandidate(above.cells[0].node, "bottom"),
					tableBorderCandidate(below.cells[0].node, "top"),
				}
				if above.node != nil {
					candidates = append(candidates, tableBorderCandidate(above.node, "bottom"))
				}
				if below.node != nil {
					candidates = append(candidates, tableBorderCandidate(below.node, "top"))
				}
				if above.group != below.group {
					if above.group != nil {
						candidates = append(candidates, tableBorderCandidate(above.group.node, "bottom"))
					}
					if below.group != nil {
						candidates = append(candidates, tableBorderCandidate(below.group.node, "top"))
					}
				}
				g.rowBorders[boundary] = resolveCollapsedBorder(candidates...)
			}
		}
		return
	}
	for boundary := 1; boundary < len(g.rows); boundary++ {
		segments := make([]*collapsedTableBorder, g.columns)
		above, below := g.rows[boundary-1], g.rows[boundary]
		// Empty rows are bridged by bridgeEmptyRows, which preserves the
		// continuous vertical cell borders through their full height.
		if len(above.cells) == 0 || len(below.cells) == 0 {
			g.rowBorderSegments[boundary] = segments
			continue
		}
		for col := 0; col < g.columns; col++ {
			candidates := make([]collapsedTableBorder, 0, 6)
			for _, cell := range above.cells {
				if cell.col <= col && col < cell.col+cell.colspan && cell.row+cell.rowspan == boundary {
					candidates = append(candidates, tableBorderCandidate(cell.node, "bottom"))
				}
			}
			for _, cell := range below.cells {
				if cell.col <= col && col < cell.col+cell.colspan && cell.row == boundary {
					candidates = append(candidates, tableBorderCandidate(cell.node, "top"))
				}
			}
			if above.node != nil {
				candidates = append(candidates, tableBorderCandidate(above.node, "bottom"))
			}
			if below.node != nil {
				candidates = append(candidates, tableBorderCandidate(below.node, "top"))
			}
			if above.group != below.group {
				if above.group != nil {
					candidates = append(candidates, tableBorderCandidate(above.group.node, "bottom"))
				}
				if below.group != nil {
					candidates = append(candidates, tableBorderCandidate(below.group.node, "top"))
				}
			}
			segments[col] = resolveCollapsedBorder(candidates...)
			if edge := segments[col]; edge != nil &&
				(g.rowBorders[boundary] == nil || winsCollapsedBorder(edge, g.rowBorders[boundary])) {
				g.rowBorders[boundary] = edge
			}
		}
		g.rowBorderSegments[boundary] = segments
	}
}

func (g *tableGrid) hasRowBorderAt(boundary int) bool {
	if boundary <= 0 || boundary >= len(g.rowBorderSegments) {
		return false
	}
	if g.rowBorders[boundary] != nil {
		return true
	}
	for _, edge := range g.rowBorderSegments[boundary] {
		if edge != nil {
			return true
		}
	}
	return false
}

// resolveSingleRowCellBorders handles shared vertical edges in one row when
// every grid column has exactly one unspanned cell. More complex grids need
// independently segmented candidates for each row and span.
func (g *tableGrid) resolveSingleRowCellBorders() {
	g.columnBorders = make([]*collapsedTableBorder, g.columns+1)
	if !g.collapse || len(g.rows) != 1 || g.columns < 2 {
		return
	}

	row := g.rows[0]
	if len(row.cells) != g.columns {
		return
	}
	cells := make([]*tableCellBox, g.columns)
	for _, cell := range row.cells {
		if cell.colspan != 1 || cell.rowspan != 1 || cell.col < 0 || cell.col >= g.columns ||
			cells[cell.col] != nil {
			return
		}
		cells[cell.col] = cell
	}
	for col := 1; col < g.columns; col++ {
		left, right := cells[col-1], cells[col]
		if left == nil || right == nil {
			return
		}
		candidates := []collapsedTableBorder{
			tableBorderCandidate(left.node, "right"),
			tableBorderCandidate(right.node, "left"),
		}
		if col-1 < len(g.cols) {
			candidates = append(candidates, tableBorderCandidate(g.cols[col-1], "right"))
			if col-1 < len(g.colGroups) && g.colGroups[col-1] != nil {
				candidates = append(candidates, tableBorderCandidate(g.colGroups[col-1], "right"))
			}
		}
		if col < len(g.cols) {
			candidates = append(candidates, tableBorderCandidate(g.cols[col], "left"))
			if col < len(g.colGroups) && g.colGroups[col] != nil {
				candidates = append(candidates, tableBorderCandidate(g.colGroups[col], "left"))
			}
		}
		g.columnBorders[col] = resolveCollapsedBorder(candidates...)
	}
}

func borderOriginRank(node *StyledNode) int {
	if node == nil || node.Node == nil {
		return 0
	}
	display := strings.ToLower(strings.TrimSpace(node.Style["display"]))
	switch display {
	case "table-cell":
		return 6
	case "table-row":
		return 5
	case "table-row-group", "table-header-group", "table-footer-group":
		return 4
	case "table-column":
		return 3
	case "table-column-group":
		return 2
	case "table", "inline-table":
		return 1
	}
	if display != "" {
		return 0
	}
	// Computed styles normally include display. Keep the HTML role fallback
	// for synthetic StyledNodes that have not passed through the cascade.
	switch strings.ToLower(node.Node.Name) {
	case "td", "th":
		return 6
	case "tr":
		return 5
	case "thead", "tbody", "tfoot":
		return 4
	case "col":
		return 3
	case "colgroup":
		return 2
	case "table":
		return 1
	default:
		return 0
	}
}

// resolveOuterBorders resolves each table perimeter edge against the
// table-part borders that touch it. The collapsed border is a single edge:
// its width is split between the table and touching cells rather than
// painting the table border beside a complete cell border. This is
// intentionally bounded to one winner per side; segmented perimeter winners
// are tracked separately.
func (g *tableGrid) resolveOuterBorders(table *StyledNode) {
	if !g.collapse || len(g.rows) == 0 || g.columns == 0 {
		return
	}
	cellNodes := make(map[*StyledNode]struct{})
	for _, row := range g.rows {
		for _, cell := range row.cells {
			cellNodes[cell.node] = struct{}{}
		}
	}
	edges := []string{"top", "right", "bottom", "left"}
	for i, side := range edges {
		count := g.columns
		if i == 1 || i == 3 {
			count = len(g.rows)
		}
		segments := make([]*collapsedTableBorder, count)
		tableCandidate := tableBorderCandidate(table, side)
		candidates := []collapsedTableBorder{tableCandidate}
		rowStart, rowEnd := 0, len(g.rows)
		if side == "top" {
			rowEnd = 1
		} else if side == "bottom" {
			rowStart = len(g.rows) - 1
		}
		for rowIndex := rowStart; rowIndex < rowEnd; rowIndex++ {
			for _, cell := range g.rows[rowIndex].cells {
				touches := (side == "top" && cell.row == 0) ||
					(side == "bottom" && cell.row+cell.rowspan == len(g.rows)) ||
					(side == "left" && cell.col == 0) ||
					(side == "right" && cell.col+cell.colspan == g.columns)
				if touches {
					candidates = append(candidates, tableBorderCandidate(cell.node, side))
				}
			}
		}
		for rowIndex := rowStart; rowIndex < rowEnd; rowIndex++ {
			row := g.rows[rowIndex]
			if row.node != nil {
				candidates = append(candidates, tableBorderCandidate(row.node, side))
			}
			if row.group != nil {
				candidates = append(candidates, tableBorderCandidate(row.group.node, side))
			}
		}
		colStart, colEnd := 0, g.columns
		if side == "left" {
			colEnd = 1
		} else if side == "right" {
			colStart = g.columns - 1
		}
		for col := colStart; col < colEnd && col < len(g.cols); col++ {
			candidates = append(candidates, tableBorderCandidate(g.cols[col], side))
			if col < len(g.colGroups) && g.colGroups[col] != nil {
				candidates = append(candidates, tableBorderCandidate(g.colGroups[col], side))
			}
		}
		if tableCandidate.width == 0 && tableCandidate.style != "hidden" {
			// Preserve the established cell-only perimeter geometry when no
			// table/row/group/column border competes. A non-cell candidate
			// makes this a real conflict even if the table itself has no edge.
			nonCellBorder := false
			for _, candidate := range candidates[1:] {
				if candidate.style == "none" ||
					(candidate.width == 0 && candidate.style != "hidden") {
					continue
				}
				if _, isCell := cellNodes[candidate.node]; !isCell {
					nonCellBorder = true
					break
				}
			}
			if !nonCellBorder {
				continue
			}
		}
		g.outerBorders[i] = resolveCollapsedBorder(candidates...)
		// Resolve each perimeter interval independently.  In particular, a
		// cell spanning the first two columns must not make its border win
		// over a different cell at the next perimeter interval.
		for segment := 0; segment < count; segment++ {
			segmentCandidates := []collapsedTableBorder{tableCandidate}
			for rowIndex, row := range g.rows {
				for _, cell := range row.cells {
					touches := (i == 0 && cell.row == 0 && cell.col <= segment && segment < cell.col+cell.colspan) ||
						(i == 2 && cell.row+cell.rowspan == len(g.rows) && cell.col <= segment && segment < cell.col+cell.colspan) ||
						(i == 3 && cell.col == 0 && cell.row <= segment && segment < cell.row+cell.rowspan) ||
						(i == 1 && cell.col+cell.colspan == g.columns && cell.row <= segment && segment < cell.row+cell.rowspan)
					if touches {
						segmentCandidates = append(segmentCandidates, tableBorderCandidate(cell.node, side))
					}
				}
				if rowIndex < len(g.rows) &&
					((i == 0 && rowIndex == 0) || (i == 2 && rowIndex == len(g.rows)-1) ||
						((i == 1 || i == 3) && rowIndex == segment)) {
					if row.node != nil {
						segmentCandidates = append(segmentCandidates, tableBorderCandidate(row.node, side))
					}
					if row.group != nil {
						segmentCandidates = append(segmentCandidates, tableBorderCandidate(row.group.node, side))
					}
				}
			}
			col := segment
			if i == 1 {
				col = g.columns - 1
			}
			if i == 0 || i == 2 || i == 1 || i == 3 {
				if col >= 0 && col < len(g.cols) {
					segmentCandidates = append(segmentCandidates, tableBorderCandidate(g.cols[col], side))
					if col < len(g.colGroups) && g.colGroups[col] != nil {
						segmentCandidates = append(segmentCandidates, tableBorderCandidate(g.colGroups[col], side))
					}
				}
			}
			segments[segment] = resolveCollapsedBorder(segmentCandidates...)
		}
		same := true
		for _, segment := range segments {
			if (segment == nil) != (g.outerBorders[i] == nil) ||
				(segment != nil && (segment.node != g.outerBorders[i].node ||
					segment.width != g.outerBorders[i].width || segment.style != g.outerBorders[i].style)) {
				same = false
				break
			}
		}
		if !same {
			g.outerBorderSegments[i] = segments
		}
	}
}

func (g *tableGrid) outerBorderAt(side, segment int) *collapsedTableBorder {
	if side >= 0 && side < len(g.outerBorderSegments) &&
		segment >= 0 && segment < len(g.outerBorderSegments[side]) {
		return g.outerBorderSegments[side][segment]
	}
	return g.outerBorders[side]
}

func (g *tableGrid) collapsedCellBorderWidths(cell *tableCellBox, border [4]int) [4]int {
	if !g.collapse {
		return border
	}
	// Internal horizontal edges are painted by row-border fragments. Remove
	// the cell's copy even when the edge crosses a colspan or rowspan.
	if g.hasRowBorderAt(cell.row) {
		border[0] = 0
	}
	if g.hasRowBorderAt(cell.row + cell.rowspan) {
		border[2] = 0
	}
	if cell.colspan == 1 && cell.rowspan == 1 {
		if cell.row == 0 && cell.row < len(g.rows) && g.rows[cell.row].cells != nil &&
			g.columnBorders != nil {
			if cell.col > 0 && cell.col < len(g.columnBorders) && g.columnBorders[cell.col] != nil {
				border[3] = 0
			}
			if cell.col+1 < len(g.columnBorders) && g.columnBorders[cell.col+1] != nil {
				border[1] = g.columnBorders[cell.col+1].width
			}
		}
		if g.columns == 1 {
			if cell.row > 0 && cell.row < len(g.rowBorders) && g.rowBorders[cell.row] != nil {
				border[0] = 0
			}
			if cell.row+1 < len(g.rows) && cell.row+1 < len(g.rowBorders) &&
				g.rowBorders[cell.row+1] != nil {
				border[2] = 0
			}
		}
	}
	if g.collapse && len(g.rows) > 0 {
		sides := []struct {
			index int
			edge  *collapsedTableBorder
			touch bool
		}{
			{0, g.outerBorderAt(0, cell.col), cell.row == 0},
			{1, g.outerBorderAt(1, cell.row), cell.col+cell.colspan == g.columns},
			{2, g.outerBorderAt(2, cell.col), cell.row+cell.rowspan == len(g.rows)},
			{3, g.outerBorderAt(3, cell.row), cell.col == 0},
		}
		for _, side := range sides {
			if side.touch && side.edge != nil {
				// The cell owns the inward half of the shared edge.
				border[side.index] = side.edge.width / 2
			}
		}
	}
	return border
}

func (g *tableGrid) collapsedCellPaintBorderWidths(cell *tableCellBox, border [4]int) [4]int {
	border = g.collapsedCellBorderWidths(cell, border)
	if cell.col > 0 && cell.col < len(g.columnBorders) && g.columnBorders[cell.col] != nil {
		border[3] = 0
	}
	if cell.col+1 < len(g.columnBorders) && g.columnBorders[cell.col+1] != nil {
		border[1] = 0
	}
	return border
}

// addRow records one row, wrapping any non-cell children in anonymous cells.
func (g *tableGrid) addRow(node *StyledNode, children []*StyledNode, group *tableGroupBox) {
	row := &tableRowBox{node: node, group: group}
	var loose []*StyledNode
	flush := func() {
		if len(loose) == 0 {
			return
		}
		anonymous := &StyledNode{Node: node.nodeOrNil(), Style: ComputedStyle{anonymousCellKey: "1"}, Children: loose}
		caption := len(loose) == 1 && isTableCaptionNode(loose[0])
		g.caption = g.caption || caption
		row.cells = append(row.cells, &tableCellBox{node: anonymous, colspan: 1, rowspan: 1, caption: caption})
		loose = nil
	}
	for _, child := range children {
		switch {
		case hiddenNode(child) || blankText(child):
			continue
		case isCellNode(child):
			flush()
			row.cells = append(row.cells, &tableCellBox{node: child,
				colspan: spanAttribute(child, "colspan"), rowspan: spanAttribute(child, "rowspan")})
		default:
			loose = append(loose, child)
		}
	}
	flush()
	g.rows = append(g.rows, row)
}

func (n *StyledNode) nodeOrNil() *Node {
	if n == nil {
		return nil
	}
	return n.Node
}

// assignColumns resolves the grid positions of every cell, honoring colspan
// and rowspan occupancy, and records the resulting column count.
func (g *tableGrid) assignColumns() {
	occupied := map[[2]int]bool{}
	columns := 0
	for r, row := range g.rows {
		col := 0
		for _, cell := range row.cells {
			for occupied[[2]int{r, col}] {
				col++
				if col > maxTableCols {
					break
				}
			}
			cell.row = r
			cell.col = col
			if cell.colspan < 1 {
				cell.colspan = 1
			}
			if cell.rowspan < 1 {
				cell.rowspan = 1
			}
			if cell.col+cell.colspan > maxTableCols {
				cell.colspan = max(1, maxTableCols-cell.col)
			}
			if remaining := len(g.rows) - r; cell.rowspan > remaining {
				cell.rowspan = max(1, remaining)
			}
			for dr := 0; dr < cell.rowspan; dr++ {
				for dc := 0; dc < cell.colspan; dc++ {
					occupied[[2]int{r + dr, col + dc}] = true
				}
			}
			col += cell.colspan
			columns = max(columns, col)
		}
	}
	g.columns = min(columns, maxTableCols)
}

// cellEdges returns the horizontal and vertical border+padding of a cell,
// defaulting to the table's cellpadding when the cell declares no padding.
func (g *tableGrid) cellEdges(cell *tableCellBox) (padding, border [4]int) {
	border = boxEdges(cell.node, "border-width", 0)
	border = g.collapsedCellBorderWidths(cell, border)
	padding = boxEdges(cell.node, "padding", 0)
	for i, side := range []string{"top", "right", "bottom", "left"} {
		if cell.node.Style["padding-"+side] == "" {
			padding[i] = g.padding[i]
		}
	}
	return padding, border
}

// collapsedCellTrailingEdges accounts for the half of an outer collapsed cell
// border by which a caption's anonymous table wrapper is widened and extended.
// Borders are still painted inside Box.Rect by the partial border model, so
// reserving that space on the trailing edges keeps the painted cell and text
// aligned with the caption without changing conflict behavior for internal
// edges or uncaptioned tables.
func (g *tableGrid) collapsedCellTrailingEdges(cell *tableCellBox, border [4]int) (right, bottom int) {
	if !g.collapse || !g.caption {
		return 0, 0
	}
	if cell.col+cell.colspan >= g.columns {
		right = border[1] / 2
	}
	if cell.row+cell.rowspan >= len(g.rows) {
		bottom = border[2] / 2
	}
	return right, bottom
}

func (g *tableGrid) measureCells(faces *faceSet) {
	for _, row := range g.rows {
		for _, cell := range row.cells {
			padding, border := g.cellEdges(cell)
			extra := padding[1] + padding[3] + border[1] + border[3]
			trailingRight, _ := g.collapsedCellTrailingEdges(cell, border)
			extra += trailingRight
			minWidth, maxWidth := contentIntrinsicWidths(cell.node, faces)
			cell.minWidth = minWidth + extra
			cell.maxWidth = max(minWidth, maxWidth) + extra
			if advance, ok := singleTextAdvance(cell.node, faces); ok {
				cell.maxAdvance = advance + fixed.I(extra)
			} else {
				cell.maxAdvance = fixed.I(cell.maxWidth)
			}
			cell.fixed = -1
			cell.percent = -1
			switch value := strings.TrimSpace(cell.node.Style["width"]); {
			case value == "" || strings.EqualFold(value, "auto"):
			case classifyValue(value).Kind == "percentage":
				cell.percent = math.Min(100, math.Max(0, classifyValue(value).Number))
			default:
				// An explicit cell width pins the cell rather than acting as a
				// lower bound, matching the HTML width attribute closely
				// enough for the narrow fixed columns Hacker News uses.
				cell.fixed = max(0, int(math.Round(px(value, 0, 0))))
				cell.minWidth = cell.fixed + extra
				cell.maxWidth = cell.minWidth
				cell.maxAdvance = fixed.I(cell.maxWidth)
			}
			if cell.caption {
				// The caption constrains the anonymous table box by its
				// min-content width, not by its no-wrap max-content width.
				cell.maxWidth = cell.minWidth
			}
		}
	}
}

// singleTextAdvance returns a precise no-wrap advance for a cell containing
// exactly one text node. Integer intrinsic widths remain the allocation
// unit, but this value lets the following column retain the fractional pen
// phase when its position is content-sized. More complex content keeps the
// existing integer geometry until it has an unambiguous intrinsic model.
func singleTextAdvance(n *StyledNode, faces *faceSet) (fixed.Int26_6, bool) {
	var text *StyledNode
	var visit func(*StyledNode) bool
	visit = func(current *StyledNode) bool {
		if current == nil || hiddenNode(current) {
			return true
		}
		if current.Node != nil && current.Node.Type == TextNode {
			if text != nil {
				return false
			}
			text = current
			return true
		}
		for _, child := range current.Children {
			if !visit(child) {
				return false
			}
		}
		return true
	}
	if !visit(n) || text == nil {
		return 0, false
	}
	fields := collapsibleFields(text.Node.Data)
	if len(fields) == 0 {
		return 0, true
	}
	return faces.metrics(text.Style).advance(strings.Join(fields, " ")), true
}

type columnSizes struct {
	min     []int
	max     []int
	percent []float64
	fixed   []bool
}

func (g *tableGrid) columnSizes() columnSizes {
	sizes := columnSizes{min: make([]int, g.columns), max: make([]int, g.columns),
		percent: make([]float64, g.columns), fixed: make([]bool, g.columns)}
	for i := range sizes.percent {
		sizes.percent[i] = -1
	}
	for _, row := range g.rows {
		for _, cell := range row.cells {
			if cell.colspan != 1 || cell.col >= g.columns {
				continue
			}
			sizes.min[cell.col] = max(sizes.min[cell.col], cell.minWidth)
			sizes.max[cell.col] = max(sizes.max[cell.col], cell.maxWidth)
			if cell.percent >= 0 {
				sizes.percent[cell.col] = math.Max(sizes.percent[cell.col], cell.percent)
			}
			if cell.fixed >= 0 {
				// An explicit cell width pins its column, which is how the
				// HTML width attribute behaves on single-column cells.
				sizes.fixed[cell.col] = true
			}
		}
	}
	// Spanning cells only raise the totals of the columns they cover.
	for _, row := range g.rows {
		for _, cell := range row.cells {
			if cell.colspan <= 1 || cell.col >= g.columns {
				continue
			}
			end := min(g.columns, cell.col+cell.colspan)
			span := end - cell.col
			if span <= 0 {
				continue
			}
			inner := g.hspacing * (span - 1)
			distribute(sizes.min[cell.col:end], cell.minWidth-inner)
			distribute(sizes.max[cell.col:end], cell.maxWidth-inner)
		}
	}
	for i := range sizes.max {
		sizes.max[i] = max(sizes.max[i], sizes.min[i])
	}
	return sizes
}

// distribute raises values so that they sum to at least total, adding the
// deficit evenly and deterministically.
func distribute(values []int, total int) {
	if len(values) == 0 {
		return
	}
	sum := 0
	for _, v := range values {
		sum += v
	}
	deficit := total - sum
	if deficit <= 0 {
		return
	}
	share := deficit / len(values)
	remainder := deficit % len(values)
	for i := range values {
		values[i] += share
		if i < remainder {
			values[i]++
		}
	}
}

// resolveColumns turns intrinsic sizes into final column widths that fit
// available, which excludes border spacing.
func resolveColumns(sizes columnSizes, available int) []int {
	count := len(sizes.min)
	widths := make([]int, count)
	if count == 0 {
		return widths
	}
	available = max(0, available)
	assigned := 0
	flexible := make([]int, 0, count)
	for i := 0; i < count; i++ {
		if sizes.percent[i] >= 0 {
			widths[i] = max(sizes.min[i], int(math.Round(float64(available)*sizes.percent[i]/100)))
			assigned += widths[i]
			continue
		}
		widths[i] = sizes.min[i]
		assigned += widths[i]
		if !sizes.fixed[i] {
			flexible = append(flexible, i)
		}
	}
	if assigned > available {
		shrink(widths, available)
		return widths
	}
	extra := available - assigned
	if extra == 0 {
		return widths
	}
	if len(flexible) == 0 {
		// Every column is pinned; trailing space goes to the last column so
		// that an explicitly sized table is still filled.
		widths[count-1] += extra
		return widths
	}
	room := 0
	for _, i := range flexible {
		room += sizes.max[i] - sizes.min[i]
	}
	if room > 0 {
		grow := min(extra, room)
		given := 0
		for index, i := range flexible {
			var share int
			if index == len(flexible)-1 {
				share = grow - given
			} else {
				share = grow * (sizes.max[i] - sizes.min[i]) / room
			}
			widths[i] += share
			given += share
		}
		extra -= grow
	}
	if extra > 0 {
		// Auto tables never exceed their max-content width, so leftover space
		// is only handed out when the table has an explicit width.
		share := extra / len(flexible)
		remainder := extra % len(flexible)
		for index, i := range flexible {
			widths[i] += share
			if index < remainder {
				widths[i]++
			}
		}
	}
	return widths
}

// resolveFixedColumns implements the width-selection portion of CSS fixed
// table layout. Column declarations win, then widths from the first
// non-caption row, and any remaining room is shared by unspecified columns.
// A <col> width is the used width of the column itself: cell padding and
// borders fit inside it rather than being added to it.
func (g *tableGrid) resolveFixedColumns(available int) []int {
	widths := make([]int, g.columns)
	specified := make([]bool, g.columns)
	available = max(0, available)

	for col, node := range g.cols {
		if col >= g.columns {
			break
		}
		value := strings.TrimSpace(node.Style["width"])
		if value == "" || strings.EqualFold(value, "auto") {
			continue
		}
		widths[col] = max(0, int(math.Round(px(value, float64(available), float64(available)))))
		specified[col] = true
	}

	for _, row := range g.rows {
		firstDataRow := false
		for _, cell := range row.cells {
			if !cell.caption {
				firstDataRow = true
				break
			}
		}
		if !firstDataRow {
			continue
		}
		for _, cell := range row.cells {
			if cell.caption || cell.col >= g.columns {
				continue
			}
			target := -1
			switch {
			case cell.percent >= 0:
				target = int(math.Round(float64(available) * cell.percent / 100))
			case cell.fixed >= 0:
				// measureCells includes the cell's padding and border in its
				// fixed min width, as required for first-row cell widths.
				target = cell.minWidth
			}
			if target < 0 {
				continue
			}
			end := min(g.columns, cell.col+cell.colspan)
			innerSpacing := g.hspacing * max(0, end-cell.col-1)
			target = max(0, target-innerSpacing)
			current, open := 0, make([]int, 0, end-cell.col)
			for col := cell.col; col < end; col++ {
				current += widths[col]
				if !specified[col] {
					open = append(open, col)
				}
			}
			if target > current && len(open) > 0 {
				distributeFixedExtra(widths, open, target-current)
				for _, col := range open {
					specified[col] = true
				}
			}
		}
		break
	}

	total := 0
	var open []int
	for col, width := range widths {
		total += width
		if !specified[col] {
			open = append(open, col)
		}
	}
	if remaining := available - total; remaining > 0 {
		if len(open) == 0 {
			open = make([]int, g.columns)
			for i := range open {
				open[i] = i
			}
		}
		distributeFixedExtra(widths, open, remaining)
	}
	return widths
}

func distributeFixedExtra(widths, columns []int, extra int) {
	if len(columns) == 0 || extra <= 0 {
		return
	}
	share, remainder := extra/len(columns), extra%len(columns)
	for i, column := range columns {
		widths[column] += share
		if i < remainder {
			widths[column]++
		}
	}
}

// shrink scales widths down proportionally so their sum fits available.
func shrink(widths []int, available int) {
	total := 0
	for _, w := range widths {
		total += w
	}
	if total <= available || total <= 0 {
		return
	}
	used := 0
	for i := range widths {
		if i == len(widths)-1 {
			widths[i] = max(0, available-used)
			continue
		}
		widths[i] = widths[i] * available / total
		used += widths[i]
	}
}

// layoutTable lays out a table element at x, y inside width pixels and returns
// its box plus the vertical space it consumes, including margins. parentTextAlign
// is kept separate from the table's inherited text-align so centering the table
// does not change alignment of its inline descendants.
func layoutTable(n *StyledNode, x, y, width int, parentTextAlign string, faces *faceSet) (*Box, int) {
	width = max(0, width)
	margin := boxEdges(n, "margin", float64(width))
	border := boxEdges(n, "border-width", float64(width))
	autoLeft := strings.EqualFold(strings.TrimSpace(n.Style["margin-left"]), "auto")
	autoRight := strings.EqualFold(strings.TrimSpace(n.Style["margin-right"]), "auto")
	specifiedWidth := strings.TrimSpace(n.Style["width"])
	fixedLayout := strings.EqualFold(strings.TrimSpace(n.Style["table-layout"]), "fixed") &&
		specifiedWidth != "" && !strings.EqualFold(specifiedWidth, "auto")
	if !autoLeft && !autoRight && specifiedWidth != "" &&
		!strings.EqualFold(specifiedWidth, "auto") &&
		strings.EqualFold(strings.TrimSpace(parentTextAlign), "center") {
		// Legacy <center> applies text-align:center to its children. Browsers
		// also center explicitly sized descendant tables as block boxes.
		autoLeft, autoRight = true, true
	}
	available := max(0, width-margin[1]-margin[3]-border[1]-border[3])

	grid := buildTableGrid(n)
	grid.resolveRowBorderSegments()
	grid.resolveSingleRowCellBorders()
	grid.resolveOuterBorders(n)
	// Keep the table's outer half of a collapsed perimeter edge in its
	// border box. The other half is included in the touching cell below.
	for i, edge := range grid.outerBorders {
		if edge != nil {
			border[i] = edge.width - edge.width/2
		}
	}
	grid.measureCells(faces)
	sizes := grid.columnSizes()

	contentWidth := available
	if value := specifiedWidth; value != "" && !strings.EqualFold(value, "auto") {
		requested := max(0, int(math.Round(px(value, float64(available), float64(available)))))
		// CSS fixed-table width measures the table including its borders; the
		// grid resolved below occupies the inner distance between them.
		if fixedLayout {
			requested = max(0, requested-border[1]-border[3])
		}
		contentWidth = min(available, requested)
	} else {
		intrinsic := grid.hspacing * (grid.columns + 1)
		for _, w := range sizes.max {
			intrinsic += w
		}
		contentWidth = min(available, intrinsic)
	}
	contentWidth = max(contentWidth, tableCaptionMinWidth(n, faces))
	// Horizontal border spacing never pushes the table past the space it was
	// given, so very narrow viewports shrink the column gaps before they clip
	// content. Vertical spacing is unconstrained and kept as specified.
	if grid.columns > 0 && grid.hspacing*(grid.columns+1) > contentWidth {
		grid.hspacing = max(0, contentWidth/(grid.columns+1))
	}
	spacingTotal := grid.hspacing * (grid.columns + 1)
	columnWidths := resolveColumns(sizes, contentWidth-spacingTotal)
	if fixedLayout {
		columnWidths = grid.resolveFixedColumns(contentWidth - spacingTotal)
	}

	free := max(0, width-margin[1]-margin[3]-border[1]-border[3]-contentWidth)
	switch {
	case autoLeft && autoRight:
		margin[3] += free / 2
		margin[1] += free - free/2
	case autoLeft:
		margin[3] += free
	case autoRight:
		margin[1] += free
	}

	originX := x + margin[3] + border[3]

	columnX := make([]int, grid.columns+1)
	cursorX := originX + grid.hspacing
	for i := 0; i < grid.columns; i++ {
		columnX[i] = cursorX
		cursorX += columnWidths[i] + grid.hspacing
	}
	columnX[grid.columns] = cursorX
	tableWidth := max(contentWidth, cursorX-originX)
	// Captions are block containers of the anonymous table wrapper, not
	// cells. Lay them out against its border-box width before positioning the
	// grid, so their margins/backgrounds and inline alignment remain intact.
	captionX := x + margin[3]
	captionWidth := tableWidth + border[1] + border[3]
	layoutCaptions := func(nodes []*StyledNode, top int) ([]*Box, int) {
		var boxes []*Box
		for _, caption := range nodes {
			wrapper := &StyledNode{Style: ComputedStyle{}, Children: []*StyledNode{caption}}
			children, height := layoutChildren(wrapper, captionX, top, captionWidth, faces,
				containingBlock{x: captionX, y: top, width: captionWidth})
			// Inline line boxes reuse their parent's DOM node for text context.
			// They must not paint the caption background a second time: a line
			// taller than an explicit caption height can overflow into the grid.
			for _, box := range children {
				if box.Node == caption.Node {
					for _, line := range box.Children {
						if line.Node == caption.Node {
							line.Node = nil
						}
					}
				}
			}
			boxes = append(boxes, children...)
			top += height
		}
		return boxes, top
	}
	topCaptions, gridTop := layoutCaptions(grid.topCaptions, y+margin[0])
	originY := gridTop + border[0]

	// Keep fractional content-sized column boundaries for text in the next
	// cell. Pixel box widths still use columnWidths; only inline pen origins
	// carry the sub-pixel remainder. A flexible/grown column has no intrinsic
	// text boundary to preserve and therefore contributes no correction.
	columnPhase := make([]fixed.Int26_6, grid.columns+1)
	for col := 0; col < grid.columns; col++ {
		correction := fixed.Int26_6(0)
		hasCorrection := false
		if columnWidths[col] == sizes.max[col] {
			for _, row := range grid.rows {
				for _, cell := range row.cells {
					if cell.col != col || cell.colspan != 1 || cell.maxWidth != sizes.max[col] ||
						cell.maxAdvance.Ceil() != cell.maxWidth {
						continue
					}
					candidate := cell.maxAdvance - fixed.I(cell.maxWidth)
					if !hasCorrection || candidate > correction {
						correction = candidate
						hasCorrection = true
					}
				}
			}
		}
		columnPhase[col+1] = columnPhase[col] + correction
	}

	// First pass: lay out cell content to learn row heights.
	for _, row := range grid.rows {
		for _, cell := range row.cells {
			cellWidth := 0
			end := min(grid.columns, cell.col+cell.colspan)
			for i := cell.col; i < end; i++ {
				cellWidth += columnWidths[i]
			}
			if span := end - cell.col; span > 1 {
				cellWidth += grid.hspacing * (span - 1)
			}
			padding, cellBorder := grid.cellEdges(cell)
			cellBorder = grid.collapsedCellBorderWidths(cell, cellBorder)
			innerWidth := max(0, cellWidth-padding[1]-padding[3]-cellBorder[1]-cellBorder[3])
			contentX := columnX[min(cell.col, grid.columns)] + padding[3] + cellBorder[3]
			// The cell box below owns the cell's background and border. Make
			// its inline-content wrapper anonymous so those decorations are
			// not painted a second time around the content width.
			contentNode := *cell.node
			contentNode.Node = nil
			children, height := layoutChildren(&contentNode, contentX, 0, innerWidth, faces,
				containingBlock{x: contentX, width: innerWidth,
					inlinePenPhase: columnPhase[min(cell.col, grid.columns)], hasInlinePenPhase: true})
			if value := strings.TrimSpace(cell.node.Style["height"]); value != "" && !strings.EqualFold(value, "auto") {
				height = max(height, int(math.Max(0, px(value, 0, float64(height)))))
			}
			cell.box = &Box{Node: cell.node.nodeOrNil(), Children: children}
			cell.box.Rect = image.Rect(columnX[min(cell.col, grid.columns)], 0,
				columnX[min(cell.col, grid.columns)]+cellWidth, 0)
			cell.box.Content = image.Rect(contentX, 0, contentX+innerWidth, height)
			cell.height = height + padding[0] + padding[2] + cellBorder[0] + cellBorder[2]
			_, trailingBottom := grid.collapsedCellTrailingEdges(cell, cellBorder)
			cell.height += trailingBottom
		}
	}

	for _, row := range grid.rows {
		if row.node != nil {
			if value := strings.TrimSpace(row.node.Style["height"]); value != "" && !strings.EqualFold(value, "auto") {
				row.height = max(0, int(math.Max(0, px(value, 0, 0))))
			}
		}
		for _, cell := range row.cells {
			if cell.rowspan == 1 {
				row.height = max(row.height, cell.height)
			}
		}
	}
	// In fixed layout, an explicit table height is a minimum for the row
	// grid. Captions sit outside that grid, so they do not consume any of the
	// height distributed to data rows.
	if fixedLayout {
		if value := strings.TrimSpace(n.Style["height"]); value != "" && !strings.EqualFold(value, "auto") {
			target := max(0, int(math.Round(px(value, 0, 0))))
			have := 0
			var dataRows []int
			for i, row := range grid.rows {
				captionOnly := len(row.cells) > 0
				for _, cell := range row.cells {
					if !cell.caption {
						captionOnly = false
						break
					}
				}
				if captionOnly {
					continue
				}
				have += row.height
				dataRows = append(dataRows, i)
			}
			if deficit := target - have; deficit > 0 && len(dataRows) > 0 {
				share, remainder := deficit/len(dataRows), deficit%len(dataRows)
				for i, row := range dataRows {
					grid.rows[row].height += share
					if i < remainder {
						grid.rows[row].height++
					}
				}
			}
		}
	}
	// Spanning cells add any extra height they need to their last row.
	for _, row := range grid.rows {
		for _, cell := range row.cells {
			if cell.rowspan <= 1 {
				continue
			}
			last := min(len(grid.rows)-1, cell.row+cell.rowspan-1)
			have := grid.vspacing * (last - cell.row)
			for i := cell.row; i <= last; i++ {
				have += grid.rows[i].height
			}
			if cell.height > have {
				grid.rows[last].height += cell.height - have
			}
		}
	}

	cursorY := originY + grid.vspacing
	for i, row := range grid.rows {
		cursorY += grid.collapsedGapBefore(i)
		row.y = cursorY
		cursorY += row.height + grid.vspacing
	}
	if grid.collapse && len(grid.rows) > 0 {
		cursorY += groupBorder(grid.rows[len(grid.rows)-1].group, "bottom")
	}
	tableHeight := max(0, cursorY-originY)

	// Second pass: move cell content into place now that rows are positioned.
	var rowBoxes []*Box
	groupBoxes := map[*tableGroupBox]*Box{}
	for rowIndex, row := range grid.rows {
		rowBox := &Box{Node: row.node.nodeOrNil()}
		rowHeight := row.height
		for _, cell := range row.cells {
			last := min(len(grid.rows)-1, cell.row+cell.rowspan-1)
			spanHeight := grid.vspacing * (last - cell.row)
			for i := cell.row; i <= last; i++ {
				spanHeight += grid.rows[i].height
			}
			padding, cellBorder := grid.cellEdges(cell)
			cellBorder = grid.collapsedCellBorderWidths(cell, cellBorder)
			top := row.y
			offsetY := top + padding[0] + cellBorder[0]
			translateBox(cell.box, 0, offsetY)
			cell.box.Rect = image.Rect(cell.box.Rect.Min.X, top, cell.box.Rect.Max.X, top+max(0, spanHeight))
			cell.box.Content = image.Rect(cell.box.Content.Min.X, offsetY,
				cell.box.Content.Max.X, offsetY+max(0, cell.box.Content.Dy()))
			paintBorders := grid.collapsedCellPaintBorderWidths(cell, cellBorder)
			cell.box.BorderWidths = &paintBorders
			if grid.collapse {
				colors := [4]color.RGBA{
					borderColor(cell.node.Style, "top"),
					borderColor(cell.node.Style, "right"),
					borderColor(cell.node.Style, "bottom"),
					borderColor(cell.node.Style, "left"),
				}
				styles := [4]string{
					borderStyle(cell.node.Style, "top"),
					borderStyle(cell.node.Style, "right"),
					borderStyle(cell.node.Style, "bottom"),
					borderStyle(cell.node.Style, "left"),
				}
				hasColors := false
				for side := range grid.outerBorders {
					edge := grid.outerBorderAt(side, func() int {
						if side == 0 || side == 2 {
							return cell.col
						}
						return cell.row
					}())
					if edge == nil {
						continue
					}
					touches := (side == 0 && cell.row == 0) ||
						(side == 1 && cell.col+cell.colspan == grid.columns) ||
						(side == 2 && cell.row+cell.rowspan == len(grid.rows)) ||
						(side == 3 && cell.col == 0)
					if touches {
						colors[side] = borderColor(edge.node.Style, edge.side)
						styles[side] = edge.style
						hasColors = true
					}
				}
				if hasColors {
					cell.box.BorderColors = &colors
					cell.box.BorderStyles = &styles
				}
			}
			rowBox.Children = append(rowBox.Children, cell.box)
		}
		rowBox.Rect = image.Rect(originX+grid.hspacing, row.y,
			max(originX+grid.hspacing, columnX[grid.columns]-grid.hspacing), row.y+max(0, rowHeight))
		rowBox.Content = rowBox.Rect
		// The resolved collapsed edge is painted once in the gap between
		// rows. Suppress the participating row borders here as well, or
		// transparent cells expose a second copy inside the row box.
		if grid.collapse && row.node != nil &&
			(grid.hasRowBorderAt(rowIndex) || grid.hasRowBorderAt(rowIndex+1)) {
			widths := [4]int{
				borderWidth(row.node.Style, "top"),
				borderWidth(row.node.Style, "right"),
				borderWidth(row.node.Style, "bottom"),
				borderWidth(row.node.Style, "left"),
			}
			if grid.hasRowBorderAt(rowIndex) {
				widths[0] = 0
			}
			if grid.hasRowBorderAt(rowIndex + 1) {
				widths[2] = 0
			}
			rowBox.BorderWidths = &widths
		}
		if grid.hasRowBorderAt(rowIndex) {
			gapTop := grid.rows[rowIndex-1].y + grid.rows[rowIndex-1].height
			if len(grid.rowBorderSegments[rowIndex]) == 0 {
				edge := grid.rowBorders[rowIndex]
				widths := [4]int{edge.width, 0, 0, 0}
				colors := [4]color.RGBA{borderColor(edge.node.Style, edge.side), color.RGBA{}, color.RGBA{}, color.RGBA{}}
				styles := [4]string{edge.style, "", "", ""}
				rowBox.Children = append(rowBox.Children, &Box{
					Node:         rowBox.Node,
					Rect:         image.Rect(rowBox.Rect.Min.X, gapTop, rowBox.Rect.Max.X, row.y),
					Content:      image.Rect(rowBox.Rect.Min.X, gapTop, rowBox.Rect.Max.X, row.y),
					BorderWidths: &widths, BorderColors: &colors,
					BorderStyles: &styles, BorderOnly: true,
				})
			}
			for col, edge := range grid.rowBorderSegments[rowIndex] {
				if edge == nil || col+1 >= len(columnX) {
					continue
				}
				widths := [4]int{edge.width, 0, 0, 0}
				colors := [4]color.RGBA{borderColor(edge.node.Style, edge.side), color.RGBA{}, color.RGBA{}, color.RGBA{}}
				styles := [4]string{edge.style, "", "", ""}
				rowBox.Children = append(rowBox.Children, &Box{
					Node:         rowBox.Node,
					Rect:         image.Rect(columnX[col], gapTop, columnX[col+1], row.y),
					Content:      image.Rect(columnX[col], gapTop, columnX[col+1], row.y),
					BorderWidths: &widths, BorderColors: &colors,
					BorderStyles: &styles, BorderOnly: true,
				})
			}
		}
		for col := 1; col < len(grid.columnBorders)-1; col++ {
			edge := grid.columnBorders[col]
			if edge == nil {
				continue
			}
			borderRect := image.Rect(columnX[col]-edge.width, row.y,
				columnX[col], row.y+row.height)
			widths := [4]int{0, edge.width, 0, 0}
			colors := [4]color.RGBA{}
			colors[1] = borderColor(edge.node.Style, edge.side)
			styles := [4]string{}
			styles[1] = edge.style
			rowBox.Children = append(rowBox.Children, &Box{
				Node: rowBox.Node, Rect: borderRect, Content: borderRect,
				BorderWidths: &widths, BorderColors: &colors,
				BorderStyles: &styles, BorderOnly: true,
			})
		}
		row.box = rowBox
		if row.group == nil {
			rowBoxes = append(rowBoxes, rowBox)
			continue
		}
		groupBox, ok := groupBoxes[row.group]
		if !ok {
			groupBox = &Box{Node: row.group.node.nodeOrNil(), Rect: rowBox.Rect}
			groupBoxes[row.group] = groupBox
			rowBoxes = append(rowBoxes, groupBox)
		}
		groupBox.Children = append(groupBox.Children, rowBox)
		groupBox.Rect = groupBox.Rect.Union(rowBox.Rect)
		groupBox.Content = groupBox.Rect
	}

	if grid.collapse {
		// Collapsed row-group borders sit in the gaps reserved between rows,
		// so the group boxes grow to cover them.
		for group, groupBox := range groupBoxes {
			top, bottom := groupBorder(group, "top"), groupBorder(group, "bottom")
			groupBox.Content = groupBox.Rect
			groupBox.Rect.Min.Y -= top
			groupBox.Rect.Max.Y += bottom
			widths := [4]int{
				borderWidth(group.node.Style, "top"),
				borderWidth(group.node.Style, "right"),
				borderWidth(group.node.Style, "bottom"),
				borderWidth(group.node.Style, "left"),
			}
			groupBox.BorderWidths = &widths
		}
		// A collapsed edge is painted once by its winning row-group side.
		// Otherwise overlapping expanded group boxes can paint two colors
		// into the same reserved gap when the preceding bottom border wins.
		for i := 1; i < len(grid.rows); i++ {
			previous, current := grid.rows[i-1].group, grid.rows[i].group
			if previous == current || previous == nil || current == nil {
				continue
			}
			previousBox, currentBox := groupBoxes[previous], groupBoxes[current]
			if previousBox == nil || currentBox == nil {
				continue
			}
			previousWidths, currentWidths := previousBox.BorderWidths, currentBox.BorderWidths
			if previousWidths == nil || currentWidths == nil {
				continue
			}
			if i < len(grid.rowBorders) && grid.rowBorders[i] != nil {
				(*previousWidths)[2], (*currentWidths)[0] = 0, 0
				continue
			}
			if (*previousWidths)[2] >= (*currentWidths)[0] {
				(*currentWidths)[0] = 0
			} else {
				(*previousWidths)[2] = 0
			}
		}
	}

	if grid.collapse {
		grid.bridgeEmptyRows()
	}

	content := image.Rect(originX, originY, originX+tableWidth, originY+tableHeight)
	rect := image.Rect(captionX, gridTop, content.Max.X+border[1], content.Max.Y+border[2])
	bottomCaptions, end := layoutCaptions(grid.bottomCaptions, rect.Max.Y)
	children := append(topCaptions, rowBoxes...)
	children = append(children, bottomCaptions...)
	box := &Box{Node: n.Node, Rect: rect, Content: content, Children: children}
	if grid.collapse {
		widths := border
		for side := range grid.outerBorders {
			if len(grid.outerBorderSegments[side]) != 0 {
				widths[side] = 0
			}
		}
		box.BorderWidths = &widths
		colors := [4]color.RGBA{
			borderColor(n.Style, "top"),
			borderColor(n.Style, "right"),
			borderColor(n.Style, "bottom"),
			borderColor(n.Style, "left"),
		}
		styles := [4]string{
			borderStyle(n.Style, "top"),
			borderStyle(n.Style, "right"),
			borderStyle(n.Style, "bottom"),
			borderStyle(n.Style, "left"),
		}
		hasColors := false
		for side := range grid.outerBorders {
			edge := grid.outerBorders[side]
			if edge != nil {
				colors[side] = borderColor(edge.node.Style, edge.side)
				styles[side] = edge.style
				hasColors = true
			}
		}
		if hasColors {
			box.BorderColors = &colors
			box.BorderStyles = &styles
		}
		// Paint the perimeter as independent intervals. The table box still
		// reserves the maximum width on each side for layout, but must not
		// paint that maximum winner across intervals won by other cells.
		for side, segments := range grid.outerBorderSegments {
			for segment, edge := range segments {
				if edge == nil || edge.width == 0 {
					continue
				}
				var r image.Rectangle
				switch side {
				case 0:
					r = image.Rect(columnX[segment], rect.Min.Y, columnX[segment+1], rect.Min.Y+edge.width)
				case 2:
					r = image.Rect(columnX[segment], rect.Max.Y-edge.width, columnX[segment+1], rect.Max.Y)
				case 3:
					row := grid.rows[segment]
					r = image.Rect(rect.Min.X, row.y, rect.Min.X+edge.width, row.y+row.height)
				case 1:
					row := grid.rows[segment]
					r = image.Rect(rect.Max.X-edge.width, row.y, rect.Max.X, row.y+row.height)
				}
				widths := [4]int{}
				widths[side] = edge.width
				colors := [4]color.RGBA{}
				styles := [4]string{}
				colors[side] = borderColor(edge.node.Style, edge.side)
				styles[side] = edge.style
				box.Children = append(box.Children, &Box{
					Node: box.Node, Rect: r, Content: r,
					BorderWidths: &widths, BorderColors: &colors,
					BorderStyles: &styles, BorderOnly: true,
				})
			}
		}
	}
	return box, end - y + margin[2]
}

func translateBox(b *Box, dx, dy int) {
	if b != nil {
		if b.externalPositionX {
			dx = 0
		}
		if b.externalPositionY {
			dy = 0
		}
	}
	if b == nil || (dx == 0 && dy == 0) {
		return
	}
	offset := image.Pt(dx, dy)
	b.Rect = b.Rect.Add(offset)
	b.Content = b.Content.Add(offset)
	b.LastBaseline += dy
	for i := range b.InlineBackgrounds {
		b.InlineBackgrounds[i].Rect = b.InlineBackgrounds[i].Rect.Add(offset)
	}
	for i := range b.Text {
		b.Text[i].Rect = b.Text[i].Rect.Add(offset)
		b.Text[i].PenX += fixed.I(dx)
	}
	for i := range b.Images {
		b.Images[i].Rect = b.Images[i].Rect.Add(offset)
	}
	for _, child := range b.Children {
		translateBox(child, dx, dy)
	}
}

// intrinsicWidths reports the minimum (longest unbreakable content) and
// maximum (no wrapping) content widths of a styled subtree.
func intrinsicWidths(n *StyledNode, faces *faceSet) (int, int) {
	if n == nil || n.Node == nil {
		return 0, 0
	}
	if hiddenNode(n) {
		return 0, 0
	}
	if n.Node.Type == TextNode {
		return textIntrinsic(n, faces)
	}
	if n.Node.Type != ElementNode && n.Node.Type != DocumentNode {
		return 0, 0
	}
	if n.Node.Type == ElementNode && strings.EqualFold(n.Node.Name, "img") {
		width, _ := imageDimensions(n, faces.images[n.Node], 0)
		return width, width
	}
	if isTableNode(n) && n.Node.Type == ElementNode {
		return tableIntrinsic(n, faces)
	}

	minWidth, maxWidth := contentIntrinsicWidths(n, faces)
	if n.Node.Type == ElementNode {
		padding := boxEdges(n, "padding", 0)
		border := boxEdges(n, "border-width", 0)
		margin := boxEdges(n, "margin", 0)
		extra := padding[1] + padding[3] + border[1] + border[3] + margin[1] + margin[3]
		if value := strings.TrimSpace(n.Style["width"]); value != "" && !strings.EqualFold(value, "auto") &&
			classifyValue(value).Kind != "percentage" {
			fixed := max(0, int(math.Round(px(value, 0, 0))))
			minWidth, maxWidth = fixed, fixed
		}
		// An atomic inline contributes at least its used min-width (#285).
		// Block-level min/max-width remain unimplemented.
		if isAtomicInline(n) {
			if floor, ok := minContentWidth(n, 0); ok {
				minWidth, maxWidth = max(minWidth, floor), max(maxWidth, floor)
			}
		}
		minWidth += extra
		maxWidth += extra
	}
	return minWidth, max(minWidth, maxWidth)
}

// contentIntrinsicWidths measures the children of a node, keeping consecutive
// inline children on one hypothetical line and stacking block children.
func contentIntrinsicWidths(n *StyledNode, faces *faceSet) (int, int) {
	if n == nil {
		return 0, 0
	}
	if n.Node != nil && n.Node.Type == ElementNode && isFlexContainer(n) {
		return flexIntrinsicWidths(n, faces)
	}
	minWidth, maxWidth := 0, 0
	inlineMin, inlineMax := 0, 0
	flush := func() {
		minWidth = max(minWidth, inlineMin)
		maxWidth = max(maxWidth, inlineMax)
		inlineMin, inlineMax = 0, 0
	}
	for _, child := range wrapAnonymousTables(n, n.Children) {
		if hiddenNode(child) {
			continue
		}
		childMin, childMax := intrinsicWidths(child, faces)
		// Floats are blockified, but with unlimited width consecutive floats
		// sit side by side with each other and the surrounding inline
		// content, so their margin boxes add to the current line (#349). A
		// clearing float starts a new line below the earlier floats.
		if floatSide(child) != "" && !positioned(child) {
			if clear := strings.ToLower(strings.TrimSpace(child.Style["clear"])); clear != "" && clear != "none" {
				flush()
			}
			inlineMin = max(inlineMin, childMin)
			inlineMax += childMax
			continue
		}
		if child.Node != nil && child.Node.Type != TextNode && (displayBlock(child) || isTableNode(child)) {
			flush()
			minWidth = max(minWidth, childMin)
			maxWidth = max(maxWidth, childMax)
			continue
		}
		if noWrap(n.Style) {
			// Without soft wrap opportunities between inline children, the
			// run's minimum is the sum of their minimums (CSS Text 3 §3).
			inlineMin += childMin
		} else {
			inlineMin = max(inlineMin, childMin)
		}
		inlineMax += childMax
	}
	flush()
	return minWidth, max(minWidth, maxWidth)
}

func textIntrinsic(n *StyledNode, faces *faceSet) (int, int) {
	m := faces.metrics(n.Style)
	fields := collapsibleFields(n.Node.Data)
	if len(fields) == 0 {
		return 0, 0
	}
	minWidth, maxWidth := 0, 0
	for i, field := range fields {
		w := m.width(field)
		minWidth = max(minWidth, w)
		maxWidth += w
		if i > 0 {
			maxWidth += m.width(" ")
		}
	}
	if noWrap(n.Style) {
		// nowrap removes the soft wrap opportunities at spaces.
		minWidth = maxWidth
	}
	return minWidth, maxWidth
}

// tableIntrinsic reports the intrinsic widths of a nested table, including its
// border spacing, so that outer tables can size columns around it.
func tableIntrinsic(n *StyledNode, faces *faceSet) (int, int) {
	grid := buildTableGrid(n)
	grid.measureCells(faces)
	sizes := grid.columnSizes()
	spacing := grid.hspacing * (grid.columns + 1)
	minWidth, maxWidth := spacing, spacing
	for i := range sizes.min {
		minWidth += sizes.min[i]
		maxWidth += sizes.max[i]
	}
	captionMinWidth := tableCaptionMinWidth(n, faces)
	border := boxEdges(n, "border-width", 0)
	margin := boxEdges(n, "margin", 0)
	extra := border[1] + border[3] + margin[1] + margin[3]
	if value := strings.TrimSpace(n.Style["width"]); value != "" && !strings.EqualFold(value, "auto") &&
		classifyValue(value).Kind != "percentage" {
		fixed := max(0, int(math.Round(px(value, 0, 0))))
		return max(fixed+extra, captionMinWidth), max(max(fixed, minWidth)+extra, captionMinWidth)
	}
	return max(minWidth+extra, captionMinWidth), max(max(minWidth, maxWidth)+extra, captionMinWidth)
}

// inlineTablePart lays out an inline-level table as an atomic inline box at
// the origin. The fragment is shrink-to-fit inside the available inline
// width, so auto margins resolve to zero rather than centering the table in
// the line, and the caller translates it onto the line it lands on.
func inlineTablePart(n *StyledNode, available int, faces *faceSet) inlinePart {
	available = max(0, available)
	_, preferred := tableIntrinsic(n, faces)
	box, height := layoutTable(n, 0, 0, min(available, max(0, preferred)), "", faces)
	// The table and its descendants form a single atomic inline paint layer,
	// after the surrounding line's inline background fragments.
	box.AtomicInline = true
	margin := boxEdges(n, "margin", float64(available))
	width := box.Rect.Max.X + margin[1]
	baseline, ok := firstTextBaseline(box, faces)
	if !ok {
		// With no in-flow line box the bottom margin edge is the baseline,
		// matching how browsers align an inline table with no text.
		baseline = height
	}
	return inlinePart{node: n.Node, style: n.Style, table: box, tableW: max(0, width),
		tableH: max(0, height), tableBaseline: baseline, isTable: true}
}

// firstTextBaseline returns the baseline of the first text run in a laid out
// subtree, in the subtree's own coordinate space. An inline table uses the
// baseline of its first row, which is the first text a row lays out.
func firstTextBaseline(b *Box, faces *faceSet) (int, bool) {
	if b == nil {
		return 0, false
	}
	for _, run := range b.Text {
		ascent, _ := faces.metrics(run.Style).lineMetrics()
		return run.Rect.Min.Y + ascent, true
	}
	for _, child := range b.Children {
		if baseline, ok := firstTextBaseline(child, faces); ok {
			return baseline, true
		}
	}
	return 0, false
}

// bridgeEmptyRows paints the collapsed borders that span rows without cells.
// In the collapsing model half of a cell's bottom (top) border lies outside
// the cell, in the row below (above). When that row has no cells of its own,
// nothing else paints there, so the partial model would otherwise leave a
// background-colored gap across every column (WPT
// tables/border-collapse-empty-row). Each run of consecutive empty rows gets
// border-only bands: the upper half-border of the cell above hangs down from
// the run's top edge and the lower half-border of the cell below rises from
// its bottom edge, each clipped to the run. Rows taller than both halves keep
// a gap in between, as they do with genuinely collapsed borders.
func (g *tableGrid) bridgeEmptyRows() {
	occupant := map[[2]int]*tableCellBox{}
	for _, row := range g.rows {
		for _, cell := range row.cells {
			for r := cell.row; r < min(len(g.rows), cell.row+cell.rowspan); r++ {
				for c := cell.col; c < min(g.columns, cell.col+cell.colspan); c++ {
					occupant[[2]int{r, c}] = cell
				}
			}
		}
	}
	empty := func(r int) bool {
		for c := 0; c < g.columns; c++ {
			if occupant[[2]int{r, c}] != nil {
				return false
			}
		}
		return true
	}
	for first := 0; first < len(g.rows); first++ {
		if !empty(first) {
			continue
		}
		last := first
		for last+1 < len(g.rows) && empty(last+1) {
			last++
		}
		host := g.rows[first].box
		top, bottom := g.rows[first].y, g.rows[last].y+g.rows[last].height
		if first > 0 && last+1 < len(g.rows) && host != nil && bottom > top {
			seen := map[*tableCellBox]bool{}
			for c := 0; c < g.columns; c++ {
				if above := occupant[[2]int{first - 1, c}]; above != nil && !seen[above] {
					seen[above] = true
					if band := collapsedBorderBand(above, "bottom", top, bottom, false); band != nil {
						host.Children = append(host.Children, band)
					}
				}
				if below := occupant[[2]int{last + 1, c}]; below != nil && !seen[below] {
					seen[below] = true
					if band := collapsedBorderBand(below, "top", top, bottom, true); band != nil {
						host.Children = append(host.Children, band)
					}
				}
			}
		}
		first = last
	}
}

// collapsedBorderBand returns a border-only box covering the outer half of a
// cell's collapsed top or bottom border within [top, bottom). fromBottom
// anchors the band to bottom (a top border rising from the next cell).
func collapsedBorderBand(cell *tableCellBox, side string, top, bottom int, fromBottom bool) *Box {
	if cell == nil || cell.box == nil || cell.node == nil {
		return nil
	}
	extent := min(bottom-top, borderWidth(cell.node.Style, side)/2)
	if extent <= 0 {
		return nil
	}
	rect := image.Rect(cell.box.Rect.Min.X, top, cell.box.Rect.Max.X, top+extent)
	if fromBottom {
		rect = image.Rect(cell.box.Rect.Min.X, bottom-extent, cell.box.Rect.Max.X, bottom)
	}
	widths := [4]int{}
	if side == "top" {
		widths[0] = extent
	} else {
		widths[2] = extent
	}
	return &Box{Node: cell.node.nodeOrNil(), Rect: rect, Content: rect, BorderWidths: &widths, BorderOnly: true}
}
