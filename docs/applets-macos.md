# Applets on macOS

Applets (`tubeless exec`) work on Linux only at this time. This page
lists the work that makes them work on macOS. Do it on a Mac: each
step needs a build and a run to check it.

The Linux design is in [the README](../README.md#applets). The plan
and the decisions behind it are in the vault note
`exo/tubeless/js-applets-idea.md`.

## Parts that stay the same

These parts have no Linux-only code. Do not change them for macOS:

- `pkg/applet`: the bridge server, the `tubeless://` pages, esbuild,
  the theme and the permissions.
- `cmd/tubeless/exec.go`: the OSC 7777 request, the placeholder cells
  and the exec socket. Without `XDG_RUNTIME_DIR`, the socket goes in
  `$TMPDIR`, which macOS makes for each user.
- `pkg/render/applettexture.go` and `DrawApplets`: the page texture
  (BGRA, `SRGB8_ALPHA8`) and its position in the scene.
- `cmd/tubeless/appletkeys.go`: on macOS the GLFW scancode is the
  native key code that CEF wants. Thus `scanOffset` stays 0.
- `pkg/applet/cef/cefload.c`: libcef loads with `dlopen`, and the
  framework binary loads with the same procedure.
- The applets in `applets/`.

## What to do

### 1. Get the CEF build

Download the `macosarm64` "minimal" build of the same CEF version
(158) from cef-builds.spotifycdn.com. It holds
`Release/Chromium Embedded Framework.framework` and the headers.
Use `CEF_ROOT` for it, as on Linux.

### 2. Make the app bundle

CEF on macOS loads only from an app bundle with this layout:

```
Tubeless.app/Contents/
  MacOS/tubeless
  Frameworks/Chromium Embedded Framework.framework
  Frameworks/tubeless Helper.app
  Frameworks/tubeless Helper (GPU).app
  Frameworks/tubeless Helper (Renderer).app
  Frameworks/tubeless Helper (Plugin).app
```

- Each helper app holds the same helper binary, renamed, and its own
  `Info.plist` with a unique `CFBundleIdentifier` and `LSUIElement`
  set to true.
- Extend `install-darwin` and `package-darwin` in the Makefile to copy
  the framework and to make the four helpers. Add a `tubeless-cef`
  build for macOS: the same `cef` build tag, with `CGO_CFLAGS` set to
  `-I$(CEF_ROOT)`.
- Sign the bundle ad hoc (`codesign --force --deep -s -`). Remove the
  quarantine flag from the downloaded framework
  (`xattr -dr com.apple.quarantine`).

### 3. Find and open the framework

- In `cmd/tubeless/applets.go`, `newAppletHost` sets
  `LibPath` to `libcef.so`. On macOS, set it to
  `<bundle>/Contents/Frameworks/Chromium Embedded Framework.framework/Chromium Embedded Framework`.
  Find `<bundle>` from the path of the binary that runs. `tubeless`
  in `~/.local/bin` is a symlink into the bundle. Thus resolve the
  symlink first.
- Set `SubprocessPath` to
  `<bundle>/Contents/Frameworks/tubeless Helper.app/Contents/MacOS/tubeless Helper`.
- In `tb_cef_init` (`pkg/applet/cef/shim.c`), also set
  `framework_dir_path` and `main_bundle_path` in `cef_settings_t`.
  Send them through `tb_options` and `cef.Options`.
- Make `cefRuntimeDir` give the bundle's `Frameworks` folder on
  macOS, so `ResourcesDir` and `LocalesDir` point into the framework's
  `Resources`.

### 4. Make the helper load the framework

`pkg/applet/cef/helper/helper.c` links libcef directly. On macOS it
must load the framework with `dlopen` before it calls
`cef_execute_process`. The framework path is relative to the helper:
`../../../Chromium Embedded Framework.framework/Chromium Embedded Framework`.
Use `cefload.h` and `cefload.c` for this, the same as the browser
process. Keep `-fno-stack-protector` on the helper.

### 5. Give NSApplication the protocol CEF needs

This is the step with the most risk. On macOS, CEF requires that
`NSApp` conforms to `CrAppControlProtocol`: the methods
`isHandlingSendEvent` and `setHandlingSendEvent:`, and a `sendEvent:`
that sets the flag while it runs. If it does not, CEF stops the process
at start.

GLFW makes `NSApp` with `[NSApplication sharedApplication]` in
`glfw.Init` (`pkg/render/window.go`). Before `glfw.Init`, on the main
thread, make the shared application from a subclass that has these
methods:

- Add an Objective-C file to `pkg/applet/cef` with build tags `cef`
  and `darwin`. Declare `TubelessApplication : NSApplication
  <CrAppControlProtocol>` in it.
- Run `[TubelessApplication sharedApplication]` from a Go function in
  that package, before `glfw.Init`. GLFW then uses this `NSApp`.
- On other builds, that Go function does nothing.

If GLFW replaces `NSApp`, the alternative is to add the methods and the
protocol to GLFW's class at run time with `class_addMethod` and
`class_addProtocol`, before `cef_initialize`.

### 6. Keep the message loop on the main thread

Do not change the design: `external_message_pump`,
`cef_do_message_loop_work` on the main thread, and
`glfw.PostEmptyEvent` to wake the loop. macOS does not let you use
`multi_threaded_message_loop`. Check that `cef.Init` runs after
`glfw.Init` and on the main OS thread.

### 7. Send the edit shortcuts to CEF

In offscreen mode on macOS, Cmd+C, Cmd+V, Cmd+X, Cmd+A, Cmd+Z and
Shift+Cmd+Z do not work as plain key events. In `appletHost.Key`
(`cmd/tubeless/appletinput.go`), on macOS, send these shortcuts to the
frame's `copy`, `paste`, `cut`, `select_all`, `undo` and `redo`.
Add a `tb_browser_edit` function to the shim for them.

### 8. Check the Retina scale

`cs.dpiX` is 2 on a Retina display. The page view is
`pixels / scale` in DIPs, and `get_screen_info` gives CEF the scale.
This code is not tested at a scale other than 1.

## Test list

Do these on the Mac, in this sequence:

1. `tubeless` starts with no framework installed and logs
   `applets disabled`.
2. With the bundle installed, the log shows `applets: cef started`.
3. `tubeless exec demo` shows the shader at the display frame rate.
4. Text in the demo is sharp on a Retina display.
5. Keys, the tmux prefix and pane focus work as on Linux.
6. Cmd+C and Cmd+V work in the excalidraw and markdown applets.
7. Mouse clicks, drag and the wheel work in excalidraw and image.
8. `q` in pdf prints the page, and `tubeless exec` exits with 0.
9. The helper processes stop when the window closes.

## Not in this work

- The GPU path (IOSurface) instead of the CPU copy of each frame.
- The CEF sandbox.
- Applets in the release packages: the packages are built without
  the `cef` tag on all systems.
