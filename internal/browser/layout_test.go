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
	if out.Rect.Min.Y != flow.Rect.Min.Y+20 || out.Rect.Dy() != 30 {
		t.Fatalf("positioned geometry = %v, want fixed top margin below flow geometry %v", out.Rect, flow.Rect)
	}
}

func TestPositionedAutoWidthIncludesChildOuterWidth(t *testing.T) {
	doc := styledForLayout(t, `<body style="margin:0">
		<div id="wrapper" style="position:relative">
			<div id="reference" style="position:absolute;left:0;top:0;width:60px;height:60px;border:5px solid red"></div>
			<div id="subject" style="position:absolute;left:0;top:0;border:5px solid green">
				<div id="content" style="width:30px;height:30px;margin:10px;border:5px solid green"></div>
			</div>
		</div>
	</body>`)
	got, err := LayoutWithViewport(doc, image.Rect(0, 0, 200, 120))
	if err != nil {
		t.Fatal(err)
	}
	boxes := boxesByID(got.Root, "reference", "subject", "content")
	if got, want := boxes["subject"].Rect, boxes["reference"].Rect; got != want {
		t.Fatalf("later positioned sibling geometry = %v content %v child %v, want reference geometry %v",
			got, boxes["subject"].Content, boxes["content"].Rect, want)
	}
	if got, want := boxes["content"].Rect, image.Rect(15, 15, 55, 55); got != want {
		t.Fatalf("positioned child geometry = %v, want %v", got, want)
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

func TestAbsolutePositionedAutoVerticalMarginsCenterInConstraintSpace(t *testing.T) {
	doc := styledForLayout(t, `<body style="margin:0"><div id="parent" style="position:relative;margin:0;width:240px;height:3in">
		<div id="centered" style="position:absolute;left:20px;top:.5in;bottom:.5in;width:140px;height:1in;margin-top:auto;margin-bottom:auto"></div>
	</div></body>`)
	got, err := LayoutWithViewport(doc, image.Rect(0, 0, 300, 400))
	if err != nil {
		t.Fatal(err)
	}
	boxes := boxesByID(got.Root, "parent", "centered")
	parent, centered := boxes["parent"], boxes["centered"]
	wantY := parent.Content.Min.Y + 96
	if centered.Rect.Min.Y != wantY || centered.Rect.Dy() != 96 {
		t.Fatalf("centered box = %v, want y=%d and 96px height inside parent %v", centered.Rect, wantY, parent.Content)
	}
	if centered.Rect.Max.Y != parent.Content.Max.Y-96 {
		t.Fatalf("remaining top/bottom constraint space is unbalanced: child %v, parent content %v", centered.Rect, parent.Content)
	}
}

func TestAbsolutePositionedSingleAutoVerticalMarginUsesRemainingSpace(t *testing.T) {
	doc := styledForLayout(t, `<div id="parent" style="position:relative;margin:0;width:200px;height:200px">
		<div id="target" style="position:absolute;top:20px;bottom:30px;width:20px;height:60px;margin-top:10px;margin-bottom:auto"></div>
	</div>`)
	got, err := LayoutWithViewport(doc, image.Rect(0, 0, 300, 300))
	if err != nil {
		t.Fatal(err)
	}
	boxes := boxesByID(got.Root, "parent", "target")
	parent, target := boxes["parent"], boxes["target"]
	// 200 - top 20 - bottom 30 - fixed top margin 10 - height 60 = 80.
	if target.Rect.Min.Y != parent.Content.Min.Y+30 || target.Rect.Max.Y != parent.Content.Max.Y-110 {
		t.Fatalf("single auto margin constraint = child %v, parent %v; want top margin 10px and remaining bottom margin 80px", target.Rect, parent.Content)
	}
}

func TestAbsolutelyPositionedFixedVerticalMargins(t *testing.T) {
	doc := styledForLayout(t, `<body style="margin:0"><div id="parent" style="position:relative;margin:0;width:300px;height:300px;border:10px solid black">
		<div id="static" style="position:absolute;margin-top:50px;width:20px;height:20px"></div>
		<div id="top" style="position:absolute;left:30px;top:50px;margin-top:50px;width:20px;height:20px"></div>
		<div id="over" style="position:absolute;left:60px;top:50px;bottom:50px;margin-top:50px;margin-bottom:50px;width:20px;height:150px"></div>
		<div id="bottom" style="position:absolute;left:90px;bottom:25px;margin-bottom:15px;width:20px;height:20px"></div>
	</div></body>`)
	got, err := LayoutWithViewport(doc, image.Rect(0, 0, 400, 400))
	if err != nil {
		t.Fatal(err)
	}
	boxes := boxesByID(got.Root, "parent", "static", "top", "over", "bottom")
	parent := boxes["parent"]
	for id, want := range map[string]int{
		"static": parent.Content.Min.Y + 50,
		"top":    parent.Content.Min.Y + 100,
		"over":   parent.Content.Min.Y + 100,
	} {
		if got := boxes[id].Rect.Min.Y; got != want {
			t.Errorf("%s top = %d, want %d", id, got, want)
		}
	}
	if got, want := boxes["bottom"].Rect.Max.Y, parent.Content.Max.Y-25-15; got != want {
		t.Errorf("bottom-positioned border edge = %d, want %d after fixed bottom margin", got, want)
	}
}

func TestAbsolutelyPositionedFixedHorizontalMargins(t *testing.T) {
	doc := styledForLayout(t, `<body style="margin:0"><div id="parent" style="position:relative;margin:0;width:300px;height:100px">
		<div id="left" style="position:absolute;left:25px;margin-left:15px;width:20px;height:20px"></div>
		<div id="right" style="position:absolute;right:25px;margin-right:15px;width:20px;height:20px"></div>
		<div id="over" style="position:absolute;left:25px;right:25px;margin-left:15px;margin-right:15px;width:20px;height:20px"></div>
	</div></body>`)
	got, err := LayoutWithViewport(doc, image.Rect(0, 0, 400, 200))
	if err != nil {
		t.Fatal(err)
	}
	boxes := boxesByID(got.Root, "parent", "left", "right", "over")
	parent := boxes["parent"]
	if got, want := boxes["left"].Rect.Min.X, parent.Content.Min.X+40; got != want {
		t.Errorf("left-positioned border edge = %d, want %d", got, want)
	}
	if got, want := boxes["right"].Rect.Max.X, parent.Content.Max.X-40; got != want {
		t.Errorf("right-positioned border edge = %d, want %d", got, want)
	}
	if got, want := boxes["over"].Rect.Min.X, parent.Content.Min.X+40; got != want {
		t.Errorf("over-constrained left edge = %d, want %d (left wins)", got, want)
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

func TestPositionedTextStaysInFlowInsidePositionedContainer(t *testing.T) {
	tests := []struct {
		name, markup, id string
		wantX, wantY     int
	}{
		{"absolute", `<div id="target" style="position:absolute;left:7px;top:9px;color:red">hello</div>`, "target", 7, 9},
		{"fixed", `<div id="target" style="position:fixed;left:7px;top:9px;color:red">hello</div>`, "target", 7, 9},
		{"nested absolute", `<div style="position:relative;margin:0;width:100px;height:20px"><div id="target" style="position:absolute;left:7px;top:9px;color:red">hello</div></div>`, "target", 7, 9},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			doc := styledForLayout(t, tt.markup)
			got, err := LayoutWithViewport(doc, image.Rect(0, 0, 200, 120))
			if err != nil {
				t.Fatal(err)
			}
			// The anonymous inline line box carries its parent's DOM node;
			// select the enclosing positioned box, not that line box.
			var box *Box
			var find func(*Box)
			find = func(b *Box) {
				if box == nil && b.Node != nil {
					if id, ok := b.Node.Attribute("id"); ok && id.Value == tt.id {
						box = b
					}
				}
				for _, child := range b.Children {
					find(child)
				}
			}
			find(got.Root)
			if box == nil {
				t.Fatal("missing positioned box")
			}
			if box.Rect.Min != image.Pt(tt.wantX, tt.wantY) || box.Rect.Dy() <= 0 {
				t.Fatalf("positioned box = %v, want origin (%d,%d) and nonzero height", box.Rect, tt.wantX, tt.wantY)
			}
			if len(box.Children) != 1 || len(box.Children[0].Text) != 1 {
				t.Fatalf("positioned text missing: %#v", box.Children)
			}
			run := box.Children[0].Text[0]
			if run.Text != "hello" || run.Rect.Min.Y != box.Content.Min.Y ||
				run.Rect.Max.Y > box.Content.Max.Y {
				t.Fatalf("text run = %#v, container content = %v", run, box.Content)
			}
			if run.Style["position"] != "" || run.Style["color"] != "red" {
				t.Fatalf("text leaf style = %v; want inherited color but no position", run.Style)
			}
		})
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

func TestFontFamilyMapping(t *testing.T) {
	for _, tc := range []struct {
		value, want string
	}{
		{"Verdana, Geneva, sans-serif", "verdana"},
		{`"Missing Face", Geneva, sans-serif`, "verdana"},
		{`'DejaVu Sans'`, "verdana"},
		{`"Missing Face", Courier, monospace`, "mono"},
		{"Arial, Verdana", "sans"}, // first supported family wins.
		{"sans-serif", "sans"},
		{"Arial", "sans"},
		{"Helvetica", "sans"},
		{"Times New Roman, serif", "sans"}, // Go fonts have no serif face.
		{"unknown", "sans"},
	} {
		if got := mappedFontFamily(tc.value); got != tc.want {
			t.Errorf("mappedFontFamily(%q) = %q, want %q", tc.value, got, tc.want)
		}
	}

	faces := newFaceSet()
	defer faces.close()
	sans := faces.metrics(ComputedStyle{"font-size": "16px", "font-family": "Arial"})
	mono := faces.metrics(ComputedStyle{"font-size": "16px", "font-family": "Courier"})
	if sans.width("iiii") == mono.width("iiii") {
		t.Fatal("Courier should use Go Mono rather than the sans face")
	}
}

func TestLayoutLineHeightGeometry(t *testing.T) {
	for _, tc := range []struct {
		name, value string
		want        int
	}{
		{"normal", "normal", 24},
		{"unitless", "1.5", 30},
		{"length", "32px", 32},
		{"percentage", "150%", 30},
	} {
		t.Run(tc.name, func(t *testing.T) {
			doc := styledForLayout(t, `<p style="margin:0;font-size:20px;line-height:`+tc.value+`">first<br>second</p>`)
			got, err := LayoutWithViewport(doc, image.Rect(0, 0, 300, 200))
			if err != nil {
				t.Fatal(err)
			}
			line := got.Root.Children[0].Children[0]
			if line.Rect.Dy() != 2*tc.want {
				t.Fatalf("two-line box height = %d, want %d; box=%v", line.Rect.Dy(), 2*tc.want, line.Rect)
			}
			if len(line.Text) != 2 || line.Text[1].Rect.Min.Y-line.Text[0].Rect.Min.Y != tc.want {
				t.Fatalf("text baselines did not advance by %dpx: %+v", tc.want, line.Text)
			}
		})
	}
}

func TestLayoutFontShorthandGeometry(t *testing.T) {
	doc := styledForLayout(t, `<p style='margin:0;font:20px/2 "Go Mono", monospace'>first<br>second</p>`)
	got, err := LayoutWithViewport(doc, image.Rect(0, 0, 300, 200))
	if err != nil {
		t.Fatal(err)
	}
	line := got.Root.Children[0].Children[0]
	if line.Rect.Dy() != 80 || len(line.Text) != 2 ||
		line.Text[1].Rect.Min.Y-line.Text[0].Rect.Min.Y != 40 {
		t.Fatalf("font shorthand line geometry = rect %v, runs %+v", line.Rect, line.Text)
	}
	if got := line.Text[0].Style["font-family"]; got != `"Go Mono", monospace` {
		t.Fatalf("font shorthand family = %q", got)
	}
}

func TestLayoutEmMarginUsesElementsOwnFontSize(t *testing.T) {
	doc := styledForLayout(t, `<div style="font-size:10px;margin-left:2em;padding:0;width:30px;height:1px"></div>`)
	got, err := LayoutWithViewport(doc, image.Rect(0, 0, 100, 20))
	if err != nil {
		t.Fatal(err)
	}
	box := got.Root.Children[0]
	if box.Rect.Min.X != 20 || box.Content.Dx() != 30 {
		t.Fatalf("em margin/width geometry = rect %v content %v, want x=20 width=30", box.Rect, box.Content)
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
	if got, want := runs[2].PenX, runs[1].PenX+faces.metrics(runs[1].Style).advance(runs[1].Text); got != want {
		t.Fatalf("adjacent run pen = %v, want fractional advance %v: %+v", got, want, runs)
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

func TestLayoutWrappedInlineBackgroundFragments(t *testing.T) {
	doc := styledForLayout(t, `<style>.highlight { background:#00ff00 }</style>
		<p style="margin:0;width:55px"><span class="highlight">alpha beta gamma</span></p>`)
	got, err := LayoutWithViewport(doc, image.Rect(0, 0, 100, 100))
	if err != nil {
		t.Fatal(err)
	}
	lineBox := got.Root.Children[0].Children[0]
	if len(lineBox.InlineBackgrounds) != 3 {
		t.Fatalf("background fragments = %#v, want one for each of three wrapped lines",
			lineBox.InlineBackgrounds)
	}
	node := lineBox.InlineBackgrounds[0].Node
	previousY := -1
	for i, fragment := range lineBox.InlineBackgrounds {
		if fragment.Node != node {
			t.Fatalf("fragment %d belongs to %p, want %p", i, fragment.Node, node)
		}
		if fragment.Rect.Empty() || fragment.Rect.Dx() >= lineBox.Rect.Dx() {
			t.Fatalf("fragment %d wrongly fills line box: %v in %v", i, fragment.Rect, lineBox.Rect)
		}
		if fragment.Rect.Min.Y <= previousY {
			t.Fatalf("fragment %d did not advance to a new line: %#v", i, lineBox.InlineBackgrounds)
		}
		previousY = fragment.Rect.Min.Y
	}
}

func TestLayoutInlineImagesShareTextBaselineAndMargins(t *testing.T) {
	doc := styledForLayout(t, `<p style="margin:0">A<img style="width:20px;height:20px;margin:2px 3px 4px 5px;padding:1px;border:1px solid">z</p>`)
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
	doc := styledForLayout(t, `<div style="padding: 20px; border: 5px solid"><p style="margin:0">long word</p></div>`)
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
