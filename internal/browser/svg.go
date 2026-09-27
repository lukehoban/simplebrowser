package browser

import (
	"bytes"
	"encoding/xml"
	"errors"
	"image"
	"image/color"
	"io"
	"math"
	"strconv"
	"strings"

	"golang.org/x/image/vector"
)

// A minimal SVG subset, enough for simple icons such as the Hacker News logo
// and vote arrow: <svg> sizing/viewBox/preserveAspectRatio, <g>, <path>, and
// the basic shapes (<rect>, <circle>, <ellipse>, <line>, <polyline>,
// <polygon>) with solid fills, strokes and transforms. Other elements are
// skipped.
// A document that cannot be parsed returns an error so
// callers keep their existing placeholder or empty-background behavior.

const (
	maxSVGBytes       = 1 << 20
	maxSVGElements    = 10000
	maxSVGPathSegs    = 200000
	maxSVGRasterSide  = 4096
	svgNamespace      = "http://www.w3.org/2000/svg"
	defaultSVGWidth   = 300
	defaultSVGHeight  = 150
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
	stroke     color.NRGBA
	width      float64
	cap, join  string
	miterLimit float64
}

// svgImage is an image.Image rasterized at its intrinsic size. Painters that
// know the used size call rasterize to render crisply at that size instead.
type svgImage struct {
	width, height float64
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

func decodeSVG(data []byte) (*svgImage, error) {
	if len(data) > maxSVGBytes {
		return nil, errUnsupportedSVG
	}
	decoder := xml.NewDecoder(bytes.NewReader(data))
	decoder.Strict = true
	img := &svgImage{align: "xMidYMid"}
	type frame struct {
		fill          color.NRGBA
		hasFill       bool
		opacity       float64
		stroke        color.NRGBA
		hasStroke     bool
		strokeOpacity float64
		width         float64
		cap, join     string
		miterLimit    float64
		transform     svgAffine
		skip          bool
	}
	stack := []frame{{fill: color.NRGBA{A: 255}, hasFill: true, opacity: 1, strokeOpacity: 1, width: 1, cap: "butt", join: "miter", miterLimit: 4, transform: svgIdentity}}
	elements, segments := 0, 0
	sawRoot := false
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
			parent := stack[len(stack)-1]
			attrs := svgAttributes(t)
			if !sawRoot {
				if t.Name.Local != "svg" || (t.Name.Space != svgNamespace && t.Name.Space != "") {
					return nil, errUnsupportedSVG
				}
				sawRoot = true
				if err := img.parseRoot(attrs); err != nil {
					return nil, err
				}
				// The root's own transform attribute (SVG 2) is not applied.
				delete(attrs, "transform")
			}
			current := parent
			if t.Name.Space != svgNamespace && t.Name.Space != "" {
				current.skip = true
			}
			switch {
			case elements == 1, svgRenderedElements[t.Name.Local]:
			default:
				// Nested <svg>, <defs>, <use>, <title>, <text>, ... are
				// not rendered by this subset; skip the whole subtree.
				current.skip = true
			}
			if !current.skip {
				if value, ok := attrs["fill"]; ok {
					current.fill, current.hasFill = svgPaint(value, parent.fill, parent.hasFill)
				}
				if value, ok := attrs["fill-opacity"]; ok {
					if n, valid := svgUnitInterval(value); valid {
						// fill-opacity is inherited, not accumulated through
						// ancestors: an explicit child value replaces the
						// inherited value. Group opacity is separate compositing
						// behavior and is not implemented by this renderer.
						current.opacity = n
					}
				}
				if value, ok := attrs["stroke"]; ok {
					current.stroke, current.hasStroke = svgPaint(value, parent.stroke, parent.hasStroke)
				}
				if value, ok := attrs["stroke-opacity"]; ok {
					if n, valid := svgUnitInterval(value); valid {
						current.strokeOpacity = n
					}
				}
				if value, ok := attrs["stroke-width"]; ok {
					if n, valid := svgLength(value); valid && n <= maxSVGStrokeWidth {
						current.width = n
					}
				}
				if value := attrs["stroke-linecap"]; value == "butt" || value == "square" || value == "round" {
					current.cap = value
				}
				if value := attrs["stroke-linejoin"]; value == "miter" || value == "bevel" || value == "round" {
					current.join = value
				}
				if value, ok := attrs["stroke-miterlimit"]; ok {
					if n, err := strconv.ParseFloat(strings.TrimSpace(value), 64); err == nil && n >= 1 && n <= maxSVGStrokeWidth {
						current.miterLimit = n
					}
				}
				if value, ok := attrs["transform"]; ok {
					transform, ok := parseSVGTransform(value)
					if !ok {
						// Per SVG, an invalid transform disables rendering of the element.
						current.skip = true
					} else {
						current.transform = transform.then(parent.transform)
					}
				}
			}
			if !current.skip && (current.hasFill || current.hasStroke) {
				var shape []svgSegment
				switch t.Name.Local {
				case "path":
					shape = parseSVGPath(attrs["d"], maxSVGPathSegs-segments)
				case "rect":
					shape = svgRect(attrs)
				case "circle":
					r, ok := svgLength(attrs["r"])
					if ok {
						shape = svgEllipse(attrs, r, r)
					}
				case "ellipse":
					shape = svgEllipseAttrs(attrs)
				case "line":
					shape = svgLine(attrs)
				case "polyline", "polygon":
					shape = svgPolyline(attrs["points"], t.Name.Local == "polygon", maxSVGPathSegs-segments)
				}
				segments += len(shape)
				if segments >= maxSVGPathSegs {
					return nil, errUnsupportedSVG
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
					img.shapes = append(img.shapes, svgShape{segments: shape, transform: current.transform, fill: fill, stroke: stroke, width: current.width, cap: current.cap, join: current.join, miterLimit: current.miterLimit})
				}

			}
			stack = append(stack, current)
		case xml.EndElement:
			if len(stack) > 1 {
				stack = stack[:len(stack)-1]
			}
		}
	}
	if !sawRoot {
		return nil, errUnsupportedSVG
	}
	raster := img.rasterize(int(math.Round(img.width)), int(math.Round(img.height)))
	if raster == nil {
		return nil, errUnsupportedSVG
	}
	img.RGBA = raster
	return img, nil
}

