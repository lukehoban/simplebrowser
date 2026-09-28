# Inline SVG in HTML

![An inline red branch icon between the words “before” and “after”.](screenshots/inline-svg-html.png)

`simplebrowser` paints bounded SVG content embedded directly in HTML as an inline replaced element. The rendering path reuses the existing SVG decoder, including supported `viewBox` sizing and paths, and the inline element inherits the computed HTML `color` used by SVG `currentColor`. Inline SVGs used as flex items also retain their replaced dimensions and paint their decoded image content.

The reproducible inline fixture is [`testdata/svg/inline-html.html`](../testdata/svg/inline-html.html); the flex-item case is [`testdata/flex/inline-svg-item.html`](../testdata/flex/inline-svg-item.html), rendered at 800×600 below.

![Inline SVG flex item with its following sibling](screenshots/flex-inline-svg-item.png)

Inline SVG currently inherits HTML `color`; complete SVG, HTML-to-SVG CSS cascade integration, text, scripting, and external SVG resources are not implied by this support.
