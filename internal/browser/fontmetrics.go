package browser

import (
	"strings"
	"sync"

	"github.com/lukehoban/simplebrowser/internal/fonts/dejavu"

	"golang.org/x/image/font"
	"golang.org/x/image/font/gofont/gobold"
	"golang.org/x/image/font/gofont/gobolditalic"
	"golang.org/x/image/font/gofont/goitalic"
	"golang.org/x/image/font/gofont/gomono"
	"golang.org/x/image/font/gofont/gomonobold"
	"golang.org/x/image/font/gofont/gomonobolditalic"
	"golang.org/x/image/font/gofont/gomonoitalic"
	"golang.org/x/image/font/gofont/goregular"
	"golang.org/x/image/font/sfnt"
	"golang.org/x/image/math/fixed"
)

// fontRatios holds size-independent metrics, as fractions of the em size,
// used to resolve the CSS ex and ch units (CSS Values 4 §6.1.1).
type fontRatios struct {
	ex, ch float64
}

// fallbackFontRatios is the spec-sanctioned 0.5em used when a face lacks the
// needed metric.
var fallbackFontRatios = fontRatios{ex: 0.5, ch: 0.5}

type fontVariant struct {
	family       string
	bold, italic bool
}

// fontSources lists every embedded face by mapped family ("sans" = Go Sans,
// "mono" = Go Mono, "verdana" = DejaVu Sans) and weight/style.
var fontSources = map[fontVariant][]byte{
	{"sans", false, false}: goregular.TTF, {"sans", true, false}: gobold.TTF,
	{"sans", false, true}: goitalic.TTF, {"sans", true, true}: gobolditalic.TTF,
	{"mono", false, false}: gomono.TTF, {"mono", true, false}: gomonobold.TTF,
	{"mono", false, true}: gomonoitalic.TTF, {"mono", true, true}: gomonobolditalic.TTF,
	{"verdana", false, false}: dejavu.Regular, {"verdana", true, false}: dejavu.Bold,
	{"verdana", false, true}: dejavu.Oblique, {"verdana", true, true}: dejavu.BoldOblique,
}

// styleFontVariant is the single family/weight/style mapping shared by layout
// faces and ex/ch unit resolution.
func styleFontVariant(style ComputedStyle) fontVariant {
	fontStyle := strings.TrimSpace(style["font-style"])
	return fontVariant{family: mappedFontFamily(style["font-family"]), bold: isBold(style["font-weight"]),
		italic: strings.EqualFold(fontStyle, "italic") || strings.EqualFold(fontStyle, "oblique")}
}

var (
	fontRatioOnce  sync.Once
	fontRatioTable map[fontVariant]fontRatios
)

// ratiosFor returns the ex/ch ratios of the face the layout engine selects
// for style (same family/weight/style mapping as faceSet.metrics).
func ratiosFor(style ComputedStyle) fontRatios {
	fontRatioOnce.Do(loadFontRatios)
	key := styleFontVariant(style)
	if r, ok := fontRatioTable[key]; ok {
		return r
	}
	return fallbackFontRatios
}

func loadFontRatios() {
	fontRatioTable = map[fontVariant]fontRatios{}
	for key, data := range fontSources {
		fontRatioTable[key] = measureFontRatios(data)
	}
}

func measureFontRatios(data []byte) fontRatios {
	result := fallbackFontRatios
	f, err := sfnt.Parse(data)
	if err != nil {
		return result
	}
	var buf sfnt.Buffer
	upem := float64(f.UnitsPerEm())
	if upem <= 0 {
		return result
	}
	ppem := fixed.I(int(f.UnitsPerEm()))
	// Prefer the OS/2 sxHeight; otherwise use the ink height of "x".
	if m, err := f.Metrics(&buf, ppem, font.HintingNone); err == nil && m.XHeight > 0 {
		result.ex = float64(m.XHeight) / 64 / upem
	} else if idx, err := f.GlyphIndex(&buf, 'x'); err == nil && idx != 0 {
		if bounds, _, err := f.GlyphBounds(&buf, idx, ppem, font.HintingNone); err == nil && bounds.Min.Y < 0 {
			result.ex = float64(-bounds.Min.Y) / 64 / upem
		}
	}
	if idx, err := f.GlyphIndex(&buf, '0'); err == nil && idx != 0 {
		if advance, err := f.GlyphAdvance(&buf, idx, ppem, font.HintingNone); err == nil && advance > 0 {
			result.ch = float64(advance) / 64 / upem
		}
	}
	return result
}
