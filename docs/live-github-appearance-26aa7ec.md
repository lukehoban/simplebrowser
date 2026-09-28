# Live GitHub appearance control after inline-flex SVG fix

Built simplebrowser from main [`26aa7ec8`](https://github.com/lukehoban/simplebrowser/commit/26aa7ec8bee3db93f500eccb467649f0acef85cd), the merge of PR #412. Exact-merge [CI run 36369327987](https://github.com/lukehoban/simplebrowser/actions/runs/36369327987) passed.

On 2026-09-27, rendered the public, logged-out `https://github.com/microsoft/vscode` response with:

```sh
go run ./cmd/simplebrowser -o live.png https://github.com/microsoft/vscode
```

The fresh capture is 800×600. In the top-right control beside **Sign in**, the slider glyph is visible and the `Appearance settings` tooltip text is not painted. This satisfies the live appearance-control check for #351.

![Fresh simplebrowser live screenshot at 800×600](https://raw.githubusercontent.com/lukehoban/simplebrowser/d61afe16d69d4e5ddd88d8cb67db400d8c60ab36/docs/screenshots/live-github-330/simplebrowser-26aa7ec-appearance-2026-09-27.png)

The nearby #377 selector/control area still has `main` below the first outlined control rather than inside it. Unlike the earlier empty-box report, small branch/tag glyphs are now visible in the controls. The selector layout remains a gap; this is an observation only. CSS Grid follow-up #386 is still open in draft PR #389.

<!-- repo-agent-task:278f2438485a9cc8153fd60454b530ff -->
