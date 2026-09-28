# Inline SVG in HTML

![An inline red branch icon between the words “before” and “after”.](screenshots/inline-svg-html.png)

`simplebrowser` paints bounded SVG content embedded directly in HTML as an inline replaced element. The rendering path reuses the existing SVG decoder, including supported `viewBox` sizing and paths. Host-document CSS declarations matching inline SVG descendants participate in the cascade for the presentation and geometry properties supported by that decoder; SVG presentation attributes, embedded rules, and inline styles retain their cascade roles. The inline element also inherits the computed HTML `color` used by SVG `currentColor`. Inline SVGs used as flex items also retain their replaced dimensions and paint their decoded image content.

![A blue inline SVG path between “before” and “after”.](screenshots/inline-svg-host-css.png)

The fixtures are [`testdata/svg/inline-html.html`](../testdata/svg/inline-html.html) and [`testdata/svg/host-css.html`](../testdata/svg/host-css.html); the flex-item case is [`testdata/flex/inline-svg-item.html`](../testdata/flex/inline-svg-item.html), rendered at 800×600 below.

![Inline SVG flex item with its following sibling](screenshots/flex-inline-svg-item.png)

This is bounded CSS/DOM integration rather than complete SVG support; text, scripting, and external SVG resources are not implied.
