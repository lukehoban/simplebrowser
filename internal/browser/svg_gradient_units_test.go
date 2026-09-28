package browser

import (
	"bytes"
	"image/color"
	"testing"
)

const gradientStops = `<stop offset="0" stop-color="red"/><stop offset="1" stop-color="blue"/>`

// The #435 repro: x2="2em" resolves against the gradient's 10px font, so the
// gradient ends at x=20 and the right half is solid blue.
func TestSVGGradientBareUnitsIssueRepro(t *testing.T) {
	img := decodeSVGString(t, svgOpen+`width="40" height="10"><defs>
		<linearGradient id="g" gradientUnits="userSpaceOnUse" style="font-size:10px" x1="0" x2="2em">`+gradientStops+`</linearGradient>
		</defs><rect width="40" height="10" fill="url(#g)"/></svg>`)
	if got := img.RGBAAt(30, 5); got != (color.RGBA{0, 0, 255, 255}) {
		t.Errorf("x=30 = %v, want pure blue past the 20px gradient end", got)
	}
}

// Each case renders the same gradient with a unit-bearing value and with the
// equivalent user-unit value; the pixels must match exactly.
func TestSVGGradientBareUnitsMatchUserUnits(t *testing.T) {
	render := func(defs string) []byte {
		src := svgOpen + `width="40" height="40" style="font-size:20px"><defs>` + defs +
			`</defs><rect width="40" height="40" fill="url(#g)"/></svg>`
		return decodeSVGString(t, src).Pix
	}
	lin := func(attrs string) string {
		return `<linearGradient id="g" gradientUnits="userSpaceOnUse" style="font-size:10px;--len:2em" ` + attrs + `>` + gradientStops + `</linearGradient>`
	}
	rad := func(attrs string) string {
		return `<radialGradient id="g" gradientUnits="userSpaceOnUse" style="font-size:10px" ` + attrs + `>` + gradientStops + `</radialGradient>`
	}
	for _, tc := range []struct{ name, got, want string }{
		{"linear em", lin(`x2="2em"`), lin(`x2="20"`)},
		{"linear uppercase EM", lin(`x2="2EM"`), lin(`x2="20"`)},
		{"linear rem uses root", lin(`x2="1rem"`), lin(`x2="20"`)},
		{"linear pt", lin(`x2="15pt"`), lin(`x2="20"`)},
		{"linear pc", lin(`x2="1.25pc"`), lin(`x2="20"`)},
		{"linear in", lin(`x2="0.25in"`), lin(`x2="24"`)},
		{"linear cm", lin(`x2="0.635cm"`), lin(`x2="24"`)},
		{"linear mm", lin(`x2="6.35mm"`), lin(`x2="24"`)},
		{"linear Q", lin(`x2="25.4Q"`), lin(`x2="24"`)},
		{"linear all coords", lin(`x1="0.5em" y1="3pt" x2="3em" y2="1rem"`), lin(`x1="5" y1="4" x2="30" y2="20"`)},
		{"linear calc", lin(`x2="calc(1em + 10px)"`), lin(`x2="20"`)},
		{"linear calc percent", lin(`x2="calc(50% + 1em)"`), lin(`x2="30"`)},
		{"linear var", lin(`x2="var(--len)"`), lin(`x2="20"`)},
		{"linear invalid unit falls back", lin(`x2="2foo"`), lin(``)},
		{"linear inherited from defs ancestor",
			`<g style="font-size:12px"><linearGradient id="g" gradientUnits="userSpaceOnUse" x2="2em">` + gradientStops + `</linearGradient></g>`,
			lin(`x2="24"`)},
		{"linear href source font",
			`<linearGradient id="base" gradientUnits="userSpaceOnUse" style="font-size:10px" x2="2em">` + gradientStops + `</linearGradient>` +
				`<linearGradient id="g" href="#base" style="font-size:30px"/>`,
			lin(`x2="20"`)},
		{"radial em and pt", rad(`cx="1em" cy="1.5em" r="1.5em" fx="12pt" fy="1rem" fr="3pt"`), rad(`cx="10" cy="15" r="15" fx="16" fy="20" fr="4"`)},
		{"radial calc", rad(`r="calc(1em + 5px)"`), rad(`r="15"`)},
		{"radial invalid unit falls back", rad(`r="1foo"`), rad(``)},
		{"radial href source font",
			`<radialGradient id="base" gradientUnits="userSpaceOnUse" style="font-size:10px" cx="1em" r="2em">` + gradientStops + `</radialGradient>` +
				`<radialGradient id="g" href="#base" style="font-size:30px"/>`,
			rad(`cx="10" r="20"`)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if !bytes.Equal(render(tc.got), render(tc.want)) {
				t.Errorf("%s renders differently from %s", tc.got, tc.want)
			}
		})
	}
}

// In objectBoundingBox units a resolved length is used as a box fraction,
// and percentages inside calc() are fractions of 1, matching patterns.
func TestSVGGradientBareUnitsObjectBoundingBox(t *testing.T) {
	render := func(kind, attrs string) []byte {
		src := svgOpen + `width="40" height="40"><defs><` + kind + ` id="g" style="font-size:0.5px" ` + attrs + `>` + gradientStops +
			`</` + kind + `></defs><rect x="5" y="5" width="30" height="30" fill="url(#g)"/></svg>`
		return decodeSVGString(t, src).Pix
	}
	for _, tc := range []struct{ kind, got, want string }{
		{"linearGradient", `x2="1em"`, `x2="0.5"`},
		{"linearGradient", `x2="calc(50%)"`, `x2="0.5"`},
		{"linearGradient", `x1="calc(25% + 0.25px)" x2="calc(1em + 0.25px)"`, `x1="0.5" x2="0.75"`},
		{"radialGradient", `r="1em"`, `r="0.5"`},
		{"radialGradient", `r="calc(25%)" cx="calc(1em - 0.25px)"`, `r="0.25" cx="0.25"`},
	} {
		if !bytes.Equal(render(tc.kind, tc.got), render(tc.kind, tc.want)) {
			t.Errorf("%s %s renders differently from %s", tc.kind, tc.got, tc.want)
		}
	}
}

// ex and ch depend on measured font ratios, so compare bare units with the
// calc() form rather than hard-coding a ratio.
func TestSVGGradientBareExChMatchCalc(t *testing.T) {
	render := func(v string) []byte {
		src := svgOpen + `width="60" height="10"><defs><linearGradient id="g" gradientUnits="userSpaceOnUse" style="font-size:24px" x2="` + v + `">` +
			gradientStops + `</linearGradient></defs><rect width="60" height="10" fill="url(#g)"/></svg>`
		return decodeSVGString(t, src).Pix
	}
	def := render("100%")
	for _, unit := range []string{"ex", "ch"} {
		bare, calc := render("1"+unit), render("calc(1"+unit+")")
		if !bytes.Equal(bare, calc) {
			t.Errorf("bare 1%s gradient differs from calc(1%s)", unit, unit)
		}
		if bytes.Equal(bare, def) {
			t.Errorf("bare 1%s gradient fell back to the default x2", unit)
		}
	}
}
