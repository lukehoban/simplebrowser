package browser

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"testing"
)

// The HTML hidden attribute maps to display:none at UA level (HTML
// Rendering §15.3.1), so every boolean form suppresses boxes while author CSS
// can still override it through the normal cascade.
const hiddenFixture = `<!doctype html>
<style>
  body { margin: 0; }
  div { height: 10px; }
  #shown-by-author { display: block; background: rgb(0, 0, 255); }
</style>
<div id=bare hidden style="background: rgb(255, 0, 0)">bare</div>
<div id=empty hidden="" style="background: rgb(255, 0, 0)">empty</div>
<div id=named hidden="hidden" style="background: rgb(255, 0, 0)"><span hidden>nested</span></div>
<div id=upper HIDDEN style="background: rgb(255, 0, 0)">upper</div>
<div id=visible style="background: rgb(0, 128, 0)"></div>
<div id=shown-by-author hidden></div>`

func TestParseHTMLKeepsHiddenAttributeForms(t *testing.T) {
	root := ParseHTML(hiddenFixture)
	for id, want := range map[string]string{"bare": "", "empty": "", "named": "hidden", "upper": ""} {
		n := findNodeByID(root, id)
		if n == nil {
			t.Fatalf("no element %q", id)
		}
		attr, ok := n.Attribute("hidden")
		if !ok || attr.Value != want {
			t.Errorf("%s hidden attribute = %#v, %v; want present %q", id, attr, ok, want)
		}
	}
}

func TestUserAgentHiddenAttributeCascade(t *testing.T) {
	styled := styledForLayout(t, hiddenFixture)
	for _, id := range []string{"bare", "empty", "named", "upper"} {
		if got := styledElementByID(styled.StyleRoot, id).Style["display"]; got != "none" {
			t.Errorf("%s display = %q, want UA none", id, got)
		}
	}
	if got := styledElementByID(styled.StyleRoot, "visible").Style["display"]; got != "block" {
		t.Errorf("visible display = %q, want block", got)
	}
	if got := styledElementByID(styled.StyleRoot, "shown-by-author").Style["display"]; got != "block" {
		t.Errorf("author override display = %q, want block", got)
	}
}

func TestLayoutHiddenAttributeDoesNotDisplaceVisibleSibling(t *testing.T) {
	styled := styledForLayout(t, hiddenFixture)
	got, err := LayoutWithViewport(styled, image.Rect(0, 0, 40, 40))
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"bare", "empty", "named", "upper"} {
		if box := findBox(got.Root, func(n *Node) bool {
			a, ok := n.Attribute("id")
			return ok && a.Value == id
		}); box != nil {
			t.Errorf("%s generated box %v", id, box.Rect)
		}
	}
	if r := findBoxByID(t, got.Root, "visible").Rect; r != image.Rect(0, 0, 40, 10) {
		t.Errorf("visible rect = %v, want (0,0)-(40,10)", r)
	}
	if r := findBoxByID(t, got.Root, "shown-by-author").Rect; r != image.Rect(0, 10, 40, 20) {
		t.Errorf("author-shown rect = %v, want (0,10)-(40,20)", r)
	}

	var out bytes.Buffer
	if err := paint(got, &out, renderOptions{}); err != nil {
		t.Fatal(err)
	}
	img, err := png.Decode(&out)
	if err != nil {
		t.Fatal(err)
	}
	rgba := img.(*image.RGBA)
	pixel(t, rgba, 20, 5, color.RGBA{0, 128, 0, 255})
	pixel(t, rgba, 20, 15, color.RGBA{0, 0, 255, 255})
	pixel(t, rgba, 20, 30, color.RGBA{255, 255, 255, 255})
}

func findNodeByID(n *Node, id string) *Node {
	if n == nil {
		return nil
	}
	if n.Type == ElementNode {
		if a, ok := n.Attribute("id"); ok && a.Value == id {
			return n
		}
	}
	for _, c := range n.Children {
		if found := findNodeByID(c, id); found != nil {
			return found
		}
	}
	return nil
}
