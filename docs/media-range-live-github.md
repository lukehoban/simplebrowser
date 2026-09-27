# Live GitHub media-range diagnostic (#334)

On 2026-09-27, rendered `https://github.com/microsoft/vscode` at the CLI's
fixed 800×600 viewport with `go run ./cmd/simplebrowser -o OUTPUT.png URL`,
once at baseline [`ffdd556`](https://github.com/lukehoban/simplebrowser/tree/ffdd556)
and once with the range-query change
[`0388ecd`](https://github.com/lukehoban/simplebrowser/tree/0388ecd). Repeating
both renders produced byte-identical PNGs to the captures below. The two PNGs
are bounded first-viewport observations of the changing public response, not
deterministic goldens or a browser-reference comparison.

| Before (`ffdd556`, no range comparisons) | After (`0388ecd`, range comparisons) |
| --- | --- |
| ![Before: expanded marketing menu covers most of viewport](https://raw.githubusercontent.com/lukehoban/simplebrowser/0388ecd86bf96da3fbe77bd607728bb13b311184/docs/screenshots/media-range/github-before.png) | ![After: closed menu exposes repo name and navigation](https://raw.githubusercontent.com/lukehoban/simplebrowser/0388ecd86bf96da3fbe77bd607728bb13b311184/docs/screenshots/media-range/github-after.png) |

The closed-menu media rule now applies: `microsoft / vscode` and repository
navigation are visible instead of the six expanded marketing menu entries.
The visible stale-session message is hidden markup tracked by #333, not fixed
by media-query evaluation. Once #333 lands, #330 tracks a fresh rerender and
inventory against a logged-out browser reference.
