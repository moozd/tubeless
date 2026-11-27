package render

import (
	"fmt"

	"github.com/go-gl/gl/v3.3-core/gl"
)

// DMABuf is a frame in GPU memory that another process (CEF's GPU
// process) shares as Linux dmabuf planes. The file descriptors are
// valid only until the code that received them returns.
type DMABuf struct {
	W, H     int
	FourCC   uint32 // DRM format, such as DRMFormatARGB8888
	Modifier uint64
	FDs      []int
	Strides  []uint32
	Offsets  []uint64
}

// DRM formats of 8-bit RGBA frames, by their byte order in memory.
const (
	DRMFormatARGB8888 uint32 = 0x34325241 // B, G, R, A
	DRMFormatABGR8888 uint32 = 0x34324241 // R, G, B, A
)

// CopyDMABuf copies a GPU frame into the texture with no CPU copy: it
// imports the buffer as an EGL image and blits it. The blit runs with
// sRGB encoding off, so the bytes arrive as they are and the
// SRGB8_ALPHA8 texture decodes them when sampled, as with UpdateRect.
// Must run on the GL thread, while d's descriptors are valid.
func (t *AppletTexture) CopyDMABuf(d DMABuf) error {
	t.Resize(d.W, d.H)
	t.ensureGPUCopy()
	gl.BindTexture(gl.TEXTURE_2D, t.srcTex)
	err := bindDMABuf(d)
	gl.BindTexture(gl.TEXTURE_2D, 0)
	if err != nil {
		return err
	}
	return t.blitFromSource()
}

func (t *AppletTexture) ensureGPUCopy() {
	if t.srcTex != 0 {
		return
	}
	gl.GenTextures(1, &t.srcTex)
	gl.BindTexture(gl.TEXTURE_2D, t.srcTex)
	gl.TexParameteri(gl.TEXTURE_2D, gl.TEXTURE_MIN_FILTER, gl.NEAREST)
	gl.TexParameteri(gl.TEXTURE_2D, gl.TEXTURE_MAG_FILTER, gl.NEAREST)
	gl.BindTexture(gl.TEXTURE_2D, 0)
	gl.GenFramebuffers(1, &t.readFBO)
	gl.GenFramebuffers(1, &t.drawFBO)
}

// blitFromSource copies srcTex into tex and restores the framebuffer
// state the caller had.
func (t *AppletTexture) blitFromSource() error {
	var readWas, drawWas int32
	gl.GetIntegerv(gl.READ_FRAMEBUFFER_BINDING, &readWas)
	gl.GetIntegerv(gl.DRAW_FRAMEBUFFER_BINDING, &drawWas)
	isSRGB := gl.IsEnabled(gl.FRAMEBUFFER_SRGB)
	defer restoreFramebuffers(uint32(readWas), uint32(drawWas), isSRGB)

	gl.BindFramebuffer(gl.READ_FRAMEBUFFER, t.readFBO)
	gl.FramebufferTexture2D(gl.READ_FRAMEBUFFER, gl.COLOR_ATTACHMENT0, gl.TEXTURE_2D, t.srcTex, 0)
	gl.BindFramebuffer(gl.DRAW_FRAMEBUFFER, t.drawFBO)
	gl.FramebufferTexture2D(gl.DRAW_FRAMEBUFFER, gl.COLOR_ATTACHMENT0, gl.TEXTURE_2D, t.tex, 0)
	if status := gl.CheckFramebufferStatus(gl.READ_FRAMEBUFFER); status != gl.FRAMEBUFFER_COMPLETE {
		return fmt.Errorf("dmabuf framebuffer incomplete: %#x", status)
	}
	gl.Disable(gl.FRAMEBUFFER_SRGB)
	w, h := int32(t.w), int32(t.h)
	gl.BlitFramebuffer(0, 0, w, h, 0, 0, w, h, gl.COLOR_BUFFER_BIT, gl.NEAREST)
	return nil
}

func restoreFramebuffers(read, draw uint32, isSRGB bool) {
	gl.BindFramebuffer(gl.READ_FRAMEBUFFER, read)
	gl.BindFramebuffer(gl.DRAW_FRAMEBUFFER, draw)
	if isSRGB {
		gl.Enable(gl.FRAMEBUFFER_SRGB)
	}
}
