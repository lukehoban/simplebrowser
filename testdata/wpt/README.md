# Pinned WPT reftest subset

These are unmodified files from [web-platform-tests/wpt](https://github.com/web-platform-tests/wpt)
at commit [`647d3bdf133159739b57cfb7afa0be3f5d76b9db`](https://github.com/web-platform-tests/wpt/commit/647d3bdf133159739b57cfb7afa0be3f5d76b9db),
under the upstream [WPT 3-clause BSD license](https://github.com/web-platform-tests/wpt/blob/647d3bdf133159739b57cfb7afa0be3f5d76b9db/LICENSE.md).
Each path here corresponds to `css/CSS2/<path>` upstream. The 39 blocking
tests and 6 diagnostic coverage tests, their references, and required PNG
support assets are included. No local edits were made to these upstream fixtures.
The harness renders the vendored files directly without adapting their
contents. Local `.xht` files use the browser's focused XHTML mode, so XML
`<![CDATA[` / `]]>` wrappers around CSS are interpreted while the fixtures
remain byte-for-byte unchanged.

Selection covers color inheritance, cascade through tables, background
painting, normal block flow, block-in-inline, anonymous tables, and collapsed
borders. This is **not** a representative aggregate WPT pass rate; it is a
small regression benchmark with known unsupported behavior. Selection is in
`cmd/wptbench/main.go`; add a vendored test and its reference/assets there
and regenerate the report with `make compatibility`.

The second tranche was selected for coverage **before** looking at results:
four direction/inline-box cases (`box/ltr-basic.xht`, `rtl-basic.xht`,
`ltr-ib.xht`, `rtl-ib.xht`), four margin cases
(`margin-padding-clear/margin-001.xht` through `margin-004.xht`), four
absolute/relative position cases (`positioning/absolute-non-replaced-height-003.xht`,
`absolute-non-replaced-height-006.xht`, `position-relative-001.xht`,
`position-relative-003.xht`), two initial containing-block cases
(`abspos/abspos-containing-block-initial-004a.xht`, `007.xht`), six table
cases (`tables/border-collapse-offset-001.xht`, `002.xht`,
`border-collapse-empty-row.html`, `separated-border-model-007.xht`,
`caption-position-001.xht`, `fixed-table-layout-002a.xht`), and five
color cases (`colors/color-applies-to-002.xht` through `005.xht` and
`colors-007.xht`). All paths are relative to upstream `css/CSS2/`.
Candidates requiring scripts, remote dependencies, or WPT's server-root Ahem
font were excluded because these fixtures must run unchanged and offline.
Tests and references are byte-for-byte upstream files, including the required
`support/` images. Results here are diagnostic, not a representative score.

The coverage-matrix tranche adds margin collapsing, float clearance,
percentage and relative positioning, body background propagation, and line-box
height cases. These WPT assertions are diagnostic, except `floats-clear/clear-001.xht`,
which was promoted to the blocking set once clearance (#68) landed. Four repo-owned
references under `testdata/wpt-local/` cover known gaps that do not have a
compact suitable WPT; they are scored separately. Every report row states a
material behavior that its fixture does not cover.

`make compatibility` renders at 800×600, reads every `rel=match` and
`rel=mismatch` relation in document order, compares exact pixel colors against
each reference, and writes one report entry per relation to
`docs/compatibility.{json,md}`. `make compatibility-check` checks
committed reports and the README score without rewriting them. Update the
README score when refreshing the report. Each relation has an independent
status; mismatches are reported as failures, while missing references, invalid
paths, render/decode errors, or diagnostic write failures are runner errors.
Test/reference/diff PNGs are generated per failed relation under
`artifacts/wpt/<test>/reference-<number>-<relation>/` (repo-owned tests add a
`local/` path segment; all are ignored by Git and
uploaded by CI). Red pixels in diff PNGs are different pixels; transparent
pixels match. Network dialing is forbidden during the run, including CSS/image
loads.

The fixed viewport and embedded font produce repeatable output on a given
platform, but cross-platform font rasterization can differ. CI checks its
report on Linux; if platform output diverges, regenerate using the same
platform as CI rather than masking failures or rounding pixels.
