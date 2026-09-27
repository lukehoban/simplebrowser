package main

import (
	"bytes"
	"image"
	_ "image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunWritesPNG(t *testing.T) {
	output := filepath.Join(t.TempDir(), "page.png")
	var stderr bytes.Buffer

	if err := run([]string{"-o", output, "example.html"}, &stderr); err != nil {
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
