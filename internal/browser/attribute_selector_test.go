package browser

import (
	"image"
	"image/color"
	"reflect"
	"testing"
)

func TestParseAttributeSelectors(t *testing.T) {
	sheet := ParseCSS(`[title], a[href=next][data-kind = "primary"] { color: red }
		[data-empty=''], [checked] { color: green }`)
	if len(sheet.Rules) != 2 {
		t.Fatalf("rules = %d, want 2: %#v", len(sheet.Rules), sheet.Rules)
	}
	got := sheet.Rules[0].Selectors
	want := []Selector{
		{Parts: []SelectorPart{{Attributes: []AttributeSelector{{Name: "title"}}}}},
		{Parts: []SelectorPart{{Tag: "a", Attributes: []AttributeSelector{
			{Name: "href", Value: "next", HasValue: true},
			{Name: "data-kind", Value: "primary", HasValue: true},
		}}}},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("selectors = %#v, want %#v", got, want)
	}
}

func TestInvalidAttributeSelectorsFailClosed(t *testing.T) {
	for _, selector := range []string{
		`[title~=x]`, `[title|=x]`, `[title^=x]`, `[title$=x]`, `[title*=x]`,
		`[title=x i]`, `[title=x s]`, `[ns|title]`, `[title=]`, `[title`,
		`[]`,
	} {
		sheet := ParseCSS(selector + `{ color: red } p { color: green }`)
		if len(sheet.Rules) != 1 || sheet.Rules[0].Selectors[0].Parts[0].Tag != "p" {
			t.Errorf("%q did not fail closed: %#v", selector, sheet.Rules)
		}
		if rules := ParseCSS(`[title="x] { color: red }`).Rules; len(rules) != 0 {
			t.Errorf("unclosed string did not fail closed: %#v", rules)
		}
	}
}

func TestAttributeSelectorMatchingAndSpecificity(t *testing.T) {
	doc, err := parse(Resource{URL: "index.html", Body: []byte(
		`<a id=present title href=next data-kind=Primary></a><a id=absent href=other></a>`,
	)})
	if err != nil {
		t.Fatal(err)
	}
	present := doc.Root.Children[0]
	absent := doc.Root.Children[1]
	for _, tc := range []struct {
		selector string
		node     *Node
		want     bool
	}{
		{`[title]`, present, true},
		{`[title=""]`, present, true},
		{`a[href=next]`, present, true},
		{`[data-kind=Primary]`, present, true},
		{`[data-kind=primary]`, present, false},
		{`[title]`, absent, false},
		{`[href=next]`, absent, false},
	} {
		sel := ParseCSS(tc.selector + `{}`).Rules[0].Selectors[0]
		if got := matchesSelector(tc.node, sel); got != tc.want {
			t.Errorf("%q match = %v, want %v", tc.selector, got, tc.want)
		}
	}
	sel := ParseCSS(`a[href][title=x]{}`).Rules[0].Selectors[0]
	if got, want := specificity(sel), [3]int{0, 2, 1}; got != want {
		t.Errorf("specificity = %v, want %v", got, want)
	}
}

func TestAttributeSelectorCascadeAndPixels(t *testing.T) {
	img := painted(t, `<style>
		div { width: 20px; height: 10px; background: red }
		[data-state=ready] { background: green }
		[hidden] { background: blue }
	</style>
	<div data-state=ready></div><div data-state=other></div><div hidden></div>`,
		image.Rect(0, 0, 20, 30))
	pixel(t, img, 5, 5, color.RGBA{0, 128, 0, 255})
	pixel(t, img, 5, 15, color.RGBA{255, 0, 0, 255})
	pixel(t, img, 5, 25, color.RGBA{0, 0, 255, 255})
}
