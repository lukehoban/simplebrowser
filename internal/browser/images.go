package browser

import (
	"bytes"
	"encoding/base64"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"math"
	"mime"
	"strconv"
	"strings"
)

const maxDecodedImagePixels int64 = 16 << 20
const maxDataImageBytes = 16 << 20
const maxDataURLHeaderBytes = 4 << 10

// fetchImages loads visible img resources once per render. Individual failures
// are deliberately non-fatal: layout still reserves the dimensions requested
// by HTML/CSS and can display a broken-image placeholder in a later painter.
func fetchImages(document Document, root *StyledNode, fetcher *Fetcher) (map[*Node]image.Image, map[*Node][]image.Image, map[*Node][]image.Image) {
	if fetcher == nil {
		fetcher = &Fetcher{}
	}
	images := make(map[*Node]image.Image)
	backgrounds := make(map[*Node][]image.Image)
	masks := make(map[*Node][]image.Image)
	cache := make(map[string]image.Image)
	visited := make(map[string]bool)
	load := func(base, source string) image.Image {
		source = strings.TrimSpace(source)
		if data, ok := decodeDataImageURL(source); ok {
			return decodeImage(data)
		}
		// Never hand a malformed or unsupported data URL to the network
		// fetcher as a fallback. Data URLs are local resources, even on error.
		if hasDataURLScheme(source) {
			return nil
		}
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
		if n.Node.Type == ElementNode && n.Style["background-image"] != "" &&
			!strings.EqualFold(strings.TrimSpace(n.Style["background-image"]), "none") {
			for _, layer := range backgroundLayers(n.Style["background-image"]) {
				if source := backgroundURL(layer); source != "" {
					backgrounds[n.Node] = append(backgrounds[n.Node], load(document.BaseURL, source))
				} else {
					backgrounds[n.Node] = append(backgrounds[n.Node], nil)
				}
			}
		}
		if n.Node.Type == ElementNode && hasMask(n.Style) {
			for _, layer := range backgroundLayers(n.Style["mask-image"]) {
				var decoded image.Image
				if source := backgroundURL(layer); source != "" {
					decoded = load(document.BaseURL, source)
				}
				masks[n.Node] = append(masks[n.Node], decoded)
			}
		}
		for _, child := range n.Children {
			visit(child)
		}
	}
	visit(root)
	return images, backgrounds, masks
}

// decodeDataImageURL decodes supported image data URLs without network access.
func decodeDataImageURL(source string) ([]byte, bool) {
	if len(source) > maxDataImageBytes*2 {
		return nil, false
	}
	if !hasDataURLScheme(source) {
		return nil, false
	}
	// A raw # starts the URL fragment, which is not part of the data payload.
	if fragment := strings.IndexByte(source, '#'); fragment >= 0 {
		source = source[:fragment]
	}
	comma := strings.IndexByte(source, ',')
	if comma < 0 {
		return nil, false
	}
	header, payload := source[len("data:"):comma], source[comma+1:]
	if len(header) > maxDataURLHeaderBytes {
		return nil, false
	}
	base64Encoded := false
	if separator := strings.LastIndexByte(header, ';'); separator >= 0 &&
		strings.EqualFold(header[separator+1:], "base64") {
		base64Encoded = true
		header = header[:separator]
	}

	mediaType, _, err := mime.ParseMediaType(header)
	legacySVGUTF8 := false
	if err != nil {
		// Accept the historical data:image/svg+xml;utf8 spelling used by
		// existing pages, while requiring ordinary parameters to be valid MIME.
		// Some legacy CSS embeds the SVG as raw text, including literal spaces.
		if !strings.EqualFold(header, "image/svg+xml;utf8") {
			return nil, false
		}
		mediaType = "image/svg+xml"
		legacySVGUTF8 = true
	}
	switch strings.ToLower(mediaType) {
	case "image/svg+xml", "image/png", "image/jpeg", "image/gif":
	default:
		return nil, false
	}
	if base64Encoded {
		encodedLimit := base64.StdEncoding.EncodedLen(maxDataImageBytes)
		encoded, ok := percentDecodeBounded(payload, encodedLimit, false)
		if !ok || len(encoded) > encodedLimit ||
			base64.StdEncoding.DecodedLen(len(encoded)) > maxDataImageBytes {
			return nil, false
		}
		// Strict base64 still ignores CR and LF. Reject all decoded control
		// characters in the base64 text before handing it to the decoder.
		for _, b := range encoded {
			if b < 0x20 || b == 0x7f {
				return nil, false
			}
		}
		decoded := make([]byte, base64.StdEncoding.DecodedLen(len(encoded)))
		n, err := base64.StdEncoding.Strict().Decode(decoded, encoded)
		if err != nil {
			return nil, false
		}
		return decoded[:n], true
	}
	return percentDecodeBounded(payload, maxDataImageBytes, legacySVGUTF8)
}

func hasDataURLScheme(source string) bool {
	return len(source) >= len("data:") && strings.EqualFold(source[:len("data:")], "data:")
}

// percentDecodeBounded validates escapes and computes the decoded length
// before allocating the output buffer.
func percentDecodeBounded(source string, limit int, allowSpaces bool) ([]byte, bool) {
	length := 0
	for i := 0; i < len(source); i++ {
		if source[i] == '%' {
			if i+2 >= len(source) || fromHex(source[i+1]) < 0 || fromHex(source[i+2]) < 0 {
				return nil, false
			}
			i += 2
		} else if (source[i] < 0x21 && !(allowSpaces && source[i] == ' ')) || source[i] > 0x7e {
			// Control whitespace and non-ASCII bytes must be percent-encoded;
			// literal spaces are allowed only for legacy UTF-8 SVG payloads.
			return nil, false
		}
		length++
		if length > limit {
			return nil, false
		}
	}
	decoded := make([]byte, length)
	n := 0
	for i := 0; i < len(source); i++ {
		if source[i] == '%' {
			decoded[n] = byte(fromHex(source[i+1])<<4 | fromHex(source[i+2]))
			i += 2
		} else {
			decoded[n] = source[i]
		}
		n++
	}
	return decoded, true
}

func fromHex(b byte) int {
	switch {
	case b >= '0' && b <= '9':
		return int(b - '0')
	case b >= 'a' && b <= 'f':
		return int(b-'a') + 10
	case b >= 'A' && b <= 'F':
		return int(b-'A') + 10
	default:
		return -1
	}
}

func decodeImage(data []byte) image.Image {
	config, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil && looksLikeSVG(data) {
		// The minimal SVG subset (svg.go); unsupported or malformed SVG
		// returns nil so callers keep their placeholder behavior.
		if svg, err := decodeSVG(data); err == nil {
			return svg
		}
		return nil
	}
	if err != nil || config.Width <= 0 || config.Height <= 0 ||
		int64(config.Width)*int64(config.Height) > maxDecodedImagePixels {
		return nil
	}
	switch format {
	case "gif", "png", "jpeg":
	default:
		// Unknown raster formats remain unsupported by design.
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
