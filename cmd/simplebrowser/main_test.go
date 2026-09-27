package main

import (
	"bytes"
	"image"
	_ "image/png"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func TestRunWritesPNG(t *testing.T) {
	source := filepath.Join(t.TempDir(), "page.html")
	if err := os.WriteFile(source, []byte("<html>fixture</html>"), 0o600); err != nil {
		t.Fatal(err)
	}
	output := filepath.Join(t.TempDir(), "page.png")
	var stderr bytes.Buffer

	if err := run([]string{"-o", output, source}, &stderr); err != nil {
		t.Fatalf("run() error = %v, stderr = %q", err, stderr.String())
	}

	file, err := os.Open(output)
	if err != nil {
		t.Fatalf("open output: %v", err)
	}
	defer file.Close()
	_, format, err := image.Decode(file)
	if err != nil {
		t.Fatalf("decode output: %v", err)
	}
	if format != "png" {
		t.Fatalf("format = %q, want png", format)
	}
}

func TestRunRendersHackerNewsFixture(t *testing.T) {
	source := filepath.Join("..", "..", "testdata", "hn", "news.html")
	output := filepath.Join(t.TempDir(), "hn.png")
	var stderr bytes.Buffer

	if err := run([]string{"-o", output, source}, &stderr); err != nil {
		t.Fatalf("run() error = %v, stderr = %q", err, stderr.String())
	}

	file, err := os.Open(output)
	if err != nil {
		t.Fatalf("open output: %v", err)
	}
	defer file.Close()
	config, format, err := image.DecodeConfig(file)
	if err != nil {
		t.Fatalf("decode output: %v", err)
	}
	if format != "png" {
		t.Fatalf("format = %q, want png", format)
	}
	if config.Width <= 0 || config.Height <= 0 {
		t.Fatalf("image dimensions = %dx%d, want positive dimensions", config.Width, config.Height)
	}
}

// TestMoonFixtureIsOffline guards the pinned Wikipedia Moon fixture (#245):
// every stylesheet, image and CSS url() must resolve to a committed local
// file so the non-blocking baseline renders without network access.
func TestMoonFixtureIsOffline(t *testing.T) {
	dir := filepath.Join("..", "..", "testdata", "wikipedia-moon")
	html, err := os.ReadFile(filepath.Join(dir, "moon.html"))
	if err != nil {
		t.Fatal(err)
	}
	if regexp.MustCompile(`(?i)<script`).Match(html) {
		t.Error("moon.html contains a <script> element")
	}
	check := func(base, ref string) {
		t.Helper()
		if strings.HasPrefix(ref, "data:") {
			return
		}
		if strings.Contains(ref, "//") || strings.Contains(ref, ":") {
			t.Errorf("%s references non-local resource %q", base, ref)
			return
		}
		if _, err := os.Stat(filepath.Join(filepath.Dir(base), filepath.FromSlash(ref))); err != nil {
			t.Errorf("%s references missing file %q", base, ref)
		}
	}
	page := filepath.Join(dir, "moon.html")
	for _, m := range regexp.MustCompile(`<img\b[^>]*\ssrc="([^"]*)"`).FindAllSubmatch(html, -1) {
		check(page, string(m[1]))
	}
	for _, m := range regexp.MustCompile(`<link\b[^>]*rel="stylesheet"[^>]*href="([^"]*)"`).FindAllSubmatch(html, -1) {
		check(page, string(m[1]))
	}
	urlRef := regexp.MustCompile(`url\(\s*['"]?([^'")]*)`)
	sheets, _ := filepath.Glob(filepath.Join(dir, "styles", "*.css"))
	if len(sheets) == 0 {
		t.Fatal("no fixture stylesheets found")
	}
	for _, sheet := range append(sheets, page) {
		data, err := os.ReadFile(sheet)
		if err != nil {
			t.Fatal(err)
		}
		for _, m := range urlRef.FindAllSubmatch(data, -1) {
			if ref := string(m[1]); ref != "http://www.w3.org/1998/Math/MathML" {
				check(sheet, ref)
			}
		}
	}
}

func TestRunRendersMoonFixture(t *testing.T) {
	source := filepath.Join("..", "..", "testdata", "wikipedia-moon", "moon.html")
	output := filepath.Join(t.TempDir(), "moon.png")
	var stderr bytes.Buffer
	if err := run([]string{"-o", output, source}, &stderr); err != nil {
		t.Fatalf("run() error = %v, stderr = %q", err, stderr.String())
	}
	file, err := os.Open(output)
	if err != nil {
		t.Fatalf("open output: %v", err)
	}
	defer file.Close()
	config, _, err := image.DecodeConfig(file)
	if err != nil {
		t.Fatalf("decode output: %v", err)
	}
	if config.Width != 800 || config.Height != 600 {
		t.Fatalf("image dimensions = %dx%d, want 800x600", config.Width, config.Height)
	}
}

func TestRunRequiresOneSource(t *testing.T) {
	var stderr bytes.Buffer
	err := run(nil, &stderr)
	if err == nil {
		t.Fatal("run() error = nil, want an error")
	}
	if !strings.Contains(stderr.String(), "Usage: simplebrowser") {
		t.Fatalf("stderr = %q, want usage", stderr.String())
	}
}
