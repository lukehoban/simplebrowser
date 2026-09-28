package browser

import (
	"image"
	"sort"
	"strings"
	"testing"
)

// lineTops returns the distinct top edges of the text runs painted inside the
// box with the given id, keyed by run text, so tests can count lines.
func lineTops(t *testing.T, root *Box, id string) (map[string]int, []int) {
	t.Helper()
	box := boxesByID(root, id)[id]
	if box == nil {
		t.Fatalf("no box with id %q", id)
	}
	byText := map[string]int{}
	seen := map[int]bool{}
	var tops []int
	for _, run := range flexBoxTextRuns(box) {
		byText[run.Text] = run.Rect.Min.Y
		if !seen[run.Rect.Min.Y] {
			seen[run.Rect.Min.Y] = true
			tops = append(tops, run.Rect.Min.Y)
		}
	}
	sort.Ints(tops)
	return byText, tops
}

func TestWhiteSpaceNowrapLineBreaking(t *testing.T) {
	const style = `<body style="margin:0;font:16px sans-serif">`
	cases := []struct {
		name   string
		source string
		lines  int
	}{
		{"normal wraps at spaces", `<div id="t" style="width:100px">Security and quality settings</div>`, 3},
		{"nowrap block stays on one line", `<div id="t" style="width:100px;white-space:nowrap">Security and quality settings</div>`, 1},
		{"nowrap inherits through inline elements", `<div id="t" style="width:100px;white-space:nowrap">Pull <b>requests</b> and <i>code review</i> history</div>`, 1},
		{"inherit keyword", `<div style="white-space:nowrap"><div id="t" style="width:100px;white-space:inherit">Security and quality settings</div></div>`, 1},
		{"normal descendant restores wrapping", `<div style="white-space:nowrap"><div id="t" style="width:100px;white-space:normal">Security and quality settings</div></div>`, 3},
		{"nowrap parent glues adjacent inline-blocks", `<div id="t" style="width:60px;white-space:nowrap">A<span style="display:inline-block;width:30px">1</span> <span style="display:inline-block;width:30px">2</span> <span style="display:inline-block;width:30px">3</span></div>`, 1},
		{"atomic inline's own nowrap does not glue its siblings", `<div id="t" style="width:60px">A <span style="display:inline-block;width:30px;white-space:nowrap">1</span> <span style="display:inline-block;width:30px;white-space:nowrap">2</span> <span style="display:inline-block;width:30px;white-space:nowrap">3</span></div>`, 3},
		{"unsupported pre-wrap keeps normal wrapping", `<div id="t" style="width:100px;white-space:pre-wrap">Security and quality settings</div>`, 3},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root := layoutMarkup(t, style+tc.source+`</body>`, image.Rect(0, 0, 400, 200))
			_, tops := lineTops(t, root, "t")
			if len(tops) != tc.lines {
				t.Errorf("got %d lines (tops %v), want %d", len(tops), tops, tc.lines)
			}
		})
	}
}

func TestWhiteSpaceNowrapMixedInlineBoundaries(t *testing.T) {
	const style = `<body style="margin:0;font:16px sans-serif">`
	t.Run("nowrap span moves as one unit inside wrapping text", func(t *testing.T) {
		root := layoutMarkup(t, style+`<div id="t" style="width:150px">Open the <span style="white-space:nowrap">Security and quality</span> tab now</div></body>`, image.Rect(0, 0, 400, 200))
		byText, tops := lineTops(t, root, "t")
		if len(tops) != 3 {
			t.Fatalf("got lines %v (%v), want 3", tops, byText)
		}
		if byText["Security and quality"] == byText["Open the"] || byText["Security and quality"] == byText["tab now"] {
			t.Errorf("nowrap span should occupy its own line: %v", byText)
		}
	})
	t.Run("space in nowrap context before a normal span is not a break", func(t *testing.T) {
		root := layoutMarkup(t, style+`<div id="t" style="width:60px;white-space:nowrap">One line <span style="white-space:normal">but this part may wrap</span></div></body>`, image.Rect(0, 0, 400, 200))
		byText, tops := lineTops(t, root, "t")
		if len(tops) < 2 {
			t.Fatalf("normal span should still wrap internally: %v", byText)
		}
		foundBut := false
		for text, top := range byText {
			if top == tops[0] && strings.HasPrefix(strings.TrimSpace(text), "but") {
				foundBut = true
			}
		}
		if !foundBut {
			t.Errorf("\"but\" should stay on the first line after the nowrap space: %v", byText)
		}
	})
}

func TestWhiteSpaceNowrapIntrinsicWidths(t *testing.T) {
	const style = `<body style="margin:0;font:16px sans-serif">`
	t.Run("table column min-content keeps nowrap cell text on one line", func(t *testing.T) {
		root := layoutMarkup(t, style+`<table style="width:200px;border-collapse:collapse"><tr><td id="c" style="padding:0;white-space:nowrap">Security and quality</td><td style="padding:0">a b c d e f g h i j k l m n o p q r s t u v w x y z a b c d e f g h</td></tr></table></body>`, image.Rect(0, 0, 400, 200))
		byText, tops := lineTops(t, root, "c")
		if len(tops) != 1 {
			t.Fatalf("nowrap cell wrapped: %v", byText)
		}
		cell := boxesByID(root, "c")["c"]
		var textRight int
		for _, run := range flexBoxTextRuns(cell) {
			textRight = max(textRight, run.Rect.Max.X)
		}
		if cell.Rect.Max.X < textRight {
			t.Errorf("cell %v narrower than its nowrap text (right edge %d)", cell.Rect, textRight)
		}
	})
	t.Run("shrink-to-fit float keeps nowrap text on one line", func(t *testing.T) {
		root := layoutMarkup(t, style+`<div style="width:60px"><div id="f" style="float:left;white-space:nowrap">Security and quality</div></div></body>`, image.Rect(0, 0, 400, 200))
		if _, tops := lineTops(t, root, "f"); len(tops) != 1 {
			t.Errorf("nowrap float wrapped onto %d lines", len(tops))
		}
	})
	t.Run("flex item automatic minimum honours nowrap (GitHub tabs)", func(t *testing.T) {
		root := layoutMarkup(t, style+`<nav style="display:flex;width:120px;overflow:hidden;white-space:nowrap">
			<a id="pulls" style="display:flex;align-items:center;padding:0 8px">Pull requests<span style="margin-left:8px">12</span></a>
			<a id="security" style="display:flex;align-items:center;padding:0 8px">Security and quality<span style="margin-left:8px">3</span></a>
		</nav></body>`, image.Rect(0, 0, 400, 200))
		for _, id := range []string{"pulls", "security"} {
			if byText, tops := lineTops(t, root, id); len(tops) != 1 {
				t.Errorf("%s tab label wrapped: %v", id, byText)
			}
		}
	})
}

func TestWhiteSpaceComputedValueInherits(t *testing.T) {
	doc := styledForLayout(t, `<body><div style="white-space:nowrap"><span class="c">x</span><span class="i" style="white-space:initial">y</span></div></body>`)
	if got := styledByClass(doc.StyleRoot, "c").Style["white-space"]; got != "nowrap" {
		t.Errorf("child white-space = %q, want inherited nowrap", got)
	}
	if got := styledByClass(doc.StyleRoot, "i").Style["white-space"]; got != "normal" {
		t.Errorf("initial white-space = %q, want normal", got)
	}
}
