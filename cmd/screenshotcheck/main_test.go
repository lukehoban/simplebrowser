package main

import (
	"image"
	"image/color"
	"strings"
	"testing"
)

func TestScreenshotTolerance(t *testing.T) {
	base := image.NewNRGBA(image.Rect(0, 0, 8, 8))
	for y := 0; y < 8; y++ {
		for x := 0; x < 8; x++ {
			base.SetNRGBA(x, y, color.NRGBA{100, 120, 140, 255})
		}
	}
	if err := compare(base, base); err != nil {
		t.Fatal(err)
	}
	copyImage := func() *image.NRGBA {
		out := image.NewNRGBA(base.Bounds())
		copy(out.Pix, base.Pix)
		return out
	}
	noise := copyImage()
	for i := 0; i < 3; i++ {
		noise.SetNRGBA(i, 0, color.NRGBA{101, 119, 140, 255})
	}
	if err := compare(base, noise); err != nil {
		t.Fatalf("measured 3-pixel noise should pass: %v", err)
	}
	noise.SetNRGBA(3, 0, color.NRGBA{101, 120, 140, 255})
	if err := compare(base, noise); err == nil || !strings.Contains(err.Error(), "exceed limit") {
		t.Fatalf("stale fourth pixel must fail: %v", err)
	}
	stale := copyImage()
	stale.SetNRGBA(0, 0, color.NRGBA{102, 120, 140, 255})
	if err := compare(base, stale); err == nil || !strings.Contains(err.Error(), "beyond tolerance") {
		t.Fatalf("visible color change must fail: %v", err)
	}
	if err := compare(base, image.NewNRGBA(image.Rect(0, 0, 9, 8))); err == nil {
		t.Fatal("changed dimensions must fail")
	}
}
