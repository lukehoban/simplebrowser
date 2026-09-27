package main

import (
	"bytes"
	"image"
	"image/color"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestReferenceLinkMetadata(t *testing.T) {
	got, err := reference([]byte(`<link rel="match" href="same.html"><link rel="mismatch" href="different.html"><link rel="mismatch" href="also-different.html"><link rel="help" href="spec">`))
	want := []reftestReference{
		{Relation: "match", Href: "same.html"},
		{Relation: "mismatch", Href: "different.html"},
		{Relation: "mismatch", Href: "also-different.html"},
	}
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("reference() = %#v, %v; want %#v", got, err, want)
	}
	got, err = reference([]byte(`<link rel='help' href='spec'><link rel='mismatch other match' href='ordered.html'>`))
	want = []reftestReference{{Relation: "mismatch", Href: "ordered.html"}, {Relation: "match", Href: "ordered.html"}}
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("reference() should retain relation token order: %#v, %v", got, err)
	}
	if _, err := reference([]byte(`<link rel="help" href="spec">`)); err == nil {
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

func TestRunAllRelationsAndPerReferenceErrors(t *testing.T) {
	root := t.TempDir()
	testDir := filepath.Join(root, "relations")
	if err := os.MkdirAll(testDir, 0755); err != nil {
		t.Fatal(err)
	}
	page := func(color string) string {
		return "<!doctype html><html><body style=\"background-color:" + color + "\"></body></html>"
	}
	testHTML := `<link rel="match" href="same.html">` +
		`<link rel="mismatch" href="different.html">` +
		`<link rel="match" href="different.html">` +
		`<link rel="match">` +
		`<link rel="match" href="missing.html">` +
		`<link rel="match" href="../../outside.html">` +
		page("red")
	for path, content := range map[string]string{
		"test.html":      testHTML,
		"same.html":      page("red"),
		"different.html": page("blue"),
	} {
		if err := os.WriteFile(filepath.Join(testDir, path), []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}

	originalTests := tests
	tests = []string{"relations/test.html"}
	defer func() { tests = originalTests }()
	diagnostics := filepath.Join(t.TempDir(), "diagnostics")
	r := run(root, diagnostics)
	if r.Total != 6 || r.Pass != 2 || r.Fail != 1 || r.Error != 3 || len(r.Results) != 6 {
		t.Fatalf("unexpected multi-relation report: %+v", r)
	}
	for i, relation := range []string{"match", "mismatch", "match", "match", "match", "match"} {
		if r.Results[i].Relation != relation || r.Results[i].Test != "relations/test.html" {
			t.Fatalf("relation %d out of deterministic order: %+v", i, r.Results[i])
		}
	}
	if r.Results[3].Status != "error" || !strings.Contains(r.Results[3].Error, "empty reference href") {
		t.Fatalf("missing href should be a per-reference error: %+v", r.Results[3])
	}
	if r.Results[4].Status != "error" || !strings.Contains(r.Results[4].Error, "missing.html") {
		t.Fatalf("missing reference should be a per-reference error: %+v", r.Results[4])
	}
	if r.Results[5].Status != "error" || !strings.Contains(r.Results[5].Error, "escapes vendor directory") {
		t.Fatalf("escaping reference should be a per-reference error: %+v", r.Results[5])
	}
	if _, err := os.Stat(filepath.Join(diagnostics, "relations", "test", "reference-003-match", "diff.png")); err != nil {
		t.Fatalf("failed relation did not get its own diagnostic: %v", err)
	}
}

func TestRunOriginalXHTMLFixtureWithCDATA(t *testing.T) {
	root := filepath.Join("..", "..", "testdata", "wpt")
	path := filepath.Join(root, "colors", "color-175-ref.xht")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(data, []byte("<![CDATA[")) || !bytes.Contains(data, []byte("]]>")) {
		t.Fatal("vendored fixture must retain its original CDATA delimiters")
	}

	originalTests := tests
	tests = []string{"colors/color-175.xht"}
	defer func() { tests = originalTests }()
	r := run(root, filepath.Join(t.TempDir(), "diagnostics"))
	if r.Pass != 1 || r.Fail != 0 || r.Error != 0 {
		t.Fatalf("runner did not apply CDATA stylesheet from original fixture: %+v", r)
	}
}
