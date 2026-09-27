package browser

import (
	"bytes"
	"encoding/xml"
	"errors"
	"image"
	"image/color"
	"image/draw"
	"io"
	"math"
	"strconv"
	"strings"

	"golang.org/x/image/vector"
)

// A minimal SVG subset, enough for simple icons such as the Hacker News logo
// and vote arrow: <svg> sizing/viewBox/preserveAspectRatio, <g>, <path>, and
// the basic shapes (<rect>, <circle>, <ellipse>, <line>, <polyline>,
// <polygon>) with solid fills, strokes, transforms and local <defs>/<use>.
// Other elements are skipped.
// A document that cannot be parsed returns an error so
// callers keep their existing placeholder or empty-background behavior.

const (
	maxSVGBytes       = 1 << 20
	maxSVGElements    = 10000
	maxSVGPathSegs    = 200000
	maxSVGRasterSide  = 4096
	svgNamespace      = "http://www.w3.org/2000/svg"
	svgXLinkNamespace = "http://www.w3.org/1999/xlink"
	maxSVGUseDepth    = 64
	defaultSVGWidth   = 300
	defaultSVGHeight  = 150
	maxSVGGeometry    = 1e7
	svgCircleConstant = 0.5522847498307936 // 4/3*(sqrt(2)-1), cubic arc control distance
)

var errUnsupportedSVG = errors.New("unsupported svg")

// svgAffine is a 2D affine transform [a c e; b d f].
type svgAffine struct{ a, b, c, d, e, f float64 }

var svgIdentity = svgAffine{a: 1, d: 1}

// then returns the transform that applies t first and then u.
func (t svgAffine) then(u svgAffine) svgAffine {
	return svgAffine{
		a: u.a*t.a + u.c*t.b, b: u.b*t.a + u.d*t.b,
		c: u.a*t.c + u.c*t.d, d: u.b*t.c + u.d*t.d,
		e: u.a*t.e + u.c*t.f + u.e, f: u.b*t.e + u.d*t.f + u.f,
	}
}

func (t svgAffine) apply(x, y float64) (float64, float64) {
	return t.a*x + t.c*y + t.e, t.b*x + t.d*y + t.f
}

// svgSegment is one absolute path command in user space: 'M', 'L', 'Q', 'C'
// or 'Z'. Shorthand, relative, H/V, and arc commands are normalized when parsing.
type svgSegment struct {
	op  byte
	pts [3][2]float64
}

type svgShape struct {
	segments   []svgSegment
	transform  svgAffine
	fill       color.NRGBA
	fillRule   string // "nonzero" (default) or "evenodd"
	stroke     color.NRGBA
	width      float64
	cap, join  string
	miterLimit float64
	dashArray  []float64
	dashOffset float64
}

// svgImage is an image.Image rasterized at its intrinsic size. Painters that
// know the used size call rasterize to render crisply at that size instead.
type svgImage struct {
	width, height float64
	dashBasis     float64
	userWidth     float64
	userHeight    float64
	rootFontSize  float64
	viewBox       [4]float64
	hasViewBox    bool
	align         string // "none" or e.g. "xMidYMid"
	slice         bool
	shapes        []svgShape
	*image.RGBA
}

// looksLikeSVG reports whether data appears to be an SVG document rather than
// a raster format; it only inspects a short prefix.
func looksLikeSVG(data []byte) bool {
	prefix := data
	if len(prefix) > 4096 {
		prefix = prefix[:4096]
	}
	return bytes.Contains(prefix, []byte("<svg"))
}

// Keep the small source tree so forward references work. Only elements and
// attributes are retained; no text, network resources or external URLs.
type svgNode struct {
	name     string
	attrs    map[string]string
	href     string
	valid    bool
	children []*svgNode
}

type svgFrame struct {
	fill          color.NRGBA
	hasFill       bool
	fontSize      float64
	fillRule      string
	opacity       float64
	stroke        color.NRGBA
	hasStroke     bool
	strokeOpacity float64
	width         float64
	cap, join     string
	miterLimit    float64
	dashArray     []float64
	dashOffset    float64
	transform     svgAffine
}

func svgDefaultFrame() svgFrame {
	return svgFrame{fill: color.NRGBA{A: 255}, hasFill: true, fillRule: "nonzero",
		fontSize: 16, opacity: 1, strokeOpacity: 1, width: 1, cap: "butt", join: "miter",
		miterLimit: 4, transform: svgIdentity}
}

func decodeSVG(data []byte) (*svgImage, error) {
	if len(data) > maxSVGBytes {
		return nil, errUnsupportedSVG
	}
	decoder := xml.NewDecoder(bytes.NewReader(data))
	decoder.Strict = true
	img := &svgImage{align: "xMidYMid"}
	var root *svgNode
	var stack []*svgNode
	ids := make(map[string]*svgNode)
	elements := 0
	for {
		token, err := decoder.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, errUnsupportedSVG
		}
		switch t := token.(type) {
		case xml.StartElement:
			elements++
			if elements > maxSVGElements {
				return nil, errUnsupportedSVG
			}
			node := &svgNode{name: t.Name.Local, attrs: svgAttributes(t),
				valid: t.Name.Space == svgNamespace || t.Name.Space == ""}
			hasHref := false
			for _, a := range t.Attr {
				if a.Name.Local == "href" && (a.Name.Space == "" || a.Name.Space == svgNamespace) {
					node.href = a.Value // unqualified href takes precedence
					hasHref = true
					break
				}
			}
			if !hasHref {
				for _, a := range t.Attr {
					if a.Name.Space == svgXLinkNamespace && a.Name.Local == "href" {
						node.href = a.Value
						break
					}
				}
			}
			if root == nil {
				if t.Name.Local != "svg" || (t.Name.Space != svgNamespace && t.Name.Space != "") {
					return nil, errUnsupportedSVG
				}
				root = node
				if err := img.parseRoot(node.attrs); err != nil {
					return nil, err
				}
				// The root's own transform attribute (SVG 2) is not applied.
				delete(node.attrs, "transform")
			} else if len(stack) == 0 {
				// Multiple root elements are not an SVG document.
				return nil, errUnsupportedSVG
			} else {
				parent := stack[len(stack)-1]
				parent.children = append(parent.children, node)
			}
			if node.valid && node.attrs["id"] != "" {
				if _, exists := ids[node.attrs["id"]]; !exists {
					ids[node.attrs["id"]] = node
				}
			}
			stack = append(stack, node)
		case xml.EndElement:
			stack = stack[:len(stack)-1]
		}
	}
	if root == nil {
		return nil, errUnsupportedSVG
	}
	state := svgExpansion{img: img, root: root, ids: ids, active: make(map[*svgNode]bool)}
	if err := state.walk(root, svgDefaultFrame(), false, 0); err != nil {
		return nil, err
	}
	raster := img.rasterize(int(math.Round(img.width)), int(math.Round(img.height)))
	if raster == nil {
		return nil, errUnsupportedSVG
	}
	img.RGBA = raster
	return img, nil
}

