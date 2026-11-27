// shim.h is the small C surface the Go side of package cef calls. It
// hides CEF's C API structs (function-pointer tables with manual
// reference counting) behind plain functions, so Go never builds or
// frees a CEF struct itself.
#ifndef TUBELESS_CEF_SHIM_H
#define TUBELESS_CEF_SHIM_H

#include <stddef.h>
#include <stdint.h>

typedef struct tb_options {
	const char* subprocess_path;
	const char* resources_dir;
	const char* locales_dir;
	const char* cache_dir;
	const char* log_file;
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

typedef struct tb_key {
	int type;
	uint32_t modifiers;
	int windows_key_code;
	int native_key_code;
	uint16_t character;
	uint16_t unmodified_character;
} tb_key;

int tb_cef_init(const tb_options* opts);
void tb_cef_do_work(void);
void tb_cef_shutdown(void);

tb_browser* tb_browser_create(uintptr_t handle, const char* url,
	int width, int height, float scale, uint32_t background, int fps);
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

#endif
