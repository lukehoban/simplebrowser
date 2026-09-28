package browser

import (
	"bytes"
	"image/color"
	"image/png"
	"path/filepath"
	"testing"
)

func TestInlineSVGHTMLRendersCurrentColorAtInlineSize(t *testing.T) {
	root := filepath.Join("..", "..", "testdata", "svg")
	var output bytes.Buffer
	if err := RenderWithFetcher(filepath.Join(root, "inline-html.html"), &output, &Fetcher{}); err != nil {
		t.Fatalf("RenderWithFetcher() error = %v", err)
	}
	img, err := png.Decode(&output)
	if err != nil {
		t.Fatalf("decode rendered PNG: %v", err)
	}
	redPixels := 0
	for y := img.Bounds().Min.Y; y < img.Bounds().Max.Y; y++ {
		for x := img.Bounds().Min.X; x < img.Bounds().Max.X; x++ {
			r, g, b, _ := img.At(x, y).RGBA()
			if r > 0x7000 && g < 0x5000 && b < 0x5000 {
				redPixels++
			}
		}
	}
	if redPixels < 20 {
		t.Fatalf("red inline SVG pixels = %d, want a painted icon", redPixels)
	}
}

func TestInlineSVGInheritsHTMLCurrentColor(t *testing.T) {
	node := ParseHTML(`<span style="color: rgb(20, 80, 160)"><svg width="10" height="10" viewBox="0 0 10 10" fill="currentColor"><path d="M0 0H10V10H0Z"/></svg></span>`)
	styled, err := style(Document{Root: node}, &Fetcher{})
	if err != nil {
		t.Fatalf("style document: %v", err)
	}
	svgNode := styled.StyleRoot.Children[0].Children[0]
	img, ok := inlineSVGImage(svgNode).(*svgImage)
	if !ok || img == nil {
		t.Fatal("inlineSVGImage() did not decode the SVG")
	}
	got := color.NRGBAModel.Convert(img.At(5, 5)).(color.NRGBA)
	if got.R != 20 || got.G != 80 || got.B != 160 || got.A != 255 {
		t.Fatalf("SVG currentColor pixel = %#v, want opaque rgb(20, 80, 160)", got)
	}
}

func TestInlineSVGRestoresSpreadMethodCase(t *testing.T) {
	node := ParseHTML(`<span><svg width="100" height="10" viewBox="0 0 100 10"><defs><linearGradient id="g" x1="0%" x2="50%" spreadMethod="reflect"><stop offset="0%" stop-color="red"/><stop offset="100%" stop-color="blue"/></linearGradient></defs><rect width="100" height="10" fill="url(#g)"/></svg></span>`)
	styled, err := style(Document{Root: node}, &Fetcher{})
	if err != nil {
		t.Fatalf("style document: %v", err)
	}
	img := inlineSVGImage(styled.StyleRoot.Children[0].Children[0])
	if img == nil {
		t.Fatal("inlineSVGImage() did not decode the reflecting gradient")
	}
	got := color.NRGBAModel.Convert(img.At(75, 5)).(color.NRGBA)
	if got.R < 80 || got.R > 180 || got.B < 80 || got.B > 180 {
		t.Fatalf("reflecting gradient pixel = %#v, want an interpolated color after the gradient repeats", got)
	}
}

func TestInlineSVGResolvesXLinkHrefWithoutHTMLNamespaceDeclaration(t *testing.T) {
	node := ParseHTML(`<span><svg width="10" height="10" viewBox="0 0 10 10"><defs><path id="p" d="M0 0H10V10H0Z"/></defs><use xlink:href="#p" fill="#1450a0"/></svg></span>`)
	styled, err := style(Document{Root: node}, &Fetcher{})
	if err != nil {
		t.Fatalf("style document: %v", err)
	}
	img := inlineSVGImage(styled.StyleRoot.Children[0].Children[0])
	if img == nil {
		t.Fatal("inlineSVGImage() did not decode use with xlink:href")
	}
	got := color.NRGBAModel.Convert(img.At(5, 5)).(color.NRGBA)
	if got.R != 20 || got.G != 80 || got.B != 160 || got.A != 255 {
		t.Fatalf("xlink:href pixel = %#v, want opaque rgb(20, 80, 160)", got)
	}
}

func TestInlineSVGXLinkDiscoveryRespectsSerializationDepthLimit(t *testing.T) {
	root := &Node{Type: ElementNode, Name: "svg"}
	child := root
	for range 64 {
		next := &Node{Type: ElementNode, Name: "g"}
		child.Children = []*Node{next}
		child = next
	}
	// A cycle below the accepted serialization depth must not be traversed by
	// a separate xlink discovery pass. The bounded serializer rejects it first.
	child.Children = []*Node{child}

	if got := inlineSVGImage(&StyledNode{Node: root}); got != nil {
		t.Fatalf("inlineSVGImage() = %T, want nil beyond the depth limit", got)
	}
}
