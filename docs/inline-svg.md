# Inline SVG in HTML

![An inline red branch icon between the words “before” and “after”.](screenshots/inline-svg-html.png)

`simplebrowser` paints bounded SVG content embedded directly in HTML as an inline replaced element. The rendering path reuses the existing SVG decoder, including supported `viewBox` sizing and paths, and the inline element inherits the computed HTML `color` used by SVG `currentColor`.

The reproducible fixture is [`testdata/svg/inline-html.html`](../testdata/svg/inline-html.html). Inline SVG currently inherits HTML `color`; complete SVG, HTML-to-SVG CSS cascade integration, text, scripting, and external SVG resources are not implied by this support.
