// tubeless-cef-helper is the program CEF starts for its renderer, GPU
// and utility processes (cef_settings_t.browser_subprocess_path). It is
// plain C on purpose: Chromium's switches would trip tubeless's own flag
// parsing, and V8 in the renderer installs signal handlers the Go
// runtime must not share a process with.
#include "include/capi/cef_app_capi.h"
#include "include/cef_api_hash.h"

int main(int argc, char* argv[]) {
	cef_api_hash(CEF_API_VERSION, 0);
	cef_main_args_t args = {argc, argv};
	return cef_execute_process(&args, NULL, NULL);
}
