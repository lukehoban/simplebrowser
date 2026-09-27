package browser

import (
	"image"
	"testing"
)

func styledForLayout(t *testing.T, source string) StyledDocument {
	t.Helper()
	doc := Document{Root: ParseHTML(source)}
	result, err := style(doc, nil)
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func TestLayoutBlockGeometryAndWrapping(t *testing.T) {
	doc := styledForLayout(t, `<div style="width: 140px; padding: 10px; margin: 4px"><p style="margin: 0">one two three four five six seven</p></div>`)
	got, err := LayoutWithViewport(doc, image.Rect(0, 0, 300, 400))
	if err != nil {
		t.Fatal(err)
	}
	if got.Root == nil || len(got.Root.Children) != 1 {
		t.Fatalf("root children = %#v", got.Root)
	}
	outer := got.Root.Children[0]
	if outer.Rect.Min.X != 4 || outer.Content.Dx() != 140 {
		t.Fatalf("outer geometry = rect %v content %v", outer.Rect, outer.Content)
	}
	if len(outer.Children) != 1 || len(outer.Children[0].Children) != 1 ||
		len(outer.Children[0].Children[0].Text) < 2 {
		t.Fatalf("expected wrapped paragraph text, got %#v", outer.Children)
	}
	runs := outer.Children[0].Children[0].Text
	if runs[0].Rect.Min.Y >= runs[1].Rect.Min.Y {
		t.Fatal("text runs did not advance vertically")
	}
}

func TestLayoutCollapsesWhitespaceAndHonorsDisplayNone(t *testing.T) {
	doc := styledForLayout(t, `<p>a   <span>b</span> <span style="display:none">hidden</span> c</p>`)
	got, err := LayoutWithViewport(doc, image.Rect(0, 0, 200, 100))
	if err != nil {
		t.Fatal(err)
	}
	p := got.Root.Children[0]
	var text string
	for _, child := range p.Children {
		for _, run := range child.Text {
			if text != "" {
				text += " "
			}
			text += run.Text
		}
	}
	if text != "a b c" {
		t.Fatalf("collapsed text = %#v", p.Children)
	}
}
