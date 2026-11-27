# Applets on macOS

Applets (`tubeless exec`) work on macOS when tubeless runs from an app
bundle that holds the CEF framework. This page tells how the macOS
part works and which tests are done.

The Linux design is in [the README](../README.md#applets). The plan
and the decisions behind it are in the vault note
`exo/tubeless/js-applets-idea.md`.

## Install

1. Download the `macosarm64` (or `macosx64`) "minimal" build of CEF
   158 from cef-builds.spotifycdn.com. Extract it to `CEF_ROOT`
   (default `~/.cache/tubeless/cef`).
2. Run `make install-darwin APPLETS=1`.

The build uses the CEF headers in `third_party/cef`. `CEF_ROOT` gives
only the framework that goes in the bundle. `APPLETS=1` also puts the
prebuilt applets (`make applets-dist`, needs node) in
`Contents/Resources/applets`. An applet with the same name in
`~/.config/tubeless/applets` replaces it.

`APPLETS=1` also works with `package-darwin`. Without it, the two targets
make the same bundle as before, with no applets. The release packages
do not use it.

## The app bundle

CEF on macOS loads only from this layout:

```
Tubeless.app/Contents/
  MacOS/tubeless
  Frameworks/Chromium Embedded Framework.framework
  Frameworks/tubeless Helper.app
  Frameworks/tubeless Helper (GPU).app
  Frameworks/tubeless Helper (Renderer).app
  Frameworks/tubeless Helper (Plugin).app
```

- `packaging/darwin/add-cef.sh` copies the framework and makes the
  four helper apps. Each helper holds the same binary with its own
  name, with its own `CFBundleIdentifier` and `LSUIElement`.
- The Makefile signs the full bundle ad hoc after this step.
- `make cef-helper-darwin` builds the helper binary. It does not link
  the framework.

## How tubeless finds CEF

- `cmd/tubeless/cefpaths_darwin.go` finds the bundle from the path of
  the binary that runs. It resolves symlinks first, because
  `~/.local/bin/tubeless` is a symlink into the bundle.
- `TUBELESS_CEF_DIR` can point at the `Frameworks` folder of a
  different bundle.
- tubeless sends `framework_dir_path` and `main_bundle_path` to CEF.
  Thus CEF does not use `NSBundle mainBundle`, which is incorrect when
  tubeless starts through the symlink.
- CEF loads its resources and locales from the framework itself.

## The helper

`pkg/applet/cef/helper/helper.c` loads the framework with `dlopen`
before it runs `cef_execute_process`. The path is
`@executable_path/../../../Chromium Embedded Framework.framework/...`.
The Linux helper continues to link `libcef.so`.

## NSApp

CEF stops the process at start when `NSApp` does not conform to
`CefAppProtocol`.

- `pkg/applet/cef/app_darwin.m` declares `TubelessApplication`, an
  `NSApplication` subclass with that protocol. Its `sendEvent:` sets
  the "handling send event" flag while it runs.
- `main` runs `cef.PrepareApplication` before anything starts GLFW.
  GLFW then keeps this `NSApp`.
- `cef.Init` makes sure that `NSApp` conforms before it starts CEF. If
  it does not, the log shows `applets disabled` and the process does
  not stop.

The message loop is the same as on Linux: `external_message_pump`
on the main thread.

## Edit shortcuts

An offscreen page has no menu bar. Thus Cmd+C, Cmd+V, Cmd+X, Cmd+A,
Cmd+Z and Shift+Cmd+Z do not work as plain key events. On macOS,
`appletHost.Key` sends them to the focused frame as edit commands
(`tb_browser_edit`).

## Test list

Done on an arm64 Mac (macOS 27) with CEF 158.0.6, on a display at
scale 1:

| # | Test | Result |
|---|------|--------|
| 1 | Not in a bundle, or with no framework, the first `tubeless exec` shows `applets are off in this window` | OK |
| 2 | In the bundle, the first `tubeless exec` logs `applets: cef started` | OK |
| 3 | `tubeless exec demo` shows the shader at the display frame rate | Not looked at. The page sends a new frame on each loop: 27 to 28 frames each second without window focus (the loop limit is 30 then). Focused rate not measured |
| 4 | Text in the demo is sharp on a Retina display | Not done |
| 5 | Keys, the tmux prefix and pane focus work as on Linux | Not done |
| 6 | Cmd+C and Cmd+V work in excalidraw and markdown | Not done |
| 7 | Mouse clicks, drag and the wheel work in excalidraw and image | Not done |
| 8 | `q` in pdf prints the page, and `tubeless exec` exits with 0 | Not done |
| 9 | The helper processes stop when the window closes | OK |
| - | Start through a symlink to the bundle binary | OK |
| - | demo and markdown open from `Contents/Resources/applets`, with no user applets | OK |

Do tests 3 to 8 by hand.

## Not in this work

- The GPU path (IOSurface) as an alternative to the CPU copy of each
  frame.
- The CEF sandbox.
- Applets in the macOS release packages.
