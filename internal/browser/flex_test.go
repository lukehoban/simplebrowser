package browser

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"testing"
)

func TestFlexImageUsesReplacedLayoutAndPaint(t *testing.T) {
	doc := styledForLayout(t, `<body style="margin:0"><div style="display:flex;align-items:flex-start;width:60px"><img id="logo"><div id="next" style="width:10px;height:10px"></div></div></body>`)
	var imageNode *Node
	var find func(*StyledNode)
	find = func(n *StyledNode) {
		if n.Node != nil && n.Node.Name == "img" {
			imageNode = n.Node
		}
		for _, c := range n.Children {
			find(c)
		}
	}
	find(doc.StyleRoot)
	picture := image.NewRGBA(image.Rect(0, 0, 8, 4))
	for y := 0; y < 4; y++ {
		for x := 0; x < 8; x++ {
			picture.SetRGBA(x, y, color.RGBA{255, 0, 0, 255})
		}
	}
	doc.Images = map[*Node]image.Image{imageNode: picture}
	layout, err := LayoutWithViewport(doc, image.Rect(0, 0, 80, 40))
	if err != nil {
		t.Fatal(err)
	}
	b := boxesByID(layout.Root, "logo", "next")
	if b["logo"].Rect != image.Rect(0, 0, 8, 4) || b["next"].Rect.Min.X != 8 || len(b["logo"].Images) != 1 {
		t.Fatalf("image flex item: logo=%+v next=%+v", b["logo"], b["next"])
	}
	var buf bytes.Buffer
	if err := paint(layout, &buf, renderOptions{}); err != nil {
		t.Fatal(err)
	}
	rendered, err := png.Decode(&buf)
	if err != nil {
		t.Fatal(err)
	}
	pixel(t, rendered.(*image.RGBA), 2, 2, color.RGBA{255, 0, 0, 255})
}

func TestFlexAnonymousTextItemsRowAndColumn(t *testing.T) {
	const source = `<body style="margin:0;font:20px monospace">
<div id="row" style="display:flex;width:200px;align-items:flex-start">Hello <span id="world">world</span> again</div>
<div id="column" style="display:flex;flex-direction:column;width:100px;align-items:flex-start">Up <span id="middle">middle</span> Down</div>
</body>`
	layout, err := LayoutWithViewport(styledForLayout(t, source), image.Rect(0, 0, 220, 150))
	if err != nil {
		t.Fatal(err)
	}
	boxes := boxesByID(layout.Root, "row", "world", "column", "middle")
	row, column := boxes["row"], boxes["column"]
	if len(row.Children) != 3 || len(column.Children) != 3 {
		t.Fatalf("text-element-text should create three items: row=%d column=%d", len(row.Children), len(column.Children))
	}
	for _, container := range []*Box{row, column} {
		for _, index := range []int{0, 2} {
			item := container.Children[index]
			if !item.Anonymous || len(item.Children) != 1 || len(item.Children[0].Text) == 0 {
				t.Fatalf("missing anonymous text item at %d: %+v", index, item)
			}
		}
	}
	if row.Children[0].Rect.Max.X != boxes["world"].Rect.Min.X ||
		boxes["world"].Rect.Max.X != row.Children[2].Rect.Min.X {
		t.Fatalf("row adjacency: %v %v %v", row.Children[0].Rect, boxes["world"].Rect, row.Children[2].Rect)
	}
	if column.Children[0].Rect.Max.Y != boxes["middle"].Rect.Min.Y ||
		boxes["middle"].Rect.Max.Y != column.Children[2].Rect.Min.Y {
		t.Fatalf("column adjacency: %v %v %v", column.Children[0].Rect, boxes["middle"].Rect, column.Children[2].Rect)
	}
	img := painted(t, source, image.Rect(0, 0, 220, 150))
	for _, item := range []*Box{row.Children[0], row.Children[2], column.Children[0], column.Children[2]} {
		found := false
		for y := item.Rect.Min.Y; y < item.Rect.Max.Y && !found; y++ {
			for x := item.Rect.Min.X; x < item.Rect.Max.X; x++ {
				r, g, b, _ := img.At(x, y).RGBA()
				if r < 0x8000 && g < 0x8000 && b < 0x8000 {
					found = true
					break
				}
			}
		}
		if !found {
			t.Errorf("no text pixels in anonymous item %v", item.Rect)
		}
	}
}

func TestFlexWhitespaceAndBlockFlowText(t *testing.T) {
	const source = `<body style="margin:0"><div id="row" style="display:flex;width:200px">
  <span id="first" style="width:20px;height:10px;background:red"></span>
  <span id="last" style="width:20px;height:10px;background:blue"></span>
  </div><div id="flow">plain text</div></body>`
	layout, err := LayoutWithViewport(styledForLayout(t, source), image.Rect(0, 0, 220, 100))
	if err != nil {
		t.Fatal(err)
	}
	boxes := boxesByID(layout.Root, "row", "first", "last", "flow")
	if len(boxes["row"].Children) != 2 || boxes["last"].Rect.Min.X != boxes["first"].Rect.Max.X {
		t.Fatalf("whitespace generated an item: %+v", boxes["row"].Children)
	}
	if len(boxes["flow"].Text) == 0 && (len(boxes["flow"].Children) == 0 || len(boxes["flow"].Children[0].Text) == 0) {
		t.Fatalf("block flow text changed: %+v", boxes["flow"].Children)
	}
}

