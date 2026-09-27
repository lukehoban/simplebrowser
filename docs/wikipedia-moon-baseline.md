# Wikipedia Moon: 800×600 offline baseline (#245)

Evidence for the first [Wikipedia epic (#243)](https://github.com/lukehoban/simplebrowser/issues/243)
milestone: a deterministic, static, above-the-fold render of the default-skin
[Moon](https://en.wikipedia.org/wiki/Moon) article. Fixture, provenance and
licenses: [`testdata/wikipedia-moon/`](../testdata/wikipedia-moon/README.md).

- **Chrome reference:**

  ![Chrome reference for the offline Moon fixture](screenshots/wikipedia-moon/chrome-reference.png)

  [`chrome-reference.png`](screenshots/wikipedia-moon/chrome-reference.png)
  Headless Chrome 154 on macOS renders the same offline fixture served over
  local HTTP at 800×600, device scale 1. macOS font substitution:
  `sans-serif` → Helvetica, and `"Linux Libertine", Georgia, …` → Georgia.
  Only a 70×18 px header area, where live JavaScript adds a reading-list icon,
  differs from a live capture of the page.
- **Current simplebrowser baseline:**

  ![Current simplebrowser diagnostic baseline for the offline Moon fixture](screenshots/wikipedia-moon/baseline.png)

  [`baseline.png`](screenshots/wikipedia-moon/baseline.png)
  is today's output. It is a **non-blocking record, not a golden**: CI renders
  it as a `render` job artifact but never compares it. Refresh it with
  `make moon-baseline` when the renderer changes. `make baselines` refreshes
  this diagnostic together with the GitHub diagnostic and blocking HN golden.

Reproduce offline from a clean checkout:

```sh
go run ./cmd/simplebrowser -o moon.png testdata/wikipedia-moon/moon.html
```

`TestMoonFixtureIsOffline` checks that every stylesheet, image and CSS `url()`
resolves to a committed file and that the page has no scripts.

## What the view actually uses

Chrome computed styles for elements that intersect the 800×600 viewport:

| Feature | Observed above the fold | Current renderer |
|---|---|---|
| `@media` | Vector and TemplateStyles rules (the infobox float sits in `@media (min-width:640px)`; some print rules hide screen UI) | viewport media conditions and nested rules supported → [#250](https://github.com/lukehoban/simplebrowser/issues/250) |
| `@supports` | icon `mask-image` vs `background-image` fallback, `round()` image width | evaluated against rendered features → [#251](https://github.com/lukehoban/simplebrowser/issues/251); `mask-image`, `grid` and `round()` report unsupported, so icon fallbacks are selected. Their SVG `data:` backgrounds decode offline, including Vector's legacy `image/svg+xml;utf8` form with literal spaces → [#268](https://github.com/lukehoban/simplebrowser/issues/268); full-page icon fidelity still depends on pseudo-element layout and sizing work such as [#254](https://github.com/lukehoban/simplebrowser/issues/254) |
| `@supports` | icon `mask-image` vs `background-image` fallback, `round()` image width | evaluated against the features this engine actually renders → [#251](https://github.com/lukehoban/simplebrowser/issues/251); `mask-image`, `grid` and `round()` report unsupported, so icon fallbacks are selected. Their SVG `data:` backgrounds decode offline, including Vector's legacy `image/svg+xml;utf8` form with literal spaces → [#268](https://github.com/lukehoban/simplebrowser/issues/268); full-page icon fidelity still depends on surrounding pseudo-element layout and sizing work such as [#254](https://github.com/lukehoban/simplebrowser/issues/254) |
| Flexbox | header, logo, user links, title bar, tab toolbar, indicators, dropdown buttons (36 flex and 9 inline-flex boxes) | core single-line row/column, sizing, gaps and alignment implemented → [#247](https://github.com/lukehoban/simplebrowser/issues/247); wrapping → [#272](https://github.com/lukehoban/simplebrowser/issues/272) |
| Floats | infobox `right`, language button `right`, indicators `right`, logo `left` | left/right placement and line wrapping → [#252](https://github.com/lukehoban/simplebrowser/issues/252); float clearance → [#68](https://github.com/lukehoban/simplebrowser/issues/68) |
| Custom properties | 134 `var()` uses: link colors, font sizes, borders, image size | ordinary declarations now substitute inherited variables and fallbacks ([#246](https://github.com/lukehoban/simplebrowser/issues/246), shared with #242); `calc()` lengths still need #254 |
| `calc()` | image width, spacing, media conditions | [#254](https://github.com/lukehoban/simplebrowser/issues/254) |
| `overflow:hidden` / `clip` | hidden skip link, dropdown label text | descendant padding-box and absolute `clip:rect()` painting implemented → [#253](https://github.com/lukehoban/simplebrowser/issues/253); [before/after repro](screenshots/wikipedia-moon/jump-link-before-after.png) |
| Fonts | title in `"Linux Libertine", Georgia, …, serif` at 28.8px; body `sans-serif` 14–17.6px | serif → [#87](https://github.com/lukehoban/simplebrowser/issues/87); Arial/Helvetica metrics → [#120](https://github.com/lukehoban/simplebrowser/issues/120) |
| Images / `srcset` | five visible images (wordmark, tagline, two indicators, 280×266 Moon photo); each thumbnail has a `2x` `srcset` candidate | the 1× `src` is right at device scale 1; `srcset` is not needed for this view |
| Grid | only inside `@media (min-width:1120px)` and larger | not used at 800px, no issue opened |
| Not isolated yet | `border-radius` on buttons/links, `text-overflow:ellipsis`, `position:absolute` dropdown checkboxes, `mask-image` itself | revisit after #250/#251 land, when the page shows which of these still differ |

## Prioritized next behaviors

1. [#254](https://github.com/lukehoban/simplebrowser/issues/254) `calc()` for
   the image width and spacing.
2. [#68](https://github.com/lukehoban/simplebrowser/issues/68) float
   clearance and margin collapse, after [#252](https://github.com/lukehoban/simplebrowser/issues/252)
   float placement.
3. Fonts [#87](https://github.com/lukehoban/simplebrowser/issues/87)
   and [#120](https://github.com/lukehoban/simplebrowser/issues/120).

Each new issue has a small repro in
[`testdata/wikipedia-moon/repros/`](../testdata/wikipedia-moon/repros) and an
expected-versus-current visual in `docs/screenshots/wikipedia-moon/`.
The [float before/after comparison](screenshots/wikipedia-moon/float-before-after.png)
shows the right box and line wrapping in isolation. In the full Moon baseline
the infobox now floats right and the lead text is visible after ordinary
custom-property substitution and invalid-at-computed-value-time fallback
([#246](https://github.com/lukehoban/simplebrowser/issues/246),
[#266](https://github.com/lukehoban/simplebrowser/issues/266)).
Core flex layout keeps the logo/header controls and title toolbar on their
authored single lines at 800px. Responsive multi-line flex remains #272.

## Out of scope

JavaScript behavior (dropdowns, search, sticky header, reading lists,
preferences), live-site parity, other skins, dark mode, text-size preferences,
other viewports and device scales, and everything below the lead paragraphs.
The trimmed-away article sections are not part of this target.
Painting beyond #253's bounded clip behavior is tracked separately:
[split overflow axes (#258)](https://github.com/lukehoban/simplebrowser/issues/258),
[clip-path (#259)](https://github.com/lukehoban/simplebrowser/issues/259),
and [scrollable overflow (#261)](https://github.com/lukehoban/simplebrowser/issues/261).
