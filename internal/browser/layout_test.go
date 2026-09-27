package browser

import (
	"image"
	"strings"
	"sync"
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
	if outer.Rect.Min.X != 4 || outer.Content.Dx() != 140 || outer.Rect.Dx() != 160 {
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
			text += run.Text
		}
	}
	if text != "a b c" {
		t.Fatalf("collapsed text = %#v", p.Children)
	}
}

func TestLayoutSharedInlineFlowAndStyleIdentity(t *testing.T) {
	doc := styledForLayout(t, `<p style="margin:0">Hello <b style="font-size:20px">world</b>!<span>Joined</span><span>Up</span><br>next<br><br>end</p>`)
	got, err := LayoutWithViewport(doc, image.Rect(0, 0, 600, 200))
	if err != nil {
		t.Fatal(err)
	}
	p := got.Root.Children[0]
	if len(p.Children) != 1 {
		t.Fatalf("inline sibling boxes = %d, want one shared flow", len(p.Children))
	}
	runs := p.Children[0].Text
	var text strings.Builder
	for _, r := range runs {
		text.WriteString(r.Text)
	}
	if text.String() != "Hello world!JoinedUpnextend" {
		t.Fatalf("text = %q", text.String())
	}
	for i, r := range runs {
		if r.Node == nil || r.Style == nil {
			t.Fatalf("run %d missing source/style: %+v", i, r)
		}
	}
	if runs[0].Rect.Min.Y != runs[1].Rect.Min.Y || runs[1].Rect.Min.Y != runs[2].Rect.Min.Y {
		t.Fatalf("adjacent elements did not share a line: %+v", runs)
	}
	if runs[1].Style["font-size"] != "20px" || runs[0].Style["font-size"] == "20px" {
		t.Fatalf("nested inline style lost: %+v", runs)
	}
	if runs[2].Rect.Min.X != runs[1].Rect.Max.X {
		t.Fatalf("adjacent runs do not meet: %+v", runs)
	}
	if runs[len(runs)-1].Rect.Min.Y <= runs[0].Rect.Min.Y+20 {
		t.Fatalf("br did not advance lines: %+v", runs)
	}
	if p.Content.Dy() < 4*19 {
		t.Fatalf("consecutive br lines missing: %v", p.Content)
	}
}

func TestLayoutInlineWrappingAcrossNodeBoundaries(t *testing.T) {
	doc := styledForLayout(t, `<p style="margin:0">abc<b>def</b> ghi</p>`)
	got, err := LayoutWithViewport(doc, image.Rect(0, 0, 75, 200))
	if err != nil {
		t.Fatal(err)
	}
	runs := got.Root.Children[0].Children[0].Text
	if len(runs) < 3 || runs[0].Rect.Min.Y != runs[1].Rect.Min.Y ||
		runs[2].Rect.Min.Y <= runs[1].Rect.Min.Y {
		t.Fatalf("split word should stay together, next word should wrap: %+v", runs)
	}
}

func TestLayoutTrimsTrailingCollapsibleWhitespacePerLine(t *testing.T) {
	doc := styledForLayout(t, `<p style="margin:0"><a>first word </a><br><a>last  </a></p>`)
	got, err := LayoutWithViewport(doc, image.Rect(0, 0, 300, 100))
	if err != nil {
		t.Fatal(err)
	}
	runs := got.Root.Children[0].Children[0].Text
	var text strings.Builder
	for _, run := range runs {
		text.WriteString(run.Text)
		if strings.HasSuffix(run.Text, " ") {
			t.Errorf("line-ending run retains collapsible whitespace: %q", run.Text)
		}
	}
	if text.String() != "first wordlast" {
		t.Fatalf("line text = %q, want trailing whitespace removed", text.String())
	}
	if len(runs) < 2 || !strings.Contains(runs[0].Text, " ") {
		t.Fatalf("space between words was not retained: %+v", runs)
	}
}

func TestLayoutClampsNarrowContentWidth(t *testing.T) {
	doc := styledForLayout(t, `<div style="padding: 20px; border-width: 5px"><p style="margin:0">long word</p></div>`)
	got, err := LayoutWithViewport(doc, image.Rect(0, 0, 30, 200))
	if err != nil {
		t.Fatal(err)
	}
	outer := got.Root.Children[0]
	if outer.Content.Dx() != 0 || outer.Rect.Dx() != 50 {
		t.Fatalf("narrow box geometry = %v, content %v", outer.Rect, outer.Content)
	}
	p := outer.Children[0]
	if p.Content.Dx() != 0 || p.Rect.Dx() != 0 || len(p.Children) != 1 {
		t.Fatalf("nested narrow box geometry = %+v", p)
	}
}

func TestLayoutConcurrent(t *testing.T) {
	doc := styledForLayout(t, `<p style="margin:0">many <b style="font-size:22px">inline</b> siblings<br>on another line</p>`)
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 10; j++ {
				got, err := LayoutWithViewport(doc, image.Rect(0, 0, 240, 200))
				if err != nil || len(got.Root.Children) != 1 || len(got.Root.Children[0].Children) != 1 {
					t.Errorf("concurrent layout: %v, %+v", err, got.Root)
					return
				}
			}
		}()
	}
	wg.Wait()
}
