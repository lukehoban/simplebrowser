# CSS Grid subset

The renderer supports a deliberately narrow Grid formatting context for
three-area controls:

- `display: grid` with `grid-template-columns: min-content minmax(0,auto) min-content`
- one `grid-template-areas` row of three distinct names (no `.` null cells or
  repeated names) and matching `grid-area` items
- one `column-gap` (or `gap`) and `align-items: center`

This is not general CSS Grid. Other track definitions, multiple rows, spans,
implicit tracks, and Grid auto-placement remain unsupported. The implementation
therefore does not claim broad `(display: grid)` support through `@supports`,
and `@supports (grid-template-areas: ...)` uses the same validator as layout,
so it is false for any areas value that would fall back to normal flow.
The deterministic fixture is `testdata/grid-three-area-button.html`. It
deliberately isolates the Grid formatting context in a fixed-size control
surrogate: it does not include the production button's outer flex layout or
browser-native button styling. That broader control composition remains outside
this Grid subset. The fixture's outer control is 88×32px at (16, 16) in both
renders, so the comparison checks the supported Grid child ordering, placement,
and single 8px `column-gap` without implying that the original button's outer
layout is supported. The `column-gap` is the sole inter-item spacing; adding
8px right margins to the first two items would double each intended gap.

The fixture render is compared with Chrome at the same 800×600 viewport:

![Three-area Grid fixture rendered by simplebrowser](screenshots/grid-three-area-button.png)

![Three-area Grid fixture rendered by Chrome](screenshots/grid-three-area-button-chrome.png)
