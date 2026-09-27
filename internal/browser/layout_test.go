package browser

import (
	"image"
	"os"
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

func TestAbsolutelyPositionedBoxesDoNotContributeToFlowHeight(t *testing.T) {
	doc := styledForLayout(t, `<div id="parent" style="margin:0;padding:10px">
		<div id="out" style="position:absolute;width:20px;height:30px;margin:20px"></div>
		<div id="flow" style="width:40px;height:10px"></div>
	</div>`)
	got, err := LayoutWithViewport(doc, image.Rect(0, 0, 200, 120))
	if err != nil {
		t.Fatal(err)
	}
	boxes := boxesByID(got.Root, "parent", "out", "flow")
	parent, out, flow := boxes["parent"], boxes["out"], boxes["flow"]
	if parent.Content.Dy() != 10 {
		t.Fatalf("parent content height = %d, want only the 10px in-flow child", parent.Content.Dy())
	}
	if len(parent.Children) != 2 || parent.Children[1] != out {
		t.Fatalf("positioned box should remain in the layout tree after in-flow siblings: %#v", parent.Children)
	}
	if out.Rect.Min.Y != flow.Rect.Min.Y || out.Rect.Dy() != 30 {
		t.Fatalf("positioned geometry = %v, flow geometry = %v", out.Rect, flow.Rect)
	}
}

func TestPositionedOffsetsUseNearestPositionedAncestorPaddingBox(t *testing.T) {
	doc := styledForLayout(t, `<div id="outer" style="position:relative;margin:0;padding:10px;width:100px;height:80px">
		<div id="static">
			<div id="abs" style="position:absolute;left:5px;top:7px;width:10px;height:12px"></div>
			<div id="right-bottom" style="position:absolute;right:5px;bottom:6px;width:10px;height:12px"></div>
		</div>
	</div>`)
	got, err := LayoutWithViewport(doc, image.Rect(0, 0, 300, 200))
	if err != nil {
		t.Fatal(err)
	}
	boxes := boxesByID(got.Root, "outer", "abs", "right-bottom")
	outer, abs, rightBottom := boxes["outer"], boxes["abs"], boxes["right-bottom"]
	want := image.Pt(outer.Rect.Min.X+5, outer.Rect.Min.Y+7)
	if abs.Rect.Min != want {
		t.Fatalf("absolute box origin = %v, want %v from padding-box origin %v", abs.Rect.Min, want, outer.Rect.Min)
	}
	if rightBottom.Rect.Max.X != outer.Rect.Max.X-5 || rightBottom.Rect.Max.Y != outer.Rect.Max.Y-6 {
		t.Fatalf("right/bottom offsets = %v, want bottom-right (%d,%d)", rightBottom.Rect,
			outer.Rect.Max.X-5, outer.Rect.Max.Y-6)
	}
}

func TestFixedPositionOffsetsUseViewport(t *testing.T) {
	doc := styledForLayout(t, `<div style="position:relative;margin:0;width:100px;height:80px">
		<div id="fixed" style="position:fixed;left:3px;top:4px;width:10px;height:12px"></div>
	</div>`)
	viewport := image.Rect(20, 30, 320, 230)
	got, err := LayoutWithViewport(doc, viewport)
	if err != nil {
		t.Fatal(err)
	}
	fixed := boxesByID(got.Root, "fixed")["fixed"]
	if fixed.Rect.Min != image.Pt(viewport.Min.X+3, viewport.Min.Y+4) {
		t.Fatalf("fixed box origin = %v, want viewport-relative (%d,%d)", fixed.Rect.Min,
			viewport.Min.X+3, viewport.Min.Y+4)
	}
}

func TestPositionedAutoOffsetsUseStaticPosition(t *testing.T) {
	doc := styledForLayout(t, `<div id="parent" style="margin:0">
		<div id="before" style="width:20px;height:11px"></div>
		<div id="abs" style="position:absolute;left:auto;top:auto;width:8px;height:6px"></div>
	</div>`)
	got, err := LayoutWithViewport(doc, image.Rect(0, 0, 200, 100))
	if err != nil {
		t.Fatal(err)
	}
	boxes := boxesByID(got.Root, "before", "abs")
	before, abs := boxes["before"], boxes["abs"]
	if abs.Rect.Min.X != before.Rect.Min.X || abs.Rect.Min.Y != before.Rect.Max.Y {
		t.Fatalf("auto-offset static position = %v, want (%d,%d)", abs.Rect.Min, before.Rect.Min.X, before.Rect.Max.Y)
	}
}

func TestAnonymousTableWPTAbsoluteBoxDoesNotMoveTable(t *testing.T) {
	source, err := os.ReadFile("../../testdata/wpt/tables/anonymous-table-box-width-001.xht")
	if err != nil {
		t.Fatal(err)
	}
	doc := styledForLayout(t, string(source))
	got, err := LayoutWithViewport(doc, image.Rect(0, 0, 800, 600))
	if err != nil {
		t.Fatal(err)
	}
	table := boxesByID(got.Root, "overlapping-green")["overlapping-green"]
	if table.Rect.Min.Y != 52 {
		t.Fatalf("table starts at y=%d, want y=52; absolute block affected flow", table.Rect.Min.Y)
	}
}

func boxesByID(root *Box, ids ...string) map[string]*Box {
	wanted := make(map[string]bool, len(ids))
	found := make(map[string]*Box, len(ids))
	for _, id := range ids {
		wanted[id] = true
	}
	var walk func(*Box)
	walk = func(box *Box) {
		if box.Node != nil {
			if value, ok := box.Node.Attribute("id"); ok && wanted[value.Value] {
				found[value.Value] = box
			}
		}
		for _, child := range box.Children {
			walk(child)
		}
	}
	walk(root)
	for _, id := range ids {
		if found[id] == nil {
			panic("missing box id " + id)
		}
	}
	return found
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
	faces := newFaceSet()
	defer faces.close()
	baseline := func(r TextRun) int {
		ascent, _ := faces.metrics(r.Style).lineMetrics()
		return r.Rect.Min.Y + ascent
	}
	if baseline(runs[0]) != baseline(runs[1]) || baseline(runs[1]) != baseline(runs[2]) {
		t.Fatalf("adjacent elements did not share a baseline: %+v", runs)
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

func TestLayoutBlockInsideInlineSplitsLinesWithoutAligningBlock(t *testing.T) {
	doc := styledForLayout(t, `<section style="width:200px;text-align:right"><span><b style="color:red">before<div style="width:40px">block</div>after</b></span><i>tail</i></section>`)
	got, err := LayoutWithViewport(doc, image.Rect(0, 0, 300, 200))
	if err != nil {
		t.Fatal(err)
	}
	section := got.Root.Children[0]
	if len(section.Children) != 3 {
		t.Fatalf("expected inline fragment, block, inline fragment: %#v", section.Children)
	}
	before, block, after := section.Children[0], section.Children[1], section.Children[2]
	if len(before.Text) != 1 || before.Text[0].Text != "before" ||
		len(block.Children) != 1 || block.Children[0].Text[0].Text != "block" ||
		len(after.Text) != 2 || after.Text[0].Text != "after" || after.Text[1].Text != "tail" {
		t.Fatalf("split lost ordered inline content: %#v", section.Children)
	}
	if before.Text[0].Style["color"] != "red" || after.Text[0].Style["color"] != "red" {
		t.Fatal("inline descendants lost their inherited style")
	}
	if before.Text[0].Rect.Max.X != section.Content.Max.X ||
		after.Text[1].Rect.Max.X != section.Content.Max.X {
		t.Fatalf("text-align:right did not align fragments: %#v", section.Children)
	}
	if block.Rect.Min.X != section.Content.Min.X || block.Rect.Dx() != 40 {
		t.Fatalf("block wrongly aligned by text-align: %v", block.Rect)
	}
	if before.Rect.Max.Y != block.Rect.Min.Y || block.Rect.Max.Y != after.Rect.Min.Y {
		t.Fatalf("block did not split the inline lines: %#v", section.Children)
	}
}

func TestLayoutBlockInsideInlineWithEmptySides(t *testing.T) {
	doc := styledForLayout(t, `<div><span><div>one</div><span style="display:none">hidden</span><div>two</div></span></div>`)
	got, err := LayoutWithViewport(doc, image.Rect(0, 0, 200, 200))
	if err != nil {
		t.Fatal(err)
	}
	children := got.Root.Children[0].Children
	if len(children) != 2 || children[0].Children[0].Text[0].Text != "one" ||
		children[1].Children[0].Text[0].Text != "two" {
		t.Fatalf("empty inline fragments should not create line boxes: %#v", children)
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

func TestLayoutInlineWrapsUsingUnroundedLineWidth(t *testing.T) {
	doc := styledForLayout(t, `<p style="margin:0">hello world</p>`)
	faces := newFaceSet()
	defer faces.close()
	exactWidth := faces.metrics(doc.StyleRoot.Style).width("hello world")

	for _, tc := range []struct {
		name      string
		width     int
		wantLines int
	}{
		{name: "exact width fits", width: exactWidth, wantLines: 1},
		{name: "narrower width wraps", width: exactWidth - 1, wantLines: 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := LayoutWithViewport(doc, image.Rect(0, 0, tc.width, 100))
			if err != nil {
				t.Fatal(err)
			}
			runs := got.Root.Children[0].Children[0].Text
			lineYs := make(map[int]bool)
			for _, run := range runs {
				lineYs[run.Rect.Min.Y] = true
			}
			if len(lineYs) != tc.wantLines {
				t.Fatalf("line count at width %d = %d, want %d; runs: %+v",
					tc.width, len(lineYs), tc.wantLines, runs)
			}
		})
	}
}

func TestLayoutInlineImagesShareTextBaselineAndMargins(t *testing.T) {
	doc := styledForLayout(t, `<p style="margin:0">A<img style="width:20px;height:20px;margin:2px 3px 4px 5px;padding:1px;border-width:1px">z</p>`)
	got, err := LayoutWithViewport(doc, image.Rect(0, 0, 200, 100))
	if err != nil {
		t.Fatal(err)
	}
	line := got.Root.Children[0].Children[0]
	if len(line.Images) != 1 || len(line.Text) != 2 {
		t.Fatalf("inline content = images %#v text %#v", line.Images, line.Text)
	}
	faces := newFaceSet()
	defer faces.close()
	ascent, _ := faces.metrics(line.Text[0].Style).lineMetrics()
	baseline := line.Text[0].Rect.Min.Y + ascent
	// The image baseline is its margin-box bottom. Its content rectangle
	// therefore ends before its bottom padding, border, and margin.
	if bottom := line.Images[0].Rect.Max.Y + 1 + 1 + 4; bottom != baseline {
		t.Fatalf("image margin box bottom = %d, baseline = %d; image %v text %v",
			bottom, baseline, line.Images[0].Rect, line.Text[0].Rect)
	}
	if line.Images[0].Rect.Min.X != line.Text[0].Rect.Max.X+5+1+1 {
		t.Fatalf("image horizontal margins/edges not reserved: image %v text %v",
			line.Images[0].Rect, line.Text[0].Rect)
	}
}

func TestLayoutInlineImageVerticalAlignments(t *testing.T) {
	for _, tc := range []struct {
		name  string
		align string
		check func(t *testing.T, line *Box, image image.Rectangle, baseline int)
	}{
		{
			name:  "top",
			align: "top",
			check: func(t *testing.T, line *Box, picture image.Rectangle, _ int) {
				if picture.Min.Y != line.Rect.Min.Y {
					t.Fatalf("top image y = %d, line top = %d", picture.Min.Y, line.Rect.Min.Y)
				}
			},
		},
		{
			name:  "middle",
			align: "middle",
			check: func(t *testing.T, _ *Box, picture image.Rectangle, baseline int) {
				// A 16px font's approximate half x-height is 4px.
				if picture.Min.Y+picture.Dy()/2 != baseline-4 {
					t.Fatalf("middle image center = %d, want %d", picture.Min.Y+picture.Dy()/2, baseline-4)
				}
			},
		},
		{
			name:  "bottom",
			align: "bottom",
			check: func(t *testing.T, line *Box, picture image.Rectangle, _ int) {
				if picture.Max.Y != line.Rect.Max.Y {
					t.Fatalf("bottom image bottom = %d, line bottom = %d", picture.Max.Y, line.Rect.Max.Y)
				}
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			doc := styledForLayout(t, `<p style="margin:0">A<img style="width:20px;height:20px;vertical-align:`+tc.align+`">z</p>`)
			got, err := LayoutWithViewport(doc, image.Rect(0, 0, 200, 100))
			if err != nil {
				t.Fatal(err)
			}
			line := got.Root.Children[0].Children[0]
			if len(line.Images) != 1 || len(line.Text) == 0 {
				t.Fatalf("inline content = images %#v text %#v", line.Images, line.Text)
			}
			faces := newFaceSet()
			ascent, _ := faces.metrics(line.Text[0].Style).lineMetrics()
			faces.close()
			tc.check(t, line, line.Images[0].Rect, line.Text[0].Rect.Min.Y+ascent)
		})
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
