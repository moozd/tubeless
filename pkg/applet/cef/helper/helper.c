// tubeless-cef-helper is the program CEF starts for its renderer, GPU
// and utility processes (cef_settings_t.browser_subprocess_path). It is
// plain C on purpose: Chromium's switches would trip tubeless's own flag
// parsing, and V8 in the renderer installs signal handlers the Go
// runtime must not share a process with.
#include <string.h>

#include "include/capi/cef_app_capi.h"
#include "include/cef_api_hash.h"

// The helper does not link libcef: it loads it at run time, the same
// way the browser process does, so it builds from the CEF headers alone.
#include <stdio.h>
#include <stdlib.h>

#include "cefload.h"

#ifdef __APPLE__
// libPath is relative to Contents/Frameworks/<helper>.app/
// Contents/MacOS, where the helper binary is.
static const char* libPath(void) {
	return "@executable_path/../../../Chromium Embedded Framework.framework/"
		"Chromium Embedded Framework";
}
#else
// libPath is the libcef the window loaded: the window sets
// TUBELESS_CEF_LIB before CEF starts, and the helper processes inherit
// its environment.
static const char* libPath(void) {
	return getenv("TUBELESS_CEF_LIB");
}
#endif

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
	const char* path = libPath();
	if (path == NULL) {
		fprintf(stderr, "tubeless helper: TUBELESS_CEF_LIB is not set\n");
		return 1;
	}
	const char* err = tb_cef_load(path);
	if (err != NULL) {
		fprintf(stderr, "tubeless helper: %s\n", err);
		return 1;
	}
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