// No-break spaces are not collapsible white space: a direct-text run made
// only of U+00A0 (or U+00A0 surrounded by collapsible white space) forms an
// anonymous flex item, while runs of only collapsible white space do not.
func TestFlexNonCollapsibleWhitespaceTextItems(t *testing.T) {
	const source = "<body style=\"margin:0;font:20px monospace\">" +
		"<div id=\"row\" style=\"display:flex;width:200px;align-items:flex-start;background:yellow\">" +
		"<span id=\"ra\" style=\"width:20px;height:10px;background:red\"></span>&nbsp;" +
		"<span id=\"rb\" style=\"width:20px;height:10px;background:blue\"></span> \t\n " +
		"<span id=\"rc\" style=\"width:20px;height:10px;background:lime\"></span></div>" +
		"<div id=\"column\" style=\"display:flex;flex-direction:column;width:100px;align-items:flex-start;background:yellow\">" +
		"<span id=\"ca\" style=\"width:20px;height:10px;background:red\"></span>\n &nbsp;\t" +
		"<span id=\"cb\" style=\"width:20px;height:10px;background:blue\"></span>\n\n  " +
		"<span id=\"cc\" style=\"width:20px;height:10px;background:lime\"></span></div></body>"
	viewport := image.Rect(0, 0, 220, 150)
	layout, err := LayoutWithViewport(styledForLayout(t, source), viewport)
	if err != nil {
		t.Fatal(err)
	}
	boxes := boxesByID(layout.Root, "row", "ra", "rb", "rc", "column", "ca", "cb", "cc")
	row, column := boxes["row"], boxes["column"]
	// Source order: element, NBSP item, element, element (no whitespace item).
	for name, container := range map[string]*Box{"row": row, "column": column} {
		if len(container.Children) != 4 {
			t.Fatalf("%s: want 4 items (a, nbsp, b, c), got %d: %+v", name, len(container.Children), container.Children)
		}
		if item := container.Children[1]; !item.Anonymous || len(item.Children) != 1 || len(item.Children[0].Text) == 0 {
			t.Fatalf("%s: NBSP run did not form an anonymous text item: %+v", name, item)
		}
	}
	if row.Children[0] != boxes["ra"] || row.Children[2] != boxes["rb"] || row.Children[3] != boxes["rc"] ||
		column.Children[0] != boxes["ca"] || column.Children[2] != boxes["cb"] || column.Children[3] != boxes["cc"] {
		t.Fatalf("items out of source order: row=%+v column=%+v", row.Children, column.Children)
	}
	rowNBSP, colNBSP := row.Children[1].Rect, column.Children[1].Rect
	if rowNBSP.Dx() <= 0 || boxes["ra"].Rect.Max.X != rowNBSP.Min.X || rowNBSP.Max.X != boxes["rb"].Rect.Min.X ||
		boxes["rb"].Rect.Max.X != boxes["rc"].Rect.Min.X {
		t.Fatalf("row geometry: a=%v nbsp=%v b=%v c=%v", boxes["ra"].Rect, rowNBSP, boxes["rb"].Rect, boxes["rc"].Rect)
	}
	if colNBSP.Dy() <= 0 || boxes["ca"].Rect.Max.Y != colNBSP.Min.Y || colNBSP.Max.Y != boxes["cb"].Rect.Min.Y ||
		boxes["cb"].Rect.Max.Y != boxes["cc"].Rect.Min.Y {
		t.Fatalf("column geometry: a=%v nbsp=%v b=%v c=%v", boxes["ca"].Rect, colNBSP, boxes["cb"].Rect, boxes["cc"].Rect)
	}
	img := painted(t, source, viewport)
	yellow := color.RGBA{255, 255, 0, 255}
	red, blue, lime := color.RGBA{255, 0, 0, 255}, color.RGBA{0, 0, 255, 255}, color.RGBA{0, 255, 0, 255}
	ry := boxes["ra"].Rect.Min.Y + 5
	pixel(t, img, boxes["ra"].Rect.Min.X+1, ry, red)
	pixel(t, img, (rowNBSP.Min.X+rowNBSP.Max.X)/2, ry, yellow) // NBSP advance: blank glyph on container background
	pixel(t, img, rowNBSP.Max.X, ry, blue)
	pixel(t, img, boxes["rc"].Rect.Min.X, ry, lime) // collapsible-only run adds no gap
	cx := boxes["ca"].Rect.Min.X + 5
	pixel(t, img, cx, boxes["ca"].Rect.Min.Y+1, red)
	pixel(t, img, cx, (colNBSP.Min.Y+colNBSP.Max.Y)/2, yellow)
	pixel(t, img, cx, colNBSP.Max.Y, blue)
	pixel(t, img, cx, boxes["cc"].Rect.Min.Y, lime)
}

func TestFlexContiguousTextAcrossCommentIsOneItem(t *testing.T) {
	const source = `<body style="margin:0"><div id="row" style="display:flex;width:200px;color:red">Hello<!-- split --> world<span id="end" style="width:20px;height:12px"></span></div></body>`
	layout, err := LayoutWithViewport(styledForLayout(t, source), image.Rect(0, 0, 220, 50))
	if err != nil {
		t.Fatal(err)
	}
	row := boxesByID(layout.Root, "row")["row"]
	if len(row.Children) != 2 || !row.Children[0].Anonymous || row.Children[0].Rect.Max.X != row.Children[1].Rect.Min.X {
		t.Fatalf("contiguous text should be one item before span: %+v", row.Children)
	}
	if len(row.Children[0].Children) != 1 || len(row.Children[0].Children[0].Text) != 2 {
		t.Fatalf("split text not retained in one anonymous line: %+v", row.Children[0].Children)
	}
}

func TestFlexBoundsBasisAndReversePixels(t *testing.T) {
	const source = `<body style="margin:0">
	<div style="display:flex;width:100px"><div id="min" style="width:80px;min-width:80px;height:10px;background:red"></div><div id="shrunk" style="width:80px;height:10px;background:blue"></div></div>
	<div style="display:flex;width:100px"><div id="max" style="flex:1;max-width:30px;height:10px;background:red"></div><div id="grown" style="flex:1;height:10px;background:blue"></div></div>
	<div style="display:flex;flex-direction:column;width:40px;height:80px"><div id="basis" style="height:20px;flex:0 0 40px;background:red"></div><div id="auto-basis" style="height:20px;flex:0 0 auto;background:blue"></div></div>
	<div style="display:flex;flex-direction:row-reverse;width:100px;justify-content:flex-start"><div id="reverse-a" style="width:20px;height:10px;background:red"></div><div id="reverse-b" style="width:10px;height:10px;background:blue"></div></div>
	<div style="display:flex;flex-direction:column-reverse;width:30px;height:60px"><div id="reverse-column" style="width:10px;height:20px;background:red"></div></div>
	<div style="display:flex;flex-direction:column;width:20px;height:100px"><div id="min-height" style="height:80px;min-height:80px;flex-shrink:1;background:red"></div><div id="remaining-height" style="height:80px;flex-shrink:1;background:blue"></div></div>
	<div style="display:flex;flex-direction:row-reverse;width:100px;justify-content:flex-end"><div id="reverse-end" style="width:20px;height:10px;background:red"></div></div>
	</body>`
	layout, err := LayoutWithViewport(styledForLayout(t, source), image.Rect(0, 0, 130, 320))
	if err != nil {
		t.Fatal(err)
	}
	b := boxesByID(layout.Root, "min", "shrunk", "max", "grown", "basis", "auto-basis", "reverse-a", "reverse-b", "reverse-column", "min-height", "remaining-height", "reverse-end")
	want := map[string]image.Rectangle{
		"min": image.Rect(0, 0, 80, 10), "shrunk": image.Rect(80, 0, 100, 10),
		"max": image.Rect(0, 10, 30, 20), "grown": image.Rect(30, 10, 100, 20),
		"basis": image.Rect(0, 20, 40, 60), "auto-basis": image.Rect(0, 60, 40, 80),
		"reverse-a": image.Rect(80, 100, 100, 110),
		"reverse-b": image.Rect(70, 100, 80, 110), "reverse-column": image.Rect(0, 150, 10, 170),
		"min-height": image.Rect(0, 170, 20, 250), "remaining-height": image.Rect(0, 250, 20, 270),
		"reverse-end": image.Rect(0, 270, 20, 280),
	}
	for id, rect := range want {
		if b[id] == nil || b[id].Rect != rect {
			t.Errorf("%s: got %v want %v", id, b[id], rect)
		}
	}
	img := painted(t, source, image.Rect(0, 0, 130, 320))
	pixel(t, img, 85, 5, color.RGBA{0, 0, 255, 255})
	pixel(t, img, 85, 105, color.RGBA{255, 0, 0, 255})
	pixel(t, img, 5, 155, color.RGBA{255, 0, 0, 255})
	pixel(t, img, 5, 255, color.RGBA{0, 0, 255, 255})
}

