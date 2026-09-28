package browser

import (
	"image"
	"math"
	"sort"
)

// golang.org/x/image/vector fills with the non-zero winding rule only. Simple
// even-odd paths, where both rules are equivalent, use that rasterizer too.
// Complex even-odd paths are rasterized here: each pixel row is sampled on
// svgFillSubSamples sub-scanlines whose parity spans are accumulated with
// analytic horizontal coverage.

const (
	svgFillSubSamples        = 16
	svgFillQualitySubSamples = 128
	// Quality sampling is only selected when this conservative estimate of
	// edge visits plus covered pixel visits remains bounded. Larger inputs
	// retain the 16-sample fallback.
	maxSVGQualitySampleWork = 1 << 22
)

// The exact even-odd rasterizer splits the path into y-monotone trapezoids.
// These caps keep intersection discovery and per-pixel clipping bounded;
// inputs beyond them retain the existing bounded scanline approximation.
// The edge limit is deliberately above the common dense-path threshold: a
// path can have many edges without having any interior crossings, and those
// paths remain cheap in the slab sweep below.
const (
	maxSVGExactEdges       = 4096
	maxSVGExactEvents      = 16384
	maxSVGExactIntersects  = 8192
	maxSVGExactPixelChecks = 1 << 24
)

type svgEdge struct {
	x0, y0, x1, y1 float64 // y0 < y1
	// slope is dx/dy, used to find the crossing x for a sample row.
	slope float64
}

type svgSubpath struct {
	points []svgPoint
	closed bool
}

// flattenSVGShape is the bounded curve-flattening path shared by fills and
// strokes. Points are transformed by m; callers decide whether open subpaths
// are implicitly closed (fills) or left open (strokes).
func flattenSVGShape(shape svgShape, m svgAffine) []svgSubpath {
	var paths []svgSubpath
	var cur []svgPoint
	used := 0
	pt := func(p [2]float64) svgPoint {
		x, y := m.apply(p[0], p[1])
		return svgPoint{x, y}
	}
	flush := func(closed bool) {
		if len(cur) > 2 {
			paths = append(paths, svgSubpath{points: cur, closed: closed})
		} else if len(cur) > 1 {
			// A two-point subpath cannot be filled, but can still be stroked.
			paths = append(paths, svgSubpath{points: cur, closed: closed})
		}
		cur = nil
	}
	for _, seg := range shape.segments {
		if used >= maxSVGPathSegs {
			break
		}
		switch seg.op {
		case 'M':
			flush(false)
			cur = append(cur, pt(seg.pts[0]))
			used++
		case 'L':
			cur = append(cur, pt(seg.pts[0]))
			used++
		case 'Q', 'C':
			if len(cur) == 0 {
				continue
			}
			start := cur[len(cur)-1]
			a, b := pt(seg.pts[0]), pt(seg.pts[1])
			for i := 1; i <= 32 && used < maxSVGPathSegs; i++ {
				t := float64(i) / 32
				u := 1 - t
				if seg.op == 'Q' {
					cur = append(cur, start.mul(u*u).add(a.mul(2*u*t)).add(b.mul(t*t)))
				} else {
					c := pt(seg.pts[2])
					cur = append(cur, start.mul(u*u*u).add(a.mul(3*u*u*t)).add(b.mul(3*u*t*t)).add(c.mul(t*t*t)))
				}
				used++
			}
		case 'Z':
			flush(true)
		}
	}
	flush(false)
	return paths
}

// svgFillRulesEquivalent reports the conservative case where even-odd and
// non-zero coverage describe the same area. Keeping this check bounded avoids
// quadratic work on adversarial paths; uncertain paths use the parity filler.
func svgFillRulesEquivalent(paths []svgSubpath) bool {
	if len(paths) != 1 || len(paths[0].points) < 3 || len(paths[0].points) > 256 {
		return false
	}
	points := paths[0].points
	if len(points) > 1 && points[0] == points[len(points)-1] {
		points = points[:len(points)-1]
	}
	n := len(points)
	if n < 3 {
		return false
	}
	for i := 0; i < n; i++ {
		a, b := points[i], points[(i+1)%n]
		if !finiteSVGPoint(a) || !finiteSVGPoint(b) || a == b {
			return false
		}
		for j := i + 1; j < n; j++ {
			// Consecutive edges (including the first and last) share a vertex.
			if j == i+1 || (i == 0 && j == n-1) {
				continue
			}
			if svgLineSegmentsIntersect(a, b, points[j], points[(j+1)%n]) {
				return false
			}
		}
	}
	return true
}

