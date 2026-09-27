// wptbench runs a pinned, deliberately small offline subset of WPT reftests.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/lukehoban/simplebrowser/internal/browser"
)

const revision = "647d3bdf133159739b57cfb7afa0be3f5d76b9db"

var tests = []string{
	"colors/color-175.xht",
	"colors/color-176.xht",
	"colors/color-177.xht",
	"colors/color-applies-to-001.xht",
	"backgrounds/background-001.xht",
	"backgrounds/background-002.xht",
	"normal-flow/block-formatting-contexts-001.xht",
	"normal-flow/block-formatting-contexts-003.xht",
	"normal-flow/block-formatting-contexts-005.xht",
	"normal-flow/block-formatting-context-height-001.xht",
	"normal-flow/block-in-inline-align-001.html",
	"tables/anonymous-table-box-width-001.xht",
	"tables/border-collapse-005.html",
}

type result struct {
	Test      string `json:"test"`
	Reference string `json:"reference"`
	Relation  string `json:"relation"`
	Status    string `json:"status"`
	Pixels    int    `json:"different_pixels"`
	Error     string `json:"error,omitempty"`
}

type report struct {
	Revision string   `json:"wpt_revision"`
	Viewport string   `json:"viewport"`
	Total    int      `json:"total"`
	Pass     int      `json:"pass"`
	Fail     int      `json:"fail"`
	Error    int      `json:"error"`
	Results  []result `json:"results"`
}

// Reftest link attributes can occur in either order and either quote style.
var linkRE = regexp.MustCompile(`(?is)<link\b[^>]*>`)
var attrRE = regexp.MustCompile(`(?i)([a-z-]+)\s*=\s*(?:"([^"]*)"|'([^']*)')`)

func reference(data []byte) (string, string, error) {
	for _, tag := range linkRE.FindAll(data, -1) {
		attrs := map[string]string{}
		for _, m := range attrRE.FindAllSubmatch(tag, -1) {
			value := m[2]
			if value == nil {
				value = m[3]
			}
			attrs[strings.ToLower(string(m[1]))] = string(value)
		}
		for _, word := range strings.Fields(strings.ToLower(attrs["rel"])) {
			if word == "match" || word == "mismatch" {
				return word, strings.TrimSpace(attrs["href"]), nil
			}
		}
	}
	return "", "", errors.New("missing rel=match/mismatch reference")
}

func render(path string) (image.Image, error) {
	var out bytes.Buffer
	// Network references must never be fetched, including from CSS or images.
	fetcher := &browser.Fetcher{DialContext: func(context.Context, string, string) (net.Conn, error) {
		return nil, errors.New("network forbidden in offline benchmark")
	}}
	if err := browser.RenderWithFetcher(path, &out, fetcher); err != nil {
		return nil, err
	}
	return png.Decode(&out)
}

func compare(a, b image.Image) (int, *image.RGBA) {
	bounds := a.Bounds()
	diff := image.NewRGBA(bounds)
	count := 0
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			c1, c2 := color.NRGBAModel.Convert(a.At(x, y)), color.NRGBAModel.Convert(b.At(x, y))
			if c1 != c2 {
				count++
				diff.Set(x, y, color.NRGBA{R: 255, A: 255})
			}
		}
	}
	return count, diff
}

func writePNG(path string, img image.Image) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	err = png.Encode(f, img)
	if closeErr := f.Close(); err == nil {
		err = closeErr
	}
	return err
}

func run(root, diagnostics string) report {
	r := report{Revision: revision, Viewport: "800x600", Total: len(tests), Results: make([]result, 0, len(tests))}
	// Clean old diagnostics, so only current failures are uploaded.
	_ = os.RemoveAll(diagnostics)
	for _, name := range tests {
		item := result{Test: name}
		testPath := filepath.Join(root, filepath.FromSlash(name))
		data, err := os.ReadFile(testPath)
		if err == nil {
			item.Relation, item.Reference, err = reference(data)
		}
		var testImg, refImg image.Image
		if err == nil {
			ref := filepath.Clean(filepath.Join(filepath.Dir(name), filepath.FromSlash(item.Reference)))
			if filepath.IsAbs(item.Reference) || ref == ".." || strings.HasPrefix(ref, ".."+string(filepath.Separator)) || strings.Contains(item.Reference, "://") {
				err = fmt.Errorf("reference escapes vendor directory: %q", item.Reference)
			} else {
				item.Reference = filepath.ToSlash(ref)
				testImg, err = render(testPath)
				if err == nil {
					refImg, err = render(filepath.Join(root, ref))
				}
			}
		}
		if err == nil && testImg.Bounds() != refImg.Bounds() {
			err = fmt.Errorf("image bounds differ: %v vs %v", testImg.Bounds(), refImg.Bounds())
		}
		if err != nil {
			item.Status, item.Error = "error", err.Error()
			r.Error++
		} else {
			var diff *image.RGBA
			item.Pixels, diff = compare(testImg, refImg)
			passes := (item.Relation == "match" && item.Pixels == 0) || (item.Relation == "mismatch" && item.Pixels > 0)
			if passes {
				item.Status = "pass"
				r.Pass++
			} else {
				item.Status = "fail"
				r.Fail++
				dir := filepath.Join(diagnostics, strings.TrimSuffix(name, filepath.Ext(name)))
				e := os.MkdirAll(dir, 0755)
				if e == nil {
					e = writePNG(filepath.Join(dir, "test.png"), testImg)
				}
				if e == nil {
					e = writePNG(filepath.Join(dir, "reference.png"), refImg)
				}
				if e == nil {
					e = writePNG(filepath.Join(dir, "diff.png"), diff)
				}
				if e != nil {
					item.Status, item.Error = "error", "write diagnostics: "+e.Error()
					r.Fail--
					r.Error++
				}
			}
		}
		r.Results = append(r.Results, item)
	}
	return r
}

