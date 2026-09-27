package browser

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
)

func TestCustomPropertiesCascadeInheritanceAndSubstitution(t *testing.T) {
	doc, err := parse(Resource{URL: "index.html", Body: []byte(`
		<style>
		  :root { --Accent: #0969da; --accent: red; --distance: 2em }
		  .sample { color: #cf222e; color: var(--Accent); width: var(--distance) }
		  .inner { --Accent: rgb(0, 128, 0) !important }
		</style>
		<p id="outer" class="sample">blue</p>
		<div class="inner"><p id="inner" class="sample" style="--Accent: yellow; color: var(--Accent)">green</p></div>`)})
	if err != nil {
		t.Fatal(err)
	}
	styled, err := style(doc, &Fetcher{})
	if err != nil {
		t.Fatal(err)
	}
	styled = computeStyles(styled, image.Pt(800, 600))
	outer := styledElementByID(styled.StyleRoot, "outer").Style
	inner := styledElementByID(styled.StyleRoot, "inner").Style
	if outer["color"] != "#0969da" || outer["width"] != "32px" ||
		outer["--Accent"] != "#0969da" || outer["--distance"] != "2em" {
		t.Fatalf("inherited variables: %v", outer)
	}
	// Inline normal importance does not beat an inherited custom property:
	// inheritance is weaker than any declaration on the child.
	if inner["color"] != "yellow" || inner["--Accent"] != "yellow" {
		t.Fatalf("inline variable: %v", inner)
	}
}

func TestCustomPropertyFallbackCyclesAndComputedInvalid(t *testing.T) {
	doc, err := parse(Resource{URL: "index.html", Body: []byte(`
		<style>:root { --a: var(--b); --b: var(--a); --size: 10em }
		#child { --b: 4px; --local: var(--b); font-size: var(--missing); width: var(--a, var(--local)); height: var(--void, var(--size)) }
		#cyclic { font-size:var(--a); line-height:var(--a); color:blue; color:var(--a) }
		#nested { color:var(--absent, rgb(1, 2, 3)); width:var(--missing, 10px) }
		</style><div style="font-size:16px">
		<div id="child" style="font-size:var(--missing); background:blue"></div>
		<div id="cyclic"></div><div id="nested"></div></div>`)})
	if err != nil {
		t.Fatal(err)
	}
	styled, err := style(doc, &Fetcher{})
	if err != nil {
		t.Fatal(err)
	}
	styled = computeStyles(styled, image.Pt(800, 600))
	child := styledElementByID(styled.StyleRoot, "child").Style
	cyclic := styledElementByID(styled.StyleRoot, "cyclic").Style
	nested := styledElementByID(styled.StyleRoot, "nested").Style
	if child["font-size"] != "16px" || child["width"] != "4px" || child["height"] != "160px" ||
		child["--a"] != invalidVariable {
		t.Fatalf("missing var / inherited invalid cycle / nested fallback: %v", child)
	}
	if cyclic["font-size"] != "16px" || cyclic["line-height"] != "normal" ||
		cyclic["color"] != "black" {
		t.Fatalf("invalid computed declarations should unset, not use lower-priority declarations: %v", cyclic)
	}
	if nested["color"] != "rgb(1, 2, 3)" || nested["width"] != "10px" {
		t.Fatalf("fallback values: %v", nested)
	}
}

func TestCustomPropertyCycleCannotUseFallbackInsideCycle(t *testing.T) {
	doc, err := parse(Resource{URL: "index.html", Body: []byte(`
		<style>:root { --a: var(--b, red); --b: var(--a, blue) }
		#target { color:var(--a, green) }</style><div id="target"></div>`)})
	if err != nil {
		t.Fatal(err)
	}
	styled, err := style(doc, &Fetcher{})
	if err != nil {
		t.Fatal(err)
	}
	styled = computeStyles(styled, image.Pt(800, 600))
	if got := styledElementByID(styled.StyleRoot, "target").Style["color"]; got != "green" {
		t.Fatalf("cycle with fallback color = %q, want green", got)
	}
}

func TestUnresolvedVariableFontSizeMoonRepro(t *testing.T) {
	doc, err := parse(Resource{URL: "index.html", Body: []byte(`<html><body style="margin:0;font-size:16px">
		<div id="article" style="font-size:var(--font-size-medium)">
		<p>Text inside an unresolved var() font-size should inherit 16px.</p>
		<div id="bar" style="width:10em;height:20px;background:#36c"></div>
		</div></body></html>`)})
	if err != nil {
		t.Fatal(err)
	}
	styled, err := style(doc, &Fetcher{})
	if err != nil {
		t.Fatal(err)
	}
	styled = computeStyles(styled, image.Pt(800, 600))
	if got := styledElementByID(styled.StyleRoot, "article").Style["font-size"]; got != "16px" {
		t.Fatalf("article font-size = %q", got)
	}
	if got := styledElementByID(styled.StyleRoot, "bar").Style["width"]; got != "160px" {
		t.Fatalf("bar width = %q", got)
	}
	laidOut, err := LayoutWithViewport(styled, image.Rect(0, 0, 800, 600))
	if err != nil {
		t.Fatal(err)
	}
	bar := boxesByID(laidOut.Root, "bar")["bar"]
	if bar == nil || bar.Content.Dx() != 160 || bar.Content.Dy() != 20 {
		t.Fatalf("Moon repro bar = %v, want 160x20", bar)
	}
}

