package browser

import (
	"image"
	"testing"
)

func layerTestStyles(t *testing.T, html string) StyledDocument {
	t.Helper()
	doc, err := parse(Resource{URL: "layers.html", Body: []byte(html)})
	if err != nil {
		t.Fatal(err)
	}
	styled, err := style(doc, &Fetcher{})
	if err != nil {
		t.Fatal(err)
	}
	return styled
}

func TestParseCascadeLayerBlocksStatementsAndConditions(t *testing.T) {
	sheet := ParseCSS(`
		@layer reset, theme;
		@layer theme {
			@media screen and (min-width: 600px) {
				@supports (display: block) { .card { color: blue } }
			}
			@layer components { .button { color: red } }
		}
		@layer { .anonymous { color: green } }
		@layer theme.components, utilities;
	`)
	if len(sheet.Rules) != 3 {
		t.Fatalf("rules = %d, want 3: %#v", len(sheet.Rules), sheet.Rules)
	}
	if len(sheet.Layers) != 5 {
		t.Fatalf("declared layers = %#v, want reset/theme/theme.components/utilities and anonymous path", sheet.Layers)
	}
	if sheet.Rules[0].Layer != "theme" || sheet.Rules[0].Media == "" {
		t.Fatalf("conditional rule lost layer or media: %#v", sheet.Rules[0])
	}
	if sheet.Rules[1].Layer != "theme.components" || sheet.Rules[2].Layer == "" {
		t.Fatalf("nested/anonymous layer metadata = %#v", sheet.Rules)
	}
	if sheet.Rules[0].LayerOrder >= sheet.Rules[1].LayerOrder {
		t.Fatalf("nested layer order = %d, parent order = %d", sheet.Rules[1].LayerOrder, sheet.Rules[0].LayerOrder)
	}
}

func TestCascadeLayerOrderAndImportantReversal(t *testing.T) {
	styled := layerTestStyles(t, `<!doctype html><style>
		@layer early, late;
		@layer early { #normal { color: red } #important { color: red !important } }
		@layer late { .target { color: blue } .important { color: blue !important } }
		#normal { color: green }
		#important { color: green !important }
	</style><p id="normal" class="target">normal</p>
	<p id="important" class="target important">important</p>
	<p id="inline" class="target" style="color: orange">inline</p>`)
	if got := styledElementByID(styled.StyleRoot, "normal").Style["color"]; got != "green" {
		t.Fatalf("unlayered normal color = %q, want green", got)
	}
	if got := styledElementByID(styled.StyleRoot, "important").Style["color"]; got != "red" {
		t.Fatalf("earliest important layer color = %q, want red", got)
	}
	if got := styledElementByID(styled.StyleRoot, "inline").Style["color"]; got != "orange" {
		t.Fatalf("normal inline style color = %q, want orange", got)
	}
}

func TestCascadeLayeredImportantCanBeatInlineImportant(t *testing.T) {
	styled := layerTestStyles(t, `<!doctype html><style>
		@layer critical { #x { color: red !important } }
	</style><p id="x" style="color: blue !important">inline</p>`)
	if got := styledElementByID(styled.StyleRoot, "x").Style["color"]; got != "red" {
		t.Fatalf("layered important color = %q, want red", got)
	}
}

func TestInvalidLayerSyntaxDoesNotCreateLayersOrSuppressFollowingRules(t *testing.T) {
	sheet := ParseCSS(`
		@layer 3invalid { p { color: red } }
		@layer valid, ;
		p { color: blue }
	`)
	if len(sheet.Layers) != 0 || len(sheet.Rules) != 1 {
		t.Fatalf("invalid layer syntax affected parse recovery: layers=%#v rules=%#v", sheet.Layers, sheet.Rules)
	}
	styled := layerTestStyles(t, `<!doctype html><style>
		@layer 3invalid { #x { color: red } }
		#x { color: blue }
	</style><p id="x">invalid layer</p>`)
	if got := styledElementByID(styled.StyleRoot, "x").Style["color"]; got != "blue" {
		t.Fatalf("rule following invalid layer = %q, want blue", got)
	}
}

