//go:build cef

#define TB_CEF_NO_MACROS
#include <dlfcn.h>
#include <stdio.h>

#include "cefload.h"

tb_cef_fns tb_cef;

static char load_error[512];

#define LOAD(field, name)                                                  \
	do {                                                                   \
		tb_cef.field = (__typeof__(tb_cef.field))dlsym(lib, name);         \
		if (tb_cef.field == NULL) {                                        \
			snprintf(load_error, sizeof(load_error),                       \
				"%s has no %s", path, name);                               \
			return load_error;                                             \
		}                                                                  \
	} while (0)

const char* tb_cef_load(const char* path) {
	void* lib = dlopen(path, RTLD_NOW | RTLD_LOCAL);
	if (lib == NULL) {
		snprintf(load_error, sizeof(load_error), "%s", dlerror());
		return load_error;
	}
	LOAD(api_hash, "cef_api_hash");
	LOAD(initialize, "cef_initialize");
	LOAD(execute_process, "cef_execute_process");
	LOAD(do_message_loop_work, "cef_do_message_loop_work");
	LOAD(shutdown, "cef_shutdown");
	LOAD(create_browser, "cef_browser_host_create_browser");
	LOAD(register_scheme_factory, "cef_register_scheme_handler_factory");
	LOAD(utf8_to_utf16, "cef_string_utf8_to_utf16");
	LOAD(utf16_to_utf8, "cef_string_utf16_to_utf8");
	LOAD(utf16_clear, "cef_string_utf16_clear");
	LOAD(utf8_clear, "cef_string_utf8_clear");
	LOAD(userfree_utf16_free, "cef_string_userfree_utf16_free");
	return NULL;
}
