# tubeless

A GPU-rendered terminal emulator, written in Go on top of GLFW/OpenGL.

![tubeless rendering true-color text with bold, italic, and a curly underline](assets/screenshots/hero.png)

Every glyph — including bold, italic, ligatures, and the cursor glow —
is drawn on the GPU, not blitted from a bitmap font cache. That's what
lets a few things most terminals don't bother with:

### Real programming ligatures, from the font's own GSUB tables

No look-alike glyph substitution — `=>`, `!=`, `<-`, `&&` and friends
are shaped by HarfBuzz straight from the font, the same way a browser
or a native text editor would render them.

![Go code rendered with FiraCode ligatures](assets/screenshots/ligatures.png)

### A GPU icon atlas wide enough for the full Nerd Font set

Powerline separators, devicons, and Nerd Font glyphs render at full
fidelity — including automatic upscaling for undersized icons — so
prompts, statuslines, and file-tree glyphs look right instead of
clipped or blurry.

![Nerd Font icons, powerline segments, and a git-branch prompt](assets/screenshots/icons.png)

### Period-accurate CRT emulation, not a generic scanline filter

Six monitor presets (IBM 5151/5153, Zenith ZVM-1220, Apple Monitor III,
Commodore 1084S, Princeton HX-12), plus a modern flat default, each
modeling one real display's curvature, phosphor persistence, and
convergence error from its actual service manual — tuned against
reference photos of the real hardware.

![The Zenith ZVM-1220 CRT preset rendering an oscilloscope screen in green P39 phosphor](assets/screenshots/crt.jpg)

### A GPU-rendered settings UI, live inside your own terminal

`tubeless config` runs as a normal VT program — it edits and previews
every setting (theme, font, cursor, CRT effects) rendered through the
same pipeline your shell uses, with a live preview strip, no separate
GUI window or restart required.

![The in-terminal config UI's Fonts & Theme tab with a live preview](assets/screenshots/config-ui.png)

## Features

- GPU-rendered text with real italic/bold glyphs, curly underlines, and
  a rounded-rect glow cursor that glides smoothly between cells
- Native Wayland and X11 backends on Linux; native Cocoa on macOS
- True-color rendering with 13 built-in themes (rosepine, rosepine-moon,
  gruvbox-dark-hard, nord, dracula, catppuccin-mocha, tokyo-night,
  one-dark, green, amber, green-p39, white-p4, cga) plus a custom theme
  editor, and a set of period-accurate 80s monitor effect presets (IBM
  5151/5153, Zenith ZVM-1220, Apple Monitor III, Commodore 1084S,
  Princeton HX-12) — all in a tabbed, in-terminal config UI
  (`tubeless config`)
- Scrollback, fast scroll/insert-line/delete-line, sixel image output
- Mouse selection and clipboard copy/paste (Cmd+C/V on macOS,
  Ctrl+Shift+C/V on Linux), aware of apps that request their own mouse
  reporting (tmux, vim, htop) so it doesn't fight them for the mouse
- Config-file driven: `~/.config/tubeless/config.toml` (same path on
  every platform, including macOS; respects `$XDG_CONFIG_HOME`)

## Install

```sh
./install.sh
```

Detects your OS and installs the right package: `.deb` on Debian/Ubuntu,
`.rpm` on Fedora/RHEL/openSUSE, an Arch package on Arch/Manjaro, a
generic tarball into `~/.local` on any other Linux, or the `.app` bundle
(symlinked onto `PATH`) on macOS. Downloads the latest GitHub release by
default; pass `--version vX.Y.Z` for a specific one, or `--local` to
install from a build you made yourself (see below).

## Build from source

Requires Go and, on Linux, the usual GLFW build dependencies
(`libgl1-mesa-dev xorg-dev libwayland-dev libxkbcommon-dev
wayland-protocols extra-cmake-modules` on Debian/Ubuntu; the equivalents
on other distros). On macOS, GLFW's Cocoa backend needs to build on an
actual Mac — it can't be cross-compiled from Linux.

```sh
make build            # tubeless, tubeless-config, tektest -> bin/
make install           # detects the OS and installs (Linux: ~/.local,
                        # override with PREFIX=; macOS: Tubeless.app +
                        # symlink onto PATH)
```

Run `make help` for every target, including `package-linux`/
`package-darwin` (build distributable packages into `dist/`) and the
`run-*` dev shortcuts.

## Usage

```sh
tubeless                  # start a terminal with your $SHELL
tubeless --theme=nord     # start with a specific theme
tubeless config           # open the in-terminal settings UI
tubeless upgrade          # check GitHub for a newer release, install it, and restart
tubeless --version        # print the build version
```

## License

MIT — see [LICENSE](LICENSE).
