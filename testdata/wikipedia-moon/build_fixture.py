#!/usr/bin/env python3
"""Regenerate the bounded Wikipedia Moon fixture from a saved live response.

Maintenance tool only (not run by tests or CI). Requires network access for
assets plus: beautifulsoup4, lxml, soupsieve, tinycss2.

  python3 build_fixture.py SAVED_MOON_HTML OUT_DIR

The live page is fetched separately (see README.md). This script strips
scripts and interactive/tracking elements, trims the article to its lead,
prunes stylesheet rules that match no element of the trimmed document, and
localizes every remaining image/stylesheet URL so the fixture renders offline.
"""
import hashlib, os, re, sys, urllib.parse, urllib.request
from bs4 import BeautifulSoup, Comment
import soupsieve as sv
import tinycss2

BASE = "https://en.wikipedia.org/wiki/Moon"
UA = "simplebrowser-fixture/1.0 (https://github.com/lukehoban/simplebrowser)"
LEAD_PARAGRAPHS = 2      # article lead paragraphs kept
INFOBOX_ROWS = 3         # infobox rows kept (image, heading, first field)
HIDDEN_LIST_ITEMS = 3    # items kept in hidden language/TOC/menu lists

src, out = sys.argv[1], sys.argv[2]
os.makedirs(os.path.join(out, "assets"), exist_ok=True)
soup = BeautifulSoup(open(src, encoding="utf-8").read(), "lxml")

def fetch(url):
    req = urllib.request.Request(url, headers={"User-Agent": UA})
    with urllib.request.urlopen(req) as r:
        return r.read()

assets = {}
def localize(url, hint):
    absu = urllib.parse.urljoin(BASE, url)
    if absu in assets:
        return assets[absu]
    path = urllib.parse.urlparse(absu).path
    ext = os.path.splitext(path)[1] or ".svg"
    if "load.php" in absu:  # Vector icon endpoint returns SVG
        q = urllib.parse.parse_qs(urllib.parse.urlparse(absu).query)
        name = "icon-" + q["image"][0] + ("-" + q["variant"][0] if "variant" in q else "") + ".svg"
    else:
        name = re.sub(r"[^A-Za-z0-9._-]+", "_", os.path.basename(urllib.parse.unquote(path)))
    name = name[:80]
    with open(os.path.join(out, "assets", name), "wb") as f:
        f.write(fetch(absu))
    assets[absu] = "assets/" + name
    return assets[absu]

# 1. Remove scripts, noscript tracking pixels, preload/meta links, comments.
for t in soup.find_all(["script", "noscript"]):
    t.decompose()
for t in soup.find_all("link"):
    rel = t.get("rel") or []
    if "stylesheet" not in rel and "mw-deduplicated-inline-style" not in rel:
        t.decompose()
for t in soup.find_all("meta"):
    if t.get("charset") is None and t.get("name") != "viewport":
        t.decompose()
for c in soup.find_all(string=lambda s: isinstance(s, Comment)):
    c.extract()

# Represent the default JavaScript-enabled first paint without running scripts:
# MediaWiki's inline startup script only swaps this class before first paint.
soup.html["class"] = ["client-js" if c == "client-nojs" else c for c in soup.html["class"]]

# 2. Keep only the above-the-fold regions.
for sel in [".vector-sticky-header-container", ".mw-footer-container", "#catlinks",
            "#p-dock-bottom", ".mw-aria-live-region"]:
    for t in soup.select(sel):
        t.decompose()
po = soup.select_one("#mw-content-text > .mw-parser-output")
sections = po.find_all("section", recursive=False)
for s in sections[1:]:
    s.decompose()
lead = sections[0]
ps = 0
for c in list(lead.children):
    if getattr(c, "name", None) == "p" and c.get_text(strip=True):
        ps += 1
        if ps > LEAD_PARAGRAPHS:
            c.decompose(); continue
    elif ps >= LEAD_PARAGRAPHS and getattr(c, "name", None) not in (None, "style", "link"):
        c.decompose()