// svgVisibleFillPaths omits closed polygonal paths whose bounds are wholly
// outside the raster. Such paths cannot affect visible even-odd parity, and
// should not force a simple visible contour onto a different antialiasing
// path.
func svgVisibleFillPaths(paths []svgSubpath, w, h int) []svgSubpath {
	visible := make([]svgSubpath, 0, len(paths))
	for _, path := range paths {
		if len(path.points) == 0 {
			continue
		}
		minX, minY := math.Inf(1), math.Inf(1)
		maxX, maxY := math.Inf(-1), math.Inf(-1)
		for _, p := range path.points {
			if !finiteSVGPoint(p) {
				minX, minY, maxX, maxY = math.Inf(-1), math.Inf(-1), math.Inf(1), math.Inf(1)
				break
			}
			minX, maxX = math.Min(minX, p.x), math.Max(maxX, p.x)
			minY, maxY = math.Min(minY, p.y), math.Max(maxY, p.y)
		}
		if maxX <= 0 || minX >= float64(w) || maxY <= 0 || minY >= float64(h) {
			continue
		}
		visible = append(visible, path)
	}
	return visible
}

func finiteSVGPoint(p svgPoint) bool {
	return !math.IsNaN(p.x) && !math.IsNaN(p.y) &&
		!math.IsInf(p.x, 0) && !math.IsInf(p.y, 0)
}

// svgLineSegmentsIntersect treats touching and collinear segments as
// intersections. False negatives would incorrectly select non-zero filling,
// while false positives merely select the parity fallback.
func svgLineSegmentsIntersect(a, b, c, d svgPoint) bool {
	cross := func(p, q, r svgPoint) float64 {
		return (q.x-p.x)*(r.y-p.y) - (q.y-p.y)*(r.x-p.x)
	}
	onSegment := func(p, q, r svgPoint) bool {
		const epsilon = 1e-9
		return r.x >= math.Min(p.x, q.x)-epsilon && r.x <= math.Max(p.x, q.x)+epsilon &&
			r.y >= math.Min(p.y, q.y)-epsilon && r.y <= math.Max(p.y, q.y)+epsilon
	}
	const epsilon = 1e-9
	abC, abD := cross(a, b, c), cross(a, b, d)
	cdA, cdB := cross(c, d, a), cross(c, d, b)
	if ((abC > epsilon && abD < -epsilon) || (abC < -epsilon && abD > epsilon)) &&
		((cdA > epsilon && cdB < -epsilon) || (cdA < -epsilon && cdB > epsilon)) {
		return true
	}
	return (math.Abs(abC) <= epsilon && onSegment(a, b, c)) ||
		(math.Abs(abD) <= epsilon && onSegment(a, b, d)) ||
		(math.Abs(cdA) <= epsilon && onSegment(c, d, a)) ||
		(math.Abs(cdB) <= epsilon && onSegment(c, d, b))
}

// svgEvenOddMask rasterizes paths into a w×h alpha mask using the even-odd
// rule, or returns nil when there is nothing to fill. Small enough paths use
// exact polygon area coverage; unusually complex paths retain the bounded
// sub-scanline approximation.
func svgEvenOddMask(paths []svgSubpath, w, h int) *image.Alpha {
	if mask, ok := svgEvenOddExactMask(paths, w, h); ok {
		return mask
	}
	return svgEvenOddSampledMask(paths, w, h)
}

