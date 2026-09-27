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
	"box/ltr-basic.xht",
	"box/rtl-basic.xht",
	"box/ltr-ib.xht",
	"box/rtl-ib.xht",
	"margin-padding-clear/margin-001.xht",
	"margin-padding-clear/margin-002.xht",
	"margin-padding-clear/margin-003.xht",
	"margin-padding-clear/margin-004.xht",
	"positioning/absolute-non-replaced-height-003.xht",
	"positioning/absolute-non-replaced-height-006.xht",
	"positioning/position-relative-001.xht",
	"positioning/position-relative-003.xht",
	"abspos/abspos-containing-block-initial-004a.xht",
	"abspos/abspos-containing-block-initial-007.xht",
	"tables/border-collapse-offset-001.xht",
	"tables/border-collapse-offset-002.xht",
	"tables/border-collapse-empty-row.html",
	"tables/separated-border-model-007.xht",
	"tables/caption-position-001.xht",
	"tables/fixed-table-layout-002a.xht",
	"colors/color-applies-to-004.xht",
	"colors/color-applies-to-005.xht",
	"colors/colors-007.xht",
	"colors/color-applies-to-002.xht",
	"colors/color-applies-to-003.xht",
}

type result struct {
	Test      string `json:"test"`
	Reference string `json:"reference"`
	Relation  string `json:"relation"`
	Status    string `json:"status"`
	Pixels    int    `json:"different_pixels"`
	Error     string `json:"error,omitempty"`
}

type reftestReference struct {
	Relation string
	Href     string
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

func reference(data []byte) ([]reftestReference, error) {
	var references []reftestReference
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
				references = append(references, reftestReference{
					Relation: word,
					Href:     strings.TrimSpace(attrs["href"]),
				})
			}
		}
	}
	if len(references) == 0 {
		return nil, errors.New("missing rel=match/mismatch reference")
	}
	return references, nil
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
	r := report{Revision: revision, Viewport: "800x600", Results: make([]result, 0, len(tests))}
	// Clean old diagnostics, so only current failures are uploaded.
	_ = os.RemoveAll(diagnostics)
	for _, name := range tests {
		testResult := result{Test: name}
		testPath := filepath.Join(root, filepath.FromSlash(name))
		data, err := os.ReadFile(testPath)
		var references []reftestReference
		if err == nil {
			references, err = reference(data)
		}
		if err != nil {
			appendError := func(item result, err error) {
				// Keep error reports reproducible across machines and runs.
				item.Status, item.Error = "error", strings.ReplaceAll(err.Error(), root, "testdata/wpt")
				r.Total++
				r.Error++
				r.Results = append(r.Results, item)
			}
			appendError(testResult, err)
			continue
		}

		testImg, renderErr := render(testPath)
		for index, ref := range references {
			// Each relation is an independent assertion. Do not let a prior
			// reference's validation or rendering error poison the next one.
			err = nil
			r.Total++
			item := result{Test: name, Relation: ref.Relation, Reference: ref.Href}
			refPath := filepath.Clean(filepath.Join(filepath.Dir(name), filepath.FromSlash(ref.Href)))
			switch {
			case ref.Href == "":
				err = errors.New("empty reference href")
			case filepath.IsAbs(ref.Href) || refPath == ".." || strings.HasPrefix(refPath, ".."+string(filepath.Separator)) || strings.Contains(ref.Href, "://"):
				err = fmt.Errorf("reference escapes vendor directory: %q", ref.Href)
			case renderErr != nil:
				err = renderErr
			default:
				item.Reference = filepath.ToSlash(refPath)
				var refImg image.Image
				refImg, err = render(filepath.Join(root, refPath))
				if err == nil && testImg.Bounds() != refImg.Bounds() {
					err = fmt.Errorf("image bounds differ: %v vs %v", testImg.Bounds(), refImg.Bounds())
				}
				if err == nil {
					var diff *image.RGBA
					item.Pixels, diff = compare(testImg, refImg)
					passes := (item.Relation == "match" && item.Pixels == 0) || (item.Relation == "mismatch" && item.Pixels > 0)
					if passes {
						item.Status = "pass"
						r.Pass++
					} else {
						item.Status = "fail"
						r.Fail++
						dir := filepath.Join(diagnostics, strings.TrimSuffix(name, filepath.Ext(name)),
							fmt.Sprintf("reference-%03d-%s", index+1, item.Relation))
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
							err = fmt.Errorf("write diagnostics: %w", e)
							item.Status = "error"
							r.Fail--
						}
					}
				}
			}
			if err != nil {
				// Keep error reports reproducible across machines and runs.
				item.Status, item.Error = "error", strings.ReplaceAll(err.Error(), root, "testdata/wpt")
				r.Error++
			}
			r.Results = append(r.Results, item)
		}
	}
	return r
}

