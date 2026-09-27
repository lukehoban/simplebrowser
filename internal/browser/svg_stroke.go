package browser

import (
	"math"

	"golang.org/x/image/vector"
)

type svgPoint struct{ x, y float64 }

func (p svgPoint) add(q svgPoint) svgPoint { return svgPoint{p.x + q.x, p.y + q.y} }
func (p svgPoint) sub(q svgPoint) svgPoint { return svgPoint{p.x - q.x, p.y - q.y} }
func (p svgPoint) mul(n float64) svgPoint  { return svgPoint{p.x * n, p.y * n} }

// Strokes are assembled as a union of polygons in one rasterizer pass. In
// particular, adjacent translucent segments must not alpha-composite twice.
// Geometry is built in SVG user space, then transformed (including non-uniform
// scales and reflections) in the same way as filled paths.
func strokeSVGPath(r *vector.Rasterizer, shape svgShape, m svgAffine) {
	var points []svgPoint
	used := 0
	flush := func(closed bool) {
		if len(points) > 1 {
			strokeSVGSubpath(r, points, closed, shape, m)
		}
		points = nil
	}
	for _, seg := range shape.segments {
		if used >= maxSVGPathSegs {
			break
		}
		switch seg.op {
		case 'M':
			flush(false)
			points = append(points, svgPoint{seg.pts[0][0], seg.pts[0][1]})
			used++
		case 'L':
			points = append(points, svgPoint{seg.pts[0][0], seg.pts[0][1]})
			used++
		case 'Q', 'C':
			if len(points) == 0 {
				continue
			}
			start := points[len(points)-1]
			// Bounded curve flattening; the segment budget also bounds the
			// rasterizer work for adversarial SVG inputs.
			for i := 1; i <= 32 && used < maxSVGPathSegs; i++ {
				t := float64(i) / 32
				u := 1 - t
				a := svgPoint{seg.pts[0][0], seg.pts[0][1]}
				b := svgPoint{seg.pts[1][0], seg.pts[1][1]}
				if seg.op == 'Q' {
					points = append(points, start.mul(u*u).add(a.mul(2*u*t)).add(b.mul(t*t)))
				} else {
					c := svgPoint{seg.pts[2][0], seg.pts[2][1]}
					points = append(points, start.mul(u*u*u).add(a.mul(3*u*u*t)).add(b.mul(3*u*t*t)).add(c.mul(t*t*t)))
				}
				used++
			}
		case 'Z':
			flush(true)
		}
	}
	flush(false)
}

func strokeSVGSubpath(r *vector.Rasterizer, pts []svgPoint, closed bool, shape svgShape, m svgAffine) {
	if closed && len(pts) > 2 && pts[len(pts)-1] == pts[0] {
		pts = pts[:len(pts)-1]
	}
	n := len(pts)
	if n < 2 {
		return
	}
	half := shape.width / 2
	count := n - 1
	if closed {
		count = n
	}
	// Unit tangents and left normals; zero-length edges are skipped.
	dirs := make([]svgPoint, count)
	for i := 0; i < count; i++ {
		delta := pts[(i+1)%n].sub(pts[i])
		length := math.Hypot(delta.x, delta.y)
		if length > 0 {
			dirs[i] = delta.mul(1 / length)
		}
	}
	for i, d := range dirs {
		if d == (svgPoint{}) {
			continue
		}
		a, b := pts[i], pts[(i+1)%n]
		if !closed && shape.cap == "square" {
			if i == 0 {
				a = a.sub(d.mul(half))
			}
			if i == count-1 {
				b = b.add(d.mul(half))
			}
		}
		normal := svgPoint{-d.y * half, d.x * half}
		svgStrokePolygon(r, m, a.add(normal), b.add(normal), b.sub(normal), a.sub(normal))
	}
	for i := 0; i < n; i++ {
		if !closed && (i == 0 || i == n-1) {
			if shape.cap == "round" {
				svgStrokeDisk(r, m, pts[i], half)
			}
			continue
		}
		before, after := dirs[(i-1+count)%count], dirs[i%count]
		cross := before.x*after.y - before.y*after.x
		if before == (svgPoint{}) || after == (svgPoint{}) || math.Abs(cross) < 1e-9 {
			continue
		}
		if shape.join == "round" {
			svgStrokeDisk(r, m, pts[i], half)
			continue
		}
		side := -1.0
		if cross < 0 {
			side = 1
		}
		a := svgPoint{-before.y * half * side, before.x * half * side}
		b := svgPoint{-after.y * half * side, after.x * half * side}
		v := pts[i]
		if shape.join == "miter" {
			denom := 1 + before.x*after.x + before.y*after.y
			if denom > 0 {
				tip := a.add(b).mul(1 / denom)
				if math.Hypot(tip.x, tip.y) <= shape.miterLimit*half {
					svgStrokePolygon(r, m, v.add(a), v.add(tip), v.add(b))
					continue
				}
			}
		}
		svgStrokePolygon(r, m, v.add(a), v, v.add(b))
	}
}

func svgStrokePolygon(r *vector.Rasterizer, m svgAffine, pts ...svgPoint) {
	if len(pts) < 3 {
		return
	}
	// Normalize winding in user space so overlapping polygons add rather
	// than cancel under the rasterizer's nonzero winding rule.
	area := 0.0
	for i, p := range pts {
		q := pts[(i+1)%len(pts)]
		area += p.x*q.y - p.y*q.x
	}
	if math.Abs(area) < 1e-12 {
		return
	}
	if area < 0 {
		for i, j := 0, len(pts)-1; i < j; i, j = i+1, j-1 {
			pts[i], pts[j] = pts[j], pts[i]
		}
	}
	for i, p := range pts {
		x, y := m.apply(p.x, p.y)
		if math.IsNaN(x) || math.IsNaN(y) || math.IsInf(x, 0) || math.IsInf(y, 0) ||
			math.Abs(x) > 1e7 || math.Abs(y) > 1e7 {
			return
		}
		if i == 0 {
			r.MoveTo(float32(x), float32(y))
		} else {
			r.LineTo(float32(x), float32(y))
		}
	}
	r.ClosePath()
}

func svgStrokeDisk(r *vector.Rasterizer, m svgAffine, center svgPoint, radius float64) {
	pts := make([]svgPoint, 24)
	for i := range pts {
		a := 2 * math.Pi * float64(i) / float64(len(pts))
		pts[i] = center.add(svgPoint{radius * math.Cos(a), radius * math.Sin(a)})
	}
	svgStrokePolygon(r, m, pts...)
}
