package browser

import (
	"image"
	"os"
	"strings"
	"testing"
)

// The Moon header pulls its icon-only main-menu button out of the header's
// padding with negative margins, inside a media query whose feature value is
// itself a calc(). All three pieces are needed to place the wordmark (#285).

func TestMediaFeatureValueKeepsItsOwnParentheses(t *testing.T) {
	for _, tc := range []struct {
		query    string
		viewport image.Point
		want     bool
	}{
		{"screen and (max-width:calc(1120px - 1px))", image.Pt(800, 600), true},
		{"screen and (max-width:calc(1120px - 1px))", image.Pt(1120, 600), false},
		{"screen and (min-width:calc(800px - 1px))", image.Pt(800, 600), true},
		{"screen and (min-width:calc(800px + 1px))", image.Pt(800, 600), false},
		{"screen and (min-width:calc(320px)) and (max-width:calc(900px))", image.Pt(800, 600), true},
		{"screen and (max-width:calc(1120px - 1px)) and (min-width:calc(900px))", image.Pt(800, 600), false},
		// Values that are not lengths still fail closed.
		{"screen and (max-width:calc(50% - 1px))", image.Pt(800, 600), false},
	} {
		if got := mediaQueryMatches(tc.query, tc.viewport); got != tc.want {
			t.Errorf("mediaQueryMatches(%q, %v) = %v, want %v", tc.query, tc.viewport, got, tc.want)
		}
	}
}

func TestNegativeHorizontalMarginsShiftBoxes(t *testing.T) {
	document := parseStyledHTML(t, `<style>
.wrap { padding-left: 40px; width: 200px; }
.pull { margin-left: calc(-1 * 12px); margin-right: -8px; width: 50px; height: 10px; }
.push { margin-left: 12px; width: 50px; height: 10px; }
.pad { padding-left: -10px; width: 50px; height: 10px; }
</style><div class="wrap"><div class="pull"></div><div class="push"></div><div class="pad"></div></div>`)

	layout, err := LayoutWithViewport(document, image.Rect(0, 0, 800, 600))
	if err != nil {
		t.Fatalf("layout: %v", err)
	}
	pull := findBoxByClass(t, layout.Root, "pull")
	if got := pull.Rect.Min.X; got != 28 {
		t.Errorf("negative margin-left: box starts at x=%d, want 28", got)
	}
	push := findBoxByClass(t, layout.Root, "push")
	if got := push.Rect.Min.X; got != 52 {
		t.Errorf("positive margin-left: box starts at x=%d, want 52", got)
	}
	// Negative padding stays invalid and is clamped to zero.
	pad := findBoxByClass(t, layout.Root, "pad")
	if got := pad.Content.Min.X; got != 40 {
		t.Errorf("negative padding-left: content starts at x=%d, want 40", got)
	}
}

func TestAtomicInlineHonorsMinWidth(t *testing.T) {
	document := parseStyledHTML(t, `<style>
body { margin: 0; }
.button { display: inline-flex; box-sizing: border-box; min-width: 44px;
	border: 1px solid #000; padding: 0; }
.icon { display: inline-block; width: 20px; height: 20px; }
.content-box { display: inline-block; min-width: 44px; border: 1px solid #000; }
</style><div><span class="button"><span class="icon"></span></span></div>
<div><span class="content-box"><span class="icon"></span></span></div>`)

	layout, err := LayoutWithViewport(document, image.Rect(0, 0, 800, 600))
	if err != nil {
		t.Fatalf("layout: %v", err)
	}
	// border-box: the 44px minimum includes the 1px borders.
	if got := findBoxByClass(t, layout.Root, "button").Rect.Dx(); got != 44 {
		t.Errorf("border-box min-width: border box is %dpx wide, want 44", got)
	}
	// content-box: the 44px minimum applies to the content only.
	if got := findBoxByClass(t, layout.Root, "content-box").Rect.Dx(); got != 46 {
		t.Errorf("content-box min-width: border box is %dpx wide, want 46", got)
	}
}

// TestMoonHeaderWordmarkPosition pins the reported symptom of #285: the
// Wikipedia wordmark starts at x=64, as it does in the Chrome reference.
func TestMoonHeaderWordmarkPosition(t *testing.T) {
	fetcher := &Fetcher{}
	resource, err := fetcher.Fetch("../../testdata/wikipedia-moon/moon.html")
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	document, err := parse(resource)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	styled, err := style(document, fetcher)
	if err != nil {
		t.Fatalf("style: %v", err)
	}
	laid, err := LayoutWithViewport(styled, image.Rect(0, 0, 800, 600))
	if err != nil {
		t.Fatalf("layout: %v", err)
	}
	wordmark := findBoxByClass(t, laid.Root, "mw-logo-wordmark")
	if got := wordmark.Rect.Min.X; got != 64 {
		t.Errorf("wordmark starts at x=%d, want 64 (Chrome reference)", got)
	}
	// The main-menu button keeps its 44px icon-only minimum and is pulled
	// 12px into the header padding.
	label := findBoxByID(t, laid.Root, "vector-main-menu-dropdown-label")
	if label.Rect.Min.X != 12 || label.Rect.Dx() != 44 {
		t.Errorf("main-menu button = %v, want x=12 width=44", label.Rect)
	}
}

func parseStyledHTML(t *testing.T, markup string) StyledDocument {
	t.Helper()
	file, err := os.CreateTemp(t.TempDir(), "*.html")
	if err != nil {
		t.Fatalf("temp file: %v", err)
	}
	if _, err := file.WriteString(markup); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := file.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	fetcher := &Fetcher{}
	resource, err := fetcher.Fetch(file.Name())
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	document, err := parse(resource)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	styled, err := style(document, fetcher)
	if err != nil {
		t.Fatalf("style: %v", err)
	}
	return styled
}

func findBoxByClass(t *testing.T, root *Box, class string) *Box {
	t.Helper()
	box := findBox(root, func(n *Node) bool {
		attribute, ok := n.Attribute("class")
		if !ok {
			return false
		}
		for _, field := range strings.Fields(attribute.Value) {
			if field == class {
				return true
			}
		}
		return false
	})
	if box == nil {
		t.Fatalf("no box for class %q", class)
	}
	return box
}

func findBoxByID(t *testing.T, root *Box, id string) *Box {
	t.Helper()
	box := findBox(root, func(n *Node) bool {
		attribute, ok := n.Attribute("id")
		return ok && attribute.Value == id
	})
	if box == nil {
		t.Fatalf("no box for id %q", id)
	}
	return box
}

func findBox(box *Box, match func(*Node) bool) *Box {
	if box == nil {
		return nil
	}
	if box.Node != nil && box.Node.Type == ElementNode && match(box.Node) {
		return box
	}
	for _, child := range box.Children {
		if found := findBox(child, match); found != nil {
			return found
		}
	}
	return nil
}
