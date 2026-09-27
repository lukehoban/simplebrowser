package browser

import (
	"image"
	"image/draw"
	"math"
	"strconv"
	"strings"
	"unicode/utf8"
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
		case s[i] == '\\' && i+1 < len(s):
			i++
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

// backgroundURLToken locates a URL function outside strings and returns its
// complete bounds and decoded argument. A ')' inside a quoted or escaped
// filename is not the end of the function.
func backgroundURLToken(layer string) (start, end int, value string) {
	var outerQuote byte
	for i := 0; i+4 <= len(layer); i++ {
		if outerQuote != 0 {
			if layer[i] == '\\' && i+1 < len(layer) {
				i++
			} else if layer[i] == outerQuote {
				outerQuote = 0
			}
			continue
		}
		if layer[i] == '"' || layer[i] == '\'' {
			outerQuote = layer[i]
			continue
		}
		if layer[i] == '\\' && i+1 < len(layer) {
			i++
			continue
		}
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
			case layer[j] == '\\' && j+1 < len(layer):
				j++
			case layer[j] == '"' || layer[j] == '\'':
				quote = layer[j]
			case layer[j] == ')':
				arg := strings.TrimSpace(layer[i+4 : j])
				if len(arg) >= 2 && (arg[0] == '"' || arg[0] == '\'') && arg[len(arg)-1] == arg[0] {
					arg = arg[1 : len(arg)-1]
				}
				return i, j + 1, unescapeBackgroundURL(arg)
			}
		}
		return 0, 0, ""
	}
	return 0, 0, ""
}

// CSS escapes in url() refer to filename characters, not URL percent escapes.
func unescapeBackgroundURL(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] != '\\' || i+1 == len(s) {
			b.WriteByte(s[i])
			continue
		}
		i++
		start := i
		for i < len(s) && i-start < 6 && ((s[i] >= '0' && s[i] <= '9') || (s[i] >= 'a' && s[i] <= 'f') || (s[i] >= 'A' && s[i] <= 'F')) {
			i++
		}
		if i > start {
			n, _ := strconv.ParseInt(s[start:i], 16, 32)
			if n == 0 || !utf8.ValidRune(rune(n)) {
				n = utf8.RuneError
			}
			b.WriteRune(rune(n))
			if i == len(s) || !cssSpace(s[i]) {
				i--
			}
			continue
		}
		if s[i] != '\n' && s[i] != '\r' {
			b.WriteByte(s[i])
		}
	}
	return b.String()
}

func backgroundURL(layer string) string {
	_, _, url := backgroundURLToken(layer)
	return url
}

func quotedBackgroundURL(url string) string {
	return `url("` + strings.ReplaceAll(strings.ReplaceAll(url, `\`, `\\`), `"`, `\"`) + `")`
}

func resolveBackgroundURL(value, base string) string {
	layers := backgroundLayers(value)
	for i, layer := range layers {
		start, end, url := backgroundURLToken(layer)
		if url == "" {
			continue
		}

		// url.Parse rejects literal quotes; escape these as URL path characters
		// after interpreting CSS escapes but before resolving the reference.
		resolved, err := ResolveCSSURL(base, strings.ReplaceAll(url, `"`, "%22"))
		if err != nil {
			continue
		}
		layers[i] = layer[:start] + quotedBackgroundURL(resolved) + layer[end:]
	}
	return strings.Join(layers, ", ")
}

// URL tokens in a custom property can be embedded in var() fallbacks or in
// multi-layer values. Resolve all of them at declaration time, rather than
// using the document URL after substitution into background-image.
func resolveCustomPropertyURLs(value, base string) string {
	var out strings.Builder
	for len(value) > 0 {
		start, end, url := backgroundURLToken(value)
		if url == "" || end == 0 {
			out.WriteString(value)
			break
		}
		out.WriteString(value[:start])
		resolved, err := ResolveCSSURL(base, strings.ReplaceAll(url, `"`, "%22"))
		if err != nil {
			out.WriteString(value[start:end])
		} else {
			out.WriteString(quotedBackgroundURL(resolved))
		}
		value = value[end:]
	}
	return out.String()
}

