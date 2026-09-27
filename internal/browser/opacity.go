package browser

import (
	"image"
	"image/color"
	"image/draw"
	"math"
	"strings"
)

// elementOpacity returns the used value of an element's opacity property
// (CSS Color 4 §11.2): a number or percentage clamped to [0, 1]. Missing or
// invalid values use the initial value 1. opacity is not inherited; its
// effect on descendants comes from group compositing in paintWithOpacity.
func elementOpacity(style ComputedStyle) float64 {
	value := strings.TrimSpace(style["opacity"])
	if value == "" {
		return 1
	}
	n, ok := svgOpacityValue(value)
	if !ok {
		return 1
	}
	return n
}

// validOpacity reports whether value is a supported opacity declaration for
// @supports: a finite number or percentage (out-of-range values are clamped).
func validOpacity(value string) bool {
	_, ok := svgOpacityValue(value)
	return ok
}

// paintWithOpacity paints a stacking context's whole subtree into an offscreen
// layer and composites it over the canvas at the given opacity, so the group
// fades as a unit (CSS Color 4 §11.2). opacity 0 paints nothing at all. The
// layer covers the current (possibly clipped) canvas rather than the box, since
// descendants may overflow the element's border box.
func (p *painter) paintWithOpacity(opacity float64, paint func()) {
	if opacity <= 0 {
		return
	}
	if opacity >= 1 {
		paint()
		return
	}
	target := p.canvas
	area := target.Bounds()
	if area.Empty() {
		return
	}
	layer := image.NewRGBA(area)
	p.canvas = layer
	paint()
	p.canvas = target
	alpha := uint8(math.Round(opacity * 255))
	draw.DrawMask(target, area, layer, area.Min, image.NewUniform(color.Alpha{A: alpha}), image.Point{}, draw.Over)
}
