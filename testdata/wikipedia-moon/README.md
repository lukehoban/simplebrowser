# Wikipedia “Moon” fixture (offline, bounded)

A trimmed, script-free static copy of the logged-out, default-skin (Vector
2022, light theme, standard text size) English Wikipedia
[Moon](https://en.wikipedia.org/wiki/Moon) article. It covers only what is
above the fold at 800×600. Baseline, reference and gap inventory:
[docs/wikipedia-moon-baseline.md](../../docs/wikipedia-moon-baseline.md)
(#245, epic #243).

```sh
go run ./cmd/simplebrowser -o moon.png testdata/wikipedia-moon/moon.html
```

## Provenance

| | |
|---|---|
| Source | `https://en.wikipedia.org/wiki/Moon`, anonymous `GET`, 2026-09-27 (response `Date: Sun, 27 Sep 2026 08:49:54 GMT`, `Last-Modified: Sun, 27 Sep 2026 07:54:06 GMT`, served from the edge cache) |
| Revision | [oldid 1376988668](https://en.wikipedia.org/w/index.php?title=Moon&oldid=1376988668) |
| Skin / variant | `skin=vector-2022`, `skin-theme-clientpref-day`, `vector-feature-limited-width`, standard thumbnail size, `lang=en`, `dir=ltr` |
| Live size | HTML 2,256,854 B; stylesheets 219,082 B (`load.php` modules bundle) + 6,839 B (`site.styles`) |
| Fixture size | about 60 KB HTML, 46 KB CSS, 18 small assets (about 170 KB, most of it the 116 KB hidden globe SVG) |

## Sanitization and trimming ([`build_fixture.py`](build_fixture.py))

- **Removed:** all `<script>`/`<noscript>`, preload and meta links, comments,
  the sticky header, footer and category links. The only JavaScript effect
  kept is MediaWiki's pre-paint `client-nojs` → `client-js` class swap, so the
  first paint matches the default JS-enabled view without running scripts.
- **Trimmed:** the article to its first two lead paragraphs and the first
  three infobox rows. Sections 2+ are dropped. Hidden language, TOC and
  main-menu lists keep three items each.
- **Rewritten:** links to absolute `en.wikipedia.org` URLs (never fetched).
  Images and CSS `url()` assets point to local copies in `assets/`. Each
  `srcset` is kept only as `data-live-srcset`; at device scale 1 the browser
  picks the 1× `src`, so the pixels do not change.
- **Pruned CSS:** `styles/` keeps, in original order, only rules whose
  selectors match an element of the trimmed document (dynamic pseudo-classes
  and pseudo-elements ignored for matching). All `@media`/`@supports` variants
  are kept. Inline TemplateStyles `<style>` blocks are verbatim.
- **Checked:** in Chrome, the pruned fixture and a copy using the full live
  stylesheets render identically at 800×600, apart from icons (full-CSS copy
  loads remote icons cross-origin). The fixture differs from a live Chrome
  capture only in a 70×18 px header area, where live JavaScript adds a
  reading-list icon.

The script records what was done; tests and CI never run it. To regenerate,
save the live HTML, then run `python3 build_fixture.py moon.html OUT`
(needs network, beautifulsoup4, lxml, soupsieve, tinycss2).

## Attribution and licenses

This directory redistributes third-party material for rendering tests only:

- **Article text:** “Moon”, Wikipedia contributors,
  [revision 1376988668](https://en.wikipedia.org/w/index.php?title=Moon&oldid=1376988668),
  [CC BY-SA 4.0](https://creativecommons.org/licenses/by-sa/4.0/). Trimmed as
  described above.
- **Styles:** MediaWiki core, Vector skin and extension styles (GPL-2.0-or-later).
  English Wikipedia `MediaWiki:Common.css` / TemplateStyles are CC BY-SA 4.0.
  Codex icons (`assets/icon-*.svg`, `arrow-down-*.svg`) are MIT.
- **`assets/330px-FullMoon2010.jpg`:** [File:FullMoon2010.jpg](https://commons.wikimedia.org/wiki/File:FullMoon2010.jpg)
  by Gregory H. Revera, [CC BY-SA 3.0](https://creativecommons.org/licenses/by-sa/3.0/).
- **`assets/20px-Cscr-featured.svg.png`:** [File:Cscr-featured.svg](https://en.wikipedia.org/wiki/File:Cscr-featured.svg), LGPL.
  **`assets/20px-Semi-protection-shackle.svg.png`:** [File:Semi-protection-shackle.svg](https://en.wikipedia.org/wiki/File:Semi-protection-shackle.svg), CC0 / public domain.
  **`assets/badge-golden-star.png`:** WikimediaBadges extension (GPL-2.0-or-later).
- **`assets/enwiki-25.svg`, `wikipedia-wordmark-en-25.svg`, `wikipedia-tagline-en-25.svg`:**
  Wikipedia 25th-anniversary logo variants. These are Wikimedia Foundation
  trademarks, included unmodified as visual test data with no endorsement
  implied. The individual variant licenses were not checked; the maintainer
  confirmed they are okay to use in this test fixture in
  [#256](https://github.com/lukehoban/simplebrowser/issues/256). On Commons
  the base logo is CC BY-SA 3.0 and the wordmark is public domain (text logo).