// svgEvenOddExactMask computes pixel areas by splitting the path at every
// vertex and edge crossing. Within each resulting slab the even-odd spans are
// trapezoids, which can be clipped against pixel columns and integrated
// exactly. It returns ok=false before exposing a partial image if a resource
// cap is exceeded.
func svgEvenOddExactMask(paths []svgSubpath, w, h int) (*image.Alpha, bool) {
	if w <= 0 || h <= 0 {
		return nil, true
	}
	var edges []svgEdge
	for _, path := range paths {
		poly := path.points
		n := len(poly)
		for i := 0; i < n; i++ {
			p, q := poly[i], poly[(i+1)%n]
			if p.y == q.y || !finiteSVGPoint(p) || !finiteSVGPoint(q) {
				continue
			}
			if p.y > q.y {
				p, q = q, p
			}
			slope := (q.x - p.x) / (q.y - p.y)
			if math.IsNaN(slope) || math.IsInf(slope, 0) {
				continue
			}
			edges = append(edges, svgEdge{x0: p.x, y0: p.y, x1: q.x, y1: q.y, slope: slope})
		}
	}
	if len(edges) == 0 {
		return nil, true
	}
	if len(edges) > maxSVGExactEdges || int64(w)*int64(h) > maxDecodedImagePixels {
		return nil, false
	}

	events := make([]float64, 0, h+2*len(edges))
	for y := 0; y <= h; y++ {
		events = append(events, float64(y))
	}
	addEvent := func(y float64) bool {
		if y <= 0 || y >= float64(h) {
			return true
		}
		if len(events) >= maxSVGExactEvents {
			return false
		}
		events = append(events, y)
		return true
	}
	for _, e := range edges {
		if !addEvent(e.y0) || !addEvent(e.y1) {
			return nil, false
		}
	}

	// Edge intersections can change which boundaries form the parity spans.
	// Pairwise work is bounded by maxSVGExactEdges; the event cap also bounds
	// the following slab sweep for paths with many self-intersections.
	intersections := 0
	for i := range edges {
		a := edges[i]
		arx, ary := a.x1-a.x0, a.y1-a.y0
		for j := i + 1; j < len(edges); j++ {
			b := edges[j]
			// Edges whose open y intervals do not overlap cannot cross.
			// Avoiding those candidates is important for paths made from many
			// short segments spread over the viewport.
			if a.y1 <= b.y0 || b.y1 <= a.y0 {
				continue
			}
			brx, bry := b.x1-b.x0, b.y1-b.y0
			den := arx*bry - ary*brx
			if den == 0 {
				continue
			}
			qpx, qpy := b.x0-a.x0, b.y0-a.y0
			t := (qpx*bry - qpy*brx) / den
			u := (qpx*ary - qpy*arx) / den
			if t <= 0 || t >= 1 || u <= 0 || u >= 1 {
				continue
			}
			y := a.y0 + t*ary
			if y > 0 && y < float64(h) {
				intersections++
				if intersections > maxSVGExactIntersects || !addEvent(y) {
					return nil, false
				}
			}
		}
	}
	sort.Float64s(events)
	unique := events[:0]
	for _, y := range events {
		if len(unique) == 0 || y != unique[len(unique)-1] {
			unique = append(unique, y)
		}
	}

	type crossing struct {
		x float64
		e svgEdge
	}
	mask := image.NewAlpha(image.Rect(0, 0, w, h))
	cov := make([]float64, w)
	var xs []crossing
	pixelChecks := 0
	currentRow := -1
	flushRow := func(y int) {
		if y < 0 {
			return
		}
		row := mask.Pix[y*mask.Stride : y*mask.Stride+w]
		for x, c := range cov {
			if c > 1 {
				c = 1
			}
			if c > 0 {
				row[x] = uint8(math.Round(c * 255))
			}
		}
	}
	for i := 0; i+1 < len(unique); i++ {
		y0, y1 := unique[i], unique[i+1]
		if y1 <= y0 {
			continue
		}
		mid := y0 + (y1-y0)/2
		row := int(math.Floor(mid))
		if row < 0 || row >= h {
			continue
		}
		if row != currentRow {
			flushRow(currentRow)
			clear(cov)
			currentRow = row
		}
		xs = xs[:0]
		for _, e := range edges {
			if mid >= e.y0 && mid < e.y1 {
				xs = append(xs, crossing{x: e.x0 + (mid-e.y0)*e.slope, e: e})
			}
		}
		sort.SliceStable(xs, func(i, j int) bool { return xs[i].x < xs[j].x })
		for j := 0; j+1 < len(xs); j += 2 {
			left, right := xs[j].e, xs[j+1].e
			xl0 := left.x0 + (y0-left.y0)*left.slope
			xl1 := left.x0 + (y1-left.y0)*left.slope
			xr0 := right.x0 + (y0-right.y0)*right.slope
			xr1 := right.x0 + (y1-right.y0)*right.slope
			if xl0 > xr0 {
				xl0, xr0 = xr0, xl0
			}
			if xl1 > xr1 {
				xl1, xr1 = xr1, xl1
			}
			minX := math.Max(0, math.Min(xl0, xl1))
			maxX := math.Min(float64(w), math.Max(xr0, xr1))
			if maxX <= minX {
				continue
			}
			xStart, xEnd := int(math.Floor(minX)), int(math.Ceil(maxX))
			if xStart < 0 {
				xStart = 0
			}
			if xEnd > w {
				xEnd = w
			}
			quad := [6]svgPoint{{xl0, y0}, {xr0, y0}, {xr1, y1}, {xl1, y1}}
			for x := xStart; x < xEnd; x++ {
				pixelChecks++
				if pixelChecks > maxSVGExactPixelChecks {
					return nil, false
				}
				clipped, n := clipSVGPolygonX(quad, 4, float64(x), true)
				clipped, n = clipSVGPolygonX(clipped, n, float64(x+1), false)
				if n < 3 {
					continue
				}
				area := 0.0
				for k := 0; k < n; k++ {
					p, q := clipped[k], clipped[(k+1)%n]
					area += p.x*q.y - p.y*q.x
				}
				cov[x] += math.Abs(area) / 2
			}
		}
	}
	flushRow(currentRow)
	return mask, true
}

