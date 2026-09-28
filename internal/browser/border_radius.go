package browser

import (
	"image"
	"image/color"
	"image/draw"
	"math"
	"strings"
)

// CSS corner order is top-left, top-right, bottom-right, bottom-left.
type cornerRadius struct{ x, y float64 }

var radiusCorners = [4]string{"top-left", "top-right", "bottom-right", "bottom-left"}

func radiusParts(value string) ([]string, bool) {
	parts, ok := splitCSSComponents(strings.TrimSpace(value))
	if !ok || len(parts) < 1 || len(parts) > 4 {
		return nil, false
	}
	for _, part := range parts {
		if !validRadiusComponent(part) {
			return nil, false
		}
	}
	switch len(parts) {
	case 1:
		return []string{parts[0], parts[0], parts[0], parts[0]}, true
	case 2:
		return []string{parts[0], parts[1], parts[0], parts[1]}, true
	case 3:
		return []string{parts[0], parts[1], parts[2], parts[1]}, true
	}
	return parts, true
}

// splitRadiusHalves finds only the top-level slash separating horizontal and
// vertical radii. A slash inside calc(), such as calc(20px / 2), belongs to
// the math expression instead.
func splitRadiusHalves(value string) ([]string, bool) {
	depth, slash := 0, -1
	var quote byte
	for i := 0; i < len(value); i++ {
		switch {
		case quote != 0:
			if value[i] == '\\' {
				i++
			} else if value[i] == quote {
				quote = 0
			}
		case value[i] == '"' || value[i] == '\'':
			quote = value[i]
		case value[i] == '(':
			depth++
		case value[i] == ')':
			depth--
			if depth < 0 {
				return nil, false
			}
		case value[i] == '/' && depth == 0:
			if slash >= 0 {
				return nil, false
			}
			slash = i
		}
	}
	if depth != 0 || quote != 0 {
		return nil, false
	}
	if slash < 0 {
		return []string{value}, true
	}
	return []string{value[:slash], value[slash+1:]}, true
}

func expandRadius(d Declaration) []Declaration {
	halves, ok := splitRadiusHalves(d.Value)
	if !ok {
		return nil
	}
	x, ok := radiusParts(halves[0])
	if !ok {
		return nil
	}
	y := x
	if len(halves) == 2 {
		y, ok = radiusParts(halves[1])
		if !ok {
			return nil
		}
	}
	result := make([]Declaration, 4)
	for i, corner := range radiusCorners {
		result[i] = Declaration{Property: "border-" + corner + "-radius", Value: x[i] + " " + y[i], Important: d.Important}
	}
	return result
}

func validRadiusComponent(part string) bool {
	if startsWithMathFunction(part) {
		return validSingleCalc("border-radius", part)
	}
	v := classifyValue(part)
	return v.Number >= 0 &&
		(v.Kind == "length" || v.Kind == "percentage" || v.Kind == "number" && v.Number == 0)
}

func validCornerRadius(value string) bool {
	parts, ok := splitCSSComponents(value)
	if !ok || len(parts) < 1 || len(parts) > 2 {
		return false
	}
	for _, part := range parts {
		if !validRadiusComponent(part) {
			return false
		}
	}
	return true
}

func usedRadii(style ComputedStyle, rect image.Rectangle) [4]cornerRadius {
	var result [4]cornerRadius
	w, h := float64(rect.Dx()), float64(rect.Dy())
	for i, corner := range radiusCorners {
		parts, ok := splitCSSComponents(style["border-"+corner+"-radius"])
		if !ok || len(parts) == 0 {
			continue
		}
		result[i].x = max(0, px(parts[0], w, 0))
		y := parts[0]
		if len(parts) > 1 {
			y = parts[1]
		}
		result[i].y = max(0, px(y, h, 0))
		// A zero in either axis makes this corner square.
		if result[i].x == 0 || result[i].y == 0 {
			result[i] = cornerRadius{}
		}
	}
	// CSS Backgrounds §4.5: uniformly scale all corners if their sums
	// exceed any side, preserving their aspect ratios.
	scale := 1.0
	for _, side := range []struct{ sum, length float64 }{
		{result[0].x + result[1].x, w}, {result[3].x + result[2].x, w},
		{result[0].y + result[3].y, h}, {result[1].y + result[2].y, h},
	} {
		if side.sum > 0 {
			scale = min(scale, side.length/side.sum)
		}
	}
	for i := range result {
		result[i].x *= scale
		result[i].y *= scale
	}
	return result
}