// The expansion budget counts every visited element (including non-painting
// groups) and every emitted segment, not just source XML nodes. Recursive
// references and exponentially branching references cannot bypass it.
type svgExpansion struct {
	img      *svgImage
	root     *svgNode
	ids      map[string]*svgNode
	active   map[*svgNode]bool
	elements int
	segments int
}

func (s *svgExpansion) walk(node *svgNode, parent svgFrame, referenced bool, useDepth int) error {
	s.elements++
	if s.elements > maxSVGElements {
		return errUnsupportedSVG
	}
	if useDepth > maxSVGUseDepth || !node.valid {
		return nil
	}
	name := node.name
	if name == "defs" && !referenced || name != "svg" && name != "defs" && name != "use" && !svgRenderedElements[name] {
		return nil
	}
	// Only the document root SVG is rendered; referenced groups/shapes work,
	// but a referenced nested SVG requires viewport semantics we do not support.
	if name == "svg" && node != s.root {
		return nil
	}
	current := parent
	a := node.attrs
	if value, ok := a["fill"]; ok {
		current.fill, current.hasFill = svgPaint(value, parent.fill, parent.hasFill)
	}
	rootFontSize := s.img.rootFontSize
	if node == s.root {
		// On the root element, rem is relative to its initial value rather
		// than to the root font size being computed here.
		rootFontSize = 16
	}
	if n, ok := svgFontSize(a["font-size"], parent.fontSize, rootFontSize); ok {
		current.fontSize = n
	}
	if n, ok := svgUnitInterval(a["fill-opacity"]); ok {
		current.opacity = n // inherited property, not ancestor compositing
	}
	if rule := strings.TrimSpace(a["fill-rule"]); rule == "evenodd" || rule == "nonzero" {
		current.fillRule = rule
	}
	if value, ok := a["stroke"]; ok {
		current.stroke, current.hasStroke = svgPaint(value, parent.stroke, parent.hasStroke)
	}
	if n, ok := svgUnitInterval(a["stroke-opacity"]); ok {
		current.strokeOpacity = n
	}
	if n, ok := svgLength(a["stroke-width"]); ok && n <= maxSVGStrokeWidth {
		current.width = n
	}
	if v := a["stroke-linecap"]; v == "butt" || v == "square" || v == "round" {
		current.cap = v
	}
	if v := a["stroke-linejoin"]; v == "miter" || v == "bevel" || v == "round" {
		current.join = v
	}
	if n, err := strconv.ParseFloat(strings.TrimSpace(a["stroke-miterlimit"]), 64); err == nil && n >= 1 && n <= maxSVGStrokeWidth {
		current.miterLimit = n
	}
	if value, ok := a["stroke-dasharray"]; ok {
		if pattern, valid := parseSVGStrokeDashArray(value, s.img.dashBasis); valid {
			current.dashArray = pattern
		}
	}
	if n, valid := parseSVGStrokeDashOffset(a["stroke-dashoffset"], s.img.dashBasis); valid {
		current.dashOffset = n
	}
	if value, ok := a["transform"]; ok {
		transform, ok := parseSVGTransform(value)
		if !ok {
			return nil
		}
		current.transform = transform.then(parent.transform)
	}
	if name == "use" {
		// Only same-document fragment IDs: never open a URL or interpret a
		// fragment as a filesystem path.
		if !strings.HasPrefix(node.href, "#") || len(node.href) < 2 {
			return nil
		}
		target := s.ids[node.href[1:]]
		if target == nil || s.active[target] {
			return nil
		}
		x, okX := svgCoordinate(a["x"])
		y, okY := svgCoordinate(a["y"])
		if a["x"] != "" && !okX || a["y"] != "" && !okY {
			return nil
		}
		current.transform = (svgAffine{a: 1, d: 1, e: x, f: y}).then(current.transform)
		s.active[target] = true
		err := s.walk(target, current, true, useDepth+1)
		delete(s.active, target)
		return err
	}
	if current.hasFill || current.hasStroke {
		var shape []svgSegment
		switch name {
		case "path":
			shape = parseSVGPath(a["d"], maxSVGPathSegs-s.segments)
		case "rect":
			shape = svgRectWithLengths(a, s.img, current.fontSize)
		case "circle":
			basis := svgLengthBasis{
				horizontal: s.img.userWidth, vertical: s.img.userHeight, diagonal: s.img.dashBasis,
				fontSize: current.fontSize, rootFontSize: s.img.rootFontSize,
			}
			if r, ok := basis.length(a["r"], svgDiagonal); ok {
				shape = svgEllipseWithLengths(a, r, r, basis)
			}
		case "ellipse":
			shape = svgEllipseAttrsWithLengths(a, s.img, current.fontSize)
		case "line":
			shape = svgLineWithLengths(a, s.img, current.fontSize)
		case "polyline", "polygon":
			shape = svgPolyline(a["points"], name == "polygon", maxSVGPathSegs-s.segments)
		}
		s.segments += len(shape)
		if s.segments >= maxSVGPathSegs {
			return errUnsupportedSVG
		}
		if len(shape) > 0 {
			fill := current.fill
			fill.A = uint8(math.Round(float64(fill.A) * current.opacity))
			if !current.hasFill {
				fill = color.NRGBA{}
			}
			stroke := current.stroke
			stroke.A = uint8(math.Round(float64(stroke.A) * current.strokeOpacity))
			if !current.hasStroke {
				stroke = color.NRGBA{}
			}
			s.img.shapes = append(s.img.shapes, svgShape{segments: shape, transform: current.transform, fill: fill, fillRule: current.fillRule, stroke: stroke, width: current.width, cap: current.cap, join: current.join, miterLimit: current.miterLimit, dashArray: current.dashArray, dashOffset: current.dashOffset})
		}
	}
	for _, child := range node.children {
		if err := s.walk(child, current, false, useDepth); err != nil {
			return err
		}
	}
	return nil
}

const maxSVGStrokeWidth = 4096
const maxSVGStrokeDashEntries = 64
const maxSVGStrokeDashLength = 1e7

