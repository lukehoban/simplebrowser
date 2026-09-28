# Live GitHub control acceptance after Grid merge (2026-09-28)

## Result

The branch selector and adjacent controls in the public, logged-out
`https://github.com/microsoft/vscode` response now paint their supplied
contents on `main` at merge commit
[`eb3bc01`](https://github.com/lukehoban/simplebrowser/commit/eb3bc01df045b205ff3ddc1f8368fcf78ddc9b93).
This is a fresh 800×600 capture after [#386](https://github.com/lukehoban/simplebrowser/issues/386)
landed in [PR #389](https://github.com/lukehoban/simplebrowser/pull/389).

![Fresh live GitHub control capture](screenshots/live-github-377/simplebrowser-eb3bc01-2026-09-28.png)

The left control contains the readable `main` label and branch/dropdown
glyphs within its outline. The adjacent branch and tag controls also show
their glyphs rather than empty outlined boxes. This resolves the visible
behavior tracked by [#377](https://github.com/lukehoban/simplebrowser/issues/377)
without a GitHub-specific workaround.

The prior post-Cascade capture on `1b703413` showed `main` below the first
outline and empty adjacent boxes. The new capture instead places `main` in
the selector and paints the branch/tag glyphs in their neighboring controls.
The live server response still contains the same relevant content: one
`aria-label="main branch"` selector with a visible `main` span, plus
`Branches` and `Tags` links containing inline SVGs. The response was 383,828
bytes; it contained one main selector, two branch links, and two tag links.
No JavaScript was executed by the CLI.

This environment did not have a Chrome/Chromium executable available for a
same-moment clean-profile reference. The prior clean-profile reference remains
useful for broader first-viewport comparison, but is not needed to establish
this focused control result because the live markup and before/after renderer
capture show the corrected control contents.

## Reproduction

```sh
go run ./cmd/simplebrowser \
  -o docs/screenshots/live-github-377/simplebrowser-eb3bc01-2026-09-28.png \
  https://github.com/microsoft/vscode
```

The PNG is 800×600 RGB with SHA-256
`11ab13db364e75566808f7c2332ce062b0ac2903d91a2f6116f785934431a225`.

## Known gaps / follow-ups

- [#330](https://github.com/lukehoban/simplebrowser/issues/330) remains open
  for broader first-viewport acceptance and vertical flow.
- [#378](https://github.com/lukehoban/simplebrowser/issues/378) remains
  deferred pending the JavaScript-hydrated metadata scope decision.
- Remaining reusable Grid gaps are tracked by
  [#414](https://github.com/lukehoban/simplebrowser/issues/414) and
  [#391](https://github.com/lukehoban/simplebrowser/issues/391)–
  [#394](https://github.com/lukehoban/simplebrowser/issues/394).

<!-- repo-agent-task:simplebrowser-issue377-live-controls-acceptance-main-eb3bc01-v1 -->
