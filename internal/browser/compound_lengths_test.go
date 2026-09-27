package browser

import (
	"image"
	"image/color"
	"math"
	"strings"
	"testing"
)

func TestCompoundLengthsComputedAndGeometry(t *testing.T) {
	doc := styledForLayout(t, `<html style="font-size:20px"><body style="margin:0">
		<table id="table" style="font-size:10px;border-spacing:1em 2ex">
			<tr><td style="width:10px;height:10px;background:red"></td></tr>
		</table>
		<div id="border" style="width:10px;height:10px;border:2vw solid red"></div>
		<div id="bg" style="width:120px;height:100px;background:linear-gradient(red,red) no-repeat 2vw 3vh/10vw 10vh"></div>
		</body></html>`)
	for _, tc := range []struct {
		viewport image.Rectangle
		width    float64
		height   float64
	}{
		{image.Rect(0, 0, 800, 600), 800, 600},
		{image.Rect(0, 0, 400, 300), 400, 300},
	} {
		got, err := LayoutWithViewport(doc, tc.viewport)
		if err != nil {
			t.Fatal(err)
		}
		table := styledElementByID(got.Document.StyleRoot, "table").Style
		ex := ratiosFor(table).ex * 20
		for _, p := range []struct{ name, want string }{
			{"border-spacing", "10px " + formatPixels(ex)},
			{"background-position", ""}, // table has no background position
		} {
			if p.want != "" && table[p.name] != p.want {
				t.Errorf("%v table %s = %q, want %q", tc.viewport, p.name, table[p.name], p.want)
			}
		}
		if grid := buildTableGrid(styledElementByID(got.Document.StyleRoot, "table")); grid.spacing != 10 {
			t.Errorf("horizontal table spacing = %d, want 10", grid.spacing)
		}
		border := styledElementByID(got.Document.StyleRoot, "border").Style
		if border["border-left"] != formatPixels(tc.width*.02)+" solid red" ||
			borderWidth(border, "left") != int(math.Round(tc.width*.02)) {
			t.Errorf("border shorthand = %q, width = %d", border["border-left"], borderWidth(border, "left"))
		}
		bg := styledElementByID(got.Document.StyleRoot, "bg").Style
		if bg["background-position"] != formatPixels(tc.width*.02)+" "+formatPixels(tc.height*.03) ||
			bg["background-size"] != formatPixels(tc.width*.1)+" "+formatPixels(tc.height*.1) {
			t.Errorf("background position/size = %q / %q", bg["background-position"], bg["background-size"])
		}
	}
}

func TestCompoundLengthsPreserveOpaqueValuesAndCascade(t *testing.T) {
	for _, tc := range []struct{ property, input, want string }{
		{"background-position", `left 2ch, right 1vw`, `left 10px, right 8px`},
		{"background-size", `auto 2em, 10vmin 5vmax`, `auto 20px, 60px 40px`},
		{"border-top", `1em solid rgb(2, 3, 4)`, `10px solid rgb(2, 3, 4)`},
		{"background-position", `url("1em 3vw") 2vw`, `url("1em 3vw") 16px`},
		{"background-position", `"2em 3vw" 1em`, `"2em 3vw" 10px`},
		{"background-size", `var(--x, 1em) 2vw`, `var(--x, 1em) 16px`},
	} {
		values := ComputedStyle{"font-size": "10px", "font-family": "monospace", tc.property: tc.input}
		resolveViewportRelativeValues(values, image.Pt(800, 600))
		resolveFontRelativeValues(values, 20)
		if tc.property == "background-position" && strings.HasPrefix(tc.input, "left") {
			tc.want = "left " + formatPixels(20*ratiosFor(values).ch) + ", right 8px"
		}
		if values[tc.property] != tc.want {
			t.Errorf("%s: got %q, want %q", tc.property, values[tc.property], tc.want)
		}
	}
	doc := styledForLayout(t, `<style>#target { background-position:1em 2ch !important }</style>
		<div id="target" style="font-size:10px;background-position:20vw 20vh"></div>`)
	style := styledElementByID(doc.StyleRoot, "target").Style
	if !strings.HasPrefix(style["background-position"], "10px ") {
		t.Errorf("important rule lost to inline declaration: %q", style["background-position"])
	}
}

func TestCompoundBackgroundPixels(t *testing.T) {
	img := painted(t, `<div style="margin:0;width:120px;height:100px;
		background:linear-gradient(red,red) no-repeat 2vw 3vh/10vw 10vh;
		background-color:green"></div>`, image.Rect(0, 0, 800, 600))
	pixel(t, img, 15, 18, color.RGBA{0, 128, 0, 255})
	pixel(t, img, 16, 18, color.RGBA{255, 0, 0, 255})
	pixel(t, img, 95, 77, color.RGBA{255, 0, 0, 255})
	pixel(t, img, 96, 78, color.RGBA{0, 128, 0, 255})
}