func TestFlexSupportsClaimsOnlyImplementedAlignment(t *testing.T) {
	if supportsConditionMatches("(align-items: baseline)") {
		t.Fatal("baseline alignment is not implemented")
	}
	if !supportsConditionMatches("(align-items: center)") {
		t.Fatal("center alignment is implemented")
	}
	for _, value := range []string{"auto", "stretch", "start", "end", "flex-start", "flex-end", "center", "self-start", "self-end"} {
		if !supportsConditionMatches("(align-self: " + value + ")") {
			t.Errorf("implemented align-self value not reported: %s", value)
		}
	}
	for _, value := range []string{"baseline", "safe center", "first baseline"} {
		if supportsConditionMatches("(align-self: " + value + ")") {
			t.Errorf("unsupported align-self value reported: %s", value)
		}
	}
	for _, query := range []string{"(flex-wrap: wrap)", "(flex-wrap: wrap-reverse)", "(align-content: center)", "(align-content: space-between)"} {
		if !supportsConditionMatches(query) {
			t.Errorf("implemented value not reported: %s", query)
		}
	}
	for _, query := range []string{"(flex-wrap: balance)", "(align-content: baseline)", "(align-content: safe center)"} {
		if supportsConditionMatches(query) {
			t.Errorf("unsupported value reported: %s", query)
		}
	}
}

func TestFlexAlignSelfGeometryPixelsAndWrappedLines(t *testing.T) {
	const source = `<body style="margin:0">
<div id="single" style="display:flex;align-items:flex-end;width:120px;height:40px;background:white">
  <div id="flex-start" style="align-self:flex-start;width:12px;height:10px;background:red"></div>
  <div id="flex-end" style="align-self:flex-end;width:12px;height:10px;background:blue"></div>
  <div id="center" style="align-self:center;width:12px;height:10px;background:green"></div>
  <div id="stretch" style="align-self:stretch;width:12px;background:yellow"></div>
  <div id="start" style="align-self:start;width:12px;height:10px;background:magenta"></div>
  <div id="end" style="align-self:end;width:12px;height:10px;background:cyan"></div>
  <div id="self-start" style="align-self:self-start;width:12px;height:10px;background:orange"></div>
  <div id="self-end" style="align-self:self-end;width:12px;height:10px;background:purple"></div>
  <div id="auto" style="align-self:auto;width:12px;height:10px;background:black"></div>
</div>
<div id="column" style="display:flex;flex-direction:column;align-items:flex-start;width:40px;height:40px;background:white">
  <div id="column-end" style="align-self:flex-end;width:10px;height:10px;background:blue"></div>
</div>
<div id="wrapped" style="display:flex;flex-wrap:wrap;align-content:flex-start;align-items:flex-start;width:80px;height:80px;background:white">
  <div id="tall-a" style="width:40px;height:30px;background:red"></div>
  <div id="wrapped-end" style="align-self:flex-end;width:40px;height:10px;background:blue"></div>
  <div id="tall-b" style="width:40px;height:20px;background:green"></div>
  <div id="wrapped-center" style="align-self:center;width:40px;height:10px;background:purple"></div>
</div>
<div id="reverse" style="display:flex;flex-wrap:wrap-reverse;align-content:flex-start;align-items:flex-start;width:80px;height:80px;background:white">
  <div id="reverse-tall-a" style="width:40px;height:30px;background:red"></div>
  <div id="reverse-end" style="align-self:flex-end;width:40px;height:10px;background:blue"></div>
  <div id="reverse-tall-b" style="width:40px;height:20px;background:green"></div>
  <div id="reverse-center" style="align-self:center;width:40px;height:10px;background:purple"></div>
</div></body>`
	viewport := image.Rect(0, 0, 160, 280)
	layout, err := LayoutWithViewport(styledForLayout(t, source), viewport)
	if err != nil {
		t.Fatal(err)
	}
	b := boxesByID(layout.Root, "single", "flex-start", "flex-end", "center", "stretch", "start", "end", "self-start", "self-end", "auto", "column-end",
		"wrapped", "wrapped-end", "wrapped-center", "reverse", "reverse-end", "reverse-center")
	wantY := map[string]int{
		"flex-start": 0, "flex-end": 30, "center": 15, "stretch": 0,
		"start": 0, "end": 30, "self-start": 0, "self-end": 30, "auto": 30,
	}
	for id, y := range wantY {
		box := b[id]
		if box == nil || box.Rect.Min.Y != y || (id == "stretch" && box.Rect.Dy() != 40) {
			t.Errorf("%s: got %v, want y=%d (stretch height 40)", id, box, y)
		}
	}
	if b["column-end"].Rect.Min.X != 30 || b["column-end"].Rect.Dy() != 10 {
		t.Errorf("column cross-axis end alignment: %v, want x=30 with 10px height", b["column-end"].Rect)
	}
	if b["wrapped-end"].Rect.Min.Y != b["wrapped"].Rect.Min.Y+20 ||
		b["wrapped-center"].Rect.Min.Y != b["wrapped"].Rect.Min.Y+35 {
		t.Errorf("wrapped line alignment: end=%v center=%v parent=%v", b["wrapped-end"].Rect, b["wrapped-center"].Rect, b["wrapped"].Rect)
	}
	if b["reverse-end"].Rect.Min.Y != b["reverse"].Rect.Min.Y+50 ||
		b["reverse-center"].Rect.Min.Y != b["reverse"].Rect.Min.Y+35 {
		t.Errorf("wrap-reverse alignment: end=%v center=%v parent=%v", b["reverse-end"].Rect, b["reverse-center"].Rect, b["reverse"].Rect)
	}

	img := painted(t, source, viewport)
	pixel(t, img, 1, 1, color.RGBA{255, 0, 0, 255})
	pixel(t, img, 13, 31, color.RGBA{0, 0, 255, 255})
	pixel(t, img, 25, 16, color.RGBA{0, 128, 0, 255})
	pixel(t, img, 37, 1, color.RGBA{255, 255, 0, 255})
	pixel(t, img, 49, 1, color.RGBA{255, 0, 255, 255})
	pixel(t, img, 61, 31, color.RGBA{0, 255, 255, 255})
	pixel(t, img, b["column-end"].Rect.Min.X+1, b["column-end"].Rect.Min.Y+1, color.RGBA{0, 0, 255, 255})
	pixel(t, img, b["wrapped-end"].Rect.Min.X+1, b["wrapped-end"].Rect.Min.Y+1, color.RGBA{0, 0, 255, 255})
	pixel(t, img, b["reverse-end"].Rect.Min.X+1, b["reverse-end"].Rect.Min.Y+1, color.RGBA{0, 0, 255, 255})
}

