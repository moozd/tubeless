//go:build linux

package render

/*
#cgo LDFLAGS: -lEGL
#include <stdint.h>
#include <string.h>
#include <EGL/egl.h>
#include <EGL/eglext.h>

typedef void (*tb_image_target_fn)(unsigned int target, void* image);

#define TB_GL_TEXTURE_2D 0x0DE1
#define TB_MOD_INVALID 0x00ffffffffffffffULL

static PFNEGLCREATEIMAGEKHRPROC tb_create_image;
static PFNEGLDESTROYIMAGEKHRPROC tb_destroy_image;
static tb_image_target_fn tb_image_target;

// tb_dmabuf_init loads the EGL entry points. It returns 0 when the
// current GL context is not EGL (X11 uses GLX) or lacks dmabuf import.
static int tb_dmabuf_init(void) {
	EGLDisplay d = eglGetCurrentDisplay();
	if (d == EGL_NO_DISPLAY) {
		return 0;
	}
	const char* ext = eglQueryString(d, EGL_EXTENSIONS);
	if (ext == NULL || strstr(ext, "EGL_EXT_image_dma_buf_import") == NULL) {
		return 0;
	}
	tb_create_image = (PFNEGLCREATEIMAGEKHRPROC)eglGetProcAddress("eglCreateImageKHR");
	tb_destroy_image = (PFNEGLDESTROYIMAGEKHRPROC)eglGetProcAddress("eglDestroyImageKHR");
	tb_image_target = (tb_image_target_fn)eglGetProcAddress("glEGLImageTargetTexture2DOES");
	return tb_create_image && tb_destroy_image && tb_image_target;
}

static const EGLint plane_attrs[4][5] = {
	{EGL_DMA_BUF_PLANE0_FD_EXT, EGL_DMA_BUF_PLANE0_OFFSET_EXT, EGL_DMA_BUF_PLANE0_PITCH_EXT,
	 EGL_DMA_BUF_PLANE0_MODIFIER_LO_EXT, EGL_DMA_BUF_PLANE0_MODIFIER_HI_EXT},
	{EGL_DMA_BUF_PLANE1_FD_EXT, EGL_DMA_BUF_PLANE1_OFFSET_EXT, EGL_DMA_BUF_PLANE1_PITCH_EXT,
	 EGL_DMA_BUF_PLANE1_MODIFIER_LO_EXT, EGL_DMA_BUF_PLANE1_MODIFIER_HI_EXT},
	{EGL_DMA_BUF_PLANE2_FD_EXT, EGL_DMA_BUF_PLANE2_OFFSET_EXT, EGL_DMA_BUF_PLANE2_PITCH_EXT,
	 EGL_DMA_BUF_PLANE2_MODIFIER_LO_EXT, EGL_DMA_BUF_PLANE2_MODIFIER_HI_EXT},
	{EGL_DMA_BUF_PLANE3_FD_EXT, EGL_DMA_BUF_PLANE3_OFFSET_EXT, EGL_DMA_BUF_PLANE3_PITCH_EXT,
	 EGL_DMA_BUF_PLANE3_MODIFIER_LO_EXT, EGL_DMA_BUF_PLANE3_MODIFIER_HI_EXT},
};

// tb_bind_dmabuf makes the bound GL_TEXTURE_2D show the dmabuf. It
// returns 0, or the EGL error. The texture keeps the buffer after the
// EGL image is gone.
static int tb_bind_dmabuf(int w, int h, uint32_t fourcc, uint64_t modifier,
		int n, const int* fds, const uint32_t* strides, const uint64_t* offsets) {
	EGLint attrs[64];
	int i = 0;
	attrs[i++] = EGL_WIDTH; attrs[i++] = w;
	attrs[i++] = EGL_HEIGHT; attrs[i++] = h;
	attrs[i++] = EGL_LINUX_DRM_FOURCC_EXT; attrs[i++] = (EGLint)fourcc;
	for (int p = 0; p < n && p < 4; p++) {
		attrs[i++] = plane_attrs[p][0]; attrs[i++] = fds[p];
		attrs[i++] = plane_attrs[p][1]; attrs[i++] = (EGLint)offsets[p];
		attrs[i++] = plane_attrs[p][2]; attrs[i++] = (EGLint)strides[p];
		if (modifier != TB_MOD_INVALID) {
			attrs[i++] = plane_attrs[p][3]; attrs[i++] = (EGLint)(modifier & 0xffffffff);
			attrs[i++] = plane_attrs[p][4]; attrs[i++] = (EGLint)(modifier >> 32);
		}
	}
	attrs[i] = EGL_NONE;
	EGLDisplay d = eglGetCurrentDisplay();
	EGLImageKHR image = tb_create_image(d, EGL_NO_CONTEXT, EGL_LINUX_DMA_BUF_EXT, NULL, attrs);
	if (image == EGL_NO_IMAGE_KHR) {
		return eglGetError();
	}
	tb_image_target(TB_GL_TEXTURE_2D, image);
	tb_destroy_image(d, image);
	return 0;
}
*/
import "C"

import (
	"fmt"
	"sync"
)

var (
	dmabufOnce  sync.Once
	dmabufReady bool
)

// GPUImportAvailable reports whether frames can come in as dmabufs:
// the GL context must be EGL (Wayland) with dmabuf import. Run it on
// the GL thread.
func GPUImportAvailable() bool {
	dmabufOnce.Do(func() { dmabufReady = C.tb_dmabuf_init() != 0 })
	return dmabufReady
}

func bindDMABuf(d DMABuf) error {
	n := len(d.FDs)
	if n == 0 || n > 4 || len(d.Strides) != n || len(d.Offsets) != n {
		return fmt.Errorf("dmabuf with %d planes", n)
	}
	var fds [4]C.int
	var strides [4]C.uint32_t
	var offsets [4]C.uint64_t
	for i := 0; i < n; i++ {
		fds[i], strides[i], offsets[i] = C.int(d.FDs[i]), C.uint32_t(d.Strides[i]), C.uint64_t(d.Offsets[i])
	}
	code := C.tb_bind_dmabuf(C.int(d.W), C.int(d.H), C.uint32_t(d.FourCC), C.uint64_t(d.Modifier),
		C.int(n), &fds[0], &strides[0], &offsets[0])
	if code != 0 {
		return fmt.Errorf("import dmabuf: EGL error %#x", int(code))
	}
	return nil
}