const maxSVGStrokeWidth = 4096

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
	return nil
}

// svgLength parses an absolute length in px (or unitless). Percentages and
// other units are treated as missing.
func svgLength(s string) (float64, bool) {
	s = strings.TrimSpace(s)
	s = strings.TrimSuffix(s, "px")
	n, err := strconv.ParseFloat(s, 64)
	if err != nil || math.IsNaN(n) || math.IsInf(n, 0) || n < 0 {
		return 0, false
	}
	return n, true
}

// svgCoordinate is like svgLength but allows negative values.
func svgCoordinate(s string) (float64, bool) {
	s = strings.TrimSpace(s)
	if rest, ok := strings.CutPrefix(s, "-"); ok {
		n, ok := svgLength(rest)
		return -n, ok
	}
	return svgLength(s)
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
	x, _ := svgCoordinate(attrs["x"])
	y, _ := svgCoordinate(attrs["y"])
	w, okW := svgLength(attrs["width"])
	h, okH := svgLength(attrs["height"])
	if !okW || !okH || w <= 0 || h <= 0 {
		return nil
	}
	rx, okRX := svgLength(attrs["rx"])
	ry, okRY := svgLength(attrs["ry"])
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
	radius := func(name string) (r float64, auto, ok bool) {
		value, present := attrs[name]
		if !present || strings.TrimSpace(value) == "auto" {
			return 0, true, true
		}
		r, ok = svgLength(value)
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
	return svgEllipse(attrs, rx, ry)
}

// svgEllipse outlines an ellipse centered at (cx, cy) with four cubics,
// starting at the rightmost point and proceeding clockwise (positive angle
// direction in SVG's y-down space). Zero radii disable rendering.
func svgEllipse(attrs map[string]string, rx, ry float64) []svgSegment {
	if !(rx > 0 && ry > 0) || math.IsInf(rx, 0) || math.IsInf(ry, 0) {
		return nil
	}
	cx, _ := svgCoordinate(attrs["cx"])
	cy, _ := svgCoordinate(attrs["cy"])
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
	var p [4]float64
	for i, name := range []string{"x1", "y1", "x2", "y2"} {
		if value, ok := attrs[name]; ok {
			n, valid := svgCoordinate(value)
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
