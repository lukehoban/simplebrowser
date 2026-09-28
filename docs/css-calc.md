# CSS `calc()`, `min()`, `max()` and `clamp()` length support

The renderer evaluates `calc()` values in the length properties its
layout engine uses: `width`, `height`, margins, padding, `flex-basis`, `gap`,
`row-gap`, `column-gap`, border widths, and `top`/`right`/`bottom`/`left` on
positioned boxes. Vertical offset percentages share the existing
width-based basis bug tracked in
[#216](https://github.com/lukehoban/simplebrowser/issues/216). The supported arithmetic is
`+`, `-`, `*`, and `/` over `px`, `em`, `rem`, `ex`, `ch`, `%`, `vw`, `vh`,
and unitless numbers, plus the comparison functions `min()`, `max()` (one or
more comma-separated arguments) and `clamp(MIN, VAL, MAX)` (exactly three;
`MIN` wins if it exceeds `MAX`). These functions and `calc()` can be nested in
each other and used as a whole declaration value, for example
`width: min(250px, 300px)` or `width: calc(min(50%, 300px) - 10px)`. Addition and subtraction require CSS whitespace around
the operator. Font- and viewport-relative units are resolved when styles are
computed. Percentage terms are kept until layout and resolved against the
property's normal basis; a comparison that mixes percentages with other
lengths, such as `min(50%, 300px)`, is kept as `calc(min(50%, 300px))` and
evaluated at layout time. For example, width uses the containing block's
content width, and `flex-basis` uses the flex container's inner main size.
Border widths do not accept percentages, so a `calc()` there that includes
`%` is invalid.

Custom properties are substituted before validation and evaluation, preserving
the cascade. An invalid expression behaves like any other invalid declaration:
it does not replace a lower-priority declaration, and an invalid `var()`
substitution is treated as unset. `@supports` recognizes the same bounded
length grammar. The `margin`, `padding`, and `border-width` shorthands accept
one to four components, including a `calc()` expression in any component;
whitespace inside each expression is kept intact.

This is intentionally not a full CSS math implementation. These are not
supported yet:

- `round()`, `abs()`, `sign()` and other CSS math functions, and the `none`
  argument of `clamp()`
- `calc()` inside the `flex` shorthand
  ([#288](https://github.com/lukehoban/simplebrowser/issues/288)); use
  `flex-basis` instead
- `calc()` mixed with other values in the `gap` shorthand, such as
  `gap: calc(10% - 1px) 0`
  ([#289](https://github.com/lukehoban/simplebrowser/issues/289))
- arithmetic in background, transform, grid, or SVG properties

`@supports` accepts only the forms listed above as supported.

Reproduce the `min()`/`max()`/`clamp()` cases
([#281](https://github.com/lukehoban/simplebrowser/issues/281)):

```sh
go run ./cmd/simplebrowser -o /tmp/min-max-clamp.png testdata/css-math/min-max-clamp.html
```

Inside the 400px gray frame the bars are 250px (`min(250px, 300px)`), 200px
(`min(50%, 300px)`), 150px (`max(10%, 150px)`), 200px
(`clamp(100px, 50%, 300px)`), 300px (`clamp(10px, 90%, 300px)`), 190px
(`calc(min(50%, 300px) - 10px)`) and 220px (`min(var(--w), 60%)` with
`--w: 220px`):

![Rendered min()/max()/clamp() widths](screenshots/css-math/min-max-clamp.png)

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