func TestCascadeLayerOrderingSpansStylesheets(t *testing.T) {
	styled := layerTestStyles(t, `<!doctype html>
		<style>@layer foundation { #x { color: red } }</style>
		<style>@layer theme { .target { color: blue } }</style>
		<p id="x" class="target">cross-sheet</p>`)
	if got := styledElementByID(styled.StyleRoot, "x").Style["color"]; got != "blue" {
		t.Fatalf("later layer from second sheet color = %q, want blue", got)
	}

	// An ordering statement in a later sheet reuses existing layer order;
	// it does not move already-introduced layers.
	styled = layerTestStyles(t, `<!doctype html>
		<style>@layer first, second; @layer first { #y { color: red } }</style>
		<style>@layer second, first; @layer second { .target { color: blue } }</style>
		<p id="y" class="target">statement</p>`)
	if got := styledElementByID(styled.StyleRoot, "y").Style["color"]; got != "blue" {
		t.Fatalf("repeated layer statement changed first-declaration order: color = %q", got)
	}
}

func TestCascadeNestedAndAnonymousLayers(t *testing.T) {
	styled := layerTestStyles(t, `<!doctype html><style>
		@layer outer {
			#normal { color: red }
			@layer inner { .normal { color: blue } }
			#important { color: red !important }
			@layer inner { .important { color: blue !important } }
		}
		@layer { #anon { color: red } }
		@layer { .anon { color: blue } }
	</style>
	<p id="normal" class="normal">nested normal</p>
	<p id="important" class="important">nested important</p>
	<p id="anon" class="anon">anonymous</p>`)
	if got := styledElementByID(styled.StyleRoot, "normal").Style["color"]; got != "blue" {
		t.Fatalf("nested normal color = %q, want blue", got)
	}
	if got := styledElementByID(styled.StyleRoot, "important").Style["color"]; got != "red" {
		t.Fatalf("nested important color = %q, want red (important precedence reverses nested layer order)", got)
	}
	if got := styledElementByID(styled.StyleRoot, "anon").Style["color"]; got != "blue" {
		t.Fatalf("later anonymous layer color = %q, want blue", got)
	}
}

func TestCascadeLayerPrecedesSpecificityAndUnlayeredImportance(t *testing.T) {
	styled := layerTestStyles(t, `<!doctype html><style>
		@layer low, high;
		@layer low { #x { color: red } #i { color: red !important } }
		@layer high { .target { color: blue } }
		#i { color: blue !important }
	</style><p id="x" class="target">specificity</p>
	<p id="i" class="target">important</p>`)
	if got := styledElementByID(styled.StyleRoot, "x").Style["color"]; got != "blue" {
		t.Fatalf("later layer did not precede selector specificity: color = %q", got)
	}
	if got := styledElementByID(styled.StyleRoot, "i").Style["color"]; got != "red" {
		t.Fatalf("layered important did not beat unlayered important: color = %q", got)
	}
}

func TestCascadeLayersWithinSupportsAndMedia(t *testing.T) {
	styled := layerTestStyles(t, `<!doctype html><style>
		@layer baseline, enhancements;
		@layer baseline { #x { color: red } }
		@layer enhancements {
			@media (min-width: 800px) { @supports (display: block) { .target { color: blue } } }
		}
	</style><p id="x" class="target">conditional</p>`)
	if got := styledElementByID(styled.StyleRoot, "x").Style["color"]; got != "blue" {
		t.Fatalf("conditional layer was not applied at 800px: color = %q", got)
	}
}

func TestCascadePresentationalHintsAndAuthorLayers(t *testing.T) {
	styled := layerTestStyles(t, `<!doctype html><style>
		@layer reset { table { background-color: blue } }
	</style><table id="table" bgcolor="red"></table>`)
	if got := styledElementByID(styled.StyleRoot, "table").Style["background-color"]; got != "blue" {
		t.Fatalf("author layer did not override presentational hint: background-color = %q", got)
	}
}

func TestCascadeLayerOriginAndSpecificityRegression(t *testing.T) {
	doc, err := parse(Resource{URL: "layers.html", Body: []byte(`<p id="x">origin</p>`)})
	if err != nil {
		t.Fatal(err)
	}
	node := findNodeByID(doc.Root, "x")
	if node == nil {
		t.Fatal("missing p node")
	}
	author := ParseCSS(`@layer author { #x { color: blue !important } }`)
	got := cascade(node, nil, 16, false, ParseCSS(`#x { color: red !important }`),
		[]Stylesheet{author}, nil, image.Pt(800, 600), "")
	if got["color"] != "red" {
		t.Fatalf("author important overrode UA important: color = %q", got["color"])
	}
}
