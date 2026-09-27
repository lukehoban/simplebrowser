package browser

import (
	"image"
	"image/color"
	"math"
	"sort"
	"strconv"
	"strings"
	"sync"
)

// Local SVG paint servers and group opacity.
//
// fill/stroke may reference a same-document gradient or pattern with url(#id)
// and an optional fallback paint. Gradients
// support explicit <stop>s (offset, stop-color, stop-opacity), gradientUnits,
// gradientTransform, spreadMethod, and attribute/stop inheritance through a
// bounded href chain. Patterns support bounded userSpaceOnUse tiles, transforms,
// inherited content, and the same fallback/cycle behavior. Other paint servers
// paint the fallback (or nothing), like a missing ID.
//
// Element opacity is not inherited: it composites the element and its
// descendants as one group. The expansion emits push/pop markers around such
// groups and rasterize draws them into bounded offscreen layers.

const (
	maxSVGGradientChain  = 16
	maxSVGGradientStops  = 256
	maxSVGLayerDepth     = 8
	maxSVGLayerPixels    = 1 << 27 // total offscreen layer pixels per rasterize
	maxSVGGradientPixels = 1 << 27 // total gradient source pixels per rasterize
	maxSVGPatternPixels  = 1 << 24 // total cached pattern tile pixels per document
	// Scaled tiles are rasterized lazily when a pattern is painted under a
	// magnifying transform. Both the per-pattern cache size and its total
	// pixels are bounded; past either limit painting falls back to the
	// largest cached tile, which keeps output deterministic.
	maxSVGPatternScale        = 16 // largest device scale a tile is rasterized for
	maxSVGPatternTiles        = 4  // cached scaled tiles per pattern
	maxSVGPatternScaledPixels = 1 << 22
)

type svgStop struct {
	offset float64
	color  color.NRGBA
}

type svgGradient struct {
	radial    bool
	userSpace bool // gradientUnits="userSpaceOnUse"
	transform svgAffine
	spread    string // "pad", "reflect" or "repeat"
	stops     []svgStop
	// Linear: x1,y1 → x2,y2. Radial: centre (x1,y1), radius r, focus (fx,fy).
	x1, y1, x2, y2 float64
	r, fx, fy      float64
}

// svgPaintServer is a server bound to one shape: toLocal maps paint-server
// space into the shape's local coordinates (the server transform, then the
// bounding box for objectBoundingBox gradient units). Patterns currently
// support userSpaceOnUse tiles.
type svgPaintServer struct {
	gradient *svgGradient
	pattern  *svgPattern
	toLocal  svgAffine
}

type svgPattern struct {
	x, y, width, height float64
	transform           svgAffine
	tile                *image.RGBA
	// content is the expanded tile, retained so the tile can be re-rasterized
	// at the device resolution a shape is actually painted at. mu guards the
	// lazily filled cache.
	content      *svgImage
	mu           sync.Mutex
	scaled       map[[2]int]*image.RGBA
	scaledPixels int64
}

// tileFor returns the tile raster to sample for the pattern-local → device
// transform m. Tiles are rasterized at the transform's scale (quantized to
// halves and bounded), so magnified patterns stay smooth; the phase and
// period of the tiling are unaffected because sampling uses tile fractions.
func (p *svgPattern) tileFor(m svgAffine) *image.RGBA {
	if p.content == nil {
		return p.tile
	}
	// Column norms bound how far m stretches the tile's axes; under rotation
	// they still grow with the device scale.
	quant := func(length, scale float64) int {
		if !(scale > 1) || math.IsInf(scale, 0) || math.IsNaN(scale) {
			scale = 1
		}
		scale = math.Min(math.Ceil(scale*2)/2, maxSVGPatternScale)
		return int(math.Ceil(length * scale))
	}
	w := quant(p.width, math.Hypot(m.a, m.b))
	h := quant(p.height, math.Hypot(m.c, m.d))
	base := p.tile.Bounds()
	if w <= base.Dx() && h <= base.Dy() {
		return p.tile
	}
	if w > maxSVGRasterSide || h > maxSVGRasterSide || int64(w)*int64(h) > maxSVGPatternScaledPixels {
		return p.largestTile()
	}

	p.mu.Lock()
	defer p.mu.Unlock()
	key := [2]int{w, h}
	if tile, ok := p.scaled[key]; ok {
		if tile == nil {
			return p.largestTileLocked()
		}
		return tile
	}
	if len(p.scaled) >= maxSVGPatternTiles || p.scaledPixels+int64(w)*int64(h) > maxSVGPatternScaledPixels {
		return p.largestTileLocked()
	}
	tile := p.content.rasterize(w, h)
	if p.scaled == nil {
		p.scaled = make(map[[2]int]*image.RGBA)
	}
	p.scaled[key] = tile // a nil entry records a rasterizer refusal
	if tile == nil {
		return p.largestTileLocked()
	}
	p.scaledPixels += int64(w) * int64(h)
	return tile
}

