# Compatibility coverage matrix

**Blocking regression set: 38/38 pinned WPT reference assertions passing.** New coverage is diagnostic: WPT 5/7, repo-owned references 1/4.

Pinned WPT revision: [`647d3bdf133159739b57cfb7afa0be3f5d76b9db`](https://github.com/web-platform-tests/wpt/commit/647d3bdf133159739b57cfb7afa0be3f5d76b9db). Viewport: 800x600. Exact PNG pixels. The selected tests are a bounded coverage matrix, not a general conformance score. See [benchmark notes](../testdata/wpt/README.md) and [machine-readable results](compatibility.json).

![Stacked pass/fail graph by benchmark suite and area](compatibility.svg)

## Per-area results

| Suite | Area | Pass | Fail | Error | Total |
| --- | --- | ---: | ---: | ---: | ---: |
| WPT | Colors | 9 | 0 | 0 | 9 |
| WPT | Backgrounds | 3 | 0 | 0 | 3 |
| WPT | Normal flow | 5 | 0 | 0 | 5 |
| WPT | Tables | 8 | 0 | 0 | 8 |
| WPT | Box direction | 4 | 0 | 0 | 4 |
| WPT | Margins | 5 | 0 | 0 | 5 |
| WPT | Positioning | 6 | 2 | 0 | 8 |
| WPT | Floats and clear | 2 | 0 | 0 | 2 |
| WPT | Line boxes | 1 | 0 | 0 | 1 |
| Local | Backgrounds | 1 | 0 | 0 | 1 |
| Local | Floats and clear | 0 | 1 | 0 | 1 |
| Local | Tables | 0 | 2 | 0 | 2 |

## Assertions

| Suite | Area | Test | Reference | Status | Different pixels | Does not cover |
| --- | --- | --- | --- | --- | ---: | --- |
| WPT | Colors | `colors/color-175.xht` | `colors/color-175-ref.xht` (match) | **pass** | 0 | Color parsing beyond this declaration and inheritance case. |
| WPT | Colors | `colors/color-176.xht` | `reference/ref-this-text-should-be-green.xht` (match) | **pass** | 0 | Color parsing beyond this declaration and inheritance case. |
| WPT | Colors | `colors/color-177.xht` | `colors/color-175-ref.xht` (match) | **pass** | 0 | Color parsing beyond this declaration and inheritance case. |
| WPT | Colors | `colors/color-applies-to-001.xht` | `colors/color-applies-to-001-ref.xht` (match) | **pass** | 0 | Color application outside the element exercised here. |
| WPT | Backgrounds | `backgrounds/background-001.xht` | `backgrounds/background-001-ref.xht` (match) | **pass** | 0 | Multiple layers, sizing, positioning, and canvas propagation. |
| WPT | Backgrounds | `backgrounds/background-002.xht` | `backgrounds/background-001-ref.xht` (match) | **pass** | 0 | Multiple layers, sizing, positioning, and canvas propagation. |
| WPT | Normal flow | `normal-flow/block-formatting-contexts-001.xht` | `normal-flow/block-formatting-contexts-001-ref.xht` (match) | **pass** | 0 | Floats, clearance, fragmentation, and writing modes. |
| WPT | Normal flow | `normal-flow/block-formatting-contexts-003.xht` | `normal-flow/block-formatting-contexts-003-ref.xht` (match) | **pass** | 0 | Floats, clearance, fragmentation, and writing modes. |
| WPT | Normal flow | `normal-flow/block-formatting-contexts-005.xht` | `normal-flow/block-formatting-contexts-005-ref.xht` (match) | **pass** | 0 | Floats, clearance, fragmentation, and writing modes. |
| WPT | Normal flow | `normal-flow/block-formatting-context-height-001.xht` | `reference/ref-filled-black-96px-square.xht` (match) | **pass** | 0 | Floats, clearance, fragmentation, and writing modes. |
| WPT | Normal flow | `normal-flow/block-in-inline-align-001.html` | `normal-flow/block-in-inline-align-001-ref.html` (match) | **pass** | 0 | General block-in-inline splitting and bidi layout. |
| WPT | Tables | `tables/anonymous-table-box-width-001.xht` | `reference/ref-filled-green-100px-square.xht` (match) | **pass** | 0 | Collapsed-border conflict resolution and spanning cells. |
| WPT | Tables | `tables/border-collapse-005.html` | `tables/border-collapse-005-ref.html` (match) | **pass** | 0 | The full collapsed-border conflict precedence algorithm. |
| WPT | Box direction | `box/ltr-basic.xht` | `box/left-ltr-ref.xht` (match) | **pass** | 0 | Vertical writing modes and bidi reordering. |
| WPT | Box direction | `box/rtl-basic.xht` | `box/right-rtl-ref.xht` (match) | **pass** | 0 | Vertical writing modes and bidi reordering. |
| WPT | Box direction | `box/ltr-ib.xht` | `box/left-ltr-ref.xht` (match) | **pass** | 0 | Vertical writing modes and bidi reordering. |
| WPT | Box direction | `box/rtl-ib.xht` | `box/right-rtl-ref.xht` (match) | **pass** | 0 | Vertical writing modes and bidi reordering. |
| WPT | Margins | `margin-padding-clear/margin-001.xht` | `margin-padding-clear/margin-001-ref.xht` (match) | **pass** | 0 | Margin collapsing with floats, clearance, or negative margins. |
| WPT | Margins | `margin-padding-clear/margin-002.xht` | `margin-padding-clear/margin-002-ref.xht` (match) | **pass** | 0 | Margin collapsing with floats, clearance, or negative margins. |
| WPT | Margins | `margin-padding-clear/margin-003.xht` | `margin-padding-clear/margin-003-ref.xht` (match) | **pass** | 0 | Margin collapsing with floats, clearance, or negative margins. |
| WPT | Margins | `margin-padding-clear/margin-004.xht` | `margin-padding-clear/margin-004-ref.xht` (match) | **pass** | 0 | Margin collapsing with floats, clearance, or negative margins. |
| WPT | Positioning | `positioning/absolute-non-replaced-height-003.xht` | `positioning/absolute-non-replaced-height-003-ref.xht` (match) | **pass** | 0 | Replaced elements, fixed positioning, and stacking. |
| WPT | Positioning | `positioning/absolute-non-replaced-height-006.xht` | `positioning/absolute-non-replaced-height-006-ref.xht` (match) | **pass** | 0 | Replaced elements, fixed positioning, and stacking. |
| WPT | Positioning | `positioning/position-relative-001.xht` | `positioning/position-relative-001-ref.xht` (match) | **pass** | 0 | Relative offsets in writing modes other than horizontal LTR. |
| WPT | Positioning | `positioning/position-relative-003.xht` | `positioning/position-relative-003-ref.xht` (match) | **pass** | 0 | Relative offsets in writing modes other than horizontal LTR. |
| WPT | Positioning | `abspos/abspos-containing-block-initial-004a.xht` | `abspos/abspos-containing-block-initial-004-ref.xht` (match) | **pass** | 0 | Nested transformed or non-initial containing blocks. |
| WPT | Positioning | `abspos/abspos-containing-block-initial-007.xht` | `abspos/abspos-containing-block-initial-007-ref.xht` (match) | **pass** | 0 | Nested transformed or non-initial containing blocks. |
| WPT | Tables | `tables/border-collapse-offset-001.xht` | `tables/border-collapse-offset-001-ref.xht` (match) | **pass** | 0 | The full collapsed-border conflict precedence algorithm. |
| WPT | Tables | `tables/border-collapse-offset-002.xht` | `tables/border-collapse-offset-002-ref.xht` (match) | **pass** | 0 | The full collapsed-border conflict precedence algorithm. |
| WPT | Tables | `tables/border-collapse-empty-row.html` | `tables/border-collapse-empty-row-ref.html` (match) | **pass** | 0 | Spans and non-empty row-group border conflicts. |
| WPT | Tables | `tables/separated-border-model-007.xht` | `tables/separated-border-model-007-ref.xht` (match) | **pass** | 0 | Collapsed borders and spanning cells. |
| WPT | Tables | `tables/caption-position-001.xht` | `tables/caption-position-001-ref.xht` (match) | **pass** | 0 | Side captions, multiple captions, and writing modes. |
| WPT | Tables | `tables/fixed-table-layout-002a.xht` | `tables/fixed-table-layout-002a-ref.xht` (match) | **pass** | 0 | Automatic table layout and spanning cells. |
| WPT | Colors | `colors/color-applies-to-004.xht` | `colors/color-applies-to-001-ref.xht` (match) | **pass** | 0 | Color application outside the element exercised here. |
| WPT | Colors | `colors/color-applies-to-005.xht` | `colors/color-applies-to-005-ref.xht` (match) | **pass** | 0 | Color application outside the element exercised here. |
| WPT | Colors | `colors/colors-007.xht` | `colors/colors-007-ref.xht` (match) | **pass** | 0 | Modern color syntaxes, profiles, and interpolation. |
| WPT | Colors | `colors/color-applies-to-002.xht` | `colors/color-applies-to-001-ref.xht` (match) | **pass** | 0 | Color application outside the element exercised here. |
| WPT | Colors | `colors/color-applies-to-003.xht` | `colors/color-applies-to-001-ref.xht` (match) | **pass** | 0 | Color application outside the element exercised here. |
| WPT | Margins | `margin-padding-clear/margin-collapse-003.xht` | `margin-padding-clear/margin-collapse-003-ref.xht` (match) | **pass** | 0 | Floats, clearance, negative margins, and margin trimming. |
| WPT | Floats and clear | `floats-clear/clear-001.xht` | `floats-clear/clear-001-ref.xht` (match) | **pass** | 0 | Right floats, multiple floats, and margin-collapse interactions. |
| WPT | Floats and clear | `floats-clear/clear-002.xht` | `floats-clear/clear-002-ref.xht` (match) | **pass** | 0 | Right floats, nested formatting contexts, and negative clearance. |
| WPT | Positioning | `positioning/bottom-offset-percentage-001.xht` ([#216](https://github.com/lukehoban/simplebrowser/issues/216)) | `positioning/bottom-offset-percentage-001-ref.xht` (match) | **fail** | 5000 | Auto offsets, replaced elements, and indefinite containing-block heights. |
| WPT | Positioning | `positioning/position-relative-004.xht` ([#76](https://github.com/lukehoban/simplebrowser/issues/76)) | `positioning/position-relative-004-ref.xht` (match) | **fail** | 36864 | Writing modes, bidi reordering, and positioned descendants. |
| WPT | Backgrounds | `backgrounds/background-body-001.xht` | `backgrounds/background-body-001-ref.xht` (match) | **pass** | 0 | Background images, repeat, position, size, and multiple layers. |
| WPT | Line boxes | `linebox/line-box-height-002.xht` | `linebox/line-box-height-002-ref.xht` (match) | **pass** | 0 | Mixed fonts, vertical-align variants, bidi, and vertical writing modes. |
| Local | Backgrounds | `canvas-background-image.html` ([#63](https://github.com/lukehoban/simplebrowser/issues/63)) | `canvas-background-image-ref.html` (match) | **pass** | 0 | Positioning, sizing, non-solid tiles, multiple layers, and root-image propagation. |
| Local | Floats and clear | `float-clearance-margin-collapse.html` ([#68](https://github.com/lukehoban/simplebrowser/issues/68)) | `float-clearance-margin-collapse-ref.html` (match) | **fail** | 37888 | Right floats, multiple floats, inline wrapping, and negative margins. |
| Local | Tables | `collapsed-border-conflict.html` ([#66](https://github.com/lukehoban/simplebrowser/issues/66)) | `collapsed-border-conflict-ref.html` (match) | **fail** | 240 | Row/table borders, style precedence, spans, and multi-row conflicts. |
| Local | Tables | `inline-table-line-edge.html` ([#209](https://github.com/lukehoban/simplebrowser/issues/209)) | `inline-table-line-edge-ref.html` (match) | **fail** | 244 | Multiple cells, spans, captions, bidi, and vertical alignment variants. |

The first 38 WPT assertions are blocking regressions. New WPT and local assertions are diagnostic: mismatches remain visible without making CI fail. On failures, run `make compatibility` and inspect `artifacts/wpt/<suite>/<test>/` (test, reference, red pixel diff).