// A calc() token is a length/percentage component, not an image function.
// Keep it intact while expanding the position/size portions of a shorthand.
func backgroundMathComponent(v CSSValue) bool {
	if v.Kind != "function" || !strings.HasPrefix(strings.ToLower(v.Text), "calc(") {
		return false
	}
	result, ok := parseCSSMathFunction(v.Text, func(n float64, unit string) (cssMathValue, bool) {
		switch unit {
		case "%":
			return cssMathValue{percent: n / 100}, true
		case "px", "em", "rem", "ex", "ch", "vw", "vh":
			return cssMathValue{px: n}, true
		}
		return cssMathValue{}, false
	})
	return ok && !result.number
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
			imageValue = quotedBackgroundURL(url)
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
				(v.Kind == "length" || v.Kind == "percentage" || backgroundMathComponent(v) || word == "auto" ||
					(sizeValues == 0 && (word == "cover" || word == "contain"))):
				if size != "" {
					size += " "
				}
				size += v.Text
				sizeValues++
			case !afterSlash && (word == "left" || word == "right" || word == "top" || word == "bottom" || word == "center" ||
				v.Kind == "length" || v.Kind == "percentage" || backgroundMathComponent(v)):
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

// validBackground rejects unrecognized tokens instead of letting the
// shorthand expander silently discard them. This matters after var()
// substitution: an invalid shorthand must invalidate all of its longhands.
func validBackground(value string) bool {
	layers := backgroundLayers(value)
	for layerIndex, layer := range layers {
		if strings.TrimSpace(layer) == "" {
			return false
		}
		seenImage, seenRepeat, seenColor, afterSlash, sizeDone := false, false, false, false, false
		positionCount, sizeCount := 0, 0
		for _, token := range parseValues(layer) {
			word := strings.ToLower(token.Text)
			// <bg-size> ends at the first token that cannot extend it, so
			// "center / 20px no-repeat" continues with the other components.
			if afterSlash && sizeCount > 0 && !(token.Kind == "length" || token.Kind == "percentage" || backgroundMathComponent(token) || word == "auto") {
				afterSlash, sizeDone = false, true
			}
			switch {
			case token.Text == "/":
				if afterSlash || sizeDone || positionCount == 0 {
					return false
				}
				afterSlash = true
			case token.Kind == "url" || token.Kind == "function" && gradientFunction(token.Text) != "" ||
				word == "none":
				if afterSlash || seenImage {
					return false
				}
				seenImage = true
			case token.Kind == "color":
				if afterSlash || seenColor || layerIndex != len(layers)-1 {
					return false
				}
				seenColor = true
			case word == "repeat" || word == "no-repeat" || word == "repeat-x" || word == "repeat-y":
				if afterSlash || seenRepeat {
					return false
				}
				seenRepeat = true
			case afterSlash && (token.Kind == "length" || token.Kind == "percentage" || backgroundMathComponent(token) || word == "auto" ||
				sizeCount == 0 && (word == "cover" || word == "contain")):
				sizeCount++
				if sizeCount > 2 {
					return false
				}
			case !afterSlash && !sizeDone && (word == "left" || word == "right" || word == "top" ||
				word == "bottom" || word == "center" || token.Kind == "length" || token.Kind == "percentage" || backgroundMathComponent(token)):
				positionCount++
				if positionCount > 2 {
					return false
				}
			default:
				return false
			}
		}
		if afterSlash && sizeCount == 0 {
			return false
		}
	}
	return true
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
	if len(s) > 5 && strings.EqualFold(s[:5], "calc(") {
		// Computed mixed lengths (e.g. calc(50% + 2px)) resolve against the
		// positioning area here, at used-value time.
		if v, ok := evaluateComputedCalc(s, float64(basis)); ok && v >= float64(math.MinInt32) && v <= float64(math.MaxInt32) {
			return int(math.Round(v)), true
		}
		return 0, false
	}
	return imageDimensionValue(s, basis)
}

// backgroundPositionAxes resolves the one- and two-value forms to their
// respective axes before converting either to a pixel offset.
func backgroundPositionAxes(words []string) (x, y string) {
	x, y = "left", "top" // CSS's initial position is 0% 0%.
	if len(words) == 0 {
		return
	}
	if len(words) == 1 {
		switch words[0] {
		case "left", "right":
			return words[0], "center"
		case "top", "bottom":
			return "center", words[0]
		case "center":
			return "center", "center"
		default:
			return words[0], "center"
		}
	}
	// A vertical keyword followed by a horizontal value (or center followed
	// by a horizontal keyword) is the swapped two-value syntax.
	if words[0] == "top" || words[0] == "bottom" ||
		(words[0] == "center" && (words[1] == "left" || words[1] == "right")) {
		return words[1], words[0]
	}
	return words[0], words[1]
}

func backgroundAxis(word string, free int) int {
	switch word {
	case "center":
		return int(math.Round(float64(free) / 2))
	case "right", "bottom":
		return free
	case "left", "top":
		return 0
	}
	if strings.HasSuffix(word, "%") {
		// Unlike image dimensions, the available positioning space can be
		// negative when the tile is larger than the padding box.
		if pct, err := strconv.ParseFloat(strings.TrimSuffix(word, "%"), 64); err == nil {
			offset := math.Round(float64(free) * pct / 100)
			if !math.IsNaN(offset) && !math.IsInf(offset, 0) &&
				offset >= float64(math.MinInt) && offset <= float64(math.MaxInt) {
				return int(offset)
			}
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
	size, _ := splitCSSComponents(style["background-size"])
	if len(size) > 0 {
		a, aok := backgroundLength(size[0], area.Dx())
		b, bok := 0, false
		if len(size) > 1 {
			b, bok = backgroundLength(size[1], area.Dy())
		}
		switch {
		case (strings.EqualFold(size[0], "contain") || strings.EqualFold(size[0], "cover")) && w > 0 && h > 0:
			// The image fills one dimension of the positioning area while
			// preserving its intrinsic aspect ratio. Cover fills both axes.
			scaleX := float64(area.Dx()) / float64(w)
			scaleY := float64(area.Dy()) / float64(h)
			scale := math.Min(scaleX, scaleY)
			if strings.EqualFold(size[0], "cover") {
				scale = math.Max(scaleX, scaleY)
			}
			w, h = int(math.Round(float64(w)*scale)), int(math.Round(float64(h)*scale))
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
	words, _ := splitCSSComponents(strings.ToLower(style["background-position"]))
	xPosition, yPosition := backgroundPositionAxes(words)
	x := area.Min.X + backgroundAxis(xPosition, area.Dx()-w)
	y := area.Min.Y + backgroundAxis(yPosition, area.Dy()-h)
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
