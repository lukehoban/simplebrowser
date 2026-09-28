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

type benchmark struct {
	Test       string
	Area       string
	Suite      string
	Diagnostic bool
	NotCovered string
	Issue      string
}

func pinned(test, area, notCovered string) benchmark {
	return benchmark{Test: test, Area: area, Suite: "WPT", NotCovered: notCovered}
}

var tests = []benchmark{
	pinned("colors/color-175.xht", "Colors", "Color parsing beyond this declaration and inheritance case."),
	pinned("colors/color-176.xht", "Colors", "Color parsing beyond this declaration and inheritance case."),
	pinned("colors/color-177.xht", "Colors", "Color parsing beyond this declaration and inheritance case."),
	pinned("colors/color-applies-to-001.xht", "Colors", "Color application outside the element exercised here."),
	pinned("backgrounds/background-001.xht", "Backgrounds", "Multiple layers, sizing, positioning, and canvas propagation."),
	pinned("backgrounds/background-002.xht", "Backgrounds", "Multiple layers, sizing, positioning, and canvas propagation."),
	pinned("normal-flow/block-formatting-contexts-001.xht", "Normal flow", "Floats, clearance, fragmentation, and writing modes."),
	pinned("normal-flow/block-formatting-contexts-003.xht", "Normal flow", "Floats, clearance, fragmentation, and writing modes."),
	pinned("normal-flow/block-formatting-contexts-005.xht", "Normal flow", "Floats, clearance, fragmentation, and writing modes."),
	pinned("normal-flow/block-formatting-context-height-001.xht", "Normal flow", "Floats, clearance, fragmentation, and writing modes."),
	pinned("normal-flow/block-in-inline-align-001.html", "Normal flow", "General block-in-inline splitting and bidi layout."),
	pinned("tables/anonymous-table-box-width-001.xht", "Tables", "Collapsed-border conflict resolution and spanning cells."),
	pinned("tables/border-collapse-005.html", "Tables", "The full collapsed-border conflict precedence algorithm."),
	pinned("box/ltr-basic.xht", "Box direction", "Vertical writing modes and bidi reordering."),
	pinned("box/rtl-basic.xht", "Box direction", "Vertical writing modes and bidi reordering."),
	pinned("box/ltr-ib.xht", "Box direction", "Vertical writing modes and bidi reordering."),
	pinned("box/rtl-ib.xht", "Box direction", "Vertical writing modes and bidi reordering."),
	pinned("margin-padding-clear/margin-001.xht", "Margins", "Margin collapsing with floats, clearance, or negative margins."),
	pinned("margin-padding-clear/margin-002.xht", "Margins", "Margin collapsing with floats, clearance, or negative margins."),
	pinned("margin-padding-clear/margin-003.xht", "Margins", "Margin collapsing with floats, clearance, or negative margins."),
	pinned("margin-padding-clear/margin-004.xht", "Margins", "Margin collapsing with floats, clearance, or negative margins."),
	pinned("positioning/absolute-non-replaced-height-003.xht", "Positioning", "Replaced elements, fixed positioning, and stacking."),
	pinned("positioning/absolute-non-replaced-height-006.xht", "Positioning", "Replaced elements, fixed positioning, and stacking."),
	pinned("positioning/position-relative-001.xht", "Positioning", "Relative offsets in writing modes other than horizontal LTR."),
	pinned("positioning/position-relative-003.xht", "Positioning", "Relative offsets in writing modes other than horizontal LTR."),
	pinned("abspos/abspos-containing-block-initial-004a.xht", "Positioning", "Nested transformed or non-initial containing blocks."),
	pinned("abspos/abspos-containing-block-initial-007.xht", "Positioning", "Nested transformed or non-initial containing blocks."),
	pinned("tables/border-collapse-offset-001.xht", "Tables", "The full collapsed-border conflict precedence algorithm."),
	pinned("tables/border-collapse-offset-002.xht", "Tables", "The full collapsed-border conflict precedence algorithm."),
	pinned("tables/border-collapse-empty-row.html", "Tables", "Spans and non-empty row-group border conflicts."),
	pinned("tables/separated-border-model-007.xht", "Tables", "Collapsed borders and spanning cells."),
	pinned("tables/caption-position-001.xht", "Tables", "Side captions, multiple captions, and writing modes."),
	pinned("tables/fixed-table-layout-002a.xht", "Tables", "Automatic table layout and spanning cells."),
	pinned("colors/color-applies-to-004.xht", "Colors", "Color application outside the element exercised here."),
	pinned("colors/color-applies-to-005.xht", "Colors", "Color application outside the element exercised here."),
	pinned("colors/colors-007.xht", "Colors", "Modern color syntaxes, profiles, and interpolation."),
	pinned("colors/color-applies-to-002.xht", "Colors", "Color application outside the element exercised here."),
	pinned("colors/color-applies-to-003.xht", "Colors", "Color application outside the element exercised here."),
	// Promoted from the diagnostic matrix once #68 implemented clearance.
	pinned("floats-clear/clear-001.xht", "Floats and clear", "Right floats, multiple floats, and margin-collapse interactions."),
	// Promoted once #216 resolved top/bottom percentages against containing-block height.
	pinned("positioning/bottom-offset-percentage-001.xht", "Positioning", "Auto offsets, replaced elements, and indefinite (auto-height) containing blocks (#217)."),

	{Test: "margin-padding-clear/margin-collapse-003.xht", Area: "Margins", Suite: "WPT", Diagnostic: true, NotCovered: "Floats, clearance, negative margins, and margin trimming."},
	{Test: "floats-clear/clear-002.xht", Area: "Floats and clear", Suite: "WPT", Diagnostic: true, NotCovered: "Nested formatting contexts and negative clearance; the reference needs inline relative offsets.", Issue: "#76"},
	{Test: "positioning/position-relative-004.xht", Area: "Positioning", Suite: "WPT", Diagnostic: true, NotCovered: "Writing modes, bidi reordering, and positioned descendants.", Issue: "#76"},
	{Test: "backgrounds/background-body-001.xht", Area: "Backgrounds", Suite: "WPT", Diagnostic: true, NotCovered: "Background images, repeat, position, size, and multiple layers."},
	{Test: "linebox/line-box-height-002.xht", Area: "Line boxes", Suite: "WPT", Diagnostic: true, NotCovered: "Mixed fonts, vertical-align variants, bidi, and vertical writing modes."},

	{Test: "canvas-background-image.html", Area: "Backgrounds", Suite: "Local", Diagnostic: true, NotCovered: "Positioning, sizing, non-solid tiles, multiple layers, and root-image propagation.", Issue: "#63"},
	{Test: "float-clearance-margin-collapse.html", Area: "Floats and clear", Suite: "Local", Diagnostic: true, NotCovered: "Right floats, multiple floats, inline wrapping, and negative margins.", Issue: "#68"},
	{Test: "float-clearance-sides.html", Area: "Floats and clear", Suite: "Local", Diagnostic: true, NotCovered: "Inline wrapping after clearance, negative margins, and clearance on a first child that collapses through its parent (#275).", Issue: "#68"},
	{Test: "collapsed-border-conflict.html", Area: "Tables", Suite: "Local", Diagnostic: true, NotCovered: "Multi-row/column segmentation, spanning cells, and padded row geometry (#397, #318).", Issue: "#66"},
	{Test: "collapsed-border-precedence.html", Area: "Tables", Suite: "Local", Diagnostic: true, NotCovered: "Multi-row/column segmentation, spanning cells, and padded row geometry (#397, #318).", Issue: "#396"},
	{Test: "collapsed-row-cell-border.html", Area: "Tables", Suite: "Local", Diagnostic: true, NotCovered: "Multi-row/column segmentation, spanning cells, and padded row geometry (#397, #318).", Issue: "#66"},
	{Test: "inline-table-line-edge.html", Area: "Tables", Suite: "Local", Diagnostic: true, NotCovered: "Multiple cells, spans, captions, bidi, and vertical alignment variants.", Issue: "#209"},
}