func (p *svgPattern) largestTile() *image.RGBA {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.largestTileLocked()
}

// largestTileLocked picks the highest-resolution cached tile, preferring
// deterministic output over a fresh rasterization past the budgets.
func (p *svgPattern) largestTileLocked() *image.RGBA {
	best, bestArea := p.tile, int64(p.tile.Bounds().Dx())*int64(p.tile.Bounds().Dy())
	for _, key := range svgSortedTileKeys(p.scaled) {
		tile := p.scaled[key]
		if tile == nil {
			continue
		}
		if area := int64(key[0]) * int64(key[1]); area > bestArea {
			best, bestArea = tile, area
		}
	}
	return best
}

func svgSortedTileKeys(m map[[2]int]*image.RGBA) [][2]int {
	keys := make([][2]int, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i][0] != keys[j][0] {
			return keys[i][0] < keys[j][0]
		}
		return keys[i][1] < keys[j][1]
	})
	return keys
}

// svgPaintValue is the computed value of fill or stroke.
type svgPaintValue struct {
	color  color.NRGBA
	server *svgPaintServer
	ok     bool
}

// parseSVGPaint parses a fill or stroke value. resolve looks up a supported
// local paint server; it returns nil for missing or unsupported references.
func parseSVGPaint(value string, inherited svgPaintValue, currentColor color.NRGBA, resolve func(string) *svgPaintServer) svgPaintValue {
	trimmed := strings.TrimSpace(value)
	if len(trimmed) >= 4 && strings.EqualFold(trimmed[:4], "url(") {
		end := strings.IndexByte(trimmed, ')')
		if end < 0 {
			return inherited
		}
		ref := strings.Trim(strings.TrimSpace(trimmed[4:end]), `"'`)
		fallback := strings.TrimSpace(trimmed[end+1:])
		if strings.HasPrefix(ref, "#") && len(ref) > 1 && resolve != nil {
			if server := resolve(ref[1:]); server != nil {
				if g := server.gradient; g != nil && len(g.stops) == 0 {
					return svgPaintValue{} // a gradient without stops paints nothing
				}
				if g := server.gradient; g != nil && len(g.stops) == 1 {
					c := g.stops[0].color
					return svgPaintValue{color: c, ok: c.A > 0}
				}
				return svgPaintValue{color: color.NRGBA{A: 255}, server: server, ok: true}
			}
		}

		// Missing, external, or unsupported paint servers use the fallback
		// paint, or none when no fallback is given.
		if fallback == "" || strings.HasPrefix(strings.ToLower(fallback), "url(") {
			return svgPaintValue{}
		}
		return parseSVGPaint(fallback, inherited, currentColor, nil)
	}
	c, ok := svgPaint(value, inherited.color, inherited.ok, currentColor)
	if strings.EqualFold(trimmed, "inherit") {
		return inherited
	}
	return svgPaintValue{color: c, ok: ok}
}

func (s *svgExpansion) resolvePaintServer(id string) *svgPaintServer {
	if g := s.resolveGradient(id); g != nil {
		return &svgPaintServer{gradient: g, toLocal: svgIdentity}
	}
	if p := s.resolvePattern(id); p != nil {
		return &svgPaintServer{pattern: p, toLocal: p.transform}
	}
	return nil
}

// svgOpacityValue parses an <alpha-value>: a number or a percentage, clamped
// to [0, 1].
func svgOpacityValue(s string) (float64, bool) {
	s = strings.TrimSpace(s)
	scale := 1.0
	if rest, ok := strings.CutSuffix(s, "%"); ok {
		s, scale = rest, 0.01
	}
	n, err := strconv.ParseFloat(s, 64)
	if err != nil || math.IsNaN(n) || math.IsInf(n, 0) {
		return 0, false
	}
	return math.Max(0, math.Min(1, n*scale)), true
}

