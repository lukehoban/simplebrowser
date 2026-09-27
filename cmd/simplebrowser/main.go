package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/lukehoban/simplebrowser/internal/browser"
)

func main() {
	if err := run(os.Args[1:], os.Stderr); err != nil {
		fmt.Fprintf(os.Stderr, "simplebrowser: %v\n", err)
		os.Exit(1)
	}
}

func run(args []string, stderr io.Writer) error {
	flags := flag.NewFlagSet("simplebrowser", flag.ContinueOnError)
	flags.SetOutput(stderr)
	outputPath := flags.String("o", "out.png", "output PNG path")
	imageBoxes := flags.Bool("image-boxes", false, "outline laid-out image boxes (layout diagnostic)")
	flags.Usage = func() {
		fmt.Fprintf(flags.Output(), "Usage: simplebrowser [-o out.png] <url|file>\n")
		flags.PrintDefaults()
	}

	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 1 {
		flags.Usage()
		return fmt.Errorf("expected exactly one URL or file")
	}

	return renderFile(flags.Arg(0), *outputPath, *imageBoxes)
}

func renderFile(source, destination string, imageBoxes bool) error {
	directory := filepath.Dir(destination)
	temp, err := os.CreateTemp(directory, ".simplebrowser-*.png")
	if err != nil {
		return fmt.Errorf("create output: %w", err)
	}
	tempName := temp.Name()
	defer os.Remove(tempName)

	render := browser.Render
	if imageBoxes {
		render = browser.RenderImageBoxes
	}
	if err := render(source, temp); err != nil {
		temp.Close()
		return err
	}
	if err := temp.Close(); err != nil {
		return fmt.Errorf("close output: %w", err)
	}
	if err := os.Rename(tempName, destination); err != nil {
		return fmt.Errorf("save output: %w", err)
	}
	return nil
}