// An empty dash array means a solid stroke. Invalid values are ignored by the
// caller, preserving the inherited value just like other presentation styles.
func parseSVGStrokeDashArray(value string, percentBasis float64) ([]float64, bool) {
	value = strings.TrimSpace(value)
	if value == "none" {
		return nil, true
	}
	values, ok := parseSVGStrokeLengths(value, percentBasis)
	if !ok || len(values) == 0 || len(values) > maxSVGStrokeDashEntries {
		return nil, false
	}
	total := 0.0
	for _, n := range values {
		if n < 0 || math.IsNaN(n) || math.IsInf(n, 0) || n > maxSVGStrokeDashLength {
			return nil, false
		}
		total += n
	}
	if total > maxSVGStrokeDashLength || math.IsInf(total, 0) {
		return nil, false
	}
	if total == 0 {
		return nil, true
	}
	if len(values)%2 != 0 {
		values = append(append([]float64(nil), values...), values...)
	}
	return values, true
}

func parseSVGStrokeDashOffset(value string, percentBasis float64) (float64, bool) {
	n, ok := parseSVGStrokeLength(value, percentBasis)
	return n, ok && math.Abs(n) <= maxSVGStrokeDashLength
}

// parseSVGStrokeLengths reads the comma/whitespace-separated CSS lengths used
// by stroke-dasharray. Restricting separators and entries keeps malformed or
// adversarial values from growing parser or rasterizer work without bound.
func parseSVGStrokeLengths(value string, percentBasis float64) ([]float64, bool) {
	p := &svgNumbers{s: strings.TrimSpace(value)}
	if p.i == len(p.s) || p.s[0] == ',' {
		return nil, false
	}
	var values []float64
	for p.i < len(p.s) {
		n, ok := p.number()
		if !ok {
			return nil, false
		}
		unitStart := p.i
		for p.i < len(p.s) && ((p.s[p.i] >= 'a' && p.s[p.i] <= 'z') ||
			(p.s[p.i] >= 'A' && p.s[p.i] <= 'Z') || p.s[p.i] == '%') {
			p.i++
		}
		length, ok := resolveSVGStrokeLength(n, strings.ToLower(p.s[unitStart:p.i]), percentBasis)
		if !ok {
			return nil, false
		}
		if len(values) >= maxSVGStrokeDashEntries {
			return nil, false
		}
		values = append(values, length)
		if p.i == len(p.s) {
			break
		}
		if p.s[p.i] != ',' && p.s[p.i] != ' ' && p.s[p.i] != '\t' &&
			p.s[p.i] != '\n' && p.s[p.i] != '\r' && p.s[p.i] != '\f' {
			return nil, false
		}
		hadComma := false
		for p.i < len(p.s) {
			switch p.s[p.i] {
			case ',':
				if hadComma {
					return nil, false
				}
				hadComma = true
				p.i++
			case ' ', '\t', '\n', '\r', '\f':
				p.i++
			default:
				goto separated
			}
		}
		return nil, false
	separated:
	}
	return values, len(values) > 0
}

func parseSVGStrokeLength(value string, percentBasis float64) (float64, bool) {
	value = strings.TrimSpace(value)
	p := &svgNumbers{s: value}
	n, ok := p.number()
	if !ok {
		return 0, false
	}
	unitStart := p.i
	for p.i < len(p.s) && ((p.s[p.i] >= 'a' && p.s[p.i] <= 'z') ||
		(p.s[p.i] >= 'A' && p.s[p.i] <= 'Z') || p.s[p.i] == '%') {
		p.i++
	}
	if p.i != len(p.s) {
		return 0, false
	}
	return resolveSVGStrokeLength(n, strings.ToLower(p.s[unitStart:p.i]), percentBasis)
}

func resolveSVGStrokeLength(n float64, unit string, percentBasis float64) (float64, bool) {
	if math.IsNaN(n) || math.IsInf(n, 0) {
		return 0, false
	}
	switch unit {
	case "":
	case "px":
	case "in":
		n *= 96
	case "cm":
		n *= 96 / 2.54
	case "mm":
		n *= 96 / 25.4
	case "q":
		n *= 96 / 101.6
	case "pt":
		n *= 96.0 / 72
	case "pc":
		n *= 16
	case "%":
		n *= percentBasis / 100
	default:
		return 0, false
	}
	return n, !math.IsNaN(n) && !math.IsInf(n, 0)
}

func svgUnitInterval(s string) (float64, bool) {
	n, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
	return math.Max(0, math.Min(1, n)), err == nil && !math.IsNaN(n)
}

// svgAttributes merges presentation attributes with the style attribute; the
// style attribute wins, as in CSS.
func svgAttributes(t xml.StartElement) map[string]string {
	attrs := make(map[string]string, len(t.Attr))
	style := ""
	for _, a := range t.Attr {
		if a.Name.Space != "" && a.Name.Space != svgNamespace {
			continue
		}
		if a.Name.Local == "style" {
			style = a.Value
			continue
		}
		attrs[a.Name.Local] = a.Value
	}
	for _, d := range ParseDeclarations(style) {
		attrs[strings.ToLower(d.Property)] = d.Value
	}
	return attrs
}

func (img *svgImage) parseRoot(attrs map[string]string) error {
	if value, ok := attrs["viewBox"]; ok {
		nums, ok := svgNumberList(value)
		if !ok || len(nums) != 4 {
			return errUnsupportedSVG
		}
		if nums[2] <= 0 || nums[3] <= 0 {
			return errUnsupportedSVG
		}
		copy(img.viewBox[:], nums)
		img.hasViewBox = true
	}
	if value, ok := attrs["preserveAspectRatio"]; ok {
		fields := strings.Fields(value)
		if len(fields) > 0 && fields[0] == "defer" {
			fields = fields[1:]
		}
		if len(fields) > 0 {
			img.align = fields[0]
		}
		img.slice = len(fields) > 1 && fields[1] == "slice"
	}
	width, hasWidth := svgLength(attrs["width"])
	height, hasHeight := svgLength(attrs["height"])
	ratio := 0.0
	if img.hasViewBox {
		ratio = img.viewBox[2] / img.viewBox[3]
	}
	switch {
	case hasWidth && hasHeight:
	case hasWidth && ratio > 0:
		height = width / ratio
	case hasHeight && ratio > 0:
		width = height * ratio
	case hasWidth:
		height = defaultSVGHeight
	case hasHeight:
		width = defaultSVGWidth
	case ratio > 0:
		width, height = defaultSVGWidth, defaultSVGWidth/ratio
	default:
		width, height = defaultSVGWidth, defaultSVGHeight
	}
	if !(width >= 0.5 && height >= 0.5 && width <= maxSVGRasterSide && height <= maxSVGRasterSide) {
		return errUnsupportedSVG
	}
	img.width, img.height = width, height
	basisWidth, basisHeight := width, height
	img.rootFontSize = 16
	if n, ok := svgFontSize(attrs["font-size"], 16, 16); ok {
		img.rootFontSize = n
	}
	if img.hasViewBox {
		basisWidth, basisHeight = img.viewBox[2], img.viewBox[3]
	}
	img.userWidth, img.userHeight = basisWidth, basisHeight
	img.dashBasis = math.Hypot(basisWidth/math.Sqrt2, basisHeight/math.Sqrt2)
	return nil
}

