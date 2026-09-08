package render

import (
	"fmt"

	"github.com/go-gl/gl/v3.3-core/gl"

	"github.com/moozd/tubeless/pkg/font"
	"github.com/moozd/tubeless/pkg/screen"
	"github.com/moozd/tubeless/pkg/theme"
)

// Renderer owns the two-pass pipeline: cell grid -> offscreen FBO -> CRT
// post-process -> default framebuffer.
type Renderer struct {
	cellPass  *CellPass
	imagePass *ImagePass
	crtPass   *CRTPass
	bloomPass *BloomPass
	fbo       *FBO
	atlas     *font.Atlas

	pendingImages []screen.PlacedImage
}

func New(atlas *font.Atlas, cols, rows int) (*Renderer, error) {
	cellPass, err := NewCellPass(atlas)
	if err != nil {
		return nil, fmt.Errorf("cell pass: %w", err)
	}
	imagePass, err := NewImagePass()
	if err != nil {
		return nil, fmt.Errorf("image pass: %w", err)
	}
	crtPass, err := NewCRTPass()
	if err != nil {
		return nil, fmt.Errorf("crt pass: %w", err)
	}
	bloomPass, err := NewBloomPass()
	if err != nil {
		return nil, fmt.Errorf("bloom pass: %w", err)
	}
	fbo := newSRGBFBO(cols*atlas.CellWidth, rows*atlas.CellHeight)
	return &Renderer{cellPass: cellPass, imagePass: imagePass, crtPass: crtPass, bloomPass: bloomPass, fbo: fbo, atlas: atlas}, nil
}

// PrepareFrame reads scr into GPU-upload-ready instance buffers. scr should
// be an immutable published snapshot (see cmd/tubeless's publish/load
// wiring) — this never mutates it and needs no locking.
func (r *Renderer) PrepareFrame(scr *screen.Screen, th theme.Theme, cellW, cellH float32, cursorOn bool) {
	r.cellPass.BuildInstances(scr, th, cellW, cellH, cursorOn)
	r.pendingImages = scr.Images
}

// RenderFrame draws whatever PrepareFrame last captured into the window of
// size outW x outH. cellW/cellH are the physical-pixel size of one cell
// (atlas cell size times the display's DPI scale) — fixed regardless of
// window size, so resizing reflows the grid (see main's resize handling)
// instead of stretching glyphs.
//
// TEMPORARY: the bloom/CRT post-process pass is bypassed while the base
// glyph rendering is being dialed in — draws straight to the (sRGB-
// capable, see window.go) default framebuffer instead of through r.fbo.
// r.fbo/bloomPass/crtPass are left wired up in New() to be reconnected
// once that's settled, rather than ripped out and rebuilt.
func (r *Renderer) RenderFrame(outW, outH int, cellW, cellH float32) {
	gl.BindFramebuffer(gl.FRAMEBUFFER, 0)
	gl.Viewport(0, 0, int32(outW), int32(outH))
	target := &FBO{W: outW, H: outH}
	r.cellPass.Draw(target, cellW, cellH)
	r.imagePass.Draw(target, r.pendingImages, cellW, cellH)
}