func markdown(r report) []byte {
	var b strings.Builder
	fmt.Fprintf(&b, "# WPT compatibility: %d/%d reference assertions passing\n\n", r.Pass, r.Total)
	fmt.Fprintf(&b, "Pinned WPT revision: [`%s`](https://github.com/web-platform-tests/wpt/commit/%s). Viewport: %s. Exact PNG pixels; %d compatibility failures, %d runner errors. See [benchmark notes](../testdata/wpt/README.md) and [machine-readable results](compatibility.json).\n\n", r.Revision, r.Revision, r.Viewport, r.Fail, r.Error)
	b.WriteString("![Stacked pass/fail graph by selected WPT tranche and area](compatibility.svg)\n\n")
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

// graph is an SVG rather than a raster chart so the committed visual can be
// reproduced exactly on every platform without a font or rasterizer dependency.
func graph(r report) []byte {
	type bucket struct {
		name               string
		pass, fail, errors int
	}
	buckets := []bucket{
		{name: "Initial baseline"},
		{name: "Box direction"},
		{name: "Margins"},
		{name: "Positioning"},
		{name: "Tables"},
		{name: "Colors"},
	}
	for i, item := range r.Results {
		index := 0
		if i >= 13 {
			switch {
			case strings.HasPrefix(item.Test, "box/"):
				index = 1
			case strings.HasPrefix(item.Test, "margin-padding-clear/"):
				index = 2
			case strings.HasPrefix(item.Test, "positioning/"), strings.HasPrefix(item.Test, "abspos/"):
				index = 3
			case strings.HasPrefix(item.Test, "tables/"):
				index = 4
			case strings.HasPrefix(item.Test, "colors/"):
				index = 5
			}
		}
		switch item.Status {
		case "pass":
			buckets[index].pass++
		case "fail":
			buckets[index].fail++
		default:
			buckets[index].errors++
		}
	}
	var b strings.Builder
	fmt.Fprintf(&b, "<svg xmlns=\"http://www.w3.org/2000/svg\" width=\"680\" height=\"%d\" viewBox=\"0 0 680 %d\" role=\"img\" aria-label=\"WPT selected reference assertions: %d pass, %d fail, %d runner errors\">\n", 92+len(buckets)*34, 92+len(buckets)*34, r.Pass, r.Fail, r.Error)
	b.WriteString("<rect width=\"100%\" height=\"100%\" fill=\"white\"/>\n")
	fmt.Fprintf(&b, "<text x=\"16\" y=\"27\" font-family=\"sans-serif\" font-size=\"18\">Pinned WPT: %d/%d passing</text>\n", r.Pass, r.Total)
	b.WriteString("<text x=\"16\" y=\"53\" font-family=\"sans-serif\" font-size=\"13\">Green: pass   Red: compatibility failure   Gray: runner error</text>\n")
	for i, item := range buckets {
		y := 76 + i*34
		fmt.Fprintf(&b, "<text x=\"16\" y=\"%d\" font-family=\"sans-serif\" font-size=\"13\">%s</text>\n", y+13, item.name)
		x := 180
		for _, part := range []struct {
			count int
			color string
		}{{item.pass, "#21864b"}, {item.fail, "#bd3636"}, {item.errors, "#666666"}} {
			if part.count > 0 {
				fmt.Fprintf(&b, "<rect x=\"%d\" y=\"%d\" width=\"%d\" height=\"20\" fill=\"%s\"/>\n", x, y, part.count*25, part.color)
				x += part.count * 25
			}
		}
		fmt.Fprintf(&b, "<text x=\"%d\" y=\"%d\" font-family=\"sans-serif\" font-size=\"13\">%d / %d</text>\n", x+8, y+15, item.pass, item.pass+item.fail+item.errors)
	}
	b.WriteString("</svg>\n")
	return []byte(b.String())
}

func main() {
	check := flag.Bool("check", false, "verify committed reports without changing them")
	flag.Parse()
	r := run("testdata/wpt", "artifacts/wpt")
	j, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		panic(err)
	}
	j = append(j, '\n')
	for path, content := range map[string][]byte{"docs/compatibility.json": j, "docs/compatibility.md": markdown(r), "docs/compatibility.svg": graph(r)} {
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
		score := fmt.Sprintf("%d/%d pinned WPT reference assertions passing", r.Pass, r.Total)
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
