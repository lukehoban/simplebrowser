# Pinned WPT reftest subset

These are unmodified files from [web-platform-tests/wpt](https://github.com/web-platform-tests/wpt)
at commit [`647d3bdf133159739b57cfb7afa0be3f5d76b9db`](https://github.com/web-platform-tests/wpt/commit/647d3bdf133159739b57cfb7afa0be3f5d76b9db),
under the upstream [WPT 3-clause BSD license](https://github.com/web-platform-tests/wpt/blob/647d3bdf133159739b57cfb7afa0be3f5d76b9db/LICENSE.md).
Each path here corresponds to `css/CSS2/<path>` upstream. Only the 13
manifest-listed tests, their references, and three PNG support assets are
included. No local edits were made to these upstream fixtures.
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

`make compatibility` renders at 800×600, reads every `rel=match` and
`rel=mismatch` relation in document order, compares exact pixel colors against
each reference, and writes one report entry per relation to
`docs/compatibility.{json,md}`. `make compatibility-check` checks
committed reports and the README score without rewriting them. Update the
README score when refreshing the report. Each relation has an independent
status; mismatches are reported as failures, while missing references, invalid
paths, render/decode errors, or diagnostic write failures are runner errors.
Test/reference/diff PNGs are generated per failed relation under
`artifacts/wpt/<test>/reference-<number>-<relation>/` (ignored by Git and
uploaded by CI). Red pixels in diff PNGs are different pixels; transparent
pixels match. Network dialing is forbidden during the run, including CSS/image
loads.

The fixed viewport and embedded font produce repeatable output on a given
platform, but cross-platform font rasterization can differ. CI checks its
report on Linux; if platform output diverges, regenerate using the same
platform as CI rather than masking failures or rounding pixels.