// svgLength parses an absolute SVG length in CSS px (or unitless user units).
// Percentages and font-relative units need a viewport/inheritance context and
// are resolved by svgLengthBasis instead.
func svgLength(s string) (float64, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, false
	}
	i := 0
	for i < len(s) && (s[i] == '+' || s[i] == '-' || s[i] == '.' ||
		(s[i] >= '0' && s[i] <= '9') || s[i] == 'e' || s[i] == 'E') {
		i++
	}
	n, err := strconv.ParseFloat(s[:i], 64)
	if err != nil || math.IsNaN(n) || math.IsInf(n, 0) || n < 0 {
		return 0, false
	}
	factor, ok := svgAbsoluteUnit(strings.ToLower(strings.TrimSpace(s[i:])))
	if !ok {
		return 0, false
	}
	n *= factor
	if math.IsNaN(n) || math.IsInf(n, 0) || n > maxSVGGeometry {
		return 0, false
	}
	return n, true
}

// svgCoordinate is like svgLength but allows negative values.
func svgCoordinate(s string) (float64, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, false
	}
	negative := strings.HasPrefix(s, "-")
	if negative || strings.HasPrefix(s, "+") {
		s = s[1:]
	}
	n, ok := svgLength(s)
	if negative {
		n = -n
	}
	return n, ok
}

func svgAbsoluteUnit(unit string) (float64, bool) {
	switch unit {
	case "", "px":
		return 1, true
	case "in":
		return 96, true
	case "cm":
		return 96 / 2.54, true
	case "mm":
		return 96 / 25.4, true
	case "q":
		return 96 / 101.6, true
	case "pt":
		return 96.0 / 72.0, true
	case "pc":
		return 16, true
	default:
		return 0, false
	}
}

type svgAxis uint8

const (
	svgHorizontal svgAxis = iota
	svgVertical
	svgDiagonal
)

type svgLengthBasis struct {
	horizontal, vertical, diagonal float64
	fontSize, rootFontSize         float64
}

func (b svgLengthBasis) length(value string, axis svgAxis) (float64, bool) {
	value = strings.TrimSpace(value)
	if strings.HasSuffix(value, "%") {
		n, err := strconv.ParseFloat(strings.TrimSpace(strings.TrimSuffix(value, "%")), 64)
		if err != nil || math.IsNaN(n) || math.IsInf(n, 0) || n < 0 {
			return 0, false
		}
		base := b.horizontal
		switch axis {
		case svgVertical:
			base = b.vertical
		case svgDiagonal:
			base = b.diagonal
		}
		n = n * base / 100
		return n, n <= maxSVGGeometry && !math.IsInf(n, 0) && !math.IsNaN(n)
	}
	if strings.HasSuffix(strings.ToLower(value), "rem") {
		n, err := strconv.ParseFloat(strings.TrimSpace(value[:len(value)-3]), 64)
		if err != nil || math.IsNaN(n) || math.IsInf(n, 0) || n < 0 {
			return 0, false
		}
		n *= b.rootFontSize
		return n, n <= maxSVGGeometry && !math.IsInf(n, 0) && !math.IsNaN(n)
	}
	if strings.HasSuffix(strings.ToLower(value), "em") {
		n, err := strconv.ParseFloat(strings.TrimSpace(value[:len(value)-2]), 64)
		if err != nil || math.IsNaN(n) || math.IsInf(n, 0) || n < 0 {
			return 0, false
		}
		n *= b.fontSize
		return n, n <= maxSVGGeometry && !math.IsInf(n, 0) && !math.IsNaN(n)
	}
	return svgLength(value)
}

func (b svgLengthBasis) coordinate(value string, axis svgAxis) (float64, bool) {
	value = strings.TrimSpace(value)
	negative := strings.HasPrefix(value, "-")
	if negative || strings.HasPrefix(value, "+") {
		value = strings.TrimSpace(value[1:])
	}
	n, ok := b.length(value, axis)
	if negative {
		n = -n
	}
	return n, ok
}

func svgFontSize(value string, parentSize, rootSize float64) (float64, bool) {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0, false
	}
	if strings.HasSuffix(value, "%") {
		n, err := strconv.ParseFloat(strings.TrimSpace(strings.TrimSuffix(value, "%")), 64)
		if err != nil || math.IsNaN(n) || math.IsInf(n, 0) || n < 0 {
			return 0, false
		}
		n = parentSize * n / 100
		return n, n <= maxSVGGeometry && !math.IsInf(n, 0)
	}
	if strings.HasSuffix(strings.ToLower(value), "rem") {
		n, err := strconv.ParseFloat(strings.TrimSpace(value[:len(value)-3]), 64)
		if err != nil || math.IsNaN(n) || math.IsInf(n, 0) || n < 0 {
			return 0, false
		}
		n *= rootSize
		return n, n <= maxSVGGeometry && !math.IsInf(n, 0)
	}
	if strings.HasSuffix(strings.ToLower(value), "em") {
		n, err := strconv.ParseFloat(strings.TrimSpace(value[:len(value)-2]), 64)
		if err != nil || math.IsNaN(n) || math.IsInf(n, 0) || n < 0 {
			return 0, false
		}
		n *= parentSize
		return n, n <= maxSVGGeometry && !math.IsInf(n, 0)
	}
	n, ok := svgLength(value)
	return n, ok
}

func svgPaint(value string, inherited color.NRGBA, inheritedOK bool) (color.NRGBA, bool) {
	value = strings.ToLower(strings.TrimSpace(value))
	switch value {
	case "none":
		return color.NRGBA{}, false
	case "inherit":
		return inherited, inheritedOK
	case "currentcolor":
		return color.NRGBA{A: 255}, true
	}
	c, ok := parseColor(value)
	if !ok {
		// Unsupported paints (for example url(#gradient)) fall back to the
		// inherited fill rather than failing the whole document.
		return inherited, inheritedOK
	}
	// parseColor returns non-premultiplied channels in an RGBA struct.
	return color.NRGBA{R: c.R, G: c.G, B: c.B, A: c.A}, c.A > 0
}

