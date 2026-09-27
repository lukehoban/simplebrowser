// Package dejavu embeds the unmodified DejaVu Sans 2.37 faces.
//
// DejaVu Sans derives from Bitstream Vera, whose wide, open letterforms were
// designed with metrics close to Verdana. simplebrowser uses it as the
// deterministic stand-in for the proprietary Verdana/Geneva stack that Hacker
// News requests. See LICENSE (Bitstream Vera / Arev terms; DejaVu changes are
// public domain) and AUTHORS in this directory, and README.md for provenance.
package dejavu

import _ "embed"

// Regular is DejaVuSans.ttf.
//
//go:embed DejaVuSans.ttf
var Regular []byte

// Bold is DejaVuSans-Bold.ttf.
//
//go:embed DejaVuSans-Bold.ttf
var Bold []byte

// Oblique is DejaVuSans-Oblique.ttf.
//
//go:embed DejaVuSans-Oblique.ttf
var Oblique []byte

// BoldOblique is DejaVuSans-BoldOblique.ttf.
//
//go:embed DejaVuSans-BoldOblique.ttf
var BoldOblique []byte
