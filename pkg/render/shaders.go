package render

import _ "embed"

//go:embed assets/shaders/cell.vert
var cellVertSrc string

//go:embed assets/shaders/cell_glyph.vert
var cellGlyphVertSrc string

//go:embed assets/shaders/cell_glyph.frag
var cellGlyphFragSrc string

//go:embed assets/shaders/cell_bg.frag
var cellBgFragSrc string

//go:embed assets/shaders/crt.vert
var crtVertSrc string

//go:embed assets/shaders/crt.frag
var crtFragSrc string

//go:embed assets/shaders/blur.frag
var blurFragSrc string

//go:embed assets/shaders/threshold.frag
var thresholdFragSrc string

//go:embed assets/shaders/image.vert
var imageVertSrc string

//go:embed assets/shaders/image.frag
var imageFragSrc string