// viewTransform maps the viewBox to a w×h pixel viewport following
// preserveAspectRatio.
func (img *svgImage) viewTransform(w, h int) svgAffine {
	if !img.hasViewBox {
		return svgAffine{a: float64(w) / img.width, d: float64(h) / img.height}
	}
	vx, vy, vw, vh := img.viewBox[0], img.viewBox[1], img.viewBox[2], img.viewBox[3]
	sx, sy := float64(w)/vw, float64(h)/vh
	if img.align == "none" {
		return svgAffine{a: sx, d: sy, e: -vx * sx, f: -vy * sy}
	}
	scale := math.Min(sx, sy)
	if img.slice {
		scale = math.Max(sx, sy)
	}
	tx, ty := -vx*scale, -vy*scale
	extraX, extraY := float64(w)-vw*scale, float64(h)-vh*scale
	switch {
	case strings.HasPrefix(img.align, "xMid"):
		tx += extraX / 2
	case strings.HasPrefix(img.align, "xMax"):
		tx += extraX
	}
	switch {
	case strings.HasSuffix(img.align, "YMid"):
		ty += extraY / 2
	case strings.HasSuffix(img.align, "YMax"):
		ty += extraY
	}
	return svgAffine{a: scale, d: scale, e: tx, f: ty}
}

// rasterize renders the document into a new w×h image, or returns nil when
// the size is outside the resource limits.
func (img *svgImage) rasterize(w, h int) *image.RGBA {
	if w <= 0 || h <= 0 || w > maxSVGRasterSide || h > maxSVGRasterSide ||
		int64(w)*int64(h) > maxDecodedImagePixels {
		return nil
	}
	dst := image.NewRGBA(image.Rect(0, 0, w, h))
	view := img.viewTransform(w, h)
	r := vector.NewRasterizer(w, h)
	for _, shape := range img.shapes {
		m := shape.transform.then(view)
		pt := func(p [2]float64) (float32, float32) {
			x, y := m.apply(p[0], p[1])
			return float32(x), float32(y)
		}
		if shape.fill.A != 0 {
			var paths []svgSubpath
			useParityRasterizer := false
			if shape.fillRule == "evenodd" {
				paths = flattenSVGShape(shape, m)
				useParityRasterizer = !svgFillRulesEquivalent(paths)
			}
			if useParityRasterizer {
				if mask := svgEvenOddMask(paths, w, h); mask != nil {
					draw.DrawMask(dst, dst.Bounds(), image.NewUniform(shape.fill), image.Point{}, mask, image.Point{}, draw.Over)
				}
			} else {
				r.Reset(w, h)
				drawn := false
				for _, seg := range shape.segments {
					switch seg.op {
					case 'M':
						x, y := pt(seg.pts[0])
						if drawn {
							r.ClosePath()
						}
						r.MoveTo(x, y)
					case 'L':
						x, y := pt(seg.pts[0])
						r.LineTo(x, y)
						drawn = true
					case 'Q':
						x1, y1 := pt(seg.pts[0])
						x, y := pt(seg.pts[1])
						r.QuadTo(x1, y1, x, y)
						drawn = true
					case 'C':
						x1, y1 := pt(seg.pts[0])
						x2, y2 := pt(seg.pts[1])
						x, y := pt(seg.pts[2])
						r.CubeTo(x1, y1, x2, y2, x, y)
						drawn = true
					case 'Z':
						r.ClosePath()
					}
				}
				if drawn {
					r.ClosePath()
					r.Draw(dst, dst.Bounds(), image.NewUniform(shape.fill), image.Point{})
				}
			}
		}
		if shape.stroke.A != 0 && shape.width > 0 {
			r.Reset(w, h)
			strokeSVGPath(r, shape, m)
			r.Draw(dst, dst.Bounds(), image.NewUniform(shape.stroke), image.Point{})
		}
	}
	return dst
}

// svgNumbers scans SVG number syntax, including forms such as "1.5.5" (two
// numbers), "10-5" and exponents, with optional comma/space separators.
type svgNumbers struct {
	s string
	i int
}

func (p *svgNumbers) skipSeparators() {
	for p.i < len(p.s) && (p.s[p.i] == ' ' || p.s[p.i] == ',' || p.s[p.i] == '\t' ||
		p.s[p.i] == '\n' || p.s[p.i] == '\r' || p.s[p.i] == '\f') {
		p.i++
	}
}

func (p *svgNumbers) number() (float64, bool) {
	p.skipSeparators()
	start := p.i
	if p.i < len(p.s) && (p.s[p.i] == '+' || p.s[p.i] == '-') {
		p.i++
	}
	digits, dot := 0, false
	for p.i < len(p.s) {
		c := p.s[p.i]
		if c >= '0' && c <= '9' {
			digits++
		} else if c == '.' && !dot {
			dot = true
		} else {
			break
		}
		p.i++
	}
	if digits == 0 {
		p.i = start
		return 0, false
	}
	if p.i < len(p.s) && (p.s[p.i] == 'e' || p.s[p.i] == 'E') {
		j := p.i + 1
		if j < len(p.s) && (p.s[j] == '+' || p.s[j] == '-') {
			j++
		}
		k := j
		for k < len(p.s) && p.s[k] >= '0' && p.s[k] <= '9' {
			k++
		}
		if k > j {
			p.i = k
		}
	}
	n, err := strconv.ParseFloat(p.s[start:p.i], 64)
	if err != nil || math.IsNaN(n) || math.IsInf(n, 0) {
		p.i = start
		return 0, false
	}
	return n, true
}

// Arc flags are single characters, not numbers: "011" means flags 0,1
// followed by coordinate 1. Signs, fractions and other digits are not flags.
func (p *svgNumbers) flag() (float64, bool) {
	p.skipSeparators()
	if p.i >= len(p.s) || (p.s[p.i] != '0' && p.s[p.i] != '1') {
		return 0, false
	}
	n := p.s[p.i] - '0'
	p.i++
	return float64(n), true
}

func svgNumberList(s string) ([]float64, bool) {
	p := &svgNumbers{s: s}
	var result []float64
	for {
		p.skipSeparators()
		if p.i >= len(p.s) {
			return result, true
		}
		n, ok := p.number()
		if !ok {
			return nil, false
		}
		result = append(result, n)
	}
}

