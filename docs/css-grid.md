# CSS Grid subset

The renderer supports a deliberately narrow Grid formatting context for
three-area controls:

- `display: grid` with `grid-template-columns: min-content minmax(0,auto) min-content`
- one three-name `grid-template-areas` row and matching `grid-area` items
- one `column-gap` (or `gap`) and `align-items: center`

This is not general CSS Grid. Other track definitions, multiple rows, spans,
implicit tracks, and Grid auto-placement remain unsupported. The implementation
therefore does not claim broad `(display: grid)` support through `@supports`.
The deterministic fixture is `testdata/grid-three-area-button.html`.

Current renderer output:

![Three-area Grid fixture rendered by simplebrowser](screenshots/grid-three-area-button.png)
