package main

import (
	"image"
	"image/color"
	"path/filepath"
	"strings"
	"testing"
)

func TestReferenceLinkMetadata(t *testing.T) {
	for _, tc := range []struct {
		html, relation, href string
	}{
		{`<link href="ref.html" rel="match">`, "match", "ref.html"},
		{`<link rel='help' href='spec'><link rel='mismatch other' href='different.html'>`, "mismatch", "different.html"},
	} {
		relation, href, err := reference([]byte(tc.html))
		if err != nil || relation != tc.relation || href != tc.href {
			t.Fatalf("reference(%q) = %q %q %v", tc.html, relation, href, err)
		}
	}
	if _, _, err := reference([]byte(`<link rel="help" href="spec">`)); err == nil {
		t.Fatal("missing metadata should be a runner error")
	}
}

func TestCompareExactPixels(t *testing.T) {
	a, b := image.NewRGBA(image.Rect(0, 0, 2, 1)), image.NewRGBA(image.Rect(0, 0, 2, 1))
	a.Set(1, 0, color.NRGBA{R: 255, A: 255})
	count, diff := compare(a, b)
	if count != 1 || diff.At(0, 0) != (color.RGBA{}) || diff.At(1, 0) == (color.RGBA{}) {
		t.Fatalf("unexpected diff: count=%d diff=%v", count, diff)
	}
}

func TestDeterministicMarkdown(t *testing.T) {
	r := report{Revision: revision, Viewport: "800x600", Total: 1, Pass: 1,
		Results: []result{{Test: "a.html", Reference: "b.html", Relation: "mismatch", Status: "pass", Pixels: 1}}}
	if got := string(markdown(r)); !strings.Contains(got, "1/1 passing") || !strings.Contains(got, "`a.html` | `b.html` | mismatch | **pass** | 1") {
		t.Fatal("unexpected markdown output")
	}
}

func TestMissingFixtureIsRunnerError(t *testing.T) {
	r := run(t.TempDir(), filepath.Join(t.TempDir(), "diagnostics"))
	if r.Error != len(tests) || r.Pass != 0 || r.Fail != 0 {
		t.Fatalf("missing fixtures should be errors, got %+v", r)
	}
	for _, item := range r.Results {
		if item.Status != "error" || !strings.Contains(item.Error, "testdata/wpt/") {
			t.Fatalf("error should have deterministic path: %+v", item)
		}
	}
}
