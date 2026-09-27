# CSS `calc()` length support

The renderer evaluates a single `calc()` value in the length properties its
layout engine uses: `width`, `height`, margins, padding, `flex-basis`, `gap`,
`row-gap`, `column-gap`, border widths, and `top`/`right`/`bottom`/`left` on
positioned boxes. Vertical offset percentages share the existing
width-based basis bug tracked in
[#216](https://github.com/lukehoban/simplebrowser/issues/216). The supported arithmetic is
`+`, `-`, `*`, and `/` over `px`, `em`, `rem`, `ex`, `ch`, `%`, `vw`, `vh`,
and unitless numbers. Addition and subtraction require CSS whitespace around
the operator. Font- and viewport-relative units are resolved when styles are
computed. Percentage terms are kept until layout and resolved against the
property's normal basis. For example, width uses the containing block's
content width, and `flex-basis` uses the flex container's inner main size.
Border widths do not accept percentages, so a `calc()` there that includes
`%` is invalid.

Custom properties are substituted before validation and evaluation, preserving
the cascade. An invalid expression behaves like any other invalid declaration:
it does not replace a lower-priority declaration, and an invalid `var()`
substitution is treated as unset. `@supports` recognizes the same bounded
length grammar.

This is intentionally not a full CSS math implementation. These are not
supported yet:

- `min()`, `max()`, `clamp()`
  ([#281](https://github.com/lukehoban/simplebrowser/issues/281)) and `round()`
- nested `calc()` and custom properties whose value is a `calc()`
  ([#287](https://github.com/lukehoban/simplebrowser/issues/287))
- `calc()` inside the `flex` shorthand
  ([#288](https://github.com/lukehoban/simplebrowser/issues/288)); use
  `flex-basis` instead
- `calc()` mixed with other values in a multi-value shorthand such as
  `margin: calc(10% - 1px) 0`, which is dropped as invalid
  ([#289](https://github.com/lukehoban/simplebrowser/issues/289))
- arithmetic in background, transform, grid, or SVG properties

`@supports` accepts only the forms listed above as supported.

Reproduce the tracked ordinary-width case:

```sh
go run ./cmd/simplebrowser -o /tmp/calc-width.png testdata/wikipedia-moon/repros/calc-width.html
```

The green and blue bars are 275px and 380px wide respectively:

![Rendered ordinary calc() width repro](screenshots/wikipedia-moon/calc-width-after.png)

Reproduce the flex-basis case:

```sh
go run ./cmd/simplebrowser -o /tmp/calc-flex-basis.png testdata/wikipedia-moon/repros/calc-flex-basis.html
```

In the 400px rows, the green item is 190px (`calc(50% - 10px)`), the blue
item is 200px (`50%`), and the orange item is 120px (`calc(100px + 20px)`):

![Rendered flex-basis calc() repro](screenshots/wikipedia-moon/calc-flex-basis-after.png)
