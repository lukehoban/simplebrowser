package browser

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"path/filepath"
	"testing"
)

func TestColorKeywordTokens(t *testing.T) {
	green := color.RGBA{0, 128, 0, 255}
	for _, input := range []string{
		"green", "GrEeN", `g\re\45n`, `\67reen`, `\000067reen`,
		`\67 reen`, "\\67\treen", "\\67\r\nreen", `\47 REEN`,
		`gree\6e`, `gree\00006e`, `gree\6e `,
	} {
		t.Run(input, func(t *testing.T) {
			if got, ok := parseColor(input); !ok || got != green {
				t.Fatalf("parseColor(%q) = %v, %v; want green", input, got, ok)
			}
			values := parseValues(input)
			if len(values) != 1 || values[0].Kind != "color" || values[0].Color != green {
				t.Fatalf("parseValues(%q) = %+v; want one green color token", input, values)
			}
		})
	}
	for _, input := range []string{
		`'red'`, `"red"`, `#red`, `#green`, `"\67 reen"`, `green red`,
		`gre en`, `gree\`, "g\\\nreen", "g\\\rreen", "g\\\freen",
		`\0000067reen`, `\67  reen`, `\0green`, `\d800green`, `\110000green`,
		`green\ `, `green\;`, `\#red`, `\72 ed extra`,
	} {
		t.Run(input, func(t *testing.T) {
			if got, ok := parseColor(input); ok {
				t.Errorf("parseColor(%q) accepted %v", input, got)
			}
		})
	}
}

func TestStandardNamedColors(t *testing.T) {
	// CSS defines 148 named colors plus the special transparent keyword.
	if got := len(namedColors); got != 149 {
		t.Fatalf("named color count = %d, want 149", got)
	}
	tests := map[string]color.RGBA{
		"aliceblue":     {240, 248, 255, 255},
		"AQUA":          {0, 255, 255, 255},
		`f\75 chsia`:    {255, 0, 255, 255},
		"OlIvE":         {128, 128, 0, 255},
		"rebeccapurple": {102, 51, 153, 255},
		"darkorange":    {255, 140, 0, 255},
		"lightseagreen": {32, 178, 170, 255},
		"yellowgreen":   {154, 205, 50, 255},
		"transparent":   {},
	}
	for input, want := range tests {
		t.Run(input, func(t *testing.T) {
			got, ok := parseColor(input)
			if !ok || got != want {
				t.Errorf("parseColor(%q) = %v, %v; want %v, true", input, got, ok, want)
			}
		})
	}
	for _, aliases := range [][2]string{
		{"aqua", "cyan"},
		{"fuchsia", "magenta"},
		{"darkgray", "darkgrey"},
		{"slategray", "slategrey"},
	} {
		left, leftOK := parseColor(aliases[0])
		right, rightOK := parseColor(aliases[1])
		if !leftOK || !rightOK || left != right {
			t.Errorf("aliases %q and %q differ: %v/%v, %v/%v",
				aliases[0], aliases[1], left, leftOK, right, rightOK)
		}
	}
}

func TestStandardNamedColorPixels(t *testing.T) {
	img := painted(t, `<body style="margin:0">
		<div style="width:20px;height:10px;background:AQUA"></div>
		<div style="width:20px;height:10px;background:fuchsia"></div>
		<div style="width:20px;height:10px;background:olive"></div>
		<div style="width:20px;height:10px;background:rebeccapurple"></div>
	</body>`, image.Rect(0, 0, 20, 40))
	for y, want := range []color.RGBA{
		{0, 255, 255, 255},
		{255, 0, 255, 255},
		{128, 128, 0, 255},
		{102, 51, 153, 255},
	} {
		pixel(t, img, 10, y*10+5, want)
	}
}

func TestInvalidColorDoesNotWinCascade(t *testing.T) {
	for _, invalid := range []string{`'red'`, `"red"`, `#red`, `gree\`, `\0green`, `green red`} {
		for _, important := range []string{"", " !important"} {
			t.Run(invalid+important, func(t *testing.T) {
				doc := styledForLayout(t, `<style>
					.parent {color:green} #fallback {color:green}
					#fallback {color:`+invalid+important+`}
					#inherited {color:`+invalid+important+`}
					</style>
					<div class="parent"><p id="inherited">Inherited</p></div>
					<p id="fallback"><span id="text">Cascade</span></p>`)
				for _, id := range []string{"inherited", "fallback", "text"} {
					node := styledElementByID(doc.StyleRoot, id)
					if node == nil || node.Style["color"] != "green" {
						t.Errorf("%s: invalid declaration replaced green: %+v", id, node)
					}
				}
			})
		}
	}
	doc := styledForLayout(t, `<div style="color:green">
		<p id="inline" style="color:#red">Inline invalid</p>
		<p id="inherit" style="color:inherit">Explicit inherit</p>
		<p id="escaped" style="color:red;color:g\re\45n">Escaped override</p>
		</div>`)
	for _, id := range []string{"inline", "inherit", "escaped"} {
		node := styledElementByID(doc.StyleRoot, id)
		got, ok := parseColor(node.Style["color"])
		if !ok || got != (color.RGBA{0, 128, 0, 255}) {
			t.Errorf("%s computed color = %q, want green", id, node.Style["color"])
		}
	}
}

func TestColorKeywordPixels(t *testing.T) {
	viewport := image.Rect(0, 0, 240, 80)
	want := painted(t, `<p style="color:green">This must be green.</p>`, viewport)
	for _, declaration := range []string{
		`color:'red';color:"red"`, `color:#red`, `color:g\re\45n`,
		`color:\67 reen`, `color:\47 REEN`,
	} {
		got := painted(t, `<style>p {color:green} p {`+declaration+
			`}</style><p>This must be green.</p>`, viewport)
		if !bytes.Equal(got.Pix, want.Pix) {
			t.Errorf("%s: painted pixels differ from green reference", declaration)
		}
	}
}

func TestWPTColorKeywordSyntaxMatchesReference(t *testing.T) {
	root := filepath.Join("..", "..", "testdata", "wpt", "colors")
	render := func(name string) *image.RGBA {
		t.Helper()
		var out bytes.Buffer
		if err := Render(filepath.Join(root, name), &out); err != nil {
			t.Fatal(err)
		}
		img, err := png.Decode(&out)
		if err != nil {
			t.Fatal(err)
		}
		return img.(*image.RGBA)
	}
	got, want := render("colors-007.xht"), render("colors-007-ref.xht")
	if got.Bounds() != want.Bounds() || !bytes.Equal(got.Pix, want.Pix) {
		t.Fatal("pinned colors-007 differs from its all-green reference")
	}
}
