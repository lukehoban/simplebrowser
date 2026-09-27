package browser

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

func TestBackgroundRasterPaintAndStylesheetBase(t *testing.T) {
	data := encodedTestImage(t, "png", image.Rect(0, 0, 2, 2))
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/css/site.css":
			_, _ = w.Write([]byte(`.tile { background: url("../assets/pixel.png") no-repeat; background-size: 4px auto; background-position: right bottom; background-color: #0000ff }`))
		case "/assets/pixel.png":
			requests.Add(1)
			_, _ = w.Write(data)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	source := `<link rel="stylesheet" href="../css/site.css"><div class="tile" style="margin:0;width:8px;height:8px;padding:2px;border:2px solid red"></div><div class="tile" style="margin:0;width:8px;height:8px"></div><img src="../assets/pixel.png">`
	doc, err := parse(Resource{URL: server.URL + "/pages/page.html", Body: []byte(source)})
	if err != nil {
		t.Fatal(err)
	}
	styled, err := style(doc, &Fetcher{})
	if err != nil {
		t.Fatal(err)
	}
	if requests.Load() != 1 {
		t.Fatalf("image fetched %d times, want once shared with img", requests.Load())
	}
	layout, err := LayoutWithViewport(styled, image.Rect(0, 0, 80, 60))
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := paint(layout, &out, renderOptions{}); err != nil {
		t.Fatal(err)
	}
	decoded, err := png.Decode(&out)
	if err != nil {
		t.Fatal(err)
	}
	img := decoded.(*image.RGBA)
	pixel(t, img, 0, 0, color.RGBA{255, 0, 0, 255})     // border
	pixel(t, img, 2, 2, color.RGBA{0, 0, 255, 255})     // color underneath
	pixel(t, img, 13, 13, color.RGBA{40, 60, 180, 255}) // scaled bottom right
	pixel(t, img, 10, 2, color.RGBA{0, 0, 255, 255})    // no repeat
}

func TestBackgroundRepeatAndFailuresHaveNoPlaceholder(t *testing.T) {
	data := encodedTestImage(t, "png", image.Rect(0, 0, 2, 2))
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/tile.png" {
			_, _ = w.Write(data)
		} else if r.URL.Path == "/arrow.svg" {
			_, _ = w.Write([]byte(`<svg xmlns="http://www.w3.org/2000/svg"/>`))
		} else {
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	for _, tc := range []struct {
		css  string
		x, y int
		want color.RGBA
	}{
		{`background-image:url(tile.png)`, 4, 4, color.RGBA{0, 0, 180, 255}},
		{`background:url(tile.png) no-repeat`, 4, 4, color.RGBA{255, 255, 255, 255}},
		{`background:url(arrow.svg) no-repeat`, 0, 0, color.RGBA{255, 255, 255, 255}},
		{`background-image:url(absent.png)`, 0, 0, color.RGBA{255, 255, 255, 255}},
	} {
		doc, err := parse(Resource{URL: server.URL + "/page", Body: []byte(`<div style="margin:0;width:8px;height:8px;` + tc.css + `"></div>`)})
		if err != nil {
			t.Fatal(err)
		}
		styled, err := style(doc, &Fetcher{})
		if err != nil {
			t.Fatal(err)
		}
		l, err := LayoutWithViewport(styled, image.Rect(0, 0, 20, 20))
		if err != nil {
			t.Fatal(err)
		}
		var out bytes.Buffer
		if err := paint(l, &out, renderOptions{}); err != nil {
			t.Fatal(err)
		}
		img, err := png.Decode(&out)
		if err != nil {
			t.Fatal(err)
		}
		pixel(t, img.(*image.RGBA), tc.x, tc.y, tc.want)
	}
}