func TestFlexColumnWrapAutoCrossWidthAndAnonymousIntrinsic(t *testing.T) {
	for _, tc := range []struct {
		name, source string
		wantX        int
	}{
		{"stretch", `<div id="c" style="display:flex;flex-direction:column;flex-wrap:wrap;height:70px;width:180px"><div id="a" style="height:40px;background:red"></div><div id="b" style="height:40px;background:blue"></div></div>`, 90},
		{"start text", `<div id="c" style="display:flex;flex-direction:column;flex-wrap:wrap;align-items:flex-start;height:70px;width:180px;font:20px monospace"><div id="a" style="height:40px;background:red">Hello</div><div id="b" style="height:40px;background:blue">World</div></div>`, 90},
	} {
		t.Run(tc.name, func(t *testing.T) {
			source := `<body style="margin:0">` + tc.source + `</body>`
			layout, err := LayoutWithViewport(styledForLayout(t, source), image.Rect(0, 0, 220, 120))
			if err != nil {
				t.Fatal(err)
			}
			boxes := boxesByID(layout.Root, "a", "b")
			if boxes["b"].Rect.Min.X != tc.wantX || boxes["b"].Rect.Min.Y != 0 {
				t.Fatalf("column lines: first=%v second=%v, want second x=%d", boxes["a"].Rect, boxes["b"].Rect, tc.wantX)
			}
			img := painted(t, source, image.Rect(0, 0, 220, 120))
			pixel(t, img, tc.wantX+55, 5, color.RGBA{0, 0, 255, 255})
		})
	}
	const floatText = `<body style="margin:0;font:20px monospace"><div id="f" style="float:left;display:flex;flex-wrap:wrap">Hello</div></body>`
	layout, err := LayoutWithViewport(styledForLayout(t, floatText), image.Rect(0, 0, 220, 120))
	if err != nil {
		t.Fatal(err)
	}
	float := boxesByID(layout.Root, "f")["f"]
	if float.Rect.Dx() < 50 || len(float.Children) != 1 || !float.Children[0].Anonymous {
		t.Fatalf("text-only float flex width/item: %+v", float)
	}
}

func TestFlexRowGapAlignmentAndPixels(t *testing.T) {
	const source = `<body style="margin:0"><div id="row" style="display:flex;width:300px;height:60px;gap:12px;align-items:center">
		<div id="first" style="width:80px;height:20px;background:red"></div>
		<div id="second" style="width:60px;height:40px;background:blue"></div>
	</div></body>`
	layout, err := LayoutWithViewport(styledForLayout(t, source), image.Rect(0, 0, 320, 100))
	if err != nil {
		t.Fatal(err)
	}
	boxes := boxesByID(layout.Root, "row", "first", "second")
	if boxes["first"].Rect != image.Rect(0, 20, 80, 40) ||
		boxes["second"].Rect != image.Rect(92, 10, 152, 50) {
		t.Fatalf("flex row boxes: first=%v second=%v", boxes["first"].Rect, boxes["second"].Rect)
	}
	img := painted(t, source, image.Rect(0, 0, 320, 100))
	pixel(t, img, 10, 25, color.RGBA{255, 0, 0, 255})
	pixel(t, img, 85, 25, color.RGBA{255, 255, 255, 255})
	pixel(t, img, 100, 25, color.RGBA{0, 0, 255, 255})
}

func TestFlexMainAxisGrowShrinkAndJustification(t *testing.T) {
	const source = `<body style="margin:0">
	<div id="grow" style="display:flex;width:300px;gap:10px">
		<div id="fixed" style="width:40px;height:10px"></div>
		<div id="one" style="flex:1;height:10px"></div>
		<div id="two" style="flex:2;height:10px"></div>
	</div>
	<div id="shrink" style="display:flex;width:100px">
		<div id="wide-a" style="width:80px;height:10px;flex-shrink:1"></div>
		<div id="wide-b" style="width:80px;height:10px;flex-shrink:1"></div>
	</div>
	<div id="justify" style="display:flex;width:200px;justify-content:space-between">
		<div id="start" style="width:20px;height:10px"></div>
		<div id="end" style="width:20px;height:10px"></div>
	</div></body>`
	layout, err := LayoutWithViewport(styledForLayout(t, source), image.Rect(0, 0, 320, 100))
	if err != nil {
		t.Fatal(err)
	}
	b := boxesByID(layout.Root, "fixed", "one", "two", "wide-a", "wide-b", "start", "end")
	if b["fixed"].Rect.Dx() != 40 || b["one"].Rect.Dx() != 80 || b["two"].Rect.Dx() != 160 ||
		b["one"].Rect.Min.X != 50 || b["two"].Rect.Min.X != 140 {
		t.Fatalf("grow geometry: fixed=%v one=%v two=%v", b["fixed"].Rect, b["one"].Rect, b["two"].Rect)
	}
	if b["wide-a"].Rect.Dx() != 50 || b["wide-b"].Rect != image.Rect(50, 10, 100, 20) {
		t.Fatalf("shrink geometry: a=%v b=%v", b["wide-a"].Rect, b["wide-b"].Rect)
	}
	if b["start"].Rect.Min.X != 0 || b["end"].Rect.Min.X != 180 {
		t.Fatalf("space-between geometry: start=%v end=%v", b["start"].Rect, b["end"].Rect)
	}
}

