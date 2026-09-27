package browser

import (
	"bytes"
	"image"
	_ "image/png"
	"os"
	"path/filepath"
	"testing"
)

func TestRenderProducesDeterministicPNG(t *testing.T) {
	source := filepath.Join(t.TempDir(), "page.html")
	if err := os.WriteFile(source, []byte("<html>local fixture</html>"), 0o600); err != nil {
		t.Fatal(err)
	}

	var first bytes.Buffer
	if err := Render(source, &first); err != nil {
		t.Fatalf("Render() error = %v", err)
	}

	var second bytes.Buffer
	if err := Render(source, &second); err != nil {
		t.Fatalf("Render() second error = %v", err)
	}

	if !bytes.Equal(first.Bytes(), second.Bytes()) {
		t.Fatal("Render() output is not deterministic")
	}

	config, format, err := image.DecodeConfig(bytes.NewReader(first.Bytes()))
	if err != nil {
		t.Fatalf("decode output: %v", err)
	}
	if format != "png" {
		t.Fatalf("format = %q, want png", format)
	}
	if config.Width != placeholderWidth || config.Height != placeholderHeight {
		t.Fatalf("dimensions = %dx%d, want %dx%d", config.Width, config.Height, placeholderWidth, placeholderHeight)
	}
}

func TestRenderRejectsEmptySource(t *testing.T) {
	var output bytes.Buffer
	if err := Render("  ", &output); err == nil {
		t.Fatal("Render() error = nil, want an error")
	}
	if output.Len() != 0 {
		t.Fatalf("Render() wrote %d bytes after invalid input", output.Len())
	}
}