ib = lead.select_one("table.infobox")
for r in ib.select("tr")[INFOBOX_ROWS:]:
    r.decompose()
for sel in ["#p-lang-btn .vector-dropdown-content ul", "#mw-panel-toc-list", ".vector-toc-contents",
            "#vector-main-menu ul"]:
    for ul in soup.select(sel):
        for li in ul.find_all("li", recursive=False)[HIDDEN_LIST_ITEMS:]:
            li.decompose()

# 3. Absolute links, local images; record srcset rather than fabricate it.
for a in soup.find_all("a", href=True):
    a["href"] = urllib.parse.urljoin(BASE, a["href"])
for f in soup.find_all("form"):
    f["action"] = urllib.parse.urljoin(BASE, f.get("action", ""))
for img in soup.find_all("img"):
    if img.has_attr("srcset"):
        img["data-live-srcset"] = img["srcset"]
        del img["srcset"]
    img["src"] = localize(img["src"].split("?")[0] if "thumb" in img["src"] else img["src"], "img")
    for k in ("resource", "loading"):
        img.attrs.pop(k, None)

# 4. Prune stylesheets against the trimmed document.
DYNAMIC = re.compile(r"::?(?:-webkit-|-moz-|-ms-)?[a-zA-Z-]+(?:\([^()]*\))?")
def matches(sel):
    sel = sel.strip()
    try:
        return bool(sv.select_one(sel, soup))
    except Exception:
        pass
    stripped = DYNAMIC.sub("", sel).strip()
    if not stripped or stripped.endswith((">", "+", "~")):
        stripped = (stripped.rstrip(">+~ ") or "*")
    try:
        return bool(sv.select_one(stripped, soup))
    except Exception:
        return True  # keep rules we cannot evaluate

def prune(rules):
    kept = []
    for r in rules:
        if r.type == "qualified-rule":
            sels = [s for s in tinycss2.serialize(r.prelude).split(",")]
            if any(matches(s) for s in sels):
                kept.append(r)
        elif r.type == "at-rule" and r.lower_at_keyword in ("media", "supports") and r.content:
            inner = prune(tinycss2.parse_rule_list(r.content, skip_whitespace=True, skip_comments=True))
            if inner:
                kept.append("@%s%s{%s}" % (r.at_keyword, tinycss2.serialize(r.prelude),
                                           "".join(x if isinstance(x, str) else tinycss2.serialize([x]) for x in inner)))
        elif r.type == "at-rule":
            kept.append(r)
    return kept

def localize_css(css):
    def rep(m):
        u = m.group(1).strip("'\"")
        if u.startswith("data:") or u.startswith("http://www.w3.org"):
            return m.group(0)
        return "url(%s)" % ("../" + localize(u, "css"))
    return re.sub(r"url\(([^)]*)\)", rep, css)

os.makedirs(os.path.join(out, "styles"), exist_ok=True)
for i, link in enumerate(soup.find_all("link", rel="stylesheet")):
    href = urllib.parse.urljoin(BASE, link["href"])
    css = fetch(href).decode("utf-8")
    rules = tinycss2.parse_stylesheet(css, skip_whitespace=True, skip_comments=True)
    text = "\n".join(x if isinstance(x, str) else tinycss2.serialize([x]) for x in prune(rules))
    name = "styles/%s.css" % ("site" if "site.styles" in href else "modules")
    with open(os.path.join(out, name), "w", encoding="utf-8") as f:
        f.write("/* Pruned from %s */\n" % href + localize_css(text) + "\n")
    link["href"] = name
# Inline TemplateStyles <style> blocks are small and kept verbatim.
for st in soup.find_all("style"):
    if st.string and "url(" in st.string:
        st.string = localize_css(st.string)

with open(os.path.join(out, "moon.html"), "w", encoding="utf-8") as f:
    f.write("<!DOCTYPE html>\n" + str(soup.html) + "\n")
for k, v in sorted(assets.items()):
    print(v, "<-", k)
