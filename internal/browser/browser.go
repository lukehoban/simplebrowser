// Package browser defines the rendering pipeline.
package browser

import (
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"io"
	"strings"
)

const (
	placeholderWidth  = 800
	placeholderHeight = 600
)

// Resource is the output of the fetch stage.
type Resource struct {
	Source string
	Body   []byte
}

// Document is the output of the parse stage.
type Document struct {
	Resource Resource
}

// StyledDocument is the output of the style stage.
type StyledDocument struct {
	Document Document
}

// Layout is the output of the layout stage.
type Layout struct {
	Document StyledDocument
	Viewport image.Rectangle
}

// Render runs source through each browser stage and writes a PNG to output.
//
// The stages currently preserve pipeline structure only. Follow-up roadmap
// issues replace each stub with real browser behavior.
func Render(source string, output io.Writer) error {
	resource, err := fetch(source)
	if err != nil {
		return fmt.Errorf("fetch: %w", err)
	}

	document, err := parse(resource)
	if err != nil {
		return fmt.Errorf("parse: %w", err)
	}

	styled, err := style(document)
	if err != nil {
		return fmt.Errorf("style: %w", err)
	}

	layout, err := layout(styled)
	if err != nil {
		return fmt.Errorf("layout: %w", err)
	}

	if err := paint(layout, output); err != nil {
		return fmt.Errorf("paint: %w", err)
	}
	return nil
}

func fetch(source string) (Resource, error) {
	if strings.TrimSpace(source) == "" {
		return Resource{}, fmt.Errorf("source must not be empty")
	}

	// Issue #4 will fetch HTTP(S) URLs and local files.
	return Resource{Source: source}, nil
}

func parse(resource Resource) (Document, error) {
	// Issue #5 will tokenize HTML and build a DOM.
	return Document{Resource: resource}, nil
}

func style(document Document) (StyledDocument, error) {
	// Issues #6 and #7 will parse CSS and compute styles.
	return StyledDocument{Document: document}, nil
}

func layout(document StyledDocument) (Layout, error) {
	// Issues #8 and #9 will create block, inline, and table layout boxes.
	return Layout{
		Document: document,
		Viewport: image.Rect(0, 0, placeholderWidth, placeholderHeight),
	}, nil
}

func paint(layout Layout, output io.Writer) error {
	// Issue #10 will rasterize the layout tree. Until then, draw a stable
	// placeholder that makes the CLI useful for testing the complete pipeline.
	canvas := image.NewRGBA(layout.Viewport)
	draw.Draw(canvas, canvas.Bounds(), &image.Uniform{C: color.RGBA{R: 247, G: 249, B: 252, A: 255}}, image.Point{}, draw.Src)

	bands := []color.RGBA{
		{R: 72, G: 95, B: 199, A: 255},
		{R: 96, G: 120, B: 220, A: 255},
		{R: 122, G: 145, B: 235, A: 255},
		{R: 151, G: 172, B: 245, A: 255},
		{R: 185, G: 201, B: 250, A: 255},
	}
	for index, band := range bands {
		x0 := 120 + index*110
		rect := image.Rect(x0, 270, x0+80, 330)
		draw.Draw(canvas, rect, &image.Uniform{C: band}, image.Point{}, draw.Src)
	}

	return png.Encode(output, canvas)
}