// resolveGradient returns the gradient with the given ID, following href
// inheritance, or nil when the ID is not a supported gradient.
func (s *svgExpansion) resolveGradient(id string) *svgGradient {
	node := s.ids[id]
	if node == nil || !node.valid || node.name != "linearGradient" && node.name != "radialGradient" {
		return nil
	}

	if g, ok := s.gradients[node]; ok {
		return g
	}
	// Collect the href chain, stopping at cycles, non-gradients, or the cap.
	chain := []*svgNode{node}
	seen := map[*svgNode]bool{node: true}
	for cur := node; len(chain) < maxSVGGradientChain; {
		if !strings.HasPrefix(cur.href, "#") || len(cur.href) < 2 {
			break
		}
		next := s.ids[cur.href[1:]]
		if next == nil || seen[next] || !next.valid || next.name != "linearGradient" && next.name != "radialGradient" {
			break
		}
		seen[next] = true
		chain = append(chain, next)
		cur = next
	}
	radial := node.name == "radialGradient"
	attr := func(name string, sameKind bool) (string, bool) {
		for _, n := range chain {
			if sameKind && (n.name == "radialGradient") != radial {
				continue
			}
			if v, ok := n.attrs[name]; ok {
				return v, true
			}
		}
		return "", false
	}
	g := &svgGradient{radial: radial, transform: svgIdentity, spread: "pad"}
	if v, ok := attr("gradientUnits", false); ok && strings.TrimSpace(v) == "userSpaceOnUse" {
		g.userSpace = true
	}
	if v, ok := attr("gradientTransform", false); ok {
		if t, ok := parseSVGTransform(v); ok {
			g.transform = t
		}
	}
	if v, ok := attr("spreadMethod", false); ok {
		if v = strings.TrimSpace(v); v == "reflect" || v == "repeat" {
			g.spread = v
		}
	}
	vw, vh := s.img.width, s.img.height
	if s.img.hasViewBox {
		vw, vh = s.img.viewBox[2], s.img.viewBox[3]
	}
	coord := func(name, def string, basis float64) float64 {
		if v, ok := attr(name, true); ok {
			if n, ok := svgGradientLength(v, basis, g.userSpace); ok {
				return n
			}
		}
		n, _ := svgGradientLength(def, basis, g.userSpace)
		return n
	}
	if radial {
		g.x1 = coord("cx", "50%", vw)
		g.y1 = coord("cy", "50%", vh)
		g.r = coord("r", "50%", math.Hypot(vw, vh)/math.Sqrt2)
		g.fx, g.fy = g.x1, g.y1
		if v, ok := attr("fx", true); ok {
			if n, ok := svgGradientLength(v, vw, g.userSpace); ok {
				g.fx = n
			}
		}
		if v, ok := attr("fy", true); ok {
			if n, ok := svgGradientLength(v, vh, g.userSpace); ok {
				g.fy = n
			}
		}
	} else {
		g.x1 = coord("x1", "0%", vw)
		g.y1 = coord("y1", "0%", vh)
		g.x2 = coord("x2", "100%", vw)
		g.y2 = coord("y2", "0%", vh)
	}
	// Stops come from the first element in the chain that has any.
	for _, n := range chain {
		for _, child := range n.children {
			if !child.valid || child.name != "stop" || len(g.stops) >= maxSVGGradientStops {
				continue
			}
			g.stops = append(g.stops, svgParseStop(s.cascadedAttributes(child), s.computedColor(child), g.stops))
		}
		if len(g.stops) > 0 {
			break
		}
	}
	s.gradients[node] = g
	return g
}