// parseSVGPath converts path data into absolute M/L/Q/C/Z segments. Following
// SVG error handling, it renders everything up to the first error.
func parseSVGPath(d string, limit int) []svgSegment {
	p := &svgNumbers{s: d}
	var out []svgSegment
	var cx, cy, sx, sy float64       // current point and subpath start
	var lastCtrlX, lastCtrlY float64 // reflected control point for S/T
	var lastOp byte                  // previous command letter (upper case)
	var cmd byte                     // current command letter (as written)
	started := false
	emit := func(seg svgSegment) bool {
		if len(out) >= limit {
			return false
		}
		out = append(out, seg)
		return true
	}
	for {
		p.skipSeparators()
		if p.i >= len(p.s) {
			break
		}
		c := p.s[p.i]
		if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') {
			cmd = c
			p.i++
		} else if cmd == 0 {
			break
		}
		upper := cmd &^ 0x20
		relative := cmd != upper
		if !started && upper != 'M' {
			break
		}
		if upper == 'Z' {
			if !emit(svgSegment{op: 'Z'}) {
				break
			}
			cx, cy = sx, sy
			lastOp = 'Z'
			// Z takes no arguments; a following number is an error unless a
			// new command letter appears.
			p.skipSeparators()
			if p.i < len(p.s) {
				n := p.s[p.i]
				if !((n >= 'a' && n <= 'z') || (n >= 'A' && n <= 'Z')) {
					break
				}
			}
			continue
		}
		var args [7]float64
		count := map[byte]int{'M': 2, 'L': 2, 'H': 1, 'V': 1, 'C': 6, 'S': 4, 'Q': 4, 'T': 2, 'A': 7}[upper]
		if count == 0 {
			break // unknown command
		}
		ok := true
		for i := 0; i < count && ok; i++ {
			if upper == 'A' && (i == 3 || i == 4) {
				args[i], ok = p.flag()
			} else {
				args[i], ok = p.number()
			}
		}
		if !ok {
			break
		}
		ox, oy := 0.0, 0.0
		if relative {
			ox, oy = cx, cy
		}
		var seg svgSegment
		switch upper {
		case 'A':
			ex, ey := args[5]+ox, args[6]+oy
			arcs, valid := svgArc(cx, cy, args[0], args[1], args[2], args[3] == 1, args[4] == 1, ex, ey)
			if !valid {
				return out
			}
			for _, arc := range arcs {
				if !emit(arc) {
					return out
				}
			}
			cx, cy = ex, ey
			// An arc is not a C/S command, even though it emits cubics.
			// Also reset reflection when identical endpoints omit the arc.
			lastOp = upper
			continue
		case 'M':
			cx, cy = args[0]+ox, args[1]+oy
			sx, sy = cx, cy
			seg = svgSegment{op: 'M', pts: [3][2]float64{{cx, cy}}}
			started = true
			// Subsequent coordinate pairs are implicit lineto commands.
			if relative {
				cmd = 'l'
			} else {
				cmd = 'L'
			}
		case 'L':
			cx, cy = args[0]+ox, args[1]+oy
			seg = svgSegment{op: 'L', pts: [3][2]float64{{cx, cy}}}
		case 'H':
			cx = args[0] + ox
			seg = svgSegment{op: 'L', pts: [3][2]float64{{cx, cy}}}
		case 'V':
			cy = args[0] + oy
			seg = svgSegment{op: 'L', pts: [3][2]float64{{cx, cy}}}
		case 'C', 'S':
			x1, y1 := cx, cy
			rest := args[:]
			if upper == 'C' {
				x1, y1 = args[0]+ox, args[1]+oy
				rest = args[2:]
			} else if lastOp == 'C' || lastOp == 'S' {
				x1, y1 = 2*cx-lastCtrlX, 2*cy-lastCtrlY
			}
			x2, y2 := rest[0]+ox, rest[1]+oy
			cx, cy = rest[2]+ox, rest[3]+oy
			lastCtrlX, lastCtrlY = x2, y2
			seg = svgSegment{op: 'C', pts: [3][2]float64{{x1, y1}, {x2, y2}, {cx, cy}}}
		case 'Q', 'T':
			x1, y1 := cx, cy
			end := args[:2]
			if upper == 'Q' {
				x1, y1 = args[0]+ox, args[1]+oy
				end = args[2:4]
			} else if lastOp == 'Q' || lastOp == 'T' {
				x1, y1 = 2*cx-lastCtrlX, 2*cy-lastCtrlY
			}
			cx, cy = end[0]+ox, end[1]+oy
			lastCtrlX, lastCtrlY = x1, y1
			seg = svgSegment{op: 'Q', pts: [3][2]float64{{x1, y1}, {cx, cy}}}
		}
		lastOp = upper
		if !emit(seg) {
			break
		}
	}
	return out
}

