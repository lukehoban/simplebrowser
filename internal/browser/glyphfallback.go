package browser

import (
	"image"
	"unicode"
	"unicode/utf8"

	"golang.org/x/image/font"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/math/fixed"
)

// fallbackFamily is the bundled face consulted for runes the selected face has
// no glyph for. DejaVu Sans covers far more of Unicode than the Go fonts (for
// example U+02BC MODIFIER LETTER APOSTROPHE, produced by small-caps ŉ → ʼN),
// and is already embedded for the Verdana stack, so no new data is shipped.
const fallbackFamily = "verdana"

// glyphFallback splits text into runs drawn by the selected face and runs
// drawn by the same-size, same-weight/style DejaVu Sans face. A cluster falls
// back only when the primary font lacks one of its runes and the fallback font
// has all of them, so
// text the primary face fully covers is measured and painted exactly as
// before. Layout (metrics.advance) and paint share eachTextSegment, so both
// see identical runs; kerning, as with small-caps segments, applies within a
// run but not across a face boundary.
//
// Line box ascent/descent still come from the primary face only.
type glyphFallback struct {
	faces              *faceSet
	primary, secondary *opentype.Font
	key, smallKey      faceKey
	monospace          bool
	// Faces are resolved lazily so styles whose text never falls back do not
	// allocate fallback faces.
	face, smallFace         font.Face
	resolved, resolvedSmall bool
}

// glyphFallback returns the fallback for the face selected by key/variant, or
// nil when the selected font already is the fallback font.
func (f *faceSet) glyphFallback(key faceKey, variant fontVariant) *glyphFallback {
	primary := f.fonts[variant]
	if primary == nil {
		primary = f.fonts[fontVariant{family: "sans"}]
	}
	fbVariant := fontVariant{family: fallbackFamily, bold: variant.bold, italic: variant.italic}
	secondary := f.fonts[fbVariant]
	if primary == nil || secondary == nil || primary == secondary {
		return nil
	}
	fb := &glyphFallback{faces: f, primary: primary, secondary: secondary,
		key:       faceKey{size: key.size, family: fallbackFamily, bold: variant.bold, italic: variant.italic},
		monospace: variant.family == "mono"}
	fb.smallKey = fb.key
	fb.smallKey.size = key.size * .8
	return fb
}

// covers reports whether font has a glyph for r, caching per render.
func (f *faceSet) covers(fnt *opentype.Font, r rune) bool {
	k := coverageKey{fnt, r}
	if v, ok := f.coverage[k]; ok {
		return v
	}
	if f.coverage == nil {
		f.coverage = make(map[coverageKey]bool)
	}
	idx, err := fnt.GlyphIndex(&f.glyphBuf, r)
	v := err == nil && idx != 0
	f.coverage[k] = v
	return v
}

type coverageKey struct {
	font *opentype.Font
	r    rune
}

func (g *glyphFallback) faceFor(small bool) font.Face {
	variant := fontVariant{family: fallbackFamily, bold: g.key.bold, italic: g.key.italic}
	if small {
		if !g.resolvedSmall {
			g.smallFace, g.resolvedSmall = g.faces.face(g.smallKey, variant), true
		}
		return g.smallFace
	}
	if !g.resolved {
		g.face, g.resolved = g.faces.face(g.key, variant), true
	}
	return g.face
}

// eachRun visits text as maximal runs of primary-face and fallback-face
// clusters. A cluster is a base rune plus any following combining marks
// (Mn/Me), so a mark is never drawn by a different font than its base.
func (g *glyphFallback) eachRun(face font.Face, small bool, text string, visit func(font.Face, string)) {
	start, inFallback := 0, false
	for i := 0; i < len(text); {
		end := clusterEnd(text, i)
		if need := g.clusterNeedsFallback(text[i:end]); need != inFallback {
			g.visitRun(face, small, inFallback, text[start:i], visit)
			start, inFallback = i, need
		}
		i = end
	}
	g.visitRun(face, small, inFallback, text[start:], visit)
}

// clusterEnd returns the end of the cluster starting at i.
func clusterEnd(text string, i int) int {
	_, n := utf8.DecodeRuneInString(text[i:])
	i += n
	for i < len(text) {
		r, n := utf8.DecodeRuneInString(text[i:])
		if !unicode.In(r, unicode.Mn, unicode.Me) {
			break
		}
		i += n
	}
	return i
}

// clusterNeedsFallback reports whether the primary font misses a rune of the
// cluster while the fallback font covers all of it.
func (g *glyphFallback) clusterNeedsFallback(cluster string) bool {
	missing := false
	for _, r := range cluster {
		if r == utf8.RuneError || !g.faces.covers(g.secondary, r) {
			return false
		}
		if !g.faces.covers(g.primary, r) {
			missing = true
		}
	}
	return missing
}

func (g *glyphFallback) visitRun(face font.Face, small, fallback bool, text string, visit func(font.Face, string)) {
	if text == "" {
		return
	}
	if fallback {
		if fb := g.faceFor(small); fb != nil {
			if g.monospace {
				if advance, ok := face.GlyphAdvance('0'); ok && advance > 0 {
					fb = monospaceFallbackFace{Face: fb, advance: advance}
				}
			}
			face = fb
		}
	}
	visit(face, text)
}

// monospaceFallbackFace draws a fallback face's glyph outlines while advancing
// each spacing glyph by the selected Go Mono face's 1ch width. Combining marks
// retain their zero advance, and kerning is disabled as it is for a monospace
// face. Wrapping the face keeps layout measurement and painting on the same
// font.Face path.
type monospaceFallbackFace struct {
	font.Face
	advance fixed.Int26_6
}

func (f monospaceFallbackFace) Glyph(dot fixed.Point26_6, r rune) (
	dr image.Rectangle, mask image.Image, maskp image.Point, advance fixed.Int26_6, ok bool,
) {
	dr, mask, maskp, advance, ok = f.Face.Glyph(dot, r)
	return dr, mask, maskp, f.fixedAdvance(advance, ok), ok
}

func (f monospaceFallbackFace) GlyphBounds(r rune) (fixed.Rectangle26_6, fixed.Int26_6, bool) {
	bounds, advance, ok := f.Face.GlyphBounds(r)
	return bounds, f.fixedAdvance(advance, ok), ok
}

func (f monospaceFallbackFace) GlyphAdvance(r rune) (fixed.Int26_6, bool) {
	advance, ok := f.Face.GlyphAdvance(r)
	return f.fixedAdvance(advance, ok), ok
}

func (f monospaceFallbackFace) Kern(_, _ rune) fixed.Int26_6 {
	return 0
}

func (f monospaceFallbackFace) fixedAdvance(native fixed.Int26_6, ok bool) fixed.Int26_6 {
	if ok && native != 0 {
		return f.advance
	}
	return native
}
