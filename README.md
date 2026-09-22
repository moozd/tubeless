<p align="center">
  <img src="packaging/icon.svg" width="64" height="64">
</p>

<h1 align="center">tubeless</h1>

A GPU-rendered terminal emulator written in Go, on top of GLFW and
OpenGL.

Built to look good, not to emulate a VT100 down to the pixel: a cursor
that glides and morphs instead of snapping between cells, and rounded
corners, a soft drop shadow, and a bloom glow on every block and
border.

## What it looks like in daily use

![Claude Code running inside tubeless, tmux status line at the bottom](assets/screenshots/daily-driver.png)

![Neovim with a Harpoon popup — rounded corners and a soft drop shadow over the live buffer](assets/screenshots/neovim-harpoon.png)

Floating windows get rounded corners, a soft drop shadow, and a bloom
glow (the `surface` and `blur` config sections) — a filter over the
block's own rendered pixels, not a fixed overlay image.

![Neovim's Telescope find-files picker, Nerd Font icons per file type](assets/screenshots/neovim-telescope.png)

## Features

- A cursor that glides and morphs into a ball-and-tail shape on fast
  jumps, with a rounded-rect glow and a breathing pulse, instead of a
  static block
- Rounded corners, a soft drop shadow, and a bloom glow on every
  block/border surface
- An experimental frosted-glass cursor that refracts and blurs the
  scene behind it (`cursor.glass`)
- Real italic/bold glyphs and ligatures shaped by HarfBuzz straight
  from the loaded font's own GSUB table
- Native Wayland and X11 backends on Linux; native Cocoa on macOS
- True-color rendering with 14 built-in themes (rosepine,
  rosepine-moon, gruvbox-dark-hard, nord, dracula, catppuccin-mocha,
  tokyo-night, one-dark, green, amber, green-p39, white-p4, cga,
  cyberpunk) plus a custom theme editor
- Period-accurate 80s monitor effect presets (IBM 5151/5153, Zenith
  ZVM-1220, Apple Monitor III, Commodore 1084S, Princeton HX-12) for
  curvature, phosphor persistence, and scanlines — off by default
- A tabbed, in-terminal config UI (`tubeless config`) with a live
  preview, covering every setting above
- Scrollback, fast scroll/insert-line/delete-line, sixel image output
- Mouse selection and clipboard copy/paste (Cmd+C/V on macOS,
  Ctrl+Shift+C/V on Linux), aware of apps that request their own mouse
  reporting (tmux, vim, htop) so it doesn't fight them for the mouse
- Config-file driven: `~/.config/tubeless/config.toml` (same path on
  every platform, including macOS; respects `$XDG_CONFIG_HOME`) — see
  [docs/config.md](docs/config.md) for the full reference

## In-terminal config UI

`tubeless config` runs as a normal VT program: it edits and previews
every setting (theme, font, cursor, CRT effects) through the same
rendering pipeline your shell uses, with a live preview strip — no
separate GUI window, no restart required.

![The in-terminal config UI's Fonts & Theme tab with a live preview](assets/screenshots/config-ui.png)

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

Requires Go 1.21+ and a C toolchain (cgo bindings to GLFW, FreeType, and
HarfBuzz). On macOS, GLFW's Cocoa backend needs to build on an actual
Mac — it can't be cross-compiled from Linux.

Install the build dependencies for your OS:

**Debian/Ubuntu**

```sh
sudo apt-get install -y \
  libgl1-mesa-dev xorg-dev \
  libwayland-dev libxkbcommon-dev wayland-protocols extra-cmake-modules \
  libharfbuzz-dev libfreetype-dev pkg-config
```

**Fedora/RHEL**

```sh
sudo dnf install -y \
  mesa-libGL-devel libXcursor-devel libXi-devel libXinerama-devel libXrandr-devel \
  wayland-devel libxkbcommon-devel wayland-protocols-devel extra-cmake-modules \
  harfbuzz-devel freetype-devel pkgconf-pkg-config
```

**openSUSE**

```sh
sudo zypper install -y \
  Mesa-libGL-devel libX11-devel libXcursor-devel libXi-devel libXinerama-devel libXrandr-devel \
  wayland-devel libxkbcommon-devel wayland-protocols-devel extra-cmake-modules \
  harfbuzz-devel freetype2-devel pkg-config
```

**Arch/Manjaro**

```sh
sudo pacman -S --needed \
  mesa libx11 libxcursor libxi libxinerama libxrandr \
  wayland libxkbcommon wayland-protocols extra-cmake-modules \
  harfbuzz freetype2 pkgconf
```

**macOS**

```sh
brew install freetype harfbuzz pkg-config
```

(GLFW's Cocoa/OpenGL backend comes from the system frameworks via Xcode
Command Line Tools — no separate package needed.)

Then build:

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
tubeless --font="JetBrains Mono" --font-size=16
tubeless --shell=/path/to/program
tubeless config           # open the in-terminal settings UI
tubeless upgrade          # check GitHub for a newer release, install it, and restart
tubeless --version        # print the build version
```

See [docs/config.md](docs/config.md) for the full config file
reference — every theme, preset, and TOML key.

## License

MIT — see [LICENSE](LICENSE).
