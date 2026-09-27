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

const svgFillSubSamples = 16

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
	n := len(points)
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
// rule, or returns nil when there is nothing to fill.
func svgEvenOddMask(paths []svgSubpath, w, h int) *image.Alpha {
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
			if p.y == q.y || math.IsNaN(p.y) || math.IsNaN(q.y) || math.IsNaN(p.x) || math.IsNaN(q.x) {
				continue
			}
			if p.y > q.y {
				p, q = q, p
			}
			edges = append(edges, svgEdge{x0: p.x, y0: p.y, x1: q.x, y1: q.y, slope: (q.x - p.x) / (q.y - p.y)})
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
	cov := make([]float64, w)
	var xs []float64
	next := 0 // index of the first edge not yet activated
	var active []svgEdge
	for y := yStart; y < yEnd; y++ {
		for i := range cov {
			cov[i] = 0
		}
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
		for s := 0; s < svgFillSubSamples; s++ {
			sy := float64(y) + (float64(s)+0.5)/svgFillSubSamples
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
				addSVGSpan(cov, xs[i], xs[i+1], 1.0/svgFillSubSamples)
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
