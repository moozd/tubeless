// cefload.h is libcef loaded at run time instead of linked: tubeless
// built with applet support must still start when the CEF runtime is
// missing or moved. shim.c includes it after the CEF headers; the
// macros then send every CEF call through the table tb_cef_load fills.
#ifndef TUBELESS_CEF_LOAD_H
#define TUBELESS_CEF_LOAD_H

#include "include/capi/cef_app_capi.h"
#include "include/capi/cef_browser_capi.h"
#include "include/capi/cef_scheme_capi.h"
#include "include/cef_api_hash.h"
#include "include/internal/cef_string_types.h"

typedef struct tb_cef_fns {
	__typeof__(&cef_api_hash) api_hash;
	__typeof__(&cef_initialize) initialize;
	__typeof__(&cef_execute_process) execute_process;
	__typeof__(&cef_do_message_loop_work) do_message_loop_work;
	__typeof__(&cef_shutdown) shutdown;
	__typeof__(&cef_browser_host_create_browser) create_browser;
	__typeof__(&cef_register_scheme_handler_factory) register_scheme_factory;
	__typeof__(&cef_string_utf8_to_utf16) utf8_to_utf16;
	__typeof__(&cef_string_utf16_to_utf8) utf16_to_utf8;
	__typeof__(&cef_string_utf16_clear) utf16_clear;
	__typeof__(&cef_string_utf8_clear) utf8_clear;
	__typeof__(&cef_string_userfree_utf16_free) userfree_utf16_free;
} tb_cef_fns;

extern tb_cef_fns tb_cef;

// tb_cef_load opens libcef at path and fills tb_cef. It returns NULL on
// success, or a message that stays valid until the next call.
const char* tb_cef_load(const char* path);

#ifndef TB_CEF_NO_MACROS
#define cef_api_hash tb_cef.api_hash
#define cef_initialize tb_cef.initialize
#define cef_execute_process tb_cef.execute_process
#define cef_do_message_loop_work tb_cef.do_message_loop_work
#define cef_shutdown tb_cef.shutdown
#define cef_browser_host_create_browser tb_cef.create_browser
#define cef_register_scheme_handler_factory tb_cef.register_scheme_factory
#define cef_string_utf8_to_utf16 tb_cef.utf8_to_utf16
#define cef_string_utf16_to_utf8 tb_cef.utf16_to_utf8
#define cef_string_utf16_clear tb_cef.utf16_clear
#define cef_string_utf8_clear tb_cef.utf8_clear
#define cef_string_userfree_utf16_free tb_cef.userfree_utf16_free
#endif

#endif
