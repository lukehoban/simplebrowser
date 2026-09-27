# WPT compatibility: 12/13 passing

Pinned WPT revision: [`647d3bdf133159739b57cfb7afa0be3f5d76b9db`](https://github.com/web-platform-tests/wpt/commit/647d3bdf133159739b57cfb7afa0be3f5d76b9db). Viewport: 800x600. Exact PNG pixels; 1 compatibility failures, 0 runner errors. See [benchmark notes](../testdata/wpt/README.md) and [machine-readable results](compatibility.json).

| Test | Reference | Relation | Status | Different pixels |
| --- | --- | --- | --- | ---: |
| `colors/color-175.xht` | `colors/color-175-ref.xht` | match | **pass** | 0 |
| `colors/color-176.xht` | `reference/ref-this-text-should-be-green.xht` | match | **pass** | 0 |
| `colors/color-177.xht` | `colors/color-175-ref.xht` | match | **pass** | 0 |
| `colors/color-applies-to-001.xht` | `colors/color-applies-to-001-ref.xht` | match | **fail** | 380 |
| `backgrounds/background-001.xht` | `backgrounds/background-001-ref.xht` | match | **pass** | 0 |
| `backgrounds/background-002.xht` | `backgrounds/background-001-ref.xht` | match | **pass** | 0 |
| `normal-flow/block-formatting-contexts-001.xht` | `normal-flow/block-formatting-contexts-001-ref.xht` | match | **pass** | 0 |
| `normal-flow/block-formatting-contexts-003.xht` | `normal-flow/block-formatting-contexts-003-ref.xht` | match | **pass** | 0 |
| `normal-flow/block-formatting-contexts-005.xht` | `normal-flow/block-formatting-contexts-005-ref.xht` | match | **pass** | 0 |
| `normal-flow/block-formatting-context-height-001.xht` | `reference/ref-filled-black-96px-square.xht` | match | **pass** | 0 |
| `normal-flow/block-in-inline-align-001.html` | `normal-flow/block-in-inline-align-001-ref.html` | match | **pass** | 0 |
| `tables/anonymous-table-box-width-001.xht` | `reference/ref-filled-green-100px-square.xht` | match | **pass** | 0 |
| `tables/border-collapse-005.html` | `tables/border-collapse-005-ref.html` | match | **pass** | 0 |

On failures, run `make compatibility` and inspect `artifacts/wpt/<test>/` (test, reference, red pixel diff). A failure is a pixel mismatch, not a test process failure.