// resolvePattern builds a bounded raster tile for a user-space pattern.
// A nil cache entry is installed before expansion so self-references and
// mutually recursive pattern paints use the URL fallback instead of recursing.
func (s *svgExpansion) resolvePattern(id string) *svgPattern {
	node := s.ids[id]
	if node == nil || !node.valid || node.name != "pattern" {
		return nil
	}
	if p, ok := s.patterns[node]; ok {
		return p
	}
	s.patterns[node] = nil

	chain := []*svgNode{node}
	seen := map[*svgNode]bool{node: true}
	for cur := node; len(chain) < maxSVGGradientChain; {
		if !strings.HasPrefix(cur.href, "#") || len(cur.href) < 2 {
			break
		}
		next := s.ids[cur.href[1:]]
		if next == nil || seen[next] || !next.valid || next.name != "pattern" {
			break
		}
		seen[next] = true
		chain = append(chain, next)
		cur = next
	}
	attr := func(name string) (string, bool) {
		for _, n := range chain {
			if v, ok := s.cascadedAttributes(n)[name]; ok {
				return v, true
			}
		}
		return "", false
	}
	// objectBoundingBox geometry/content needs shape-specific binding and is
	// deliberately left to the focused follow-up. Do not mistake it for a
	// missing reference: parseSVGPaint will apply the declared fallback.
	units, _ := attr("patternUnits")
	if strings.TrimSpace(units) != "userSpaceOnUse" {
		return nil
	}
	if units, ok := attr("patternContentUnits"); ok && strings.TrimSpace(units) != "userSpaceOnUse" {
		return nil
	}
	vw, vh := s.img.userWidth, s.img.userHeight
	length := func(name string, basis float64) (float64, bool) {
		v, present := attr(name)
		if !present {
			v = "0"
		}
		return svgGradientLength(v, basis, true)
	}
	x, okX := length("x", vw)
	y, okY := length("y", vh)
	width, okW := length("width", vw)
	height, okH := length("height", vh)
	if !okX || !okY || !okW || !okH || width <= 0 || height <= 0 {
		return nil
	}
	tw, th := int(math.Ceil(width)), int(math.Ceil(height))
	area := int64(tw) * int64(th)
	if tw <= 0 || th <= 0 || tw > maxSVGRasterSide || th > maxSVGRasterSide ||
		area > maxDecodedImagePixels || s.patternPixels+area > maxSVGPatternPixels {
		return nil
	}
	transform := svgIdentity
	if v, ok := attr("patternTransform"); ok {
		if parsed, valid := parseSVGTransform(v); valid {
			transform = parsed
		}
	}
	content := node
	for _, n := range chain {
		if len(n.children) > 0 {
			content = n
			break
		}
	}

	oldImg := s.img
	tile := &svgImage{
		width: width, height: height, userWidth: vw, userHeight: vh,
		dashBasis: oldImg.dashBasis, rootFontSize: oldImg.rootFontSize,
		align: "none",
	}
	s.img = tile
	frame := svgDefaultFrame()
	frame.userWidth, frame.userHeight, frame.dashBasis = vw, vh, oldImg.dashBasis
	frame.transform = svgAffine{a: 1, d: 1, e: -x, f: -y}
	err := s.walk(content, frame, true, 0)
	s.img = oldImg
	if err != nil {
		return nil
	}
	raster := tile.rasterize(tw, th)
	if raster == nil {
		return nil
	}
	s.patternPixels += area
	p := &svgPattern{x: x, y: y, width: width, height: height, transform: transform, tile: raster, content: tile}
	s.patterns[node] = p
	return p
}

func (s *svgExpansion) computedColor(node *svgNode) color.NRGBA {
	if c, ok := s.colors[node]; ok {
		return c
	}
	parentColor := color.NRGBA{A: 255}
	if node.parent != nil {
		parentColor = s.computedColor(node.parent)
	}
	c := svgColor(s.cascadedAttributes(node)["color"], parentColor)
	s.colors[node] = c
	return c
}

func svgParseStop(a map[string]string, currentColor color.NRGBA, previous []svgStop) svgStop {
	offset := 0.0
	if v := strings.TrimSpace(a["offset"]); v != "" {
		scale := 1.0
		if rest, ok := strings.CutSuffix(v, "%"); ok {
			v, scale = rest, 0.01
		}
		if n, err := strconv.ParseFloat(v, 64); err == nil && !math.IsNaN(n) {
			offset = math.Max(0, math.Min(1, n*scale))
		}
	}
	if len(previous) > 0 && offset < previous[len(previous)-1].offset {
		offset = previous[len(previous)-1].offset
	}
	c := color.NRGBA{A: 255}
	if v := strings.ToLower(strings.TrimSpace(a["stop-color"])); v != "" {
		if v == "currentcolor" {
			c = currentColor
		} else if parsed, ok := parseColor(v); ok {
			c = color.NRGBA{R: parsed.R, G: parsed.G, B: parsed.B, A: parsed.A}
		}
	}
	if n, ok := svgOpacityValue(a["stop-opacity"]); ok {
		c.A = uint8(math.Round(float64(c.A) * n))
	}
	return svgStop{offset: offset, color: c}
}