func TestSubstituteVarsIgnoresStringsAndRespectsNestedCommas(t *testing.T) {
	got, ok := substituteVars(`"var(--missing)" var(--missing, rgb(1, 2, 3))`, nil, nil)
	if !ok || got != `"var(--missing)"  rgb(1, 2, 3)` {
		t.Fatalf("substitution = %q, %v", got, ok)
	}
}

func TestCustomPropertiesExpandShorthandsAfterCascade(t *testing.T) {
	doc, err := parse(Resource{URL: "index.html", Body: []byte(`
		<style>:root { --insets: 1em 2em; --fill: #0969da }
		#box { margin: var(--insets); background: var(--fill);
		  color: var(--missing, red); color: var(--absent) }</style>
		<div id="box">Hello</div>`)})
	if err != nil {
		t.Fatal(err)
	}
	styled, err := style(doc, &Fetcher{})
	if err != nil {
		t.Fatal(err)
	}
	styled = computeStyles(styled, image.Pt(800, 600))
	box := styledElementByID(styled.StyleRoot, "box").Style
	if box["margin-left"] != "32px" || box["margin-top"] != "16px" ||
		box["background-color"] != "#0969da" || box["color"] != "black" {
		t.Fatalf("variable shorthand/invalid winner: %v", box)
	}
}

func TestEmptyCustomPropertyOverridesFallbackButInvalidatesOrdinaryValue(t *testing.T) {
	doc, err := parse(Resource{URL: "index.html", Body: []byte(`
		<style>:root { --empty: ; --: red }
		#target { color:blue; color: var(--empty, red); width: var(--empty, 30px) }
		#fallback { color: var(--missing, red) }</style>
		<div id="target"></div><div id="fallback"></div>`)})
	if err != nil {
		t.Fatal(err)
	}

	styled, err := style(doc, &Fetcher{})
	if err != nil {
		t.Fatal(err)
	}
	styled = computeStyles(styled, image.Pt(800, 600))
	target := styledElementByID(styled.StyleRoot, "target").Style
	fallback := styledElementByID(styled.StyleRoot, "fallback").Style
	if target["--empty"] != "" || target["color"] != "black" || target["width"] != "" ||
		fallback["color"] != "red" {
		t.Fatalf("empty and missing variable semantics: target=%v fallback=%v", target, fallback)
	}
	if _, exists := target["--"]; exists {
		t.Fatalf("invalid custom property name accepted: %v", target)
	}
}

func TestExternalCustomPropertyURLUsesStylesheetBase(t *testing.T) {
	var mu sync.Mutex
	requests := map[string]int{}
	tile := encodedTestImage(t, "png", image.Rect(0, 0, 2, 2))
	const html = `<link rel="stylesheet" href="/css/site.css"><body style="margin:0">
		<div id="direct"></div><div id="nested"></div><div id="fallback"></div></body>`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		requests[r.URL.Path]++
		mu.Unlock()
		switch r.URL.Path {
		case "/page.html":
			_, _ = w.Write([]byte(html))
		case "/css/site.css":
			_, _ = w.Write([]byte(`:root { --image: url(tile.png); --nested: var(--image);
				--fallback: var(--absent, url(tile.png)) }
				#direct, #nested, #fallback { width: 2px; height: 2px;
				background-repeat: no-repeat }
				#direct { background-image: var(--image) }
				#nested { background-image: var(--nested) }
				#fallback { background-image: var(--fallback) }`))
		case "/css/tile.png":
			w.Header().Set("Content-Type", "image/png")
			_, _ = w.Write(tile)
		default:
			http.NotFound(w, r)
		}

	}))
	defer server.Close()
	source := server.URL + "/page.html"
	doc, err := parse(Resource{URL: source, Body: []byte(html)})
	if err != nil {
		t.Fatal(err)
	}
	styled, err := style(doc, &Fetcher{})
	if err != nil {
		t.Fatal(err)
	}
	want := `url("` + server.URL + `/css/tile.png")`
	for _, id := range []string{"direct", "nested", "fallback"} {
		if got := styledElementByID(styled.StyleRoot, id).Style["background-image"]; got != want {
			t.Errorf("%s background-image = %q, want %q", id, got, want)
		}
	}
	var output bytes.Buffer
	if err := RenderWithFetcher(source, &output, &Fetcher{}); err != nil {
		t.Fatal(err)
	}
	rendered, err := png.Decode(&output)
	if err != nil {
		t.Fatalf("rendered PNG: %v", err)
	}
	if got := color.RGBAModel.Convert(rendered.At(0, 0)); got != (color.RGBA{B: 180, A: 255}) {
		t.Fatalf("rendered tile pixel = %v", got)
	}
	mu.Lock()
	defer mu.Unlock()
	if requests["/css/tile.png"] != 2 || requests["/tile.png"] != 0 {
		t.Fatalf("stylesheet-relative image requests = %v", requests)
	}
}

func TestInvalidSubstitutedColorWinnerDoesNotRevealEarlierDeclaration(t *testing.T) {
	doc, err := parse(Resource{URL: "index.html", Body: []byte(`
		<style>:root { --bad: 20px }
		#target { color: blue; color: var(--bad) }</style>
		<div style="color:green"><span id="target">inherited</span></div>`)})
	if err != nil {
		t.Fatal(err)
	}
	styled, err := style(doc, &Fetcher{})
	if err != nil {
		t.Fatal(err)
	}
	if got := styledElementByID(styled.StyleRoot, "target").Style["color"]; got != "green" {
		t.Fatalf("invalid substituted color = %q, want inherited green", got)
	}
}
