// Command screenshotcheck verifies that a committed screenshot matches a
// freshly rendered PNG, allowing only measured cross-platform raster noise.
package main

import (
	"fmt"
	"image"
	"image/color"
	"image/png"
	"os"
)

// The HN fixture rendered on macOS and Linux at 68271b1 differs in three
// pixels, by at most one 8-bit channel value each. Keep this deliberately
// narrow: a layout shift, a missing glyph, or a changed color must fail.
const maxNoisyPixels = 3
const maxChannelDelta = 1

func main() {
	if len(os.Args) != 3 {
		fmt.Fprintln(os.Stderr, "usage: screenshotcheck committed.png rendered.png")
		os.Exit(2)
	}
	a, err := readPNG(os.Args[1])
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	b, err := readPNG(os.Args[2])
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if err := compare(a, b); err != nil {
		fmt.Fprintln(os.Stderr, "stale screenshot:", err)
		os.Exit(1)
	}
	fmt.Println("Screenshot matches (at most 3 pixels with 1-level channel noise).")
}

func readPNG(path string) (image.Image, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	defer f.Close()
	img, err := png.Decode(f)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return img, nil
}

func compare(a, b image.Image) error {
	if a.Bounds() != b.Bounds() {
		return fmt.Errorf("dimensions differ: %v vs %v", a.Bounds(), b.Bounds())
	}
	noisy := 0
	for y := a.Bounds().Min.Y; y < a.Bounds().Max.Y; y++ {
		for x := a.Bounds().Min.X; x < a.Bounds().Max.X; x++ {
			// Convert via NRGBA to compare straight 8-bit channels, even if
			// PNGs use different color models.
			p, q := colorAt(a, x, y), colorAt(b, x, y)
			if p == q {
				continue
			}
			if delta(p.R, q.R) > maxChannelDelta ||
				delta(p.G, q.G) > maxChannelDelta ||
				delta(p.B, q.B) > maxChannelDelta ||
				delta(p.A, q.A) > maxChannelDelta {
				return fmt.Errorf("pixel (%d,%d) differs beyond tolerance: %v vs %v", x, y, p, q)
			}
			noisy++
			if noisy > maxNoisyPixels {
				return fmt.Errorf("%d noisy pixels exceed limit %d (last at %d,%d)", noisy, maxNoisyPixels, x, y)
			}
		}
	}
	return nil
}

func colorAt(img image.Image, x, y int) color.NRGBA {
	return color.NRGBAModel.Convert(img.At(x, y)).(color.NRGBA)
}

func delta(a, b uint8) int {
	if a > b {
		return int(a - b)
	}
	return int(b - a)
}