// svgGradientLength resolves a gradient coordinate. In objectBoundingBox
// units numbers and percentages are fractions of the box; in userSpaceOnUse
// percentages are relative to the viewport basis.
func svgGradientLength(v string, basis float64, userSpace bool) (float64, bool) {
	v = strings.TrimSpace(v)
	scale := 1.0
	if rest, ok := strings.CutSuffix(v, "%"); ok {
		v, scale = rest, 0.01
		if userSpace {
			scale *= basis
		}
	} else {
		v = strings.TrimSuffix(v, "px")
	}
	n, err := strconv.ParseFloat(v, 64)
	if err != nil || math.IsNaN(n) || math.IsInf(n, 0) || math.Abs(n) > 1e9 {
		return 0, false
	}
	return n * scale, true
}

// bindSVGPaint binds a paint server to a shape's geometry. It returns ok=false
// when an objectBoundingBox gradient is applied to geometry with zero width
// or height, which then paints nothing.
func bindSVGPaint(server *svgPaintServer, segments []svgSegment) (*svgPaintServer, bool) {
	if server == nil {
		return nil, true
	}
	bound := *server
	if server.pattern != nil {
		return &bound, true
	}
	g := server.gradient
	m := g.transform
	if !g.userSpace {
		x0, y0, x1, y1, ok := svgSegmentsBounds(segments)
		if !ok || x1-x0 <= 0 || y1-y0 <= 0 {
			return nil, false
		}
		m = m.then(svgAffine{a: x1 - x0, d: y1 - y0, e: x0, f: y0})
	}
	bound.toLocal = m
	return &bound, true
}

// svgSegmentsBounds returns the exact geometry bounds of a path, including
// curve extrema but not control points.
func svgSegmentsBounds(segments []svgSegment) (x0, y0, x1, y1 float64, ok bool) {
	x0, y0, x1, y1 = math.Inf(1), math.Inf(1), math.Inf(-1), math.Inf(-1)
	add := func(p [2]float64) {
		x0, y0 = math.Min(x0, p[0]), math.Min(y0, p[1])
		x1, y1 = math.Max(x1, p[0]), math.Max(y1, p[1])
	}
	var cur, start [2]float64
	have := false
	for _, seg := range segments {
		switch seg.op {
		case 'M':
			cur, start, have = seg.pts[0], seg.pts[0], true
			add(cur)
		case 'L':
			cur = seg.pts[0]
			add(cur)
		case 'Q':
			if !have {
				continue
			}
			p0, p1, p2 := cur, seg.pts[0], seg.pts[1]
			for axis := 0; axis < 2; axis++ {
				d := p0[axis] - 2*p1[axis] + p2[axis]
				if d != 0 {
					if t := (p0[axis] - p1[axis]) / d; t > 0 && t < 1 {
						u := 1 - t
						add([2]float64{u*u*p0[0] + 2*u*t*p1[0] + t*t*p2[0], u*u*p0[1] + 2*u*t*p1[1] + t*t*p2[1]})
					}
				}
			}
			cur = p2
			add(cur)
		case 'C':
			if !have {
				continue
			}
			p0, p1, p2, p3 := cur, seg.pts[0], seg.pts[1], seg.pts[2]
			at := func(t float64) [2]float64 {
				u := 1 - t
				var p [2]float64
				for i := range p {
					p[i] = u*u*u*p0[i] + 3*u*u*t*p1[i] + 3*u*t*t*p2[i] + t*t*t*p3[i]
				}
				return p
			}
			for axis := 0; axis < 2; axis++ {
				// Roots of the derivative a t² + b t + c.
				a := -p0[axis] + 3*p1[axis] - 3*p2[axis] + p3[axis]
				b := 2 * (p0[axis] - 2*p1[axis] + p2[axis])
				c := p1[axis] - p0[axis]
				for _, t := range svgQuadraticRoots(a, b, c) {
					if t > 0 && t < 1 {
						add(at(t))
					}
				}
			}
			cur = p3
			add(cur)
		case 'Z':
			cur = start
		}
	}
	ok = x0 <= x1 && y0 <= y1
	return
}