func markdown(r report) []byte {
	var b strings.Builder
	fmt.Fprintf(&b, "# WPT compatibility: %d/%d passing\n\n", r.Pass, r.Total)
	fmt.Fprintf(&b, "Pinned WPT revision: [`%s`](https://github.com/web-platform-tests/wpt/commit/%s). Viewport: %s. Exact PNG pixels; %d compatibility failures, %d runner errors. See [benchmark notes](../testdata/wpt/README.md) and [machine-readable results](compatibility.json).\n\n", r.Revision, r.Revision, r.Viewport, r.Fail, r.Error)
	b.WriteString("| Test | Reference | Relation | Status | Different pixels |\n| --- | --- | --- | --- | ---: |\n")
	for _, item := range r.Results {
		fmt.Fprintf(&b, "| `%s` | `%s` | %s | **%s** | %d |\n", item.Test, item.Reference, item.Relation, item.Status, item.Pixels)
		if item.Error != "" {
			fmt.Fprintf(&b, "\nError in `%s`: %s\n", item.Test, item.Error)
		}
	}
	b.WriteString("\nOn failures, run `make compatibility` and inspect `artifacts/wpt/<test>/` (test, reference, red pixel diff). A failure is a pixel mismatch, not a test process failure.\n")
	return []byte(b.String())
}

func main() {
	check := flag.Bool("check", false, "verify committed reports without changing them")
	flag.Parse()
	// WPT's .xht files are served as XHTML upstream. The browser currently
	// treats local .xht as HTML: HTML style contents do not recognize XML CDATA
	// delimiters. Strip only those delimiters in a disposable copy so CSS is
	// measured, not an artifact of the fixture's XML transport format.
	root, err := os.MkdirTemp("", "simplebrowser-wpt-*")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer os.RemoveAll(root)
	err = filepath.WalkDir("testdata/wpt", func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		relative, e := filepath.Rel("testdata/wpt", path)
		if e != nil {
			return e
		}
		dest := filepath.Join(root, relative)
		if entry.IsDir() {
			return os.MkdirAll(dest, 0755)
		}
		data, e := os.ReadFile(path)
		if e != nil {
			return e
		}
		if strings.HasSuffix(path, ".xht") {
			data = bytes.ReplaceAll(data, []byte("<![CDATA["), nil)
			data = bytes.ReplaceAll(data, []byte("]]>"), nil)
		}
		return os.WriteFile(dest, data, 0644)
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	r := run(root, "artifacts/wpt")
	j, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		panic(err)
	}
	j = append(j, '\n')
	for path, content := range map[string][]byte{"docs/compatibility.json": j, "docs/compatibility.md": markdown(r)} {
		if *check {
			old, e := os.ReadFile(path)
			if e != nil || !bytes.Equal(old, content) {
				fmt.Fprintln(os.Stderr, path, "is stale; run make compatibility")
				os.Exit(1)
			}
		} else if e := os.WriteFile(path, content, 0644); e != nil {
			fmt.Fprintln(os.Stderr, e)
			os.Exit(1)
		}
	}
	if *check {
		readme, e := os.ReadFile("README.md")
		score := fmt.Sprintf("%d/%d pinned WPT reftests passing", r.Pass, r.Total)
		if e != nil || !bytes.Contains(readme, []byte(score)) {
			fmt.Fprintln(os.Stderr, "README compatibility score is stale:", score)
			os.Exit(1)
		}
	}
	fmt.Printf("WPT: %d/%d pass, %d fail, %d errors\n", r.Pass, r.Total, r.Fail, r.Error)
	if r.Error != 0 {
		os.Exit(1)
	}
}
