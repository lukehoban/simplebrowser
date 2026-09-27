package browser

import (
	"image"
	"image/draw"
	"strings"
)

// CSS Masking (level 1) subset: mask-image layers built from url() (raster
// or SVG images, including data: URLs) or gradients, positioned and sized with
// mask-position, mask-size and mask-repeat. Every layer uses its alpha channel
// (mask-mode: match-source for images). Layers are composited with the initial
// mask-composite (add), and the mask is positioned in and clipped to the border
// box (the initial mask-origin/mask-clip). References to SVG <mask> elements,
// mask-mode: luminance, and other mask-origin/clip/composite values are not
// implemented; see docs/css-mask.md (#313, #314).

// maskLonghands lists the mask longhands this renderer consumes.
var maskLonghands = []string{"mask-image", "mask-repeat", "mask-position", "mask-size"}

// canonicalMaskProperty maps the widely used -webkit-mask-* aliases to the
// standard property names so that cascade order decides between them.
func canonicalMaskProperty(property string) string {
	if strings.HasPrefix(property, "-webkit-mask") {
		return strings.TrimPrefix(property, "-webkit-")
	}
	return property
}

// validMask accepts the subset of the mask shorthand that expandMask
// represents: the background-like image/position/size/repeat components. Mask
// layers have no color, so a color token makes the shorthand invalid.
func validMask(value string) bool {
	for _, layer := range backgroundLayers(value) {
		for _, token := range parseValues(layer) {
			if token.Kind == "color" {
				return false
			}
		}
	}
	return validBackground(value)
}

// expandMask expands the mask shorthand into the implemented longhands. An
// invalid shorthand yields no declarations so it does not win the cascade.
func expandMask(d Declaration) []Declaration {
	if !validMask(d.Value) {
		return nil
	}
	var result []Declaration
	for _, expanded := range expandBackground(d) {
		if expanded.Property == "background-color" {
			continue
		}
		property := "mask-" + strings.TrimPrefix(expanded.Property, "background-")
		value := expanded.Value
		if property == "mask-position" {
			// Unspecified background layer positions are empty; the mask
			// longhand's initial value is 0% 0%, like background-position.
			layers := backgroundLayers(value)
			for i := range layers {
				if layers[i] == "" {
					layers[i] = "0% 0%"
				}
			}
			value = strings.Join(layers, ", ")
		}
		if property == "mask-size" {
			layers := backgroundLayers(value)
			for i := range layers {
				if layers[i] == "" {
					layers[i] = "auto"
				}
			}
			value = strings.Join(layers, ", ")
		}
		result = append(result, Declaration{Property: property, Value: value, Important: d.Important})
	}
	return result
}

// hasMask reports whether a style's mask-image contains at least one layer
// that is not none. A list of only none layers leaves the element unmasked.
func hasMask(style ComputedStyle) bool {
	value := strings.TrimSpace(style["mask-image"])
	if value == "" {
		return false
	}
	for _, layer := range backgroundLayers(value) {
		if layer != "" && !strings.EqualFold(layer, "none") {
			return true
		}
	}
	return false
}

// maskLayerStyle maps mask layer i onto the background-* keys that
// drawBackgroundImage reads. No border widths are copied, so its positioning
// area is the border box, the initial mask-origin.
func maskLayerStyle(style ComputedStyle, index int) ComputedStyle {
	layer := ComputedStyle{}
	for _, property := range []string{"size", "repeat", "position"} {
		values := backgroundLayers(style["mask-"+property])
		if len(values) > 0 {
			layer["background-"+property] = values[index%len(values)]
		}
	}
	return layer
}

// buildMask renders the element's mask into an alpha image covering area.
// Pixels outside the border box, and every layer whose image failed to load,
// stay transparent: CSS Masking treats such a layer as transparent black.
func buildMask(area image.Rectangle, box *Box, style ComputedStyle, sources []image.Image) *image.Alpha {
	mask := image.NewRGBA(area)
	values := backgroundLayers(style["mask-image"])
	for i := len(values) - 1; i >= 0; i-- {
		var src image.Image
		if i < len(sources) {
			src = sources[i]
		}
		if src == nil && gradientFunction(values[i]) != "" {
			if gradient := parseGradient(values[i], box.Rect.Dx(), box.Rect.Dy()); gradient != nil {
				src = gradient
			}
		}
		// Painting layers with source-over onto transparent pixels yields
		// the union of their alpha, which is mask-composite: add.
		drawBackgroundImage(mask, &Box{Rect: box.Rect}, src, maskLayerStyle(style, i))
	}
	alpha := image.NewAlpha(area)
	for y := area.Min.Y; y < area.Max.Y; y++ {
		for x := area.Min.X; x < area.Max.X; x++ {
			alpha.Pix[alpha.PixOffset(x, y)] = mask.Pix[mask.PixOffset(x, y)+3]
		}
	}
	return alpha
}

// paintMasked paints an element's stacking context into an offscreen layer
// and composites it through the element's mask. The layer only covers the
// border box because the initial mask-clip hides everything outside it.
func (p *painter) paintMasked(box *Box, paint func()) {
	style := p.document.Styles[box.Node]
	target := p.canvas
	area := box.Rect.Intersect(target.Bounds())
	if area.Empty() {
		return
	}
	layer := image.NewRGBA(area)
	p.canvas = layer
	paint()
	p.canvas = target
	mask := buildMask(area, box, style, p.document.MaskImages[box.Node])
	draw.DrawMask(target, area, layer, area.Min, mask, area.Min, draw.Over)
}
