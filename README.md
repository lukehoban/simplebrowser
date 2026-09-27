# simplebrowser

[![CI](https://github.com/lukehoban/simplebrowser/actions/workflows/ci.yml/badge.svg?branch=main)](https://github.com/lukehoban/simplebrowser/actions/workflows/ci.yml)

An educational browser built from scratch in Go: render a URL or local HTML file
to a PNG. The [browser epic (#2)](https://github.com/lukehoban/simplebrowser/issues/2)
tracks progress toward a recognizable Hacker News page.

![Hacker News offline fixture rendered by simplebrowser](docs/screenshots/hn-fixture.png)

*Current render of the checked-in, offline [Hacker News fixture](testdata/hn/news.html);
not a pixel-perfect browser reference.*

**Compatibility benchmark: [13/13 pinned WPT reference assertions passing](docs/compatibility.md)**
([JSON results](docs/compatibility.json)). The 800×600 exact-pixel subset covers
colors, backgrounds, normal flow, and tables. This is a small, pinned test set,
not a general web-platform conformance score. Run `make compatibility` to
regenerate the committed report and inspect failure diagnostics in
`artifacts/wpt/`.

**What works:** HTTP(S) and local-file loading; HTML parsing; CSS cascade; block,
inline, and table layout; PNG painting of text, borders, images, and a subset of
SVG. Raster CSS backgrounds paint in layers, and local SVG `<defs>`/`<use>`
references are supported. Bounded two-color CSS `linear-gradient()` backgrounds
support directions, angles, percentage stops, and multiple interpolation
hints. SVG strokes support bounded dash arrays and offsets with CSS lengths
and percentages; basic shapes and stroke widths resolve font-relative
`ex`/`ch` lengths using embedded face metrics. The offline HN
fixture is rendered in CI, with the result available as a
[workflow artifact](https://github.com/lukehoban/simplebrowser/actions/workflows/ci.yml).

**What's next:** Improve [HN fidelity (#12)](https://github.com/lukehoban/simplebrowser/issues/12)
and [SVG gradients and opacity (#84)](https://github.com/lukehoban/simplebrowser/issues/84).
See [epic #2](https://github.com/lukehoban/simplebrowser/issues/2) for the live
checklist and further scope.

## Usage

Requires Go 1.24 or later.

```sh
go run ./cmd/simplebrowser -o out.png https://example.com/
# Or render the bundled offline fixture:
go run ./cmd/simplebrowser -o out.png testdata/hn/news.html
```

The output is an 800×600 viewport-clipped PNG. Use `-image-boxes` to outline
laid-out image boxes for diagnosis. Unsupported or failed image resources
retain a placeholder rather than disappearing from layout.

## Architecture

Input (URL or file) → fetch → HTML DOM → CSS cascade → layout → paint → PNG.
The implementation lives in [`internal/browser`](internal/browser);
[`cmd/simplebrowser`](cmd/simplebrowser) is the CLI. The parser, CSS engine,
layout, and SVG renderer intentionally cover practical subsets of the web
platform, not a complete browser implementation.

## Development

```sh
gofmt -l .
go vet ./...
go test -race ./...
go build ./cmd/simplebrowser
make compatibility-check
```

CI runs these checks and verifies the committed HN screenshot. To regenerate
the pinned [compatibility report](docs/compatibility.md) and inspect mismatch
images under `artifacts/wpt/`, run `make compatibility`. After an intentional
rendering change, run `make screenshot` to refresh the checked-in HN image;
`make image-boxes` refreshes its [image-box diagnostic](docs/screenshots/hn-image-boxes.png).
