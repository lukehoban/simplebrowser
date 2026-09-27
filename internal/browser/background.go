package browser

import (
	"image"
	"image/draw"
	"math"
	"strings"
)

// firstBackgroundLayer returns the first comma-separated layer, ignoring
// commas in functions and quoted URLs. Additional layers/gradients are not
// painted yet.
func firstBackgroundLayer(s string) string {
	depth := 0
	var quote byte
	for i := 0; i < len(s); i++ {
		switch {
		case quote != 0:
			if s[i] == '\\' && i+1 < len(s) {
				i++
			} else if s[i] == quote {
				quote = 0
			}
		case s[i] == '"' || s[i] == '\'':
			quote = s[i]
		case s[i] == '(':
			depth++
		case s[i] == ')' && depth > 0:
			depth--
		case s[i] == ',' && depth == 0:
			return strings.TrimSpace(s[:i])
		}
	}
	return strings.TrimSpace(s)
}

func backgroundURL(value string) string {
	for _, v := range parseValues(firstBackgroundLayer(value)) {
		if v.Kind == "url" {
			return v.Text
		}
	}
	return ""
}

func resolveBackgroundURL(value, base string) string {
	url := backgroundURL(value)
	if url == "" {
		return value
	}
	resolved, err := ResolveCSSURL(base, url)
	if err != nil {
		return value
	}
	// Replace only the first URL; retain the rest of the shorthand verbatim.
	lower := strings.ToLower(value)
	start := strings.Index(lower, "url(")
	if start < 0 {
		return value
	}
	end := strings.IndexByte(value[start+4:], ')')
	if end < 0 {
		return value
	}
	return value[:start] + `url("` + resolved + `")` + value[start+4+end+1:]
}

func expandBackground(d Declaration) []Declaration {
	layer := firstBackgroundLayer(d.Value)
	values := parseValues(layer)
	color, repeat, position, size := "transparent", "repeat", "", ""
	imageValue := "none"
	// parseValues treats '/' as a delimiter; split position/size first.
	if slash := strings.Index(layer, " / "); slash >= 0 {
		values = parseValues(layer[:slash])
		size = strings.TrimSpace(layer[slash+3:])
	}
	for _, v := range values {
		word := strings.ToLower(v.Text)
		switch {
		case v.Kind == "url":
			imageValue = `url("` + v.Text + `")`
		case v.Kind == "color":
			color = v.Text
		case word == "no-repeat" || word == "repeat" || word == "repeat-x" || word == "repeat-y":
			repeat = word
		case word == "left" || word == "right" || word == "top" || word == "bottom" || word == "center" ||
			v.Kind == "length" || v.Kind == "percentage":
			position += " " + v.Text
		}
	}
	props := map[string]string{
		"background-color": color, "background-image": imageValue,
		"background-repeat": repeat, "background-position": strings.TrimSpace(position),
		"background-size": strings.TrimSpace(size),
	}
	result := make([]Declaration, 0, len(props))
	for _, name := range []string{"background-color", "background-image", "background-repeat", "background-position", "background-size"} {
		result = append(result, Declaration{Property: name, Value: props[name], Important: d.Important})
	}
	return result
}

func backgroundLength(s string, basis int) (int, bool) {
	if strings.EqualFold(s, "auto") {
		return 0, false
	}
	return imageDimensionValue(s, basis)
}

func backgroundAxis(words []string, horizontal bool, free int) int {
	if len(words) == 0 {
		return 0
	}
	word := words[0]
	if len(words) > 1 {
		word = words[0]
		if !horizontal {
			word = words[1]
		} else if word == "top" || word == "bottom" {
			word = words[1]
		}
	} else if (horizontal && (word == "top" || word == "bottom")) ||
		(!horizontal && (word == "left" || word == "right")) {
		return 0
	}
	switch word {
	case "center":
		return free / 2
	case "right", "bottom":
		return free
	case "left", "top":
		return 0
	}
	if strings.HasSuffix(word, "%") {
		if v, ok := imageDimensionValue(word, free); ok {
			return v
		}
	}
	if v, ok := backgroundLength(word, free); ok {
		return v
	}
	return 0
}

func drawBackgroundImage(dst *image.RGBA, box *Box, src image.Image, style ComputedStyle) {
	if src == nil || box == nil {
		return
	}
	// Background positioning and clipping both use the padding box.
	area := box.Rect
	area.Min.X += borderWidth(style, "left")
	area.Min.Y += borderWidth(style, "top")
	area.Max.X -= borderWidth(style, "right")
	area.Max.Y -= borderWidth(style, "bottom")
	clip := area.Intersect(dst.Bounds())
	if clip.Empty() {
		return
	}
	w, h := src.Bounds().Dx(), src.Bounds().Dy()
	size := strings.Fields(style["background-size"])
	if len(size) > 0 {
		a, aok := backgroundLength(size[0], area.Dx())
		b, bok := 0, false
		if len(size) > 1 {
			b, bok = backgroundLength(size[1], area.Dy())
		}
		switch {
		case aok && bok:
			w, h = a, b
		case aok && w > 0:
			h, w = int(math.Round(float64(h)*float64(a)/float64(w))), a
		case bok && h > 0:
			w, h = int(math.Round(float64(w)*float64(b)/float64(h))), b
		}
	}
	if w <= 0 || h <= 0 || w > 4096 || h > 4096 {
		return
	}
	words := strings.Fields(strings.ToLower(style["background-position"]))
	x := area.Min.X + backgroundAxis(words, true, area.Dx()-w)
	y := area.Min.Y + backgroundAxis(words, false, area.Dy()-h)
	repeatX, repeatY := true, true
	switch strings.ToLower(style["background-repeat"]) {
	case "no-repeat":
		repeatX, repeatY = false, false
	case "repeat-x":
		repeatY = false
	case "repeat-y":
		repeatX = false
	}
	if repeatX {
		x += int(math.Floor(float64(clip.Min.X-x)/float64(w))) * w
	}
	if repeatY {
		y += int(math.Floor(float64(clip.Min.Y-y)/float64(h))) * h
	}
	for yy := y; yy < clip.Max.Y; yy += h {
		for xx := x; xx < clip.Max.X; xx += w {
			target := image.Rect(xx, yy, xx+w, yy+h).Intersect(clip)
			if !target.Empty() {
				// Nearest-neighbor sampling is deterministic and preserves pixel art.
				for py := target.Min.Y; py < target.Max.Y; py++ {
					for px := target.Min.X; px < target.Max.X; px++ {
						sx := src.Bounds().Min.X + (px-xx)*src.Bounds().Dx()/w
						sy := src.Bounds().Min.Y + (py-yy)*src.Bounds().Dy()/h
						draw.Draw(dst, image.Rect(px, py, px+1, py+1), src, image.Pt(sx, sy), draw.Over)
					}
				}
			}
			if !repeatX {
				break
			}
		}
		if !repeatY {
			break
		}
	}
}
