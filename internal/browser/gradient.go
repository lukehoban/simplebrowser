package browser

import (
	"image"
	"image/color"
	"math"
	"strconv"
	"strings"
)

// gradientFunction only accepts a complete function token. Other CSS image
// functions remain unpainted rather than being misinterpreted as gradients.
func gradientFunction(s string) string {
	s = strings.TrimSpace(s)
	if len(s) > len("linear-gradient()") && strings.EqualFold(s[:16], "linear-gradient(") && s[len(s)-1] == ')' {
		return s
	}
	return ""
}

type gradientStop struct {
	color color.RGBA
	at    float64 // fraction of the gradient line
	set   bool
}

type linearGradient struct {
	bounds image.Rectangle
	dx, dy float64
	stops  []gradientStop
	source string
}

func straight(c color.RGBA) color.NRGBA {
	return color.NRGBA{c.R, c.G, c.B, c.A}
}

func parseGradient(s string, w, h int) *linearGradient {
	if gradientFunction(s) == "" || w < 1 || h < 1 || w > 4096 || h > 4096 {
		return nil
	}
	parts := backgroundLayers(strings.TrimSpace(s)[16 : len(strings.TrimSpace(s))-1])
	if len(parts) < 2 || len(parts) > 33 {
		return nil
	}
	dx, dy := 0.0, 1.0 // CSS default: to bottom
	first := strings.ToLower(strings.TrimSpace(parts[0]))
	if strings.HasPrefix(first, "to ") {
		dx, dy = 0, 0
		words := strings.Fields(first[3:])
		if len(words) < 1 || len(words) > 2 {
			return nil
		}
		for _, word := range words {
			switch word {
			case "left":
				if dx != 0 {
					return nil
				}
				dx = -1
			case "right":
				if dx != 0 {
					return nil
				}
				dx = 1
			case "top":
				if dy != 0 {
					return nil
				}
				dy = -1
			case "bottom":
				if dy != 0 {
					return nil
				}
				dy = 1
			default:
				return nil
			}
		}
		d := math.Hypot(dx, dy)
		dx, dy = dx/d, dy/d
		parts = parts[1:]
	} else if strings.HasSuffix(first, "deg") {
		deg, err := strconv.ParseFloat(strings.TrimSpace(strings.TrimSuffix(first, "deg")), 64)
		if err != nil || math.IsInf(deg, 0) || math.IsNaN(deg) {
			return nil
		}
		a := deg * math.Pi / 180
		dx, dy = math.Sin(a), -math.Cos(a)
		parts = parts[1:]
	}
	if len(parts) < 2 || len(parts) > 32 {
		return nil
	}
	line := math.Abs(dx)*float64(w) + math.Abs(dy)*float64(h)
	stops := make([]gradientStop, 0, len(parts))
	for _, part := range parts {
		tokens := parseValues(strings.TrimSpace(part))
		if len(tokens) < 1 || len(tokens) > 2 {
			return nil
		}
		c, ok := parseColor(strings.ToLower(tokens[0].Text))
		if !ok {
			return nil
		}
		stop := gradientStop{color: c}
		if len(tokens) == 2 {
			pos := strings.ToLower(tokens[1].Text)
			switch {
			case strings.HasSuffix(pos, "%"):
				n, err := strconv.ParseFloat(strings.TrimSuffix(pos, "%"), 64)
				if err != nil {
					return nil
				}
				stop.at = n / 100
			case strings.HasSuffix(pos, "px"):
				n, err := strconv.ParseFloat(strings.TrimSuffix(pos, "px"), 64)
				if err != nil {
					return nil
				}
				stop.at = n / line
			default:
				return nil
			}
			if math.IsNaN(stop.at) || math.IsInf(stop.at, 0) {
				return nil
			}
			stop.set = true
		}
		stops = append(stops, stop)
	}
	if !stops[0].set {
		stops[0].at, stops[0].set = 0, true
	}
	last := len(stops) - 1
	if !stops[last].set {
		stops[last].at, stops[last].set = 1, true
	}
	// Explicit positions may fall outside [0,1], but must not move backwards.
	for i := 1; i < len(stops); i++ {
		if stops[i].set && stops[i].at < stops[i-1].at && stops[i-1].set {
			stops[i].at = stops[i-1].at
		}
	}
	for i := 1; i < len(stops); {
		if stops[i].set {
			i++
			continue
		}
		j := i
		for j < len(stops) && !stops[j].set {
			j++
		}
		start, end := stops[i-1].at, stops[j].at
		if end < start {
			end = start
			stops[j].at = end
		}
		for k := i; k < j; k++ {
			stops[k].at = start + (end-start)*float64(k-i+1)/float64(j-i+1)
			stops[k].set = true
		}
		i = j + 1
	}
	// Clamp explicit stops following an interpolated run.
	for i := 1; i < len(stops); i++ {
		if stops[i].at < stops[i-1].at {
			stops[i].at = stops[i-1].at
		}
	}
	return &linearGradient{image.Rect(0, 0, w, h), dx, dy, stops, s}
}

func (g *linearGradient) Bounds() image.Rectangle { return g.bounds }
func (g *linearGradient) ColorModel() color.Model { return color.NRGBAModel }
func (g *linearGradient) At(x, y int) color.Color {
	if !image.Pt(x, y).In(g.bounds) {
		return color.NRGBA{}
	}
	w, h := float64(g.bounds.Dx()), float64(g.bounds.Dy())
	line := math.Abs(g.dx)*w + math.Abs(g.dy)*h
	t := .5 + (g.dx*(float64(x)+.5-w/2)+g.dy*(float64(y)+.5-h/2))/line
	stops := g.stops
	if t <= stops[0].at {
		return straight(stops[0].color)
	}
	for i := 1; i < len(stops); i++ {
		if t < stops[i].at {
			u := (t - stops[i-1].at) / (stops[i].at - stops[i-1].at)
			a, b := stops[i-1].color, stops[i].color
			alpha := float64(a.A)*(1-u) + float64(b.A)*u
			if alpha == 0 {
				return color.NRGBA{}
			}
			mix := func(x, y uint8) uint8 {
				return uint8(math.Round((float64(x)*float64(a.A)*(1-u) + float64(y)*float64(b.A)*u) / alpha))
			}
			return color.NRGBA{mix(a.R, b.R), mix(a.G, b.G), mix(a.B, b.B), uint8(math.Round(alpha))}
		}
	}
	return straight(stops[len(stops)-1].color)
}