func TestFlexSizingAccountsForPaddingAndPercentageBasis(t *testing.T) {
	const source = `<body style="margin:0">
	<div style="display:flex;width:200px">
		<div id="padded" style="flex:1;padding:0 10px;height:10px"></div>
		<div id="fixed-size" style="width:50px;height:10px"></div>
	</div>
	<div style="display:flex;width:200px">
		<div id="percentage" style="flex:0 0 50%;height:10px"></div>
		<div id="remainder" style="flex:1;height:10px"></div>
	</div></body>`
	layout, err := LayoutWithViewport(styledForLayout(t, source), image.Rect(0, 0, 240, 80))
	if err != nil {
		t.Fatal(err)
	}
	b := boxesByID(layout.Root, "padded", "fixed-size", "percentage", "remainder")
	if b["padded"].Rect.Dx() != 150 || b["fixed-size"].Rect.Min.X != 150 {
		t.Fatalf("padding must count in the flex main size: padded=%v fixed=%v", b["padded"].Rect, b["fixed-size"].Rect)
	}
	if b["percentage"].Rect.Dx() != 100 || b["remainder"].Rect != image.Rect(100, 10, 200, 20) {
		t.Fatalf("percentage basis geometry: percentage=%v remainder=%v", b["percentage"].Rect, b["remainder"].Rect)
	}
}

func TestFlexColumnAndInlineFlex(t *testing.T) {
	const source = `<body style="margin:0">
	<div id="column" style="display:flex;flex-direction:column;width:80px;height:110px;gap:10px">
		<div id="top" style="flex:1;background:red"></div>
		<div id="bottom" style="flex:1;background:blue"></div>
	</div>
	<p style="margin:0">A<span id="inline" style="display:inline-flex;width:50px;height:20px"><b style="width:20px"></b><i style="width:20px"></i></span>Z</p>
	</body>`
	layout, err := LayoutWithViewport(styledForLayout(t, source), image.Rect(0, 0, 200, 180))
	if err != nil {
		t.Fatal(err)
	}
	b := boxesByID(layout.Root, "column", "top", "bottom", "inline")
	if b["top"].Rect != image.Rect(0, 0, 80, 50) || b["bottom"].Rect != image.Rect(0, 60, 80, 110) {
		t.Fatalf("column geometry: top=%v bottom=%v", b["top"].Rect, b["bottom"].Rect)
	}
	if b["inline"] == nil || b["inline"].Rect.Dx() != 50 || b["inline"].Rect.Dy() != 20 {
		t.Fatalf("inline-flex should be one 50x20 atomic inline: %#v", b["inline"])
	}
}

// Main-axis percentage min/max sizes resolve against the container's definite
// main size: height for columns, width for rows (#247 review).
func TestFlexMainAxisPercentMinMaxResolveAgainstMainSize(t *testing.T) {
	const source = `<body style="margin:0">
	<div style="display:flex;flex-direction:column;width:20px;height:100px"><div id="col-a" style="height:80px;background:blue"></div><div id="col-b" style="height:80px;min-height:75%;background:red"></div></div>
	<div style="display:flex;flex-direction:column;width:20px;height:100px"><div id="colmax-a" style="flex:1;max-height:30%;background:blue"></div><div id="colmax-b" style="flex:1;background:red"></div></div>
	<div style="display:flex;width:100px;height:20px"><div id="row-a" style="width:80px;height:10px"></div><div id="row-b" style="width:80px;min-width:75%;height:10px"></div></div>
	<div style="display:flex;flex-direction:column;width:20px"><div id="auto-a" style="height:30px;min-height:75%"></div><div id="auto-b" style="height:10px"></div></div>
	</body>`
	layout, err := LayoutWithViewport(styledForLayout(t, source), image.Rect(0, 0, 120, 300))
	if err != nil {
		t.Fatal(err)
	}
	b := boxesByID(layout.Root, "col-a", "col-b", "colmax-a", "colmax-b", "row-a", "row-b", "auto-a", "auto-b")
	want := map[string]image.Rectangle{
		"col-a": image.Rect(0, 0, 20, 25), "col-b": image.Rect(0, 25, 20, 100),
		"colmax-a": image.Rect(0, 100, 20, 130), "colmax-b": image.Rect(0, 130, 20, 200),
		"row-a": image.Rect(0, 200, 25, 210), "row-b": image.Rect(25, 200, 100, 210),
		// An auto-height column has no definite main size; the percentage
		// minimum behaves as auto and items keep their specified heights.
		"auto-a": image.Rect(0, 220, 20, 250), "auto-b": image.Rect(0, 250, 20, 260),
	}
	for id, rect := range want {
		if b[id] == nil || b[id].Rect != rect {
			t.Errorf("%s: got %v want %v", id, b[id], rect)
		}
	}
	img := painted(t, source, image.Rect(0, 0, 120, 300))
	pixel(t, img, 10, 20, color.RGBA{0, 0, 255, 255})
	pixel(t, img, 10, 30, color.RGBA{255, 0, 0, 255})
}

