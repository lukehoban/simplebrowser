# simplebrowser

`simplebrowser` is an educational browser built from scratch in Go. Its goal is
to turn a URL or HTML file into a PNG, with enough web-platform support to
render [Hacker News](https://news.ycombinator.com/) recognizably.

The project has a working fetch, parse, cascade, and layout pipeline covering
block, inline, and table formatting. The CLI fetches HTTP(S) pages, builds a
DOM, loads CSS and GIF/PNG/JPEG images, and computes deterministic box geometry
and wrapped text runs, including replaced image boxes and the nested tables
Hacker News uses for its page structure. Painting now rasterizes backgrounds,
borders, and embedded-font text. Drawing decoded images, SVG, and advanced CSS
remain future work.

## Rendering progress

![Current Hacker News fixture render: orange header, painted story text, and missing logo and vote arrows](docs/screenshots/hn-fixture.png)

*Offline Hacker News snapshot with link styling and text-width fixes through
[a63d208](https://github.com/lukehoban/simplebrowser/commit/a63d2084abf54bbe66050d20e0fa322e0f455a0a)
(2026-09-26). The screenshot is a progress snapshot, not a pixel-accurate
Hacker News reference.*

**What's next**
- [Paint decoded images (#11)](https://github.com/lukehoban/simplebrowser/issues/11).
- [Improve typography (#27)](https://github.com/lukehoban/simplebrowser/issues/27).

[Trailing-whitespace underlines (#25)](https://github.com/lukehoban/simplebrowser/issues/25)
are now fixed.

Every pull request and push to `main` renders the fixture and uploads the
latest PNG as an `hn-render-*` artifact on the
[CI workflow](https://github.com/lukehoban/simplebrowser/actions/workflows/ci.yml).
CI compares the generated image against this checked-in screenshot. The
comparison currently warns rather than fails: macOS and Linux rasterize a few
font pixels differently even with the same embedded font. Refresh the snapshot
after intentional rendering changes:

```sh
make screenshot
```

The offline HTML, stylesheet, and small image assets in `testdata/hn` are a
captured snapshot; rendering does not depend on live Hacker News availability.
HN's black titles and gray subtext now use the page's link styles, and the
trailing-whitespace underline fix has landed. Remaining gaps include
[typography (#27)](https://github.com/lukehoban/simplebrowser/issues/27).
Images are fetched, decoded, and laid out but [not yet painted (#11)](https://github.com/lukehoban/simplebrowser/issues/11);
the missing logo and vote arrows also need
[SVG support (#31)](https://github.com/lukehoban/simplebrowser/issues/31) and
[CSS background images (#32)](https://github.com/lukehoban/simplebrowser/issues/32).

## Architecture

```mermaid
flowchart LR
    Input[URL or file] --> Fetch
    Fetch --> Parse
    Parse --> Style
    Style --> Layout
    Layout --> Paint
    Paint --> PNG
```

The pipeline lives in `internal/browser`. `Document.Root` is a fragment-friendly
DOM with parent/child links, text, comments, doctypes, and ordered attributes.
`Document.BaseURL` holds the effective fetched source (including redirects) and
is the base for relative stylesheet and image resolution. This is a practical tolerant HTML subset,
not a complete HTML5 parsing algorithm.

`ExtractStyles` collects author stylesheets in DOM order (using `Document.BaseURL`
for linked resources) and inline declarations by node. `UserAgentStylesheet`
provides defaults separately. CSS parsing supports common selectors, values,
and `!important`; computed styles retain CSS text for layout to convert. Layout
exposes boxes, text runs, and image boxes through `Layout.Root`, with
configurable viewport geometry and basic embedded Go font metrics.

`img` elements are fetched once per render (deduplicated by resolved URL) and
decoded with the standard library's GIF, PNG, and JPEG decoders, subject to the
fetcher's response-size limit and a decoded-pixel cap. Used dimensions come from
CSS or HTML `width`/`height`, preserving the intrinsic aspect ratio when only
one is given, and fall back to intrinsic size. Failed, oversized, and
unsupported resources (including SVG) keep a sized placeholder box with no
decoded image so layout stays stable; `Box.Images` carries the rectangles and
decoded images to the painter.

Table layout uses the separated-borders model: columns are sized from intrinsic
min/max content widths plus explicit CSS and HTML widths (pixels pin a column,
percentages resolve against the table width), `colspan` widens cells across
columns, and `rowspan` cells cover their rows with any extra height added to the
last spanned row. `cellspacing` and `cellpadding` arrive through the cascade as
`border-spacing` and the table's `padding-*`, the latter acting as the default
padding of cells that declare none. Malformed tables are repaired with anonymous
rows and cells rather than dropped; `internal/browser/table.go` documents each
simplification.

## Usage

Go 1.24 or later is required.

```sh
go run ./cmd/simplebrowser -o out.png https://example.com/
```

The output is currently an 800×600 PNG with viewport-clipped boxes and text.
HTTP(S) resources are
fetched with bounded HTTP/1.1 responses, redirects, and gzip support. Local
paths and `file://` URLs read the supplied file.

Replaced-element layout can be inspected before the painter draws images:

```sh
make image-boxes
```

![Hacker News fixture with laid-out image boxes outlined; the 18x18 logo placeholder is outlined in gray at the top left](docs/screenshots/hn-image-boxes.png)

*Gray outlines are placeholders (missing, broken, or SVG resources); magenta
outlines are decoded GIF/PNG/JPEG images.*

Run the project checks locally with:

```sh
gofmt -w .
go vet ./...
go test ./...
go test -race ./...
go build ./cmd/simplebrowser
```

## Roadmap

Work is tracked under the [browser epic (#2)](https://github.com/lukehoban/simplebrowser/issues/2):

- [Project scaffold and CI (#3)](https://github.com/lukehoban/simplebrowser/issues/3)
- [HTTP/1.1 networking over TCP/TLS (#4)](https://github.com/lukehoban/simplebrowser/issues/4) — implemented
- [HTML tokenizer, parser, and DOM (#5)](https://github.com/lukehoban/simplebrowser/issues/5) — implemented; review pending
- [CSS parser and user-agent stylesheet (#6)](https://github.com/lukehoban/simplebrowser/issues/6) — implemented; review pending
- [Selector matching, cascade, and inheritance (#7)](https://github.com/lukehoban/simplebrowser/issues/7) — implemented
- [Block and inline layout (#8)](https://github.com/lukehoban/simplebrowser/issues/8) — implemented
- [Table layout (#9)](https://github.com/lukehoban/simplebrowser/issues/9) — implemented
- [PNG painting (#10)](https://github.com/lukehoban/simplebrowser/issues/10) — backgrounds, per-side borders, embedded-font text, and clipping implemented
- [GIF, PNG, and JPEG images (#11)](https://github.com/lukehoban/simplebrowser/issues/11) — fetch, decode, and layout implemented; painting and SVG pending
- [Hacker News rendering fidelity and visual CI (#12)](https://github.com/lukehoban/simplebrowser/issues/12) — offline fixture, checked-in screenshot, and render artifact in CI; visual fidelity in progress

See the linked issues for current status and implementation scope.