type result struct {
	Test       string `json:"test"`
	Reference  string `json:"reference"`
	Relation   string `json:"relation"`
	Suite      string `json:"suite"`
	Area       string `json:"area"`
	Diagnostic bool   `json:"diagnostic"`
	NotCovered string `json:"not_covered"`
	Issue      string `json:"issue,omitempty"`
	Status     string `json:"status"`
	Pixels     int    `json:"different_pixels"`
	Error      string `json:"error,omitempty"`
}

type reftestReference struct {
	Relation string
	Href     string
}

type report struct {
	Revision string      `json:"wpt_revision"`
	Viewport string      `json:"viewport"`
	Blocking score       `json:"blocking_wpt"`
	WPT      score       `json:"diagnostic_wpt"`
	Local    score       `json:"diagnostic_local"`
	Areas    []areaScore `json:"areas"`
	Results  []result    `json:"results"`
}

type score struct {
	Total int `json:"total"`
	Pass  int `json:"pass"`
	Fail  int `json:"fail"`
	Error int `json:"error"`
}

type areaScore struct {
	Suite string `json:"suite"`
	Area  string `json:"area"`
	score
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
	for _, test := range tests {
		name := test.Test
		testResult := result{
			Test: name, Suite: test.Suite, Area: test.Area, Diagnostic: test.Diagnostic,
			NotCovered: test.NotCovered, Issue: test.Issue,
		}
		testRoot := root
		if test.Suite == "Local" {
			testRoot = filepath.Join(filepath.Dir(root), "wpt-local")
		}
		testPath := filepath.Join(testRoot, filepath.FromSlash(name))
		data, err := os.ReadFile(testPath)
		var references []reftestReference
		if err == nil {
			references, err = reference(data)
		}
		if err != nil {
			appendError := func(item result, err error) {
				// Keep error reports reproducible across machines and runs.
				item.Status, item.Error = "error", reproducibleError(err, root)
				addResult(&r, item)
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
			item := testResult
			item.Relation, item.Reference = ref.Relation, ref.Href
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
				refImg, err = render(filepath.Join(testRoot, refPath))
				if err == nil && testImg.Bounds() != refImg.Bounds() {
					err = fmt.Errorf("image bounds differ: %v vs %v", testImg.Bounds(), refImg.Bounds())
				}
				if err == nil {
					var diff *image.RGBA
					item.Pixels, diff = compare(testImg, refImg)
					passes := (item.Relation == "match" && item.Pixels == 0) || (item.Relation == "mismatch" && item.Pixels > 0)
					if passes {
						item.Status = "pass"
					} else {
						item.Status = "fail"
						diagnosticName := strings.TrimSuffix(name, filepath.Ext(name))
						if test.Suite == "Local" {
							diagnosticName = filepath.Join("local", diagnosticName)
						}
						dir := filepath.Join(diagnostics, diagnosticName, fmt.Sprintf("reference-%03d-%s", index+1, item.Relation))
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
						}
					}
				}
			}
			if err != nil {
				// Keep error reports reproducible across machines and runs.
				item.Status, item.Error = "error", reproducibleError(err, root)
			}
			addResult(&r, item)
			r.Results = append(r.Results, item)
		}
	}
	r.Areas = summarizeAreas(r.Results)
	return r
}