func TestFlexWrapLinesGeometryAndPixels(t *testing.T) {
	red, blue, green := color.RGBA{255, 0, 0, 255}, color.RGBA{0, 0, 255, 255}, color.RGBA{0, 128, 0, 255}
	cases := []struct {
		name, source string
		want         map[string]image.Rectangle
		pixels       map[image.Point]color.RGBA
	}{{
		name:   "issue repro starts a second line",
		source: `<div id="c" style="display:flex;flex-wrap:wrap;width:100px;gap:10px"><div id="a" style="flex:none;width:60px;height:20px;background:red"></div><div id="b" style="flex:none;width:60px;height:20px;background:blue"></div></div><div id="after" style="height:5px"></div>`,
		want: map[string]image.Rectangle{"c": image.Rect(0, 0, 100, 50), "a": image.Rect(0, 0, 60, 20),
			"b": image.Rect(0, 30, 60, 50), "after": image.Rect(0, 50, 200, 55)},
		pixels: map[image.Point]color.RGBA{{5, 5}: red, {5, 35}: blue, {70, 5}: {255, 255, 255, 255}, {5, 25}: {255, 255, 255, 255}},
	}, {
		name:   "percentage calc flex basis participates in wrapping",
		source: `<div id="c" style="display:flex;flex-wrap:wrap;width:100px;column-gap:2px"><div id="a" style="flex-basis:calc(50% - 1px);flex-shrink:0;height:10px"></div><div id="b" style="flex-basis:calc(50% - 1px);flex-shrink:0;height:10px"></div><div id="d" style="flex-basis:calc(50% - 1px);flex-shrink:0;height:10px"></div></div>`,
		want: map[string]image.Rectangle{"c": image.Rect(0, 0, 100, 20), "a": image.Rect(0, 0, 49, 10),
			"b": image.Rect(51, 0, 100, 10), "d": image.Rect(0, 10, 49, 20)},
	}, {
		name:   "per-line flexing and wrap-reverse cross placement",
		source: `<div id="c" style="display:flex;flex-wrap:wrap-reverse;width:180px;gap:6px 10px"><div id="a" style="width:50px;height:20px;background:red"></div><div id="b" style="width:50px;height:30px;background:green"></div><div id="d" style="width:50px;height:20px"></div><div id="grow" style="flex:1 1 60px;height:20px;background:blue"></div></div>`,
		want: map[string]image.Rectangle{"c": image.Rect(0, 0, 180, 56), "grow": image.Rect(0, 0, 180, 20),
			"a": image.Rect(0, 36, 50, 56), "b": image.Rect(60, 26, 110, 56), "d": image.Rect(120, 36, 170, 56)},
		pixels: map[image.Point]color.RGBA{{175, 5}: blue, {5, 40}: red, {5, 30}: {255, 255, 255, 255}, {65, 30}: green},
	}, {
		name:   "column wrap with stretched align-content lines",
		source: `<div style="display:flex;flex-direction:column;flex-wrap:wrap;height:70px;width:180px;gap:10px"><div id="a" style="height:30px;width:40px;background:red"></div><div id="b" style="height:30px;width:40px"></div><div id="d" style="height:30px;width:40px;background:blue"></div></div>`,
		want:   map[string]image.Rectangle{"a": image.Rect(0, 0, 40, 30), "b": image.Rect(0, 40, 40, 70), "d": image.Rect(95, 0, 135, 30)},
		pixels: map[image.Point]color.RGBA{{100, 5}: blue, {90, 5}: {255, 255, 255, 255}},
	}, {
		name:   "align-content center on a definite row height",
		source: `<div style="display:flex;flex-wrap:wrap;align-content:center;width:100px;height:100px;row-gap:10px"><div id="a" style="width:60px;height:20px;background:red"></div><div id="b" style="width:60px;height:20px;background:blue"></div></div>`,
		want:   map[string]image.Rectangle{"a": image.Rect(0, 25, 60, 45), "b": image.Rect(0, 55, 60, 75)},
		pixels: map[image.Point]color.RGBA{{5, 25}: red, {5, 55}: blue, {5, 50}: {255, 255, 255, 255}},
	}, {
		name:   "align-content center with overflowing wrapped lines",
		source: `<div id="c" style="display:flex;flex-wrap:wrap;align-content:center;align-items:flex-start;width:50px;height:50px"><div id="a" style="flex:none;width:50px;height:40px;background:red"></div><div id="b" style="flex:none;width:50px;height:40px;background:blue"></div></div>`,
		want:   map[string]image.Rectangle{"c": image.Rect(0, 0, 50, 50), "a": image.Rect(0, -15, 50, 25), "b": image.Rect(0, 25, 50, 65)},
		pixels: map[image.Point]color.RGBA{{5, 0}: red, {5, 24}: red, {5, 25}: blue, {5, 49}: blue},
	}, {
		name:   "align-content end with overflowing wrapped lines and gap",
		source: `<div id="c" style="display:flex;flex-wrap:wrap;align-content:end;align-items:flex-start;width:50px;height:50px;row-gap:10px"><div id="a" style="flex:none;width:50px;height:40px;background:red"></div><div id="b" style="flex:none;width:50px;height:40px;background:blue"></div></div>`,
		want:   map[string]image.Rectangle{"c": image.Rect(0, 0, 50, 50), "a": image.Rect(0, -40, 50, 0), "b": image.Rect(0, 10, 50, 50)},
		pixels: map[image.Point]color.RGBA{{5, 5}: {255, 255, 255, 255}, {5, 10}: blue, {5, 49}: blue},
	}, {
		name:   "column wrap align-content center overflows cross axis",
		source: `<div id="c" style="display:flex;flex-direction:column;flex-wrap:wrap;align-content:center;align-items:flex-start;width:50px;height:50px"><div id="a" style="flex:none;width:40px;height:50px;background:red"></div><div id="b" style="flex:none;width:40px;height:50px;background:blue"></div></div>`,
		want:   map[string]image.Rectangle{"c": image.Rect(0, 0, 50, 50), "a": image.Rect(-15, 0, 25, 50), "b": image.Rect(25, 0, 65, 50)},
		pixels: map[image.Point]color.RGBA{{0, 5}: red, {24, 5}: red, {25, 5}: blue, {49, 5}: blue},
	}, {
		name:   "wrap-reverse row end keeps logical end with negative space",
		source: `<div id="c" style="display:flex;flex-wrap:wrap-reverse;align-content:end;align-items:flex-start;width:20px;height:30px"><div id="a" style="flex:none;width:20px;height:20px;background:red"></div><div id="b" style="flex:none;width:20px;height:20px;background:blue"></div></div>`,
		want:   map[string]image.Rectangle{"c": image.Rect(0, 0, 20, 30), "a": image.Rect(0, 10, 20, 30), "b": image.Rect(0, -10, 20, 10)},
		pixels: map[image.Point]color.RGBA{{5, 0}: blue, {5, 9}: blue, {5, 10}: red, {5, 29}: red},
	}, {
		name:   "wrap-reverse row flex-end follows reversed cross end with negative space",
		source: `<div id="c" style="display:flex;flex-wrap:wrap-reverse;align-content:flex-end;align-items:flex-start;width:20px;height:30px"><div id="a" style="flex:none;width:20px;height:20px;background:red"></div><div id="b" style="flex:none;width:20px;height:20px;background:blue"></div></div>`,
		want:   map[string]image.Rectangle{"c": image.Rect(0, 0, 20, 30), "a": image.Rect(0, 20, 20, 40), "b": image.Rect(0, 0, 20, 20)},
		pixels: map[image.Point]color.RGBA{{5, 0}: blue, {5, 19}: blue, {5, 20}: red, {5, 29}: red},
	}, {
		name:   "wrap-reverse row end keeps logical end with positive space",
		source: `<div id="c" style="display:flex;flex-wrap:wrap-reverse;align-content:end;align-items:flex-start;width:20px;height:60px"><div id="a" style="flex:none;width:20px;height:20px;background:red"></div><div id="b" style="flex:none;width:20px;height:20px;background:blue"></div></div>`,
		want:   map[string]image.Rectangle{"c": image.Rect(0, 0, 20, 60), "a": image.Rect(0, 40, 20, 60), "b": image.Rect(0, 20, 20, 40)},
		pixels: map[image.Point]color.RGBA{{5, 0}: {255, 255, 255, 255}, {5, 20}: blue, {5, 40}: red, {5, 59}: red},
	}, {
		name:   "wrap-reverse row flex-end follows reversed cross end with positive space",
		source: `<div id="c" style="display:flex;flex-wrap:wrap-reverse;align-content:flex-end;align-items:flex-start;width:20px;height:60px"><div id="a" style="flex:none;width:20px;height:20px;background:red"></div><div id="b" style="flex:none;width:20px;height:20px;background:blue"></div></div>`,
		want:   map[string]image.Rectangle{"c": image.Rect(0, 0, 20, 60), "a": image.Rect(0, 20, 20, 40), "b": image.Rect(0, 0, 20, 20)},
		pixels: map[image.Point]color.RGBA{{5, 0}: blue, {5, 20}: red, {5, 39}: red, {5, 40}: {255, 255, 255, 255}},
	}, {
		name:   "wrap-reverse column end keeps logical end with negative space",
		source: `<div id="c" style="display:flex;flex-direction:column;flex-wrap:wrap-reverse;align-content:end;align-items:flex-start;width:30px;height:20px"><div id="a" style="flex:none;width:20px;height:20px;background:red"></div><div id="b" style="flex:none;width:20px;height:20px;background:blue"></div></div>`,
		want:   map[string]image.Rectangle{"c": image.Rect(0, 0, 30, 20), "a": image.Rect(10, 0, 30, 20), "b": image.Rect(-10, 0, 10, 20)},
		pixels: map[image.Point]color.RGBA{{0, 5}: blue, {9, 5}: blue, {10, 5}: red, {29, 5}: red},
	}, {
		name:   "wrap-reverse column flex-end follows reversed cross end with negative space",
		source: `<div id="c" style="display:flex;flex-direction:column;flex-wrap:wrap-reverse;align-content:flex-end;align-items:flex-start;width:30px;height:20px"><div id="a" style="flex:none;width:20px;height:20px;background:red"></div><div id="b" style="flex:none;width:20px;height:20px;background:blue"></div></div>`,
		want:   map[string]image.Rectangle{"c": image.Rect(0, 0, 30, 20), "a": image.Rect(20, 0, 40, 20), "b": image.Rect(0, 0, 20, 20)},
		pixels: map[image.Point]color.RGBA{{0, 5}: blue, {19, 5}: blue, {20, 5}: red, {29, 5}: red},
	}, {
		name:   "wrap-reverse column end keeps logical end with positive space",
		source: `<div id="c" style="display:flex;flex-direction:column;flex-wrap:wrap-reverse;align-content:end;align-items:flex-start;width:60px;height:20px"><div id="a" style="flex:none;width:20px;height:20px;background:red"></div><div id="b" style="flex:none;width:20px;height:20px;background:blue"></div></div>`,
		want:   map[string]image.Rectangle{"c": image.Rect(0, 0, 60, 20), "a": image.Rect(40, 0, 60, 20), "b": image.Rect(20, 0, 40, 20)},
		pixels: map[image.Point]color.RGBA{{0, 5}: {255, 255, 255, 255}, {20, 5}: blue, {40, 5}: red, {59, 5}: red},
	}, {
		name:   "wrap-reverse column flex-end follows reversed cross end with positive space",
		source: `<div id="c" style="display:flex;flex-direction:column;flex-wrap:wrap-reverse;align-content:flex-end;align-items:flex-start;width:60px;height:20px"><div id="a" style="flex:none;width:20px;height:20px;background:red"></div><div id="b" style="flex:none;width:20px;height:20px;background:blue"></div></div>`,
		want:   map[string]image.Rectangle{"c": image.Rect(0, 0, 60, 20), "a": image.Rect(20, 0, 40, 20), "b": image.Rect(0, 0, 20, 20)},
		pixels: map[image.Point]color.RGBA{{0, 5}: blue, {20, 5}: red, {39, 5}: red, {40, 5}: {255, 255, 255, 255}},
	}, {
		name:   "wrapped line stretches auto-height items",
		source: `<div style="display:flex;flex-wrap:wrap;width:100px"><div id="a" style="width:60px;height:15px"></div><div id="e" style="width:30px"></div><div id="b" style="width:60px;height:20px"></div><div id="d" style="width:30px"></div></div>`,
		want:   map[string]image.Rectangle{"a": image.Rect(0, 0, 60, 15), "e": image.Rect(60, 0, 90, 15), "b": image.Rect(0, 15, 60, 35), "d": image.Rect(60, 15, 90, 35)},
	}, {
		name:   "auto-height column never wraps",
		source: `<div style="display:flex;flex-direction:column;flex-wrap:wrap;width:50px"><div id="a" style="height:30px"></div><div id="b" style="height:30px"></div></div>`,
		want:   map[string]image.Rectangle{"a": image.Rect(0, 0, 50, 30), "b": image.Rect(0, 30, 50, 60)},
	}, {
		name:   "shrink-to-fit wrapping row sizes to its items on one line",
		source: `<div id="c" style="float:left;display:flex;flex-wrap:wrap;column-gap:8px"><div id="a" style="width:20px;height:20px;background:red"></div><div id="b" style="width:20px;height:20px;background:blue"></div></div>`,
		want:   map[string]image.Rectangle{"c": image.Rect(0, 0, 48, 20), "a": image.Rect(0, 0, 20, 20), "b": image.Rect(28, 0, 48, 20)},
		pixels: map[image.Point]color.RGBA{{5, 5}: red, {40, 5}: blue},
	}, {
		name:   "shrink-to-fit row uses an inflexible flex basis (#291)",
		source: `<div id="c" style="float:left;display:flex"><div id="a" style="flex:0 0 80px;width:20px;height:20px;background:red"></div></div><div id="n" style="float:left;width:10px;height:10px;background:blue"></div>`,
		want:   map[string]image.Rectangle{"c": image.Rect(0, 0, 80, 20), "a": image.Rect(0, 0, 80, 20), "n": image.Rect(80, 0, 90, 10)},
		pixels: map[image.Point]color.RGBA{{75, 5}: red, {85, 5}: blue},
	}, {
		name:   "unitless flex shorthand implies an indefinite 0% basis (#291)",
		source: `<div id="c" style="float:left;display:flex;height:10px;background:red"><div id="a" style="flex:0;width:40px;height:10px"></div></div><div id="n" style="float:left;width:10px;height:10px;background:blue"></div>`,
		want:   map[string]image.Rectangle{"c": image.Rect(0, 0, 40, 10), "n": image.Rect(40, 0, 50, 10)},
		pixels: map[image.Point]color.RGBA{{35, 5}: {255, 0, 0, 255}, {45, 5}: {0, 0, 255, 255}},
	}, {
		name:   "two-number flex shorthand implies an indefinite 0% basis (#291)",
		source: `<div id="c" style="float:left;display:flex;height:10px;background:red"><div id="a" style="flex:0 0;width:40px;height:10px"></div></div><div id="n" style="float:left;width:10px;height:10px;background:blue"></div>`,
		want:   map[string]image.Rectangle{"c": image.Rect(0, 0, 40, 10), "n": image.Rect(40, 0, 50, 10)},
		pixels: map[image.Point]color.RGBA{{35, 5}: {255, 0, 0, 255}, {45, 5}: {0, 0, 255, 255}},
	}, {
		name:   "explicit zero-length flex basis stays definite (#291)",
		source: `<div id="c" style="float:left;display:flex;height:10px;background:red"><div id="a" style="flex:0 0 0px;width:40px;height:10px"></div></div><div id="n" style="float:left;width:10px;height:10px;background:blue"></div>`,
		want:   map[string]image.Rectangle{"c": image.Rect(0, 0, 0, 10), "a": image.Rect(0, 0, 0, 10), "n": image.Rect(0, 0, 10, 10)},
		pixels: map[image.Point]color.RGBA{{5, 5}: {0, 0, 255, 255}},
	}, {
		name:   "mixed percentage calc basis is indefinite for floated intrinsic width (#282/#291)",
		source: `<div id="c" style="float:left;display:flex"><div id="a" style="flex-grow:0;flex-shrink:0;flex-basis:calc(50% + 10px);width:40px;height:10px;background:red"></div></div>`,
		want:   map[string]image.Rectangle{"c": image.Rect(0, 0, 40, 10), "a": image.Rect(0, 0, 30, 10)},
	}, {
		name:   "mixed percentage calc basis resolves in a definite row (#282/#291)",
		source: `<div id="c" style="display:flex;width:100px"><div id="a" style="flex-grow:0;flex-shrink:0;flex-basis:calc(50% + 10px);width:40px;height:10px;background:red"></div></div>`,
		want:   map[string]image.Rectangle{"c": image.Rect(0, 0, 100, 10), "a": image.Rect(0, 0, 60, 10)},
	}, {
		name:   "shrink-to-fit wrapping row uses inflexible flex bases (#291)",
		source: `<div id="c" style="float:left;display:flex;flex-wrap:wrap;column-gap:8px"><div id="a" style="flex:0 0 50px;width:20px;height:20px;background:red"></div><div id="b" style="flex:0 0 30px;width:40px;padding-left:2px;height:20px;background:blue"></div></div>`,
		want:   map[string]image.Rectangle{"c": image.Rect(0, 0, 90, 20), "a": image.Rect(0, 0, 50, 20), "b": image.Rect(58, 0, 90, 20)},
		pixels: map[image.Point]color.RGBA{{45, 5}: red, {85, 5}: blue},
	}, {
		name:   "flexible basis keeps the content contribution (#291)",
		source: `<div id="c" style="float:left;display:flex"><div id="a" style="flex:1;width:40px;height:10px"></div><div id="b" style="flex:0 1 80px;width:30px;height:10px"></div><div id="d" style="flex:1 0 5px;width:25px;height:10px"></div></div>`,
		want:   map[string]image.Rectangle{"c": image.Rect(0, 0, 95, 10)},
	}, {
		name:   "min-width and max-width clamp a flex-basis contribution (#291)",
		source: `<div id="c" style="float:left;display:flex"><div id="a" style="flex:0 0 80px;min-width:100px;width:20px;height:10px"></div></div><div id="d" style="clear:left;float:left;display:flex"><div id="b" style="flex:0 0 80px;max-width:50px;height:10px"></div></div>`,
		want: map[string]image.Rectangle{"c": image.Rect(0, 0, 100, 10), "a": image.Rect(0, 0, 100, 10),
			"d": image.Rect(0, 10, 50, 20), "b": image.Rect(0, 10, 50, 20)},
	}, {
		name:   "nowrap keeps shrinking on one line",
		source: `<div style="display:flex;width:100px"><div id="a" style="width:60px;height:10px"></div><div id="b" style="width:60px;height:10px"></div></div>`,
		want:   map[string]image.Rectangle{"a": image.Rect(0, 0, 50, 10), "b": image.Rect(50, 0, 100, 10)},
	}}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			source := `<body style="margin:0">` + tc.source + `</body>`
			layout, err := LayoutWithViewport(styledForLayout(t, source), image.Rect(0, 0, 200, 120))
			if err != nil {
				t.Fatal(err)
			}
			ids := make([]string, 0, len(tc.want))
			for id := range tc.want {
				ids = append(ids, id)
			}
			b := boxesByID(layout.Root, ids...)
			for id, rect := range tc.want {
				if b[id] == nil || b[id].Rect != rect {
					t.Errorf("%s: got %v want %v", id, b[id], rect)
				}
			}
			if len(tc.pixels) > 0 {
				img := painted(t, source, image.Rect(0, 0, 200, 120))
				for p, c := range tc.pixels {
					pixel(t, img, p.X, p.Y, c)
				}
			}
		})
	}
}
