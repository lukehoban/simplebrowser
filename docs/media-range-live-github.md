# Live GitHub media-range diagnostic (#334)

On 2026-09-27, rendered `https://github.com/microsoft/vscode` at the CLI's
fixed 800×600 viewport with `go run ./cmd/simplebrowser -o OUTPUT.png URL`.
The two PNGs are bounded first-viewport observations of the changing public
response, not deterministic goldens or a browser-reference comparison.

| Before (`ffdd556`, no range comparisons) | After (range comparisons) |
| --- | --- |
| ![Before: expanded marketing menu covers most of viewport](screenshots/media-range/github-before.png) | ![After: closed menu exposes repo name and navigation](screenshots/media-range/github-after.png) |

The closed-menu media rule now applies: `microsoft / vscode` and repository
navigation are visible instead of the six expanded marketing menu entries.
The visible stale-session message is hidden markup tracked by #333, not fixed
by media-query evaluation. Once #333 lands, #330 tracks a fresh rerender and
inventory against a logged-out browser reference.