// clipSVGPolygonX clips a convex polygon against a vertical half-plane.
func clipSVGPolygonX(in [6]svgPoint, n int, x float64, keepGreater bool) ([6]svgPoint, int) {
	var out [6]svgPoint
	outN := 0
	if n == 0 {
		return out, 0
	}
	inside := func(p svgPoint) bool {
		if keepGreater {
			return p.x >= x
		}
		return p.x <= x
	}
	prev := in[n-1]
	prevInside := inside(prev)
	for i := 0; i < n; i++ {
		cur := in[i]
		curInside := inside(cur)
		if curInside != prevInside {
			t := (x - prev.x) / (cur.x - prev.x)
			out[outN] = svgPoint{x: x, y: prev.y + t*(cur.y-prev.y)}
			outN++
		}
		if curInside {
			out[outN] = cur
			outN++
		}
		prev, prevInside = cur, curInside
	}
	return out, outN
}

// svgEvenOddSampledMask is the bounded fallback used when exact arrangement
// construction would exceed its explicit edge, event, or pixel-work budget.
// Moderately sized inputs receive higher-quality sampling; large inputs keep
// the original 16-sample ceiling.
func svgEvenOddSampledMask(paths []svgSubpath, w, h int) *image.Alpha {
	return svgEvenOddSampledMaskWithSamples(paths, w, h, 0)
}

