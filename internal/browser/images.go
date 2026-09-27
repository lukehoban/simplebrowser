package browser

import (
	"bytes"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"math"
	"strconv"
	"strings"
)

const maxDecodedImagePixels int64 = 16 << 20

// fetchImages loads visible img resources once per render. Individual failures
// are deliberately non-fatal: layout still reserves the dimensions requested
// by HTML/CSS and can display a broken-image placeholder in a later painter.
func fetchImages(document Document, root *StyledNode, fetcher *Fetcher) (map[*Node]image.Image, map[*Node]image.Image) {
	if fetcher == nil {
		fetcher = &Fetcher{}
	}
	images := make(map[*Node]image.Image)
	backgrounds := make(map[*Node]image.Image)
	cache := make(map[string]image.Image)
	visited := make(map[string]bool)
	load := func(base, source string) image.Image {
		target, err := ResolveCSSURL(base, source)
		if err != nil {
			return nil
		}
		if !visited[target] {
			visited[target] = true
			if resource, err := fetcher.Fetch(target); err == nil {
				cache[target] = decodeImage(resource.Body)
			}
		}
		return cache[target]
	}
	var visit func(*StyledNode)
	visit = func(n *StyledNode) {
		if n == nil || n.Node == nil {
			return
		}
		if strings.EqualFold(n.Style["display"], "none") {
			return
		}
		if n.Node.Type == ElementNode && strings.EqualFold(n.Node.Name, "img") &&
			!strings.EqualFold(n.Style["display"], "none") {
			src, ok := n.Node.Attribute("src")
			if ok && strings.TrimSpace(src.Value) != "" {
				images[n.Node] = load(document.BaseURL, src.Value)
			}
		}
		if n.Node.Type == ElementNode {
			if source := backgroundURL(n.Style["background-image"]); source != "" {
				backgrounds[n.Node] = load(document.BaseURL, source)
			}
		}
		for _, child := range n.Children {
			visit(child)
		}
	}
	visit(root)
	return images, backgrounds
}

func decodeImage(data []byte) image.Image {
	config, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil || config.Width <= 0 || config.Height <= 0 ||
		int64(config.Width)*int64(config.Height) > maxDecodedImagePixels {
		return nil
	}
	switch format {
	case "gif", "png", "jpeg":
	default:
		// SVG and unknown formats remain unsupported by design.
		return nil
	}
	decoded, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil
	}
	return decoded
}

// imageDimensions returns the used dimensions of an img replaced element.
// Computed width/height include HTML presentational hints from the cascade.
func imageDimensions(n *StyledNode, decoded image.Image, basis int) (int, int) {
	intrinsicW, intrinsicH := 16, 16
	if decoded != nil {
		bounds := decoded.Bounds()
		if bounds.Dx() > 0 && bounds.Dy() > 0 {
			intrinsicW, intrinsicH = bounds.Dx(), bounds.Dy()
		}
	}
	widthValue, heightValue := "", ""
	if n != nil && n.Style != nil {
		widthValue, heightValue = strings.TrimSpace(n.Style["width"]), strings.TrimSpace(n.Style["height"])
	}
	if widthValue == "" || strings.EqualFold(widthValue, "auto") {
		widthValue = ""
	}
	if heightValue == "" || strings.EqualFold(heightValue, "auto") {
		heightValue = ""
	}
	width, hasWidth := imageDimensionValue(widthValue, basis)
	height, hasHeight := imageDimensionValue(heightValue, basis)
	switch {
	case hasWidth && hasHeight:
	case hasWidth:
		height = int(math.Round(float64(width) * float64(intrinsicH) / float64(intrinsicW)))
	case hasHeight:
		width = int(math.Round(float64(height) * float64(intrinsicW) / float64(intrinsicH)))
	default:
		width, height = intrinsicW, intrinsicH
	}
	return min(max(0, width), 1<<20), min(max(0, height), 1<<20)
}

func imageDimensionValue(value string, basis int) (int, bool) {
	if value == "" {
		return 0, false
	}
	// Legacy HTML dimensions are unitless pixels. CSS values use the same
	// parser as other layout lengths, with percentages relative to the line.
	if _, err := strconv.ParseFloat(value, 64); err == nil {
		value += "px"
	}
	parsed := classifyValue(value)
	switch parsed.Kind {
	case "length", "number":
		result := px(value, float64(basis), math.NaN())
		if math.IsNaN(result) || math.IsInf(result, 0) {
			return 0, false
		}
		return int(math.Round(result)), true
	case "percentage":
		if basis > 0 {
			return int(math.Round(float64(basis) * parsed.Number / 100)), true
		}
	}
	return 0, false
}
