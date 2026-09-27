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
| `@media` | Vector and TemplateStyles rules (the infobox float sits in `@media (min-width:640px)`; some print rules hide screen UI) | viewport media conditions and nested rules supported → [#250](https://github.com/lukehoban/simplebrowser/issues/250), including feature values that are themselves `calc()` such as `(max-width:calc(1120px - 1px))` → [#285](https://github.com/lukehoban/simplebrowser/issues/285) |
| `@supports` | icon `mask-image` vs `background-image` fallback, `round()` image width | evaluated against the features this engine actually renders → [#251](https://github.com/lukehoban/simplebrowser/issues/251); `mask-image`, `grid` and `round()` report unsupported, so icon fallbacks are selected. Their SVG `data:` backgrounds decode offline, including Vector's legacy `image/svg+xml;utf8` form with literal spaces → [#268](https://github.com/lukehoban/simplebrowser/issues/268) |
| Flexbox | header, logo, user links, title bar, tab toolbar, indicators, dropdown buttons (36 flex and 9 inline-flex boxes) | row/column, sizing, gaps, alignment ([#247](https://github.com/lukehoban/simplebrowser/issues/247)) and wrapping ([#272](https://github.com/lukehoban/simplebrowser/issues/272)) implemented; scope in [flexbox support](flexbox.md) |
| Floats | infobox `right`, language button `right`, indicators `right`, logo `left` | left/right placement and line wrapping → [#252](https://github.com/lukehoban/simplebrowser/issues/252); float clearance → [#68](https://github.com/lukehoban/simplebrowser/issues/68) |
| Custom properties | 134 `var()` uses: link colors, font sizes, borders, image size | ordinary declarations substitute inherited variables and fallbacks ([#246](https://github.com/lukehoban/simplebrowser/issues/246), shared with #242), including values used by ordinary `calc()` lengths; nested `calc()` from a variable remains [#287](https://github.com/lukehoban/simplebrowser/issues/287) |
| Negative margins / `min-width` | header main-menu button pulled 12px into the page padding (`.vector-button-flush-left/right`) at its 44px icon-only minimum | negative horizontal margins and the used `min-width` of atomic inline boxes are implemented → [#285](https://github.com/lukehoban/simplebrowser/issues/285); block-box min/max-width remain [#309](https://github.com/lukehoban/simplebrowser/issues/309) and general `box-sizing` [#310](https://github.com/lukehoban/simplebrowser/issues/310); [header before/after](screenshots/wikipedia-moon/header-flush-button-before-after.png) |
| `calc()` | image width, spacing, media conditions | bounded arithmetic in ordinary length declarations is implemented → [#254](https://github.com/lukehoban/simplebrowser/issues/254); other math functions remain [#281](https://github.com/lukehoban/simplebrowser/issues/281) |
| `overflow:hidden` / `clip` | hidden skip link, dropdown label text | descendant padding-box and absolute `clip:rect()` painting implemented → [#253](https://github.com/lukehoban/simplebrowser/issues/253); [before/after repro](screenshots/wikipedia-moon/jump-link-before-after.png) |
| `visibility:hidden` | closed Vector dropdown menus (`.vector-dropdown-content`) | hidden/collapse boxes keep layout but skip backgrounds, borders, text and images; `visibility:visible` descendants still paint → [#294](https://github.com/lukehoban/simplebrowser/issues/294); [header before/after](screenshots/visibility/moon-header-before-after.png) |
| Generated content | dropdown chevrons and tab underlines use `::after{content:""}`; the language button's chevron sits next to "293 languages" | `::before`/`::after` boxes for `none`/`normal`/string `content` are implemented → [#312](https://github.com/lukehoban/simplebrowser/issues/312); see [generated content](generated-content.md). The chevron paints as a solid square until `mask-image` → [#308](https://github.com/lukehoban/simplebrowser/issues/308) lands; other `content` values remain [#327](https://github.com/lukehoban/simplebrowser/issues/327) |
| Fonts | title in `"Linux Libertine", Georgia, …, serif` at 28.8px; body `sans-serif` 14–17.6px | serif → [#87](https://github.com/lukehoban/simplebrowser/issues/87); Arial/Helvetica metrics → [#120](https://github.com/lukehoban/simplebrowser/issues/120) |
| Images / `srcset` | five visible images (wordmark, tagline, two indicators, 280×266 Moon photo); each thumbnail has a `2x` `srcset` candidate | the 1× `src` is right at device scale 1; `srcset` is not needed for this view |
| Grid | only inside `@media (min-width:1120px)` and larger | not used at 800px, no issue opened |
| Not isolated yet | `border-radius` on buttons/links, `text-overflow:ellipsis`, `position:absolute` dropdown checkboxes, `mask-image` itself | revisit after #250/#251 land, when the page shows which of these still differ |

## Prioritized next behaviors

1. [#68](https://github.com/lukehoban/simplebrowser/issues/68) float
   clearance and margin collapse, after [#252](https://github.com/lukehoban/simplebrowser/issues/252)
   float placement.
2. Fonts [#87](https://github.com/lukehoban/simplebrowser/issues/87)
   and [#120](https://github.com/lukehoban/simplebrowser/issues/120).
3. Remaining CSS math functions and nesting:
   [#281](https://github.com/lukehoban/simplebrowser/issues/281) and
   [#287](https://github.com/lukehoban/simplebrowser/issues/287).

Each new issue has a small repro in
[`testdata/wikipedia-moon/repros/`](../testdata/wikipedia-moon/repros) and an
expected-versus-current visual in `docs/screenshots/wikipedia-moon/`.
The [float before/after comparison](screenshots/wikipedia-moon/float-before-after.png)
shows the right box and line wrapping in isolation. In the full Moon baseline
the infobox now floats right and the lead text is visible after ordinary
custom-property substitution and invalid-at-computed-value-time fallback
([#246](https://github.com/lukehoban/simplebrowser/issues/246),
[#266](https://github.com/lukehoban/simplebrowser/issues/266)).
Flex layout keeps the logo/header controls and title toolbar on their
authored lines at 800px; flex containers now report their items' combined
intrinsic width, so the shrink-to-fit user links, tabs and indicators no
longer overlap ([before/after](screenshots/wikipedia-moon/flex-wrap-before-after.png),
[#272](https://github.com/lukehoban/simplebrowser/issues/272)).

## Out of scope

JavaScript behavior (dropdowns, search, sticky header, reading lists,
preferences), live-site parity, other skins, dark mode, text-size preferences,
other viewports and device scales, and everything below the lead paragraphs.
The trimmed-away article sections are not part of this target.
Painting beyond #253's bounded clip behavior is tracked separately:
[split overflow axes (#258)](https://github.com/lukehoban/simplebrowser/issues/258),
[clip-path (#259)](https://github.com/lukehoban/simplebrowser/issues/259),
and [scrollable overflow (#261)](https://github.com/lukehoban/simplebrowser/issues/261).