// svgArc implements SVG's endpoint-to-center parameterization, then splits the
// sweep into at most four cubic Béziers, each spanning no more than 90 degrees.
// See https://www.w3.org/TR/SVG/implnote.html#ArcImplementationNotes.
// Non-finite geometry is a parse error rather than input to the rasterizer.
func svgArc(x, y, rx, ry, rotation float64, large, sweep bool, ex, ey float64) ([]svgSegment, bool) {
	finite := func(v float64) bool { return !math.IsNaN(v) && !math.IsInf(v, 0) }
	if !finite(ex) || !finite(ey) {
		return nil, false
	}
	if x == ex && y == ey {
		return nil, true
	}
	rx, ry = math.Abs(rx), math.Abs(ry)
	if rx == 0 || ry == 0 {
		return []svgSegment{{op: 'L', pts: [3][2]float64{{ex, ey}}}}, true
	}
	sin, cos := math.Sincos(math.Mod(rotation, 360) * math.Pi / 180)
	dx, dy := x/2-ex/2, y/2-ey/2
	px, py := cos*dx+sin*dy, -sin*dx+cos*dy
	u, v := px/rx, py/ry
	h := math.Hypot(u, v)
	if !finite(h) || h == 0 {
		return nil, false
	}
	// Enlarge insufficient radii. Hypot and normalized coordinates avoid
	// squaring the original radii/coordinates (which can overflow).
	if h > 1 {
		rx, ry = rx*h, ry*h
		u, v = u/h, v/h
		h = 1
	}
	sign := 1.0
	if large == sweep {
		sign = -1
	}
	factor := sign * math.Sqrt(math.Max(0, 1-h*h))
	// Center in the rotated frame, normalized by the corrected radii.
	cu, cv := factor*(v/h), -factor*(u/h)
	cpx, cpy := rx*cu, ry*cv
	cx, cy := cos*cpx-sin*cpy+x/2+ex/2, sin*cpx+cos*cpy+y/2+ey/2
	start := math.Atan2(v-cv, u-cu)
	end := math.Atan2(-v-cv, -u-cu)
	delta := end - start
	if sweep && delta < 0 {
		delta += 2 * math.Pi
	} else if !sweep && delta > 0 {
		delta -= 2 * math.Pi
	}
	// Atan2/Hypot roundoff can put an exact quarter turn just above its
	// boundary on some architectures. Snap only the segment count (not the
	// angle or endpoints) so these arcs do not acquire an extra cubic.
	quarters := math.Abs(delta) / (math.Pi / 2)
	if nearest := math.Round(quarters); nearest >= 1 && math.Abs(quarters-nearest) < 1e-12 {
		quarters = nearest
	}
	n := int(math.Ceil(quarters))
	if n < 1 || n > 4 {
		return nil, false
	}
	step := delta / float64(n)
	alpha := 4.0 / 3 * math.Tan(step/4)
	point := func(angle float64) (p, tangent [2]float64) {
		s, c := math.Sincos(angle)
		p = [2]float64{cx + cos*rx*c - sin*ry*s, cy + sin*rx*c + cos*ry*s}
		tangent = [2]float64{-cos*rx*s - sin*ry*c, -sin*rx*s + cos*ry*c}
		return
	}
	segments := make([]svgSegment, 0, n)
	for i := 0; i < n; i++ {
		p0, t0 := point(start + float64(i)*step)
		p1, t1 := point(start + float64(i+1)*step)
		seg := svgSegment{op: 'C', pts: [3][2]float64{
			{p0[0] + alpha*t0[0], p0[1] + alpha*t0[1]},
			{p1[0] - alpha*t1[0], p1[1] - alpha*t1[1]},
			p1,
		}}
		if i == n-1 {
			seg.pts[2] = [2]float64{ex, ey} // preserve the exact requested endpoint
		}
		for _, p := range seg.pts {
			if !finite(p[0]) || !finite(p[1]) {
				return nil, false
			}
		}
		segments = append(segments, seg)
	}
	return segments, true
}

// svgRect converts a <rect>, including rx/ry rounded corners, into segments.
func svgRect(attrs map[string]string) []svgSegment {
	return svgRectWithBasis(attrs, svgLengthBasis{})
}

func svgRectWithLengths(attrs map[string]string, img *svgImage, fontSize float64) []svgSegment {
	return svgRectWithBasis(attrs, svgLengthBasis{
		horizontal: img.userWidth, vertical: img.userHeight, diagonal: img.dashBasis,
		fontSize: fontSize, rootFontSize: img.rootFontSize,
	})
}

func svgRectWithBasis(attrs map[string]string, basis svgLengthBasis) []svgSegment {
	x, _ := basis.coordinate(attrs["x"], svgHorizontal)
	y, _ := basis.coordinate(attrs["y"], svgVertical)
	w, okW := basis.length(attrs["width"], svgHorizontal)
	h, okH := basis.length(attrs["height"], svgVertical)
	if !okW || !okH || w <= 0 || h <= 0 {
		return nil
	}
	rx, okRX := basis.length(attrs["rx"], svgHorizontal)
	ry, okRY := basis.length(attrs["ry"], svgVertical)
	switch {
	case okRX && !okRY:
		ry = rx
	case okRY && !okRX:
		rx = ry
	}
	rx, ry = math.Min(rx, w/2), math.Min(ry, h/2)
	pt := func(px, py float64) [2]float64 { return [2]float64{px, py} }
	if rx <= 0 || ry <= 0 {
		return []svgSegment{
			{op: 'M', pts: [3][2]float64{pt(x, y)}},
			{op: 'L', pts: [3][2]float64{pt(x+w, y)}},
			{op: 'L', pts: [3][2]float64{pt(x+w, y+h)}},
			{op: 'L', pts: [3][2]float64{pt(x, y+h)}},
			{op: 'Z'},
		}
	}
	kx, ky := rx*svgCircleConstant, ry*svgCircleConstant
	return []svgSegment{
		{op: 'M', pts: [3][2]float64{pt(x+rx, y)}},
		{op: 'L', pts: [3][2]float64{pt(x+w-rx, y)}},
		{op: 'C', pts: [3][2]float64{pt(x+w-rx+kx, y), pt(x+w, y+ry-ky), pt(x+w, y+ry)}},
		{op: 'L', pts: [3][2]float64{pt(x+w, y+h-ry)}},
		{op: 'C', pts: [3][2]float64{pt(x+w, y+h-ry+ky), pt(x+w-rx+kx, y+h), pt(x+w-rx, y+h)}},
		{op: 'L', pts: [3][2]float64{pt(x+rx, y+h)}},
		{op: 'C', pts: [3][2]float64{pt(x+rx-kx, y+h), pt(x, y+h-ry+ky), pt(x, y+h-ry)}},
		{op: 'L', pts: [3][2]float64{pt(x, y+ry)}},
		{op: 'C', pts: [3][2]float64{pt(x, y+ry-ky), pt(x+rx-kx, y), pt(x+rx, y)}},
		{op: 'Z'},
	}
}

var svgRenderedElements = map[string]bool{
	"g": true, "path": true, "rect": true, "circle": true, "ellipse": true,
	"line": true, "polyline": true, "polygon": true,
}

// svgEllipseAttrs resolves ellipse radii. A missing or "auto" radius takes
// the other one (SVG 2); a negative or unparseable radius disables rendering.
func svgEllipseAttrs(attrs map[string]string) []svgSegment {
	return svgEllipseAttrsWithBasis(attrs, svgLengthBasis{})
}

func svgEllipseAttrsWithLengths(attrs map[string]string, img *svgImage, fontSize float64) []svgSegment {
	return svgEllipseAttrsWithBasis(attrs, svgLengthBasis{
		horizontal: img.userWidth, vertical: img.userHeight, diagonal: img.dashBasis,
		fontSize: fontSize, rootFontSize: img.rootFontSize,
	})
}

func svgEllipseAttrsWithBasis(attrs map[string]string, basis svgLengthBasis) []svgSegment {
	radius := func(name string) (r float64, auto, ok bool) {
		value, present := attrs[name]
		if !present || strings.TrimSpace(value) == "auto" {
			return 0, true, true
		}
		axis := svgHorizontal
		if name == "ry" {
			axis = svgVertical
		}
		r, ok = basis.length(value, axis)
		return r, false, ok
	}
	rx, autoX, okX := radius("rx")
	ry, autoY, okY := radius("ry")
	if !okX || !okY || (autoX && autoY) {
		return nil
	}
	if autoX {
		rx = ry
	}
	if autoY {
		ry = rx
	}
	return svgEllipseWithLengths(attrs, rx, ry, basis)
}

