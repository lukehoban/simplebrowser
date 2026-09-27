package browser

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"math"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
)

func TestClassifyViewportLengths(t *testing.T) {
	for _, tc := range []struct {
		text, unit string
		number     float64
	}{
		{"50vw", "vw", 50}, {"10VH", "vh", 10},
		{".25vmin", "vmin", .25}, {"-2.5VMAX", "vmax", -2.5},
		{"1svw", "svw", 1}, {"2SVH", "svh", 2},
		{"3svmin", "svmin", 3}, {"4SVMAX", "svmax", 4},
		{"5lvw", "lvw", 5}, {"6LVH", "lvh", 6},
		{"7lvmin", "lvmin", 7}, {"8LVMAX", "lvmax", 8},
		{"9dvw", "dvw", 9}, {"10DVH", "dvh", 10},
		{"11dvmin", "dvmin", 11}, {"12DVMAX", "dvmax", 12},
		{"0vw", "vw", 0},
	} {
		v := classifyValue(tc.text)
		if v.Kind != "length" || v.Unit != tc.unit || v.Number != tc.number {
			t.Errorf("classifyValue(%q) = %+v", tc.text, v)
		}
	}
}

func TestViewportVariantLengthsGeometryAndComputedFontSize(t *testing.T) {
	viewport := image.Rect(17, 29, 817, 629)
	for _, family := range []string{"s", "l", "d"} {
		for _, suffix := range []string{"vw", "vh", "vmin", "vmax"} {
			unit := family + suffix
			t.Run(unit, func(t *testing.T) {
				basis, ok := viewportLengthBasis(unit, viewport.Size())
				if !ok {
					t.Fatalf("viewportLengthBasis(%q) was not recognized", unit)
				}
				source := fmt.Sprintf(`<body style="margin:0"><div id="box"
					style="width:50%[1]s;height:10%[1]s;font-size:2%[1]s"></div></body>`, unit)
				got, err := LayoutWithViewport(styledForLayout(t, source), viewport)
				if err != nil {
					t.Fatal(err)
				}
				box := boxesByID(got.Root, "box")["box"]
				if box == nil || box.Content.Dx() != basis/2 || box.Content.Dy() != basis/10 {
					t.Fatalf("box = %+v, want %dx%d content", box, basis/2, basis/10)
				}
				style := got.Document.Styles[box.Node]
				if want := formatPixels(float64(basis) * .02); style["font-size"] != want {
					t.Errorf("computed font-size = %q, want %q", style["font-size"], want)
				}
			})
		}
	}
}

func TestViewportVariantIssue115ReproAndCompoundValues(t *testing.T) {
	doc := styledForLayout(t, `<body style="margin:0">
		<div id="box" style="width:50svw;height:10dvh;border:1lvw solid red;
			background:linear-gradient(red,red) no-repeat 2dvw 3svh/10lvw 10dvh"></div>
		</body>`)
	got, err := LayoutWithViewport(doc, image.Rect(0, 0, 800, 600))
	if err != nil {
		t.Fatal(err)
	}
	box := boxesByID(got.Root, "box")["box"]
	if box == nil || box.Content != image.Rect(8, 8, 408, 68) {
		t.Fatalf("box content = %+v, want (8,8)-(408,68)", box)
	}
	style := got.Document.Styles[box.Node]
	for property, want := range map[string]string{
		"width":               "400px",
		"height":              "60px",
		"border-left":         "8px solid red",
		"background-position": "16px 18px",
		"background-size":     "80px 60px",
	} {
		if style[property] != want {
			t.Errorf("%s = %q, want %q", property, style[property], want)
		}
	}
}

