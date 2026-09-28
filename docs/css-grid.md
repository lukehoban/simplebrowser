# CSS Grid subset

The renderer supports a deliberately narrow Grid formatting context for
three-area controls:

- `display: grid` with `grid-template-columns: min-content minmax(0,auto) min-content`
- one three-name `grid-template-areas` row and matching `grid-area` items
- one `column-gap` (or `gap`) and `align-items: center`

This is not general CSS Grid. Other track definitions, multiple rows, spans,
implicit tracks, and Grid auto-placement remain unsupported. The implementation
therefore does not claim broad `(display: grid)` support through `@supports`.
The deterministic fixture is `testdata/grid-three-area-button.html`. Its
`column-gap: 8px` is the sole inter-item spacing; adding an 8px right margin to
the first two items would double each intended gap.

The fixture render is compared with Chrome at the same 800×600 viewport:

![Three-area Grid fixture rendered by simplebrowser](screenshots/grid-three-area-button.png)

![Three-area Grid fixture rendered by Chrome](screenshots/grid-three-area-button-chrome.png)