// svgEvenOddSampledMaskWithSamples uses the requested sample count, or picks
// the higher-quality bounded fallback when samples is zero. Explicit sample
// counts are useful for pixel-accuracy regression references.
func svgEvenOddSampledMaskWithSamples(paths []svgSubpath, w, h, samples int) *image.Alpha {
	if w <= 0 || h <= 0 {
		return nil
	}
	var edges []svgEdge
	minY, maxY := math.Inf(1), math.Inf(-1)
	for _, path := range paths {
		poly := path.points
		n := len(poly)
		for i := 0; i < n; i++ {
			p, q := poly[i], poly[(i+1)%n]
			if p.y == q.y || !finiteSVGPoint(p) || !finiteSVGPoint(q) {
				continue
			}
			if p.y > q.y {
				p, q = q, p
			}
			slope := (q.x - p.x) / (q.y - p.y)
			if math.IsNaN(slope) || math.IsInf(slope, 0) {
				continue
			}
			edges = append(edges, svgEdge{x0: p.x, y0: p.y, x1: q.x, y1: q.y, slope: slope})
			minY, maxY = math.Min(minY, p.y), math.Max(maxY, q.y)
		}
	}
	if len(edges) == 0 {
		return nil
	}
	sort.Slice(edges, func(i, j int) bool { return edges[i].y0 < edges[j].y0 })
	mask := image.NewAlpha(image.Rect(0, 0, w, h))
	yStart := int(math.Floor(minY))
	yEnd := int(math.Ceil(maxY))
	if yStart < 0 {
		yStart = 0
	}
	if yEnd > h {
		yEnd = h
	}
	if samples <= 0 {
		samples = svgFillSubSamples
		// Each edge can be visited once per overlapping row and parity spans
		// visit no more than the visible row width per sample. This estimate
		// therefore bounds the dominant loops without allocating by sample
		// count or relaxing the exact rasterizer's hostile-input safeguards.
		edgeRows := int64(0)
		for _, e := range edges {
			first := max(yStart, int(math.Floor(e.y0)))
			last := min(yEnd, int(math.Ceil(e.y1)))
			if last > first {
				edgeRows += int64(last - first)
			}
		}
		samples = svgEvenOddFallbackSampleCount(edgeRows, w, yEnd-yStart)
	}
	cov := make([]float64, w)
	var xs []float64
	next := 0
	var active []svgEdge
	for y := yStart; y < yEnd; y++ {
		clear(cov)
		rowBottom := float64(y + 1)
		for next < len(edges) && edges[next].y0 < rowBottom {
			active = append(active, edges[next])
			next++
		}
		kept := active[:0]
		for _, e := range active {
			if e.y1 > float64(y) {
				kept = append(kept, e)
			}
		}
		active = kept
		for s := 0; s < samples; s++ {
			sy := float64(y) + (float64(s)+0.5)/float64(samples)
			xs = xs[:0]
			for _, e := range active {
				if sy >= e.y0 && sy < e.y1 {
					xs = append(xs, e.x0+(sy-e.y0)*e.slope)
				}
			}
			if len(xs) < 2 {
				continue
			}
			sort.Float64s(xs)
			for i := 0; i+1 < len(xs); i += 2 {
				addSVGSpan(cov, xs[i], xs[i+1], 1.0/float64(samples))
			}
		}
		row := mask.Pix[y*mask.Stride : y*mask.Stride+w]
		for x, c := range cov {
			if c <= 0 {
				continue
			}
			if c > 1 {
				c = 1
			}
			row[x] = uint8(math.Round(c * 255))
		}
	}
	return mask
}

func svgEvenOddFallbackSampleCount(edgeRows int64, width, rows int) int {
	qualityWork := (edgeRows + int64(width)*int64(rows)) * svgFillQualitySubSamples
	if qualityWork <= maxSVGQualitySampleWork {
		return svgFillQualitySubSamples
	}
	return svgFillSubSamples
}

// addSVGSpan adds weight*coverage for the horizontal span [x0,x1) to cov,
// with fractional coverage for the partially covered end pixels.
func addSVGSpan(cov []float64, x0, x1, weight float64) {
	if x1 <= x0 {
		return
	}
	if x0 < 0 {
		x0 = 0
	}
	if x1 > float64(len(cov)) {
		x1 = float64(len(cov))
	}
	if x1 <= x0 {
		return
	}
	i0, i1 := int(math.Floor(x0)), int(math.Floor(x1))
	if i0 == i1 {
		if i0 >= 0 && i0 < len(cov) {
			cov[i0] += (x1 - x0) * weight
		}
		return
	}
	if i0 >= 0 && i0 < len(cov) {
		cov[i0] += (float64(i0+1) - x0) * weight
	}
	for x := i0 + 1; x < i1 && x < len(cov); x++ {
		cov[x] += weight
	}
	if i1 >= 0 && i1 < len(cov) {
		cov[i1] += (x1 - float64(i1)) * weight
	}
}