func reproducibleError(err error, root string) string {
	message := strings.ReplaceAll(err.Error(), filepath.Join(filepath.Dir(root), "wpt-local"), "testdata/wpt-local")
	return strings.ReplaceAll(message, root, "testdata/wpt")
}

func addScore(s *score, status string) {
	s.Total++
	switch status {
	case "pass":
		s.Pass++
	case "fail":
		s.Fail++
	default:
		s.Error++
	}
}

func addResult(r *report, item result) {
	switch {
	case !item.Diagnostic:
		addScore(&r.Blocking, item.Status)
	case item.Suite == "WPT":
		addScore(&r.WPT, item.Status)
	default:
		addScore(&r.Local, item.Status)
	}
}

func summarizeAreas(results []result) []areaScore {
	var areas []areaScore
	for _, item := range results {
		index := -1
		for i := range areas {
			if areas[i].Suite == item.Suite && areas[i].Area == item.Area {
				index = i
				break
			}
		}
		if index < 0 {
			areas = append(areas, areaScore{Suite: item.Suite, Area: item.Area})
			index = len(areas) - 1
		}
		addScore(&areas[index].score, item.Status)
	}
	return areas
}

func markdown(r report) []byte {
	var b strings.Builder
	fmt.Fprintf(&b, "# Compatibility coverage matrix\n\n")
	fmt.Fprintf(&b, "**Blocking regression set: %d/%d pinned WPT reference assertions passing.** New coverage is diagnostic: WPT %d/%d, repo-owned references %d/%d.\n\n",
		r.Blocking.Pass, r.Blocking.Total, r.WPT.Pass, r.WPT.Total, r.Local.Pass, r.Local.Total)
	fmt.Fprintf(&b, "Pinned WPT revision: [`%s`](https://github.com/web-platform-tests/wpt/commit/%s). Viewport: %s. Exact PNG pixels. The selected tests are a bounded coverage matrix, not a general conformance score. See [benchmark notes](../testdata/wpt/README.md) and [machine-readable results](compatibility.json).\n\n", r.Revision, r.Revision, r.Viewport)
	b.WriteString("![Stacked pass/fail graph by benchmark suite and area](compatibility.svg)\n\n")
	b.WriteString("## Per-area results\n\n| Suite | Area | Pass | Fail | Error | Total |\n| --- | --- | ---: | ---: | ---: | ---: |\n")
	for _, area := range r.Areas {
		fmt.Fprintf(&b, "| %s | %s | %d | %d | %d | %d |\n", area.Suite, area.Area, area.Pass, area.Fail, area.Error, area.Total)
	}
	b.WriteString("\n## Assertions\n\n| Suite | Area | Test | Reference | Status | Different pixels | Does not cover |\n| --- | --- | --- | --- | --- | ---: | --- |\n")
	for _, item := range r.Results {
		test := fmt.Sprintf("`%s`", item.Test)
		if item.Issue != "" {
			test += fmt.Sprintf(" ([%s](https://github.com/lukehoban/simplebrowser/issues/%s))", item.Issue, strings.TrimPrefix(item.Issue, "#"))
		}
		fmt.Fprintf(&b, "| %s | %s | %s | `%s` (%s) | **%s** | %d | %s |\n",
			item.Suite, item.Area, test, item.Reference, item.Relation, item.Status, item.Pixels, item.NotCovered)
		if item.Error != "" {
			fmt.Fprintf(&b, "\nError in `%s`: %s\n", item.Test, item.Error)
		}
	}
	b.WriteString("\nThe first 38 WPT assertions are blocking regressions. New WPT and local assertions are diagnostic: mismatches remain visible without making CI fail. On failures, run `make compatibility` and inspect `artifacts/wpt/<suite>/<test>/` (test, reference, red pixel diff).\n")
	return []byte(b.String())
}

