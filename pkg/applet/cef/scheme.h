// scheme.h registers the tubeless:// scheme. Every CEF process must
// register custom schemes the same way, so the browser process
// (shim.c) and tubeless-cef-helper (helper/helper.c) both include it.
#ifndef TUBELESS_CEF_SCHEME_H
#define TUBELESS_CEF_SCHEME_H

#include <string.h>

#include "include/capi/cef_app_capi.h"
#include "include/capi/cef_scheme_capi.h"

#define TB_SCHEME "tubeless"

static inline void tb_register_scheme(cef_scheme_registrar_t* registrar) {
	cef_string_t name = {0};
	cef_string_utf8_to_utf16(TB_SCHEME, strlen(TB_SCHEME), &name);
	registrar->add_custom_scheme(registrar, &name,
		CEF_SCHEME_OPTION_STANDARD | CEF_SCHEME_OPTION_SECURE |
		CEF_SCHEME_OPTION_CORS_ENABLED | CEF_SCHEME_OPTION_FETCH_ENABLED);
	cef_string_clear(&name);
}

#endif
