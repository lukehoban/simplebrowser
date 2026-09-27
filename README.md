# simplebrowser

[![CI](https://github.com/lukehoban/simplebrowser/actions/workflows/ci.yml/badge.svg?branch=main)](https://github.com/lukehoban/simplebrowser/actions/workflows/ci.yml)

An educational browser built from scratch in Go: render a URL or local HTML file
to a PNG. The completed [browser epic (#2)](https://github.com/lukehoban/simplebrowser/issues/2)
records the initial Hacker News rendering milestone.

<p align="center">
  <img src="docs/screenshots/hn-fixture.png" width="560" alt="Hacker News offline fixture rendered by simplebrowser">
</p>

*Current render of the checked-in, offline [Hacker News fixture](testdata/hn/news.html);
not a pixel-perfect browser reference.*

**What works:** HTTP(S) and local-file loading, HTML parsing, CSS cascade,
block/inline/table layout, and PNG painting of text, borders, backgrounds,
images, and a practical SVG subset. CI tests the renderer and verifies the
committed HN golden.

**What's next:** improve the bounded
[Wikipedia Moon](docs/wikipedia-moon-baseline.md) and
[GitHub repository-page](docs/github-vscode-baseline.md) views. Those checked-in
baseline/reference pairs document the current output, reproduction commands,
and evidence-backed gaps; the live [site epics (#243)](https://github.com/lukehoban/simplebrowser/issues/243)
and [(#242)](https://github.com/lukehoban/simplebrowser/issues/242) track work.

**Compatibility:** see the generated [coverage report](docs/compatibility.md),
[pass/fail graph](docs/compatibility.svg), and
[machine-readable results](docs/compatibility.json). They describe a small,
pinned reference set—not a general web-platform conformance score—and are
verified by CI rather than copied into this README.

**CSS length math:** bounded `calc()` arithmetic is documented with a rendered
[width example](docs/css-calc.md).

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
rendering change, run `make baselines` to refresh the blocking HN golden plus
the non-blocking Moon and GitHub VS Code diagnostic baselines. The individual
targets are `make screenshot`, `make moon-baseline`, and
`make github-vscode-baseline`; `make image-boxes` refreshes the HN
[image-box diagnostic](docs/screenshots/hn-image-boxes.png).
