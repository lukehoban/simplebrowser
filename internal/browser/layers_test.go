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
	if sheet.Rules[0].LayerOrder <= sheet.Rules[1].LayerOrder {
		t.Fatalf("parent layer order = %d, want greater than nested layer order = %d", sheet.Rules[0].LayerOrder, sheet.Rules[1].LayerOrder)
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

func TestCascadeInlineStyleBeatsLayeredDeclarations(t *testing.T) {
	styled := layerTestStyles(t, `<!doctype html><style>
		@layer critical {
			#normal { color: red }
			#important { color: red !important }
		}
	</style>
	<p id="normal" style="color: blue">normal inline</p>
	<p id="important" style="color: blue !important">important inline</p>`)
	for _, id := range []string{"normal", "important"} {
		if got := styledElementByID(styled.StyleRoot, id).Style["color"]; got != "blue" {
			t.Errorf("%s inline color = %q, want blue", id, got)
		}
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
	if got := styledElementByID(styled.StyleRoot, "normal").Style["color"]; got != "red" {
		t.Fatalf("parent layer normal color = %q, want red", got)
	}
	if got := styledElementByID(styled.StyleRoot, "important").Style["color"]; got != "blue" {
		t.Fatalf("nested important color = %q, want blue (important precedence reverses nested layer order)", got)
	}
	if got := styledElementByID(styled.StyleRoot, "anon").Style["color"]; got != "blue" {
		t.Fatalf("later anonymous layer color = %q, want blue", got)
	}
}

func TestCascadeParentLayerFollowsDottedAndAnonymousSublayers(t *testing.T) {
	styled := layerTestStyles(t, `<!doctype html><style>
		@layer dotted { #dotted { color: red } }
		@layer dotted.inner { .dotted { color: blue } }
		@layer {
			#anonymous { color: red }
			#important-anonymous { color: red !important }
			@layer { .anonymous { color: blue } }
			@layer { .important-anonymous { color: blue !important } }
		}
	</style>
	<p id="dotted" class="dotted">dotted normal</p>
	<p id="anonymous" class="anonymous">anonymous normal</p>
	<p id="important-anonymous" class="important-anonymous">anonymous important</p>`)
	if got := styledElementByID(styled.StyleRoot, "dotted").Style["color"]; got != "red" {
		t.Fatalf("dotted parent normal color = %q, want red", got)
	}
	if got := styledElementByID(styled.StyleRoot, "anonymous").Style["color"]; got != "red" {
		t.Fatalf("anonymous parent normal color = %q, want red", got)
	}
	if got := styledElementByID(styled.StyleRoot, "important-anonymous").Style["color"]; got != "blue" {
		t.Fatalf("nested anonymous important color = %q, want blue", got)
	}

	// The same dotted layer pair also verifies that important declarations
	// reverse the parent/child layer order.
	styled = layerTestStyles(t, `<!doctype html><style>
		@layer dotted { #x { color: red !important } }
		@layer dotted.inner { .x { color: blue !important } }
	</style><p id="x" class="x">dotted important</p>`)
	if got := styledElementByID(styled.StyleRoot, "x").Style["color"]; got != "blue" {
		t.Fatalf("dotted nested important color = %q, want blue", got)
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

func TestInactiveConditionalLayerBlockDoesNotEstablishOrder(t *testing.T) {
	styled := layerTestStyles(t, `<!doctype html><style>
		@media print {
			@layer phantom { .never { color: black } }
		}
		@layer actual {
			#normal { color: red }
			#important { color: red !important }
		}
		@layer phantom {
			.target { color: blue }
			.important { color: blue !important }
		}
	</style>
	<p id="normal" class="target">normal</p>
	<p id="important" class="important">important</p>`)
	if got := styledElementByID(styled.StyleRoot, "normal").Style["color"]; got != "blue" {
		t.Fatalf("inactive conditional block established normal layer order: color = %q, want blue", got)
	}
	if got := styledElementByID(styled.StyleRoot, "important").Style["color"]; got != "red" {
		t.Fatalf("inactive conditional block established important layer order: color = %q, want red", got)
	}
}

func TestConditionalLayerStatementsTrackViewportAndSupports(t *testing.T) {
	doc, err := parse(Resource{URL: "layers.html", Body: []byte(`<!doctype html>
		<style>
			@supports (display: block) {
				@media (min-width: 900px) { @layer conditional; }
			}
			@supports (display: unsupported) { @layer ignored; }
		</style>
		<style>
			@layer actual { #normal { color: red } #important { color: red !important } }
			@layer conditional { .target { color: blue } .important { color: blue !important } }
			@layer ignored { .ignored { background-color: green } }
		</style>
		<p id="normal" class="target">normal</p>
		<p id="important" class="important">important</p>`)})
	if err != nil {
		t.Fatal(err)
	}
	styled, err := style(doc, &Fetcher{})
	if err != nil {
		t.Fatal(err)
	}

	// At 800px the nested media statement is inactive, so the later
	// conditional layer has higher normal and lower important precedence.
	styled = computeStyles(styled, image.Pt(800, 600))
	if got := styledElementByID(styled.StyleRoot, "normal").Style["color"]; got != "blue" {
		t.Fatalf("800px normal color = %q, want blue", got)
	}
	if got := styledElementByID(styled.StyleRoot, "important").Style["color"]; got != "red" {
		t.Fatalf("800px important color = %q, want red", got)
	}

	// At 1000px the first stylesheet establishes conditional before actual.
	// This also exercises recomputing order on a previously styled document.
	styled = computeStyles(styled, image.Pt(1000, 600))
	if got := styledElementByID(styled.StyleRoot, "normal").Style["color"]; got != "red" {
		t.Fatalf("1000px normal color = %q, want red", got)
	}
	if got := styledElementByID(styled.StyleRoot, "important").Style["color"]; got != "blue" {
		t.Fatalf("1000px important color = %q, want blue", got)
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
