// app_darwin.h makes NSApp an application that CEF accepts. CEF on
// macOS stops the process at start when NSApp does not conform to
// CefAppProtocol (see include/cef_application_mac.h).
#ifndef TUBELESS_CEF_APP_DARWIN_H
#define TUBELESS_CEF_APP_DARWIN_H

// tb_app_prepare makes the shared application a TubelessApplication.
// Run it on the main thread before glfw.Init: GLFW then keeps it.
void tb_app_prepare(void);

// tb_app_is_ready reports whether NSApp conforms to CefAppProtocol.
int tb_app_is_ready(void);

#endif