// svgEllipse outlines an ellipse centered at (cx, cy) with four cubics,
// starting at the rightmost point and proceeding clockwise (positive angle
// direction in SVG's y-down space). Zero radii disable rendering.
func svgEllipse(attrs map[string]string, rx, ry float64) []svgSegment {
	return svgEllipseWithLengths(attrs, rx, ry, svgLengthBasis{})
}

func svgEllipseWithLengths(attrs map[string]string, rx, ry float64, basis svgLengthBasis) []svgSegment {
	if !(rx > 0 && ry > 0) || math.IsInf(rx, 0) || math.IsInf(ry, 0) {
		return nil
	}
	cx, _ := basis.coordinate(attrs["cx"], svgHorizontal)
	cy, _ := basis.coordinate(attrs["cy"], svgVertical)
	kx, ky := rx*svgCircleConstant, ry*svgCircleConstant
	pt := func(px, py float64) [2]float64 { return [2]float64{px, py} }
	return []svgSegment{
		{op: 'M', pts: [3][2]float64{pt(cx+rx, cy)}},
		{op: 'C', pts: [3][2]float64{pt(cx+rx, cy+ky), pt(cx+kx, cy+ry), pt(cx, cy+ry)}},
		{op: 'C', pts: [3][2]float64{pt(cx-kx, cy+ry), pt(cx-rx, cy+ky), pt(cx-rx, cy)}},
		{op: 'C', pts: [3][2]float64{pt(cx-rx, cy-ky), pt(cx-kx, cy-ry), pt(cx, cy-ry)}},
		{op: 'C', pts: [3][2]float64{pt(cx+kx, cy-ry), pt(cx+rx, cy-ky), pt(cx+rx, cy)}},
		{op: 'Z'},
	}
}

// svgLine is an open two-point subpath; its fill has no area, so only a
// stroke is visible.
func svgLine(attrs map[string]string) []svgSegment {
	return svgLineWithBasis(attrs, svgLengthBasis{})
}

func svgLineWithLengths(attrs map[string]string, img *svgImage, fontSize float64) []svgSegment {
	return svgLineWithBasis(attrs, svgLengthBasis{
		horizontal: img.userWidth, vertical: img.userHeight, diagonal: img.dashBasis,
		fontSize: fontSize, rootFontSize: img.rootFontSize,
	})
}

func svgLineWithBasis(attrs map[string]string, basis svgLengthBasis) []svgSegment {
	var p [4]float64
	for i, name := range []string{"x1", "y1", "x2", "y2"} {
		if value, ok := attrs[name]; ok {
			axis := svgHorizontal
			if i%2 == 1 {
				axis = svgVertical
			}
			n, valid := basis.coordinate(value, axis)
			if !valid {
				return nil
			}
			p[i] = n
		}
	}
	return []svgSegment{
		{op: 'M', pts: [3][2]float64{{p[0], p[1]}}},
		{op: 'L', pts: [3][2]float64{{p[2], p[3]}}},
	}
}

// svgPolyline converts a points list into M/L segments, closing it for
// <polygon>. Following SVG error handling, points up to the first parse error
// render, and an odd trailing coordinate is dropped. At most limit segments
// are returned (including the closing Z), so callers can enforce the global
// segment budget.
func svgPolyline(points string, closed bool, limit int) []svgSegment {
	p := &svgNumbers{s: points}
	var out []svgSegment
	for len(out) < limit {
		p.skipSeparators()
		if p.i >= len(p.s) {
			break
		}
		x, ok := p.number()
		if !ok {
			break
		}
		p.skipSeparators()
		y, ok := p.number()
		if !ok {
			break
		}
		op := byte('L')
		if len(out) == 0 {
			op = 'M'
		}
		out = append(out, svgSegment{op: op, pts: [3][2]float64{{x, y}}})
	}
	if len(out) < 2 {
		// A single point has nothing to fill or stroke.
		return nil
	}
	if closed && len(out) < limit {
		out = append(out, svgSegment{op: 'Z'})
	}
	return out
}

// parseSVGTransform parses a transform list such as
// "translate(10 5) scale(2) rotate(45 0 0) matrix(...)".
func parseSVGTransform(s string) (svgAffine, bool) {
	result := svgIdentity
	s = strings.TrimSpace(s)
	for s != "" {
		open := strings.IndexByte(s, '(')
		closeParen := strings.IndexByte(s, ')')
		if open <= 0 || closeParen < open {
			return svgIdentity, false
		}
		name := strings.TrimSpace(s[:open])
		args, ok := svgNumberList(s[open+1 : closeParen])
		if !ok {
			return svgIdentity, false
		}
		var t svgAffine
		switch {
		case name == "matrix" && len(args) == 6:
			t = svgAffine{args[0], args[1], args[2], args[3], args[4], args[5]}
		case name == "translate" && (len(args) == 1 || len(args) == 2):
			t = svgAffine{a: 1, d: 1, e: args[0]}
			if len(args) == 2 {
				t.f = args[1]
			}
		case name == "scale" && (len(args) == 1 || len(args) == 2):
			t = svgAffine{a: args[0], d: args[0]}
			if len(args) == 2 {
				t.d = args[1]
			}
		case name == "rotate" && (len(args) == 1 || len(args) == 3):
			rad := args[0] * math.Pi / 180
			cos, sin := math.Cos(rad), math.Sin(rad)
			t = svgAffine{a: cos, b: sin, c: -sin, d: cos}
			if len(args) == 3 {
				cx, cy := args[1], args[2]
				t = svgAffine{a: 1, d: 1, e: -cx, f: -cy}.then(t).then(svgAffine{a: 1, d: 1, e: cx, f: cy})
			}
		case name == "skewX" && len(args) == 1:
			t = svgAffine{a: 1, d: 1, c: math.Tan(args[0] * math.Pi / 180)}
		case name == "skewY" && len(args) == 1:
			t = svgAffine{a: 1, d: 1, b: math.Tan(args[0] * math.Pi / 180)}
		default:
			return svgIdentity, false
		}
		// In "A B", B applies to coordinates first: result = B then A.
		result = t.then(result)
		s = strings.TrimLeft(s[closeParen+1:], " \t\r\n,")
	}
	return result, true
}
