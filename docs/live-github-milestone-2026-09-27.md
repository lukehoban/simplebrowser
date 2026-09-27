# Live GitHub first-viewport check (2026-09-27)

## Fresh recheck on `main` `3588c1f` (21:26 UTC)

Both captures below are public, logged-out 800×600 views of
`https://github.com/microsoft/vscode` on 2026-09-27. The CLI ran on
[`3588c1f572c18a56e5d4bb61607ff65f88d53b54`](https://github.com/lukehoban/simplebrowser/commit/3588c1f572c18a56e5d4bb61607ff65f88d53b54);
Chrome 154.0.8037.58 used a fresh temporary profile and normal JavaScript.
The Chrome capture is a practical comparison, not a JS-off pixel-parity oracle.

| CLI (no JavaScript) | Clean-profile Chrome (JavaScript on) |
| --- | --- |
| ![Live CLI capture on main 3588c1f](screenshots/live-github-330/simplebrowser-3588c1f-2026-09-27.png) | ![Live Chrome reference at the same 800 by 600 viewport](screenshots/live-github-330/chrome-3588c1f-2026-09-27.png) |

**Result: improved but acceptance not yet established.** The previous nearly
blank overlay regression [#352](https://github.com/lukehoban/simplebrowser/issues/352)
is gone. The CLI shows `microsoft / vscode` and Public at approximately y=94,
repo tabs at y=200–265, branch/Go to file/Code controls at y=298–310,
and about five to six partial directory rows beginning at y=374. Chrome shows
the same repository identity at approximately y=94, tabs around y=130–174,
controls near y=200, and seven rows beginning around y=246. The CLI's tabs
and file rows are still roughly 70–130px too low; the GitHub header controls
and directory listing are unstyled or differently laid out, and its visible
rows are clipped/truncated compared with Chrome. This is partial progress on
the existing live vertical-flow [#349](https://github.com/lukehoban/simplebrowser/issues/349),
not a fresh diagnosis. The known outer white inset
[#350](https://github.com/lukehoban/simplebrowser/issues/350) and persistent
appearance label [#351](https://github.com/lukehoban/simplebrowser/issues/351)
remain. No additional visual discrepancy merits a new issue from these images.
The page is no longer dominated by an overlay/menu, but the first viewport's
repository navigation and code/file content do not yet **broadly preserve**
Chrome's layout; keep #330 open. The deterministic offline fixture [#242](https://github.com/lukehoban/simplebrowser/issues/242)
is separate.

CLI capture: `go run ./cmd/simplebrowser -o
docs/screenshots/live-github-330/simplebrowser-3588c1f-2026-09-27.png
https://github.com/microsoft/vscode`, started 21:26:16 UTC, finished 21:26:29 UTC;
SHA-256 `5a350a0e9dd1903eed229bba5c2b892344287498db78e3cb66f202a13f7dc202`.
Chrome capture: `--headless=new --window-size=800,600
--force-device-scale-factor=1 --hide-scrollbars --disable-extensions
--no-first-run --no-default-browser-check --user-data-dir=<empty temporary profile>
--screenshot=<Chrome image above> https://github.com/microsoft/vscode`,
started 21:26:20 UTC, finished 21:26:23 UTC; SHA-256
`9dcab4de214c74d08858c15f915be662965fc3323031aa70d662c428f0b14db9`.
Both PNGs measure 800×600. A separate public HTML request around 21:26:48 UTC
returned HTTP 200, 383,917 bytes, with 41 stylesheet link elements (including
deferred theme links), 10 script `src` attributes and 3 image `src` attributes.
Those are document references, not proof that each resource was fetched by
the CLI; scripts were not executed by the CLI. No raw response was committed.

## Historical regression evidence (before opacity and float fixes)

These are public, logged-out 800×600 captures of `https://github.com/microsoft/vscode`.
The renderer fetches and renders the live page without running JavaScript. The
reference is Google Chrome 154.0.8037.58 in headless mode, using a fresh
temporary profile with no cookies or signed-in state. Chrome ran with its
normal JavaScript behavior, so this is a practical visual comparison, not a
controlled script-on/script-off pixel test.

| `simplebrowser` on current `main` `f955ee4` — **nearly blank (regression)** | `simplebrowser` on previous `main` `9976c151` | Logged-out Chrome reference |
| --- | --- | --- |
| ![Nearly blank simplebrowser capture on f955ee4](screenshots/live-github-330/simplebrowser-2026-09-27.png) | ![simplebrowser capture on 9976c151](screenshots/live-github-330/simplebrowser-9976c151-2026-09-27.png) | ![Chrome capture](screenshots/live-github-330/chrome-2026-09-27.png) |

## Result

**No, not on then-current `main`.** At `f955ee437098646457612cb2c0d43de15f13e002`
the renderer's capture is a failed, nearly blank render. It shows a dark
viewport with only **Sign in** and **Appearance settings** at the top right.
It shows no repository identity, tabs, branch/file controls or file rows, so it
is not evidence of vertical flow or layout. This is a regression tracked in
[#352](https://github.com/lukehoban/simplebrowser/issues/352). Rendering the
same live page in the same minute with previous `main` (`9976c151`) still shows
repository content. Bisecting the #332 merge points to `e7bfd2c`, the merge of
`main` into the #332 integration branch. The root cause has not been diagnosed.

The `9976c151` capture is the latest evidence that a JavaScript-free render can
identify the repository. It shows `microsoft / vscode`, a **Public** badge,
Notifications/Fork/Star buttons, the repository tabs (Code, Issues, Pull
requests, Actions, Projects, Wiki, Security and quality, Insights), `main`, Go
to file, Code, a commit count, and the first two file rows (`.agents/skills/lau…`
and `.co…`). In that capture the tabs sit around y=340–410 and the file list
starts around y=480. In the Chrome reference, the tabs are around y=130–174,
the branch controls around y=200, and seven file rows are visible. Remaining
differences in that older capture are tracked in
[#349](https://github.com/lukehoban/simplebrowser/issues/349) (vertical
displacement), [#350](https://github.com/lukehoban/simplebrowser/issues/350)
(8px white inset) and
[#351](https://github.com/lukehoban/simplebrowser/issues/351) (appearance
tooltip). These screenshots alone do not diagnose any CSS cause.

The fixed-viewport inventory of the *offline stand-in* is tracked separately in
[#298](https://github.com/lukehoban/simplebrowser/issues/298). It is not a
diagnosis of the live response.

GitHub is variable production content, and Chrome may hydrate content or load
resources differently. These screenshots are diagnostics, not a golden or a CI
parity assertion. Authentication, JavaScript interaction, whole-page and
responsive fidelity remain out of scope for this milestone.

## Capture and resource notes

Both renderer captures used the same command from a checkout of the commit
named in the table:

```sh
go run ./cmd/simplebrowser \
  -o /tmp/live-github.png https://github.com/microsoft/vscode
```

The `f955ee4` capture reproduced byte-for-byte at about 20:37, 20:50 and
20:53 UTC on 2026-09-27. The `9976c151` capture was taken at about 20:53 UTC.
The Chrome reference was captured around 20:37–20:38 UTC. It used headless
mode, an 800×600 window, device scale factor 1, hidden scrollbars, disabled
extensions and an empty temporary user-data directory. Chrome's JavaScript was
not disabled. All three PNGs are 800×600 RGB images. Their SHA-256 values are:

- `cd5641f4c199d683dfae4c6582d7d6de5939101344d211336be2a27ad5c2115d` (`simplebrowser`, `f955ee4`)
- `b6e8d012db74cd83b2c0ff111f2f07f18c180c8261096f1c7c83be609c4c1212` (`simplebrowser`, `9976c151`)
- `025e90f97edafefb8b58dadaa7b0e8010b98330bad7a9cf947c98e2680d11567` (Chrome)

During the ~20:37 UTC check, a separate in-memory read of the public HTML returned HTTP 200 and 383,917
bytes. It referenced 41 stylesheet links in total: 27 active `href` links (23
unique stylesheet URLs) and 14 deferred theme `data-href` links. It also
referenced 10 external script URLs and 3 image `src` URLs. These are document
reference counts, not a claim that every resource loaded successfully; the CLI
does not emit a per-request trace. The scripts are deliberately neither
executed nor fetched by the renderer. The response was not saved, and no
cookies, credentials or raw response content are included here.

<!-- repo-agent-task:simplebrowser-issue330-live-js-free-milestone-check-9976c151-v1 -->
<!-- repo-agent-task:6694d5f38f0a7e19cc80f31a0c9ecc82 -->
<!-- repo-agent-task:simplebrowser-pr348-current-screenshot-evidence-reconciliation-9adaef6-v1 -->
