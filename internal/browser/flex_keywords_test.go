package browser

import (
	"image"
	"testing"
)

// Issue #356: the flex shorthand keywords auto (1 1 auto) and initial
// (0 1 auto) must not be ignored; none (0 0 auto) must keep working.
func TestFlexShorthandKeywordFactors(t *testing.T) {
	for _, tc := range []struct {
		flex         string
		grow, shrink float64
		wantHasBasis bool
	}{
		{"auto", 1, 1, false},
		{"AUTO", 1, 1, false},
		{"initial", 0, 1, false},
		{"none", 0, 0, false},
		{"1 1 auto", 1, 1, false},
		{"2 auto", 2, 1, false},
		{"1", 1, 1, true},
	} {
		grow, shrink, _, hasBasis := flexFactors(ComputedStyle{"flex": tc.flex}, 300, true)
		if grow != tc.grow || shrink != tc.shrink || hasBasis != tc.wantHasBasis {
			t.Errorf("flex:%s = grow %v shrink %v hasBasis %v, want %v %v %v",
				tc.flex, grow, shrink, hasBasis, tc.grow, tc.shrink, tc.wantHasBasis)
		}
		if flexBasisIsLength(ComputedStyle{"flex": tc.flex}) {
			t.Errorf("flex:%s reported a definite length basis", tc.flex)
		}
	}
	// Longhands still override the keyword's components.
	if grow, _, _, _ := flexFactors(ComputedStyle{"flex": "auto", "flex-grow": "3"}, 300, true); grow != 3 {
		t.Errorf("flex:auto with flex-grow:3 grow = %v, want 3", grow)
	}
}

func TestFlexShorthandKeywordComputedValues(t *testing.T) {
	const source = `<style>
		#auto { flex: auto }
		#initial { flex: 2; flex: initial }
		#none { flex: none }
		#var { --f: auto; flex: 3; flex: var(--f) }
	</style><body><div style="display:flex">
		<div id="auto"></div><div id="initial"></div><div id="none"></div><div id="var"></div>
	</div></body>`
	root := styledForLayout(t, source).StyleRoot
	for id, want := range map[string]string{"auto": "auto", "none": "none", "var": "auto"} {
		if got := styledElementByID(root, id).Style["flex"]; got != want {
			t.Errorf("#%s computed flex = %q, want %q", id, got, want)
		}
	}
	// The CSS-wide initial keyword resets the shorthand to its initial value
	// (0 1 auto), which is represented by the absence of a flex declaration.
	initial := styledElementByID(root, "initial").Style
	if got, ok := initial["flex"]; ok {
		t.Errorf("#initial computed flex = %q, want reset to initial", got)
	}
	if grow, shrink, _, hasBasis := flexFactors(initial, 300, true); grow != 0 || shrink != 1 || hasBasis {
		t.Errorf("#initial factors = %v %v basis=%v, want 0 1 auto", grow, shrink, hasBasis)
	}
}

func TestFlexShorthandKeywordLayoutGeometry(t *testing.T) {
	const source = `<!doctype html><body style="margin:0">
	<div style="display:flex;width:300px"><div id="auto" style="flex:auto;height:20px"></div><div id="auto-next" style="width:50px;height:20px"></div></div>
	<div style="display:flex;width:300px"><div id="initial" style="flex:initial;width:100px;height:20px"></div><div id="initial-next" style="width:50px;height:20px"></div></div>
	<div style="display:flex;width:300px"><div id="none" style="flex:none;width:100px;height:20px"></div><div id="none-next" style="width:50px;height:20px"></div></div>
	<div style="display:flex;width:300px"><div id="shrink" style="flex:initial;width:280px;height:20px"></div><div id="shrink-next" style="flex:none;width:50px;height:20px"></div></div>
	<div style="display:flex;width:300px"><div id="rigid" style="flex:none;width:280px;height:20px"></div><div id="rigid-next" style="flex:none;width:50px;height:20px"></div></div>
	<div style="display:flex;width:300px"><div id="a" style="flex:auto;width:40px;height:20px"></div><div id="b" style="flex:auto;width:100px;height:20px"></div></div>
	</body>`
	layout, err := LayoutWithViewport(styledForLayout(t, source), image.Rect(0, 0, 400, 200))
	if err != nil {
		t.Fatal(err)
	}
	boxes := boxesByID(layout.Root, "auto", "auto-next", "initial", "initial-next", "none", "none-next",
		"shrink", "shrink-next", "rigid", "rigid-next", "a", "b")
	check := func(id string, minX, width int) {
		t.Helper()
		box := boxes[id]
		if box == nil {
			t.Fatalf("missing box #%s", id)
		}
		if box.Rect.Min.X != minX || box.Rect.Dx() != width {
			t.Errorf("#%s rect = %v, want x=%d width=%d", id, box.Rect, minX, width)
		}
	}
	// Issue repro: flex:auto grows to fill the 250px of free space.
	check("auto", 0, 250)
	check("auto-next", 250, 50)
	// flex:initial and flex:none do not grow.
	check("initial", 0, 100)
	check("initial-next", 100, 50)
	check("none", 0, 100)
	check("none-next", 100, 50)
	// flex:initial shrinks; flex:none does not.
	check("shrink", 0, 250)
	check("shrink-next", 250, 50)
	check("rigid", 0, 280)
	check("rigid-next", 280, 50)
	// flex:auto keeps the auto basis: equal share of 160px free space.
	check("a", 0, 120)
	check("b", 120, 180)
}

func TestFlexSupportsShorthandKeywords(t *testing.T) {
	for _, query := range []string{"(flex: auto)", "(flex: initial)", "(flex: none)", "(flex: 1 1 auto)", "(flex: 2 auto)", "(flex: 1 0 10px)"} {
		if !supportsConditionMatches(query) {
			t.Errorf("supported flex value not reported: %s", query)
		}
	}
	for _, query := range []string{"(flex: auto 1)", "(flex: none 1)", "(flex: 1 1 bogus)"} {
		if supportsConditionMatches(query) {
			t.Errorf("invalid flex value reported: %s", query)
		}
	}
}
