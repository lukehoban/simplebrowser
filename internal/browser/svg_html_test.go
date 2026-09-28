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

func TestInlineSVGAppliesHostCSSWithCascadePriority(t *testing.T) {
	tests := []struct {
		name string
		html string
		want color.NRGBA
	}{
		{
			name: "host selector overrides presentation attribute",
			html: `<style>.mark path { fill: rgb(20, 80, 160) }</style><svg class="mark" width="10" height="10" viewBox="0 0 10 10"><path fill="red" d="M0 0H10V10H0Z"/></svg>`,
			want: color.NRGBA{R: 20, G: 80, B: 160, A: 255},
		},
		{
			name: "more specific host rule beats embedded SVG rule",
			html: `<style>.mark path { fill: rgb(20, 80, 160) }</style><svg class="mark" width="10" height="10" viewBox="0 0 10 10"><style>path { fill: red }</style><path d="M0 0H10V10H0Z"/></svg>`,
			want: color.NRGBA{R: 20, G: 80, B: 160, A: 255},
		},
		{
			name: "more specific embedded rule beats host type selector",
			html: `<style>svg path { fill: rgb(20, 80, 160) }</style><svg width="10" height="10" viewBox="0 0 10 10"><style>.inner { fill: red }</style><path class="inner" d="M0 0H10V10H0Z"/></svg>`,
			want: color.NRGBA{R: 255, G: 0, B: 0, A: 255},
		},
		{
			name: "SVG inline declaration beats host stylesheet",
			html: `<style>.mark path { fill: rgb(20, 80, 160) }</style><svg class="mark" width="10" height="10" viewBox="0 0 10 10"><path style="fill: green" d="M0 0H10V10H0Z"/></svg>`,
			want: color.NRGBA{R: 0, G: 128, B: 0, A: 255},
		},
		{
			name: "host important declaration beats normal SVG inline style",
			html: `<style>.mark path { fill: rgb(20, 80, 160) !important }</style><svg class="mark" width="10" height="10" viewBox="0 0 10 10"><path style="fill: green" d="M0 0H10V10H0Z"/></svg>`,
			want: color.NRGBA{R: 20, G: 80, B: 160, A: 255},
		},
		{
			name: "inherited host fill crosses the HTML SVG boundary",
			html: `<div style="fill: blue"><svg width="10" height="10" viewBox="0 0 10 10"><path d="M0 0H10V10H0Z"/></svg></div>`,
			want: color.NRGBA{R: 0, G: 0, B: 255, A: 255},
		},
		{
			name: "SVG root presentation attribute overrides inherited host fill",
			html: `<div style="fill: blue"><svg fill="red" width="10" height="10" viewBox="0 0 10 10"><path d="M0 0H10V10H0Z"/></svg></div>`,
			want: color.NRGBA{R: 255, G: 0, B: 0, A: 255},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			doc := Document{Root: ParseHTML(tc.html)}
			styled, err := style(doc, &Fetcher{})
			if err != nil {
				t.Fatalf("style document: %v", err)
			}
			var svgNode *StyledNode
			var find func(*StyledNode)
			find = func(node *StyledNode) {
				if node == nil || svgNode != nil {
					return
				}
				if node.Node != nil && node.Node.Type == ElementNode && node.Node.Name == "svg" {
					svgNode = node
					return
				}
				for _, child := range node.Children {
					find(child)
				}
			}
			find(styled.StyleRoot)
			img, ok := inlineSVGImage(svgNode).(*svgImage)
			if !ok || img == nil {
				t.Fatal("inlineSVGImage() did not decode the SVG")
			}
			got := color.NRGBAModel.Convert(img.At(5, 5)).(color.NRGBA)
			if got != tc.want {
				t.Fatalf("center pixel = %#v, want %#v", got, tc.want)
			}
		})
	}
}

func TestInlineSVGHostCSSFixtureRendersBluePath(t *testing.T) {
	var output bytes.Buffer
	if err := RenderWithFetcher(filepath.Join("..", "..", "testdata", "svg", "host-css.html"), &output, &Fetcher{}); err != nil {
		t.Fatalf("RenderWithFetcher() error = %v", err)
	}
	img, err := png.Decode(&output)
	if err != nil {
		t.Fatalf("decode rendered PNG: %v", err)
	}
	bluePixels := 0
	for y := img.Bounds().Min.Y; y < img.Bounds().Max.Y; y++ {
		for x := img.Bounds().Min.X; x < img.Bounds().Max.X; x++ {
			r, g, b, _ := img.At(x, y).RGBA()
			if r > 0 && r < 0x3000 && g > 0x4000 && g < 0x7000 && b > 0x8000 {
				bluePixels++
			}
		}
	}
	if bluePixels < 20 {
		t.Fatalf("blue host-styled inline SVG pixels = %d, want a painted icon", bluePixels)
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