func svgQuadraticRoots(a, b, c float64) []float64 {
	if math.Abs(a) < 1e-12 {
		if b == 0 {
			return nil
		}
		return []float64{-c / b}
	}
	disc := b*b - 4*a*c
	if disc < 0 {
		return nil
	}
	sq := math.Sqrt(disc)
	return []float64{(-b + sq) / (2 * a), (-b - sq) / (2 * a)}
}

func (t svgAffine) invert() (svgAffine, bool) {
	det := t.a*t.d - t.b*t.c
	if det == 0 || math.IsNaN(det) || math.IsInf(det, 0) || math.Abs(det) < 1e-12 {
		return svgAffine{}, false
	}
	a, b, c, d := t.d/det, -t.b/det, -t.c/det, t.a/det
	return svgAffine{a: a, b: b, c: c, d: d, e: -(a*t.e + c*t.f), f: -(b*t.e + d*t.f)}, true
}

// offset maps a gradient-space point to a stop offset before spreading.
func (g *svgGradient) offset(x, y float64) float64 {
	if !g.radial {
		dx, dy := g.x2-g.x1, g.y2-g.y1
		return ((x-g.x1)*dx + (y-g.y1)*dy) / (dx*dx + dy*dy)
	}
	// SVG 1.1 focal radial gradient: a focus outside the circle is moved
	// just inside it. With the focus at the centre this is |p-c|/r.
	ex, ey := g.x1-g.fx, g.y1-g.fy
	if dist := math.Hypot(ex, ey); dist > g.r*0.999 {
		k := g.r * 0.999 / dist
		ex, ey = ex*k, ey*k
	}
	fx, fy := g.x1-ex, g.y1-ey
	dx, dy := x-fx, y-fy
	a := ex*ex + ey*ey - g.r*g.r
	de := dx*ex + dy*ey
	return (de - math.Sqrt(de*de-a*(dx*dx+dy*dy))) / a
}

// degenerate reports whether the gradient vector or radius is empty, in
// which case the area is painted with the last stop colour.
func (g *svgGradient) degenerate() bool {
	if g.radial {
		return !(g.r > 0)
	}
	return g.x1 == g.x2 && g.y1 == g.y2
}

func (g *svgGradient) colorAt(t float64) color.NRGBA {
	switch g.spread {
	case "repeat":
		t -= math.Floor(t)
	case "reflect":
		t = math.Mod(math.Abs(t), 2)
		if t > 1 {
			t = 2 - t
		}
	}
	stops := g.stops
	if t <= stops[0].offset {
		return stops[0].color
	}
	last := stops[len(stops)-1]
	if t >= last.offset {
		return last.color
	}
	for i := 1; i < len(stops); i++ {
		b := stops[i]
		if t >= b.offset {
			continue
		}
		a := stops[i-1]
		f := (t - a.offset) / (b.offset - a.offset)
		mix := func(p, q uint8) uint8 { return uint8(math.Round(float64(p) + (float64(q)-float64(p))*f)) }
		return color.NRGBA{R: mix(a.color.R, b.color.R), G: mix(a.color.G, b.color.G), B: mix(a.color.B, b.color.B), A: mix(a.color.A, b.color.A)}
	}
	return last.color
}

// svgPaintRaster holds per-rasterize budgets for layers and paint sources.
type svgPaintRaster struct {
	layerPixels, gradientPixels int64
}

