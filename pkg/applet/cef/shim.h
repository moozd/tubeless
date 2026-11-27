// shim.h is the small C surface the Go side of package cef calls. It
// hides CEF's C API structs (function-pointer tables with manual
// reference counting) behind plain functions, so Go never builds or
// frees a CEF struct itself.
#ifndef TUBELESS_CEF_SHIM_H
#define TUBELESS_CEF_SHIM_H

#include <stddef.h>
#include <stdint.h>

typedef struct tb_options {
	const char* lib_path;
	// flags are extra Chromium switches, space-separated.
	const char* flags;
	const char* subprocess_path;
	const char* resources_dir;
	const char* locales_dir;
	const char* cache_dir;
	const char* log_file;
	// framework_dir and main_bundle are for macOS only; empty elsewhere.
	const char* framework_dir;
	const char* main_bundle;
	uint32_t background;
} tb_options;

typedef struct tb_browser tb_browser;

// tb_response is Go's answer to a tubeless:// request. mime and data
// are malloc'd; the resource handler frees them.
typedef struct tb_response {
	int status;
	char* mime;
	uint8_t* data;
	size_t len;
} tb_response;

// tb_edit is an edit command for the focused frame. macOS sends these
// for Cmd+C and friends: offscreen views get no menu to do it.
typedef enum tb_edit {
	TB_EDIT_UNDO,
	TB_EDIT_REDO,
	TB_EDIT_CUT,
	TB_EDIT_COPY,
	TB_EDIT_PASTE,
	TB_EDIT_SELECT_ALL,
} tb_edit;

// tb_gpu_frame is one page frame as a GPU buffer (Linux dmabuf). The
// file descriptors stay valid only during goOnGPUPaint.
typedef struct tb_gpu_frame {
	int width;
	int height;
	int format;
	uint64_t modifier;
	int plane_count;
	int fds[4];
	uint32_t strides[4];
	uint64_t offsets[4];
} tb_gpu_frame;

typedef struct tb_key {
	int type;
	uint32_t modifiers;
	int windows_key_code;
	int native_key_code;
	uint16_t character;
	uint16_t unmodified_character;
} tb_key;

const char* tb_cef_init(const tb_options* opts);
void tb_cef_do_work(void);
void tb_cef_shutdown(void);

tb_browser* tb_browser_create(uintptr_t handle, const char* url,
	int width, int height, float scale, uint32_t background, int fps,
	int gpu);
void tb_browser_resize(tb_browser* b, int width, int height, float scale);
void tb_browser_close(tb_browser* b);
void tb_browser_free(tb_browser* b);
void tb_browser_focus(tb_browser* b, int focused);
void tb_browser_mouse_move(tb_browser* b, int x, int y, uint32_t mods,
	int leave);
void tb_browser_mouse_click(tb_browser* b, int x, int y, uint32_t mods,
	int button, int up, int count);
void tb_browser_mouse_wheel(tb_browser* b, int x, int y, uint32_t mods,
	int dx, int dy);
void tb_browser_key(tb_browser* b, const tb_key* key);
void tb_browser_exec_js(tb_browser* b, const char* code);
void tb_browser_invalidate(tb_browser* b);
void tb_browser_edit(tb_browser* b, tb_edit cmd);

#endif
