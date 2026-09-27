package browser

import (
	"image"
	"image/color"
	"testing"
)

// <template> contents are parsed into a detached fragment (HTML "template
// contents") and the element itself is display:none in the UA stylesheet, so
// nothing inside a template styles, loads or paints, and it takes no space.
const templateFixture = `<!doctype html>
<style>
  body { margin: 0; }
  div { height: 10px; }
  #shown { display: block; }
</style>
<div id=before style="background: rgb(0, 128, 0)"></div>
<template id=tpl><style>#after { background: rgb(255, 0, 0) !important; }</style><div id=inner style="background: rgb(255, 0, 0)">{{ message }}</div></template>
<div id=after style="background: rgb(0, 0, 255)"></div>
<template id=shown><div id=shown-inner style="background: rgb(255, 0, 0)">shown</div></template>
<div id=last style="background: rgb(0, 128, 0)"></div>`

func templateNodeByID(n *Node, id string) *Node {
	if n == nil {
		return nil
	}
	if a, ok := n.Attribute("id"); ok && n.Type == ElementNode && a.Value == id {
		return n
	}
	for _, c := range n.Children {
		if found := templateNodeByID(c, id); found != nil {
			return found
		}
	}
	return nil
}

func TestParseTemplateContentsIntoFragment(t *testing.T) {
	root := ParseHTML(templateFixture)
	tpl := templateNodeByID(root, "tpl")
	if tpl == nil {
		t.Fatal("no template element")
	}
	if len(tpl.Children) != 0 {
		t.Errorf("template has %d children, want 0 (contents live in a fragment)", len(tpl.Children))
	}
	if tpl.Content == nil || tpl.Content.Type != DocumentNode {
		t.Fatalf("template content = %#v, want DocumentNode fragment", tpl.Content)
	}
	if templateNodeByID(root, "inner") != nil {
		t.Error("template contents reachable through the document tree")
	}
	inner := templateNodeByID(tpl.Content, "inner")
	if inner == nil || inner.Parent == nil || inner.Parent != tpl.Content {
		t.Fatalf("inner not parented by template content fragment: %#v", inner)
	}
	if len(inner.Children) != 1 || inner.Children[0].Data != "{{ message }}" {
		t.Errorf("inner children = %#v, want {{ message }} text", inner.Children)
	}
	if templateNodeByID(root, "after") == nil || templateNodeByID(root, "after").Parent != tpl.Parent {
		t.Error("following sibling not a sibling of the template")
	}
}

func TestParseTemplateIsScopeBoundary(t *testing.T) {
	root := ParseHTML(`<p id=p>a<template><div>x</div></div>y</template>tail</p>` +
		`<ul><li id=li>one<template><li>two</template>three</li></ul>` +
		`<table><tr id=tr><td>c<template><tr><td>t</td></tr></template>d</td></tr></table>`)
	p := templateNodeByID(root, "p")
	if p == nil || len(p.Children) != 3 || p.Children[1].Name != "template" || p.Children[2].Data != "tail" {
		t.Fatalf("outer <p> was closed by template contents: %#v", p)
	}
	content := p.Children[1].Content
	if len(content.Children) != 2 || content.Children[0].Name != "div" || content.Children[1].Data != "y" {
		t.Errorf("template content = %#v, want div then y", content.Children)
	}
	li := templateNodeByID(root, "li")
	if li == nil || len(li.Children) != 3 || li.Children[2].Data != "three" {
		t.Fatalf("outer <li> was closed by template contents: %#v", li)
	}
	if got := li.Children[1].Content.Children; len(got) != 1 || got[0].Name != "li" {
		t.Errorf("li template content = %#v", got)
	}
	tr := templateNodeByID(root, "tr")
	if tr == nil || len(tr.Children) != 1 || len(tr.Children[0].Children) != 3 {
		t.Fatalf("outer table row/cell closed by template contents: %#v", tr)
	}
}

func TestUserAgentTemplateCascade(t *testing.T) {
	styled := styledForLayout(t, templateFixture)
	if got := styledElementByID(styled.StyleRoot, "tpl").Style["display"]; got != "none" {
		t.Errorf("template display = %q, want UA none", got)
	}
	if got := styledElementByID(styled.StyleRoot, "shown").Style["display"]; got != "block" {
		t.Errorf("author-shown template display = %q, want block", got)
	}
	if got := styledElementByID(styled.StyleRoot, "after").Style["background-color"]; got == "rgb(255, 0, 0)" || got == "red" {
		t.Errorf("<style> inside template applied: after background = %q", got)
	}
	for _, id := range []string{"inner", "shown-inner"} {
		if n := styledElementByID(styled.StyleRoot, id); n != nil {
			t.Errorf("template content %q was styled", id)
		}
	}
}

func TestLayoutAndPaintTemplateTakesNoSpace(t *testing.T) {
	styled := styledForLayout(t, templateFixture)
	got, err := LayoutWithViewport(styled, image.Rect(0, 0, 40, 40))
	if err != nil {
		t.Fatal(err)
	}
	if box := findBox(got.Root, func(n *Node) bool { a, ok := n.Attribute("id"); return ok && a.Value == "tpl" }); box != nil {
		t.Errorf("template generated box %v", box.Rect)
	}
	for id, want := range map[string]image.Rectangle{
		"before": image.Rect(0, 0, 40, 10),
		"after":  image.Rect(0, 10, 40, 20),
		"shown":  image.Rect(0, 20, 40, 20), // displayed, but has no children
		"last":   image.Rect(0, 20, 40, 30),
	} {
		if r := findBoxByID(t, got.Root, id).Rect; r != want {
			t.Errorf("%s rect = %v, want %v", id, r, want)
		}
	}

	img := painted(t, templateFixture, image.Rect(0, 0, 40, 40))
	pixel(t, img, 20, 5, color.RGBA{0, 128, 0, 255})
	pixel(t, img, 20, 15, color.RGBA{0, 0, 255, 255})
	pixel(t, img, 20, 25, color.RGBA{0, 128, 0, 255})
	pixel(t, img, 20, 35, color.RGBA{255, 255, 255, 255})
	for y := 0; y < 40; y++ {
		for x := 0; x < 40; x++ {
			if c := img.RGBAAt(x, y); c.R > 200 && c.G < 80 && c.B < 80 {
				t.Fatalf("template content painted red at (%d,%d)", x, y)
			}
		}
	}
}