func TestViewportLengthsGeometry(t *testing.T) {
	for _, viewport := range []image.Rectangle{
		image.Rect(0, 0, 800, 600),
		image.Rect(17, 29, 617, 829), // portrait and nonzero origin
	} {
		for _, unit := range []string{"vw", "vh", "vmin", "vmax"} {
			t.Run(fmt.Sprintf("%s/%s", viewport, unit), func(t *testing.T) {
				basis := map[string]int{
					"vw": viewport.Dx(), "vh": viewport.Dy(),
					"vmin": min(viewport.Dx(), viewport.Dy()), "vmax": max(viewport.Dx(), viewport.Dy()),
				}[unit]
				// An explicitly smaller parent must not become the unit basis.
				source := fmt.Sprintf(`<div style="width:100px;padding:1px">
					<div id="box" style="width:50%[1]s;height:10%[1]s;margin:2%[1]s;font-size:5%[1]s"></div>
					</div>`, unit)
				styled := styledForLayout(t, source)
				got, err := LayoutWithViewport(styled, viewport)
				if err != nil {
					t.Fatal(err)
				}
				box := boxesByID(got.Root, "box")["box"]
				margin := basis * 2 / 100
				want := image.Rect(viewport.Min.X+1+margin, viewport.Min.Y+1+margin,
					viewport.Min.X+1+margin+basis/2, viewport.Min.Y+1+margin+basis/10)
				if box == nil || box.Content != want {
					t.Fatalf("box = %+v, want content %v", box, want)
				}
				style := got.Document.Styles[box.Node]
				for property, value := range map[string]int{
					"width": basis / 2, "height": basis / 10, "font-size": basis / 20,
					"margin-top": margin, "margin-right": margin, "margin-bottom": margin, "margin-left": margin,
				} {
					if style[property] != formatPixels(float64(value)) {
						t.Errorf("%s = %q, want %dpx", property, style[property], value)
					}
				}
			})
		}
	}
}

func TestViewportFontInheritanceAndRelativeLengths(t *testing.T) {
	doc := styledForLayout(t, `<html id="root" style="font-size:5vw;margin:0">
		<body style="margin:0"><div id="parent" style="font:2vh/1.5 sans-serif">
		<div id="child" style="width:2em;height:1rem;padding-top:1ex;padding-left:1ch">text</div>
		<div id="percent" style="font-size:150%"></div>
		<div id="rem" style="font-size:2rem"></div>
		</div></body></html>`)
	for _, viewport := range []image.Rectangle{image.Rect(0, 0, 800, 600), image.Rect(0, 0, 400, 1000)} {
		got, err := LayoutWithViewport(doc, viewport)
		if err != nil {
			t.Fatal(err)
		}
		rootSize := float64(viewport.Dx()) * .05
		parentSize := float64(viewport.Dy()) * .02
		check := func(id, property string, want float64) {
			t.Helper()
			style := styledElementByID(got.Document.StyleRoot, id).Style
			value := classifyValue(style[property])
			if value.Unit != "px" || math.Abs(value.Number-want) > 1e-9 {
				t.Errorf("%s %s = %q, want %gpx", id, property, style[property], want)
			}
		}
		check("root", "font-size", rootSize)
		check("parent", "font-size", parentSize)
		check("child", "font-size", parentSize)
		check("child", "width", 2*parentSize)
		check("child", "height", rootSize)
		ratios := ratiosFor(styledElementByID(got.Document.StyleRoot, "child").Style)
		check("child", "padding-top", parentSize*ratios.ex)
		check("child", "padding-left", parentSize*ratios.ch)
		check("percent", "font-size", 1.5*parentSize)
		check("rem", "font-size", 2*rootSize)
		box := boxesByID(got.Root, "child")["child"]
		if len(box.Text) != 1 {
			t.Fatalf("missing child text: %+v", box)
		}
		if height := box.Text[0].Rect.Dy(); height != int(parentSize*1.5) {
			t.Errorf("text line height = %d, want %g", height, parentSize*1.5)
		}
	}
}