func hasRadius(r [4]cornerRadius) bool {
	for _, c := range r {
		if c.x > 0 && c.y > 0 {
			return true
		}
	}
	return false
}

// insideCorners uses pixel centers so the zero-radius path remains identical
// to the existing rectangular rasterization.
func insideCorners(x, y float64, rect image.Rectangle, r [4]cornerRadius) bool {
	if x < float64(rect.Min.X) || x >= float64(rect.Max.X) || y < float64(rect.Min.Y) || y >= float64(rect.Max.Y) {
		return false
	}
	for i, c := range r {
		if c.x <= 0 || c.y <= 0 {
			continue
		}
		cx, cy := float64(rect.Min.X)+c.x, float64(rect.Min.Y)+c.y
		switch i {
		case 1:
			cx = float64(rect.Max.X) - c.x
		case 2:
			cx, cy = float64(rect.Max.X)-c.x, float64(rect.Max.Y)-c.y
		case 3:
			cy = float64(rect.Max.Y) - c.y
		}
		if math.Abs(x-cx) > c.x || math.Abs(y-cy) > c.y {
			continue
		}
		if (i == 0 && x < cx && y < cy) || (i == 1 && x > cx && y < cy) ||
			(i == 2 && x > cx && y > cy) || (i == 3 && x < cx && y > cy) {
			dx, dy := (x-cx)/c.x, (y-cy)/c.y
			if dx*dx+dy*dy > 1 {
				return false
			}
		}
	}
	return true
}

// paintRoundedBox composites just the box's own background; descendants and
// text are deliberately not clipped by border-radius.
func paintRoundedBox(dst *image.RGBA, rect image.Rectangle, style ComputedStyle, widths [4]int, radii [4]cornerRadius, background func(*image.RGBA)) {
	area := rect.Intersect(dst.Bounds())
	if area.Empty() {
		return
	}
	layer := image.NewRGBA(area)
	background(layer)
	inner := image.Rect(rect.Min.X+widths[3], rect.Min.Y+widths[0],
		rect.Max.X-widths[1], rect.Max.Y-widths[2])
	inset := [4]cornerRadius{}
	for i, r := range radii {
		left, right, top, bottom := float64(widths[3]), float64(widths[1]), float64(widths[0]), float64(widths[2])
		if i == 1 || i == 2 {
			left = right
		}
		if i == 2 || i == 3 {
			top = bottom
		}
		inset[i] = cornerRadius{max(0, r.x-left), max(0, r.y-top)}
	}
	colors := [4]color.RGBA{borderColor(style, "top"), borderColor(style, "right"), borderColor(style, "bottom"), borderColor(style, "left")}
	for y := area.Min.Y; y < area.Max.Y; y++ {
		for x := area.Min.X; x < area.Max.X; x++ {
			cx, cy := float64(x)+.5, float64(y)+.5
			if !insideCorners(cx, cy, rect, radii) {
				continue
			}
			if !inner.Empty() && insideCorners(cx, cy, inner, inset) {
				draw.Draw(dst, image.Rect(x, y, x+1, y+1), layer, image.Pt(x, y), draw.Over)
				continue
			}
			side := -1
			best := math.Inf(1)
			for i, distance := range [4]float64{cy - float64(rect.Min.Y), float64(rect.Max.X) - cx,
				float64(rect.Max.Y) - cy, cx - float64(rect.Min.X)} {
				if widths[i] > 0 && distance/float64(widths[i]) < best {
					best, side = distance/float64(widths[i]), i
				}
			}
			// Backgrounds extend under the border (background-clip:
			// border-box), so composite the box's own background first and
			// the possibly transparent border color over it.
			draw.Draw(dst, image.Rect(x, y, x+1, y+1), layer, image.Pt(x, y), draw.Over)
			if side >= 0 {
				fill(dst, image.Rect(x, y, x+1, y+1), colors[side])
			}
		}
	}
}
