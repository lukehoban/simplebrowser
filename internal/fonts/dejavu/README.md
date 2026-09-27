# DejaVu Sans (bundled font)

Unmodified `DejaVuSans*.ttf` files from the
[DejaVu fonts 2.37 release](https://github.com/dejavu-fonts/dejavu-fonts/releases/tag/version_2_37)
(`dejavu-fonts-ttf-2.37.tar.bz2`, SHA-256
`fa9ca4d13871dd122f61258a80d01751d603b4d3ee14095d65453b4e846e17d7`).

simplebrowser maps the CSS families `Verdana`, `Geneva`, and `DejaVu Sans`
to these faces (see `mappedFontFamily` in `internal/browser/layout.go`).
Other families keep their existing Go font mapping.

The fonts are distributed under the terms in [`LICENSE`](LICENSE): Bitstream
Vera Fonts copyright (c) 2003 Bitstream, Inc., Arev Fonts copyright (c) 2006
Tavmjong Bah; DejaVu changes are in the public domain. Contributors are listed
in [`AUTHORS`](AUTHORS). If these files are ever modified (e.g. subset), the
license requires that the result not be named with "Bitstream" or "Vera".