// source returns the image to paint for a shape: a uniform colour, gradient,
// or repeating pattern rendered over device-space bounds. alpha scales it.
func (p *svgPaintRaster) source(c color.NRGBA, server *svgPaintServer, toDevice svgAffine, bounds image.Rectangle, alpha float64) image.Image {
	c.A = uint8(math.Round(float64(c.A) * alpha))
	if server == nil {
		return image.NewUniform(c)
	}
	inv, invertible := server.toLocal.then(toDevice).invert()
	area := int64(bounds.Dx()) * int64(bounds.Dy())
	if server.pattern != nil {
		if !invertible || area <= 0 || p.gradientPixels+area > maxSVGGradientPixels {
			return image.NewUniform(color.NRGBA{})
		}
		p.gradientPixels += area
		pattern := server.pattern
		tile := pattern.tileFor(server.toLocal.then(toDevice))
		tileW, tileH := tile.Bounds().Dx(), tile.Bounds().Dy()
		src := image.NewRGBA(bounds)
		scale := uint32(c.A)
		mod := func(v, size float64) float64 {
			v = math.Mod(v, size)
			if v < 0 {
				v += size
			}
			return v
		}
		for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
			i := src.PixOffset(bounds.Min.X, y)
			for x := bounds.Min.X; x < bounds.Max.X; x++ {
				px, py := inv.apply(float64(x)+0.5, float64(y)+0.5)
				tx := int(mod(px-pattern.x, pattern.width) / pattern.width * float64(tileW))
				ty := int(mod(py-pattern.y, pattern.height) / pattern.height * float64(tileH))
				// Guard the open end of the tile against rounding.
				tx, ty = min(tx, tileW-1), min(ty, tileH-1)
				col := tile.RGBAAt(tx, ty)
				src.Pix[i+0] = uint8((uint32(col.R)*scale + 127) / 255)
				src.Pix[i+1] = uint8((uint32(col.G)*scale + 127) / 255)
				src.Pix[i+2] = uint8((uint32(col.B)*scale + 127) / 255)
				src.Pix[i+3] = uint8((uint32(col.A)*scale + 127) / 255)
				i += 4
			}
		}
		return src
	}
	g := server.gradient
	scale := float64(c.A) / 255
	last := g.stops[len(g.stops)-1].color
	if !invertible || g.degenerate() || area <= 0 || p.gradientPixels+area > maxSVGGradientPixels {
		last.A = uint8(math.Round(float64(last.A) * scale))
		return image.NewUniform(last)
	}
	p.gradientPixels += area
	src := image.NewRGBA(bounds)
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		i := src.PixOffset(bounds.Min.X, y)
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			gx, gy := inv.apply(float64(x)+0.5, float64(y)+0.5)
			col := g.colorAt(g.offset(gx, gy))
			a := float64(col.A) * scale / 255
			src.Pix[i+0] = uint8(math.Round(float64(col.R) * a))
			src.Pix[i+1] = uint8(math.Round(float64(col.G) * a))
			src.Pix[i+2] = uint8(math.Round(float64(col.B) * a))
			src.Pix[i+3] = uint8(math.Round(a * 255))
			i += 4
		}
	}
	return src
}

// svgDeviceBounds returns a conservative device-space bounding box of a
// shape's control points, grown by pad, clipped to the canvas.
func svgDeviceBounds(shape svgShape, m svgAffine, pad float64, canvas image.Rectangle) image.Rectangle {
	x0, y0, x1, y1 := math.Inf(1), math.Inf(1), math.Inf(-1), math.Inf(-1)
	for _, seg := range shape.segments {
		n := 1
		switch seg.op {
		case 'Z':
			continue
		case 'Q':
			n = 2
		case 'C':
			n = 3
		}
		for _, p := range seg.pts[:n] {
			x, y := m.apply(p[0], p[1])
			x0, y0, x1, y1 = math.Min(x0, x), math.Min(y0, y), math.Max(x1, x), math.Max(y1, y)
		}
	}
	if !(x0 <= x1 && y0 <= y1) {
		return image.Rectangle{}
	}
	r := image.Rect(int(math.Floor(math.Max(-1, x0-pad))), int(math.Floor(math.Max(-1, y0-pad))),
		int(math.Ceil(math.Min(float64(canvas.Max.X+1), x1+pad))), int(math.Ceil(math.Min(float64(canvas.Max.Y+1), y1+pad))))
	return r.Intersect(canvas)
}

// svgCompositeLayer draws a premultiplied layer onto dst with the given
// group opacity using source-over.
func svgCompositeLayer(dst, layer *image.RGBA, opacity float64) {
	k := uint32(math.Round(opacity * 255))
	for i := 0; i+3 < len(layer.Pix); i += 4 {
		sa := uint32(layer.Pix[i+3]) * k / 255
		if sa == 0 {
			continue
		}
		inv := 255 - sa
		for c := 0; c < 3; c++ {
			s := uint32(layer.Pix[i+c]) * k / 255
			dst.Pix[i+c] = uint8(s + (uint32(dst.Pix[i+c])*inv+127)/255)
		}
		dst.Pix[i+3] = uint8(sa + (uint32(dst.Pix[i+3])*inv+127)/255)
	}
}

// svgAffineScale bounds how much m can stretch a unit length.
func svgAffineScale(m svgAffine) float64 {
	return math.Max(math.Hypot(m.a, m.b)+math.Hypot(m.c, m.d), 1e-9)
}