func TestViewportLengthsFractionsAndFallback(t *testing.T) {
	doc := styledForLayout(t, `<div id="box" style="width:12.5vw;height:0vh;margin:0 -1.25vmin 0 0;font-size:2vmax"></div>`)
	got, err := LayoutWithViewport(doc, image.Rect(0, 0, 333, 777))
	if err != nil {
		t.Fatal(err)
	}
	style := styledElementByID(got.Document.StyleRoot, "box").Style
	for property, want := range map[string]string{
		"width": "41.625px", "height": "0px", "margin-right": "-4.1625px", "font-size": "15.54px",
	} {
		if style[property] != want {
			t.Errorf("%s = %q, want %q", property, style[property], want)
		}
	}
	if width := boxesByID(got.Root, "box")["box"].Content.Dx(); width != 41 {
		t.Errorf("used width = %d, want 41 (layout truncates block widths)", width)
	}
	// Relayout the already-restyled document; a zero viewport selects 800x600.
	fallback, err := LayoutWithViewport(got.Document, image.Rectangle{})
	if err != nil {
		t.Fatal(err)
	}
	if width := boxesByID(fallback.Root, "box")["box"].Content.Dx(); width != 100 {
		t.Errorf("default viewport width = %d, want 100", width)
	}
	if style["width"] != "41.625px" {
		t.Fatal("relayout mutated a previous layout's style")
	}
}

func TestViewportRelayoutConcurrent(t *testing.T) {
	doc := styledForLayout(t, `<div id="box" style="width:50vw;height:10vh;font-size:2vmin"></div>`)
	var wg sync.WaitGroup
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			viewport := image.Rect(0, 0, 200+i*20, 400+i*10)
			for j := 0; j < 3; j++ {
				got, err := LayoutWithViewport(doc, viewport)
				if err != nil {
					t.Error(err)
					return
				}
				box := boxesByID(got.Root, "box")["box"]
				if box.Content.Dx() != viewport.Dx()/2 || box.Content.Dy() != viewport.Dy()/10 {
					t.Errorf("concurrent viewport %v: box %v", viewport, box.Content)
				}
			}
		}(i)
	}
	wg.Wait()
	if style := styledElementByID(doc.StyleRoot, "box").Style; style["width"] != "400px" || style["font-size"] != "12px" {
		t.Fatalf("input computed style was mutated: %v", style)
	}
}

func TestViewportRelayoutRetainsLoadedResources(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		if r.URL.Path == "/style.css" {
			w.Header().Set("Content-Type", "text/css")
			fmt.Fprint(w, "#box {width:50vw;height:10vh;background-image:url(tile.png)}")
		} else {
			w.Header().Set("Content-Type", "image/png")
			if err := png.Encode(w, image.NewRGBA(image.Rect(0, 0, 1, 1))); err != nil {
				t.Error(err)
			}
		}
	}))
	defer server.Close()
	doc := Document{BaseURL: server.URL + "/index.html", Root: ParseHTML(
		`<link rel="stylesheet" href="style.css"><div id="box"></div><img src="tile.png">`)}
	styled, err := style(doc, &Fetcher{})
	if err != nil {
		t.Fatal(err)
	}
	before := requests.Load()
	if before == 0 || len(styled.Images) != 1 || len(styled.BackgroundImages) != 1 {
		t.Fatal("fixture did not load CSS and images")
	}
	got, err := LayoutWithViewport(styled, image.Rect(0, 0, 400, 300))
	if err != nil {
		t.Fatal(err)
	}
	if requests.Load() != before || len(got.Document.Images) != 1 || len(got.Document.BackgroundImages) != 1 {
		t.Fatal("relayout refetched or discarded images")
	}
	if width := boxesByID(got.Root, "box")["box"].Content.Dx(); width != 200 {
		t.Errorf("external stylesheet width = %d, want 200", width)
	}
}

func TestViewportUnitsPaintIssue100Repro(t *testing.T) {
	doc := styledForLayout(t, `<body style="margin:0"><div style="width:50vw;height:10vh;background:red"></div></body>`)
	layout, err := layout(doc)
	if err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	if err := paint(layout, &output, renderOptions{}); err != nil {
		t.Fatal(err)
	}
	img, err := png.Decode(&output)
	if err != nil {
		t.Fatal(err)
	}
	for _, point := range []image.Point{{0, 0}, {399, 59}, {400, 59}, {399, 60}} {
		want := color.RGBA{255, 255, 255, 255}
		if point.X < 400 && point.Y < 60 {
			want = color.RGBA{255, 0, 0, 255}
		}
		if got := color.RGBAModel.Convert(img.At(point.X, point.Y)); got != want {
			t.Errorf("pixel %v = %v, want %v", point, got, want)
		}
	}
}
