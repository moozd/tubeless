// tubeless-cef-helper is the program CEF starts for its renderer, GPU
// and utility processes (cef_settings_t.browser_subprocess_path). It is
// plain C on purpose: Chromium's switches would trip tubeless's own flag
// parsing, and V8 in the renderer installs signal handlers the Go
// runtime must not share a process with.
#include <string.h>

#include "include/capi/cef_app_capi.h"
#include "include/cef_api_hash.h"

#include "scheme.h"

// The app only registers the tubeless:// scheme, which every process
// must do. It lives for the full process, so counting is a no-op.
static void CEF_CALLBACK no_add_ref(cef_base_ref_counted_t* self) {
	(void)self;
}

static int CEF_CALLBACK no_release(cef_base_ref_counted_t* self) {
	(void)self;
	return 0;
}

static int CEF_CALLBACK one_ref(cef_base_ref_counted_t* self) {
	(void)self;
	return 1;
}

static void CEF_CALLBACK on_register_custom_schemes(cef_app_t* self,
		cef_scheme_registrar_t* registrar) {
	(void)self;
	tb_register_scheme(registrar);
}

int main(int argc, char* argv[]) {
	cef_api_hash(CEF_API_VERSION, 0);
	cef_app_t app;
	memset(&app, 0, sizeof(app));
	app.base.size = sizeof(app);
	app.base.add_ref = no_add_ref;
	app.base.release = no_release;
	app.base.has_one_ref = one_ref;
	app.base.has_at_least_one_ref = one_ref;
	app.on_register_custom_schemes = on_register_custom_schemes;
	cef_main_args_t args = {argc, argv};
	return cef_execute_process(&args, &app, NULL);
}
