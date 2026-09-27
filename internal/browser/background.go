package browser

import (
	"image"
	"image/draw"
	"math"
	"strings"
)

// backgroundLayers splits top-level commas, preserving commas and parentheses
// inside functions and quoted strings.
func backgroundLayers(s string) []string {
	depth := 0
	var quote byte
	start := 0
	var layers []string
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
			layers = append(layers, strings.TrimSpace(s[start:i]))
			start = i + 1
		}
	}
	return append(layers, strings.TrimSpace(s[start:]))
}

// backgroundURL extracts the URL from one layer, respecting quoted ')' and
// escaped characters rather than relying on a naive closing parenthesis.
func backgroundURL(layer string) string {
	for i := 0; i+4 <= len(layer); i++ {
		if !strings.EqualFold(layer[i:i+4], "url(") || (i > 0 && (cssIdent(layer[i-1]) || layer[i-1] == '-')) {
			continue
		}
		var quote byte
		for j := i + 4; j < len(layer); j++ {
			switch {
			case quote != 0:
				if layer[j] == '\\' && j+1 < len(layer) {
					j++
				} else if layer[j] == quote {
					quote = 0
				}
			case layer[j] == '"' || layer[j] == '\'':
				quote = layer[j]
			case layer[j] == ')':
				return strings.Trim(strings.TrimSpace(layer[i+4:j]), `"'`)
			}
		}
	}
	return ""
}

func resolveBackgroundURL(value, base string) string {
	layers := backgroundLayers(value)
	for i, layer := range layers {
		url := backgroundURL(layer)
		if url == "" {
			continue
		}
		resolved, err := ResolveCSSURL(base, url)
		if err != nil {
			continue
		}
		start := strings.Index(strings.ToLower(layer), "url(")
		// Find the end by matching the extracted URL's quoted closing
		// delimiter, which may include ')' inside the URL itself.
		quote := byte(0)
		end := -1
		for j := start + 4; j < len(layer); j++ {
			if quote != 0 {
				if layer[j] == '\\' && j+1 < len(layer) {
					j++
				} else if layer[j] == quote {
					quote = 0
				}
			} else if layer[j] == '"' || layer[j] == '\'' {
				quote = layer[j]
			} else if layer[j] == ')' {
				end = j
				break
			}
		}
		if end >= 0 {
			layers[i] = layer[:start] + `url("` + resolved + `")` + layer[end+1:]
		}
	}
	return strings.Join(layers, ", ")
}

func expandBackground(d Declaration) []Declaration {
	parts := backgroundLayers(d.Value)
	props := map[string][]string{}
	color := "transparent"
	for i, layer := range parts {
		values := parseValues(layer)
		repeat, position, size := "repeat", "", ""
		imageValue := "none"
		if url := backgroundURL(layer); url != "" {
			imageValue = `url("` + url + `")`
		}
		afterSlash := false
		sizeValues := 0
		for _, v := range values {
			word := strings.ToLower(v.Text)
			// parseValues emits a slash outside functions and quoted strings as its
			// own token, independent of surrounding whitespace.
			if v.Text == "/" {
				afterSlash = true
				continue
			}
			switch {
			case v.Kind == "url":
				// The URL was extracted above, including quoted commas and ')'.
			case v.Kind == "function" && gradientFunction(v.Text) != "":
				imageValue = v.Text
			case v.Kind == "color":
				if i == len(parts)-1 {
					color = v.Text
				}
			case word == "no-repeat" || word == "repeat" || word == "repeat-x" || word == "repeat-y":
				repeat = word
			case afterSlash && sizeValues < 2 &&
				(v.Kind == "length" || v.Kind == "percentage" || word == "auto" ||
					(sizeValues == 0 && (word == "cover" || word == "contain"))):
				if size != "" {
					size += " "
				}
				size += v.Text
				sizeValues++
			case !afterSlash && (word == "left" || word == "right" || word == "top" || word == "bottom" || word == "center" ||
				v.Kind == "length" || v.Kind == "percentage"):
				position += " " + v.Text
			}
		}
		props["background-image"] = append(props["background-image"], imageValue)
		props["background-repeat"] = append(props["background-repeat"], repeat)
		props["background-position"] = append(props["background-position"], strings.TrimSpace(position))
		props["background-size"] = append(props["background-size"], strings.TrimSpace(size))
	}
	result := make([]Declaration, 0, 5)
	for _, name := range []string{"background-color", "background-image", "background-repeat", "background-position", "background-size"} {
		value := strings.Join(props[name], ", ")
		if name == "background-color" {
			value = color
		}
		result = append(result, Declaration{Property: name, Value: value, Important: d.Important})
	}
	return result
}

func backgroundLayerStyle(style ComputedStyle, index int) ComputedStyle {
	layer := cloneStyle(style)
	for _, property := range []string{"background-size", "background-repeat", "background-position"} {
		values := backgroundLayers(style[property])
		if len(values) > 0 {
			layer[property] = values[index%len(values)]
		}
	}
	return layer
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
	if gradient, ok := src.(*linearGradient); ok {
		src = parseGradient(gradient.source, w, h)
		if src == nil {
			return
		}
	}
	if svg, ok := src.(*svgImage); ok {
		// Render vector backgrounds at the tile size; sampling is then 1:1.
		if raster := svg.rasterize(w, h); raster != nil {
			src = raster
		}
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
