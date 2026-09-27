// Package browser defines the rendering pipeline.
package browser

import (
	"fmt"
	"image"
	"io"
)

const (
	placeholderWidth  = 800
	placeholderHeight = 600
)

// Resource is the output of the fetch stage.
type Resource struct {
	Source      string // Requested source (for display and diagnostics).
	URL         string // Effective source after redirects; base for relative links.
	ContentType string // HTTP Content-Type when the transport provides one.
	Body        []byte
}

// Document is the output of the parse stage.
type Document struct {
	Resource Resource
	Root     *Node
	BaseURL  string // Effective resource URL or path; not the requested redirect URL.
}

// StyledDocument is the output of the style stage.
type StyledDocument struct {
	Document         Document
	UserAgent        Stylesheet
	Stylesheets      []Stylesheet
	InlineStyles     map[*Node][]Declaration
	StyleRoot        *StyledNode
	Styles           map[*Node]ComputedStyle
	Images           map[*Node]image.Image
	BackgroundImages map[*Node]image.Image
	styleViewport    image.Point // Size used for computed values; zero for manually constructed styles.
}

// ComputedStyle contains the values used by later layout and painting stages.
// Values remain CSS text, with font- and viewport-relative lengths resolved to
// pixels. Layout resolves percentages against their property-specific bases.
type ComputedStyle map[string]string

// StyledNode retains its source DOM node while adding its computed style.
type StyledNode struct {
	Node     *Node
	Style    ComputedStyle
	Children []*StyledNode
}

// Layout is the output of the layout stage.
type Layout struct {
	Document StyledDocument
	Viewport image.Rectangle
	Root     *Box
}

// Render runs source through each browser stage and writes a PNG to output.
func Render(source string, output io.Writer) error {
	return RenderWithFetcher(source, output, nil)
}

// RenderImageBoxes renders source and outlines laid-out image boxes. It is a
// diagnostic for reviewing replaced-element layout independently of painting.
func RenderImageBoxes(source string, output io.Writer) error {
	return renderWithOptions(source, output, nil, renderOptions{debugImageBoxes: true})
}

// RenderWithFetcher runs source through each browser stage using fetcher.
// A nil fetcher selects the default networking configuration.
func RenderWithFetcher(source string, output io.Writer, fetcher *Fetcher) error {
	return renderWithOptions(source, output, fetcher, renderOptions{})
}

type renderOptions struct {
	debugImageBoxes bool
}

func renderWithOptions(source string, output io.Writer, fetcher *Fetcher, options renderOptions) error {
	if fetcher == nil {
		fetcher = &Fetcher{}
	}
	resource, err := fetcher.Fetch(source)
	if err != nil {
		return fmt.Errorf("fetch: %w", err)
	}

	document, err := parse(resource)
	if err != nil {
		return fmt.Errorf("parse: %w", err)
	}

	styled, err := style(document, fetcher)
	if err != nil {
		return fmt.Errorf("style: %w", err)
	}

	layout, err := layout(styled)
	if err != nil {
		return fmt.Errorf("layout: %w", err)
	}

	if err := paint(layout, output, options); err != nil {
		return fmt.Errorf("paint: %w", err)
	}
	return nil
}

func layout(document StyledDocument) (Layout, error) {
	return LayoutWithViewport(document, image.Rect(0, 0, placeholderWidth, placeholderHeight))
}