// graph is an SVG rather than a raster chart so the committed visual can be
// reproduced exactly on every platform without a font or rasterizer dependency.
func graph(r report) []byte {
	var b strings.Builder
	height := 112 + len(r.Areas)*34
	fmt.Fprintf(&b, "<svg xmlns=\"http://www.w3.org/2000/svg\" width=\"760\" height=\"%d\" viewBox=\"0 0 760 %d\" role=\"img\" aria-label=\"Compatibility reference assertions by suite and area\">\n", height, height)
	b.WriteString("<rect width=\"100%\" height=\"100%\" fill=\"white\"/>\n")
	fmt.Fprintf(&b, "<text x=\"16\" y=\"27\" font-family=\"sans-serif\" font-size=\"18\">Blocking WPT: %d/%d · Diagnostic WPT: %d/%d · Local: %d/%d</text>\n", r.Blocking.Pass, r.Blocking.Total, r.WPT.Pass, r.WPT.Total, r.Local.Pass, r.Local.Total)
	b.WriteString("<text x=\"16\" y=\"53\" font-family=\"sans-serif\" font-size=\"13\">Green: pass   Red: mismatch   Gray: runner error · one block per assertion</text>\n")
	for i, item := range r.Areas {
		y := 76 + i*34
		fmt.Fprintf(&b, "<text x=\"16\" y=\"%d\" font-family=\"sans-serif\" font-size=\"13\">%s · %s</text>\n", y+13, item.Suite, item.Area)
		x := 240
		for _, part := range []struct {
			count int
			color string
		}{{item.Pass, "#21864b"}, {item.Fail, "#bd3636"}, {item.Error, "#666666"}} {
			if part.count > 0 {
				fmt.Fprintf(&b, "<rect x=\"%d\" y=\"%d\" width=\"%d\" height=\"20\" fill=\"%s\"/>\n", x, y, part.count*25, part.color)
				x += part.count * 25
			}
		}
		fmt.Fprintf(&b, "<text x=\"%d\" y=\"%d\" font-family=\"sans-serif\" font-size=\"13\">%d / %d</text>\n", x+8, y+15, item.Pass, item.Total)
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
		artifacts := []string{"docs/compatibility.md", "docs/compatibility.svg", "docs/compatibility.json"}
		for _, artifact := range artifacts {
			if e != nil || !bytes.Contains(readme, []byte(artifact)) {
				fmt.Fprintln(os.Stderr, "README does not link generated compatibility artifact:", artifact)
				os.Exit(1)
			}
		}
		if bytes.Contains(readme, []byte("pinned WPT reference assertions passing")) {
			fmt.Fprintln(os.Stderr, "README duplicates the generated compatibility score")
			os.Exit(1)
		}
	}
	fmt.Printf("Blocking WPT: %d/%d; diagnostic WPT: %d/%d; local: %d/%d\n",
		r.Blocking.Pass, r.Blocking.Total, r.WPT.Pass, r.WPT.Total, r.Local.Pass, r.Local.Total)
	if r.Blocking.Fail != 0 || r.Blocking.Error != 0 || r.WPT.Error != 0 || r.Local.Error != 0 {
		os.Exit(1)
	}
}
