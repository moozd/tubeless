package render

import _ "embed"

//go:embed assets/shaders/fullscreen.vert
var fullscreenVertSrc string

//go:embed assets/shaders/cell_glyph.vert
var cellGlyphVertSrc string

//go:embed assets/shaders/cell_glyph.frag
var cellGlyphFragSrc string

//go:embed assets/shaders/cell_rect.vert
var cellRectVertSrc string

//go:embed assets/shaders/cell_rect.frag
var cellRectFragSrc string

//go:embed assets/shaders/cell_underline.vert
var cellUnderlineVertSrc string

//go:embed assets/shaders/cell_underline.frag
var cellUnderlineFragSrc string

//go:embed assets/shaders/image.vert
var imageVertSrc string

//go:embed assets/shaders/image.frag
var imageFragSrc string

//go:embed assets/shaders/shapeblur.frag
var shapeBlurFragSrc string

//go:embed assets/shaders/copy.frag
var copyFragSrc string

//go:embed assets/shaders/inset.frag
var insetFragSrc string

//go:embed assets/shaders/cursor.frag
var cursorFragSrc string
