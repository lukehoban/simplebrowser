# CSS `calc()` length support

The renderer evaluates `calc()` in ordinary length declarations used by its
layout engine, including width/height, offsets, margins, padding, and flex
bases/gaps. The supported arithmetic is `+`, `-`, `*`, and `/` over `px`,
`em`, `rem`, `%`, `vw`, `vh`, and unitless numbers. Addition and subtraction
require CSS whitespace around the operator. Percentages remain tied to the
property's normal used-value basis; for example, width uses the containing
block's content width.

Custom properties are substituted before validation and evaluation, preserving
the cascade. An invalid expression behaves like any other invalid declaration:
it does not replace a lower-priority declaration, and an invalid `var()`
substitution is treated as unset. `@supports` recognizes the same bounded
length grammar.

This is intentionally not a full CSS math implementation. `min()`, `max()`,
`clamp()`, `round()`, and arithmetic in
background, transform, grid, or SVG properties are not supported. The
follow-up for `min()`/`max()`/`clamp()` is tracked in
[#281](https://github.com/lukehoban/simplebrowser/issues/281).

Reproduce the tracked ordinary-width case:

```sh
go run ./cmd/simplebrowser -o /tmp/calc-width.png testdata/wikipedia-moon/repros/calc-width.html
```

The green and blue bars are 275px and 380px wide respectively:

![Rendered ordinary calc() width repro](screenshots/wikipedia-moon/calc-width-after.png)
