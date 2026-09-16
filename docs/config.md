# Configuration

tubeless is config-file driven. Everything below can be edited either
by hand or through the in-terminal settings UI (`tubeless config`),
which reads and writes the same file and shows a live preview as you
adjust values.

## File location

```
$XDG_CONFIG_HOME/tubeless/config.toml
```

`$XDG_CONFIG_HOME` defaults to `~/.config` on every platform, including
macOS (deliberately — not `~/Library/Application Support`). A missing
file is not an error: tubeless just runs with defaults, and any field
you never set keeps its default. There is no need to write a complete
file — only the keys you want to override.

## Command-line flags

```sh
tubeless --theme=nord              # starting theme (config file overrides if set there)
tubeless --shell=/path/to/program  # program to run instead of $SHELL
tubeless --font="JetBrains Mono"   # installed font family (default: bundled FiraCode Nerd)
tubeless --font-size=16            # logical font size in pixels (default: config font.size)
tubeless config                    # open the in-terminal settings UI
tubeless upgrade                   # check GitHub for a newer release, install it, restart
tubeless --version                 # print the build version
```

## Two independent axes: theme and preset

Every look tubeless ships is built from two independent settings:

- **`theme`** — color. Selects `true_color`, `colors`, and `phosphor`.
- **`preset`** — every other visual effect: `surface`, `blur`,
  `cursor`, `face`, `contrast`, and `crt`.

Setting either one seeds the fields it governs; hand-editing any of
those fields afterward (or through the config TUI) flips that axis's
name to `"custom"` so it's never silently reseeded later.

```toml
theme = "rosepine"
preset = "modern"
```

### Built-in themes

`rosepine` (default) · `rosepine-moon` · `gruvbox-dark-hard` · `nord` ·
`dracula` · `catppuccin-mocha` · `tokyo-night` · `one-dark` · `green` ·
`amber` · `green-p39` · `white-p4` · `cga`

The last five (`green`, `amber`, `green-p39`, `white-p4`, `cga`) are
monochrome or fixed-palette themes — period-accurate companions to the
CRT presets below, rather than independent color schemes.

### Built-in presets

`modern` (default, every CRT effect off) · `ibm-5151` · `ibm-5153` ·
`zenith-zvm-1220` · `apple-monitor-iii` · `commodore-1084s` ·
`princeton-hx12`

Each non-`modern` preset models one real 1980s monitor's curvature,
phosphor persistence, noise, and (where applicable) shadow-mask
convergence error, tuned from that display's own service manual. Four
of them have one historically-correct theme pairing (selecting the
preset in `tubeless config` switches the theme too):

| Preset              | Paired theme |
| ------------------- | ------------ |
| `ibm-5151`          | `green-p39`  |
| `ibm-5153`          | `cga`        |
| `zenith-zvm-1220`   | `amber`      |
| `apple-monitor-iii` | `white-p4`   |

(`commodore-1084s` and `princeton-hx12` have no fixed native palette —
real analog RGB monitors that just rendered whatever the host sent
them — so picking one leaves the active theme alone.)

## Reference

All sizes are in framebuffer pixels unless noted. Colors are linear
RGB triples `[r, g, b]` in `0.0-1.0`, not raw hex.

### `[font]`

| Key           | Type    | Default | Meaning                                                                 |
| ------------- | ------- | ------- | ------------------------------------------------------------------------ |
| `family`      | string  | `""`    | Installed font family (fontconfig-resolved). Empty = bundled FiraCode Nerd. |
| `size`        | int     | `14`    | Logical font size in pixels.                                            |
| `line_height` | float   | `1`     | Multiplier on the font's own line height.                               |
| `ligatures`   | bool    | `true`  | Draw GSUB programming ligatures (`=>`, `->`, `!=`, …) the loaded font's own GSUB tables define. Off costs nothing. |

### `[atlas]`

| Key     | Type  | Default | Meaning                                                              |
| ------- | ----- | ------- | ----------------------------------------------------------------------- |
| `scale` | int   | `4`     | Glyph rasterization supersampling. `<= 0` auto-picks from the monitor's content scale. |
| `gamma` | float | `1.0`   | Glyph rasterization gamma correction.                                |

### `[padding]`

| Key    | Type  | Default | Meaning                                                       |
| ------ | ----- | ------- | ---------------------------------------------------------------- |
| `size` | float | `0`     | Empty margin kept between the window (or letterboxed content box) and the grid, on every side. |

### `[scrollback]`

| Key     | Type | Default | Meaning                                        |
| ------- | ---- | ------- | ------------------------------------------------- |
| `lines` | int  | `5000`  | Scrolled-off history retained. `<= 0` disables scrollback. Not yet exposed in the config TUI — edit the file directly. |

### `[scrolling]` (experimental)

| Key                    | Type | Default | Meaning                                                                 |
| ---------------------- | ---- | ------- | ------------------------------------------------------------------------ |
| `smooth_content_shift` | bool | `false` | Ease detected scroll/pane shifts in like Neovide instead of snapping. A heuristic frame diff — the escape hatch if it misfires on some app's output. |

### `[shell]` (experimental)

| Key        | Type | Default | Meaning                                                                 |
| ---------- | ---- | ------- | ------------------------------------------------------------------------ |
| `use_tmux` | bool | `false` | Launch into a fixed tmux session named `home` instead of a plain login shell. Only applies on the `$SHELL` auto-detect path (an explicit `--shell` always wins) and only when tmux is on `PATH`. |

### Theme axis: `[colors]` and `[phosphor]`

| Key                 | Meaning                                                                 |
| ------------------- | ------------------------------------------------------------------------ |
| `true_color`        | Whether cells use `[colors]` (true color) or the monochrome `[phosphor]` ramp. |
| `colors.default_fg` / `colors.default_bg` | Color for a cell with no explicit SGR color. |
| `colors.palette`    | The 16-entry ANSI table (0-7 normal, 8-15 bright) indexed SGR colors resolve against. |
| `phosphor.low` / `phosphor.high` | The monochrome intensity ramp's dim/bright ends. On a true-color theme, still drives the CRT chrome tint and cursor accent even though cell colors come from `[colors]`. |

### Preset axis: effects

| Table         | Key            | Meaning                                                              |
| ------------- | -------------- | ------------------------------------------------------------------------ |
| `[surface]`   | `radius`       | Block/border corner radius, in pixels.                               |
|               | `gradient`     | `0..1` top-lit/bottom-shaded sheen on block surfaces.                |
|               | `shadow`       | `0..1` soft drop shadow cast by block surfaces.                      |
| `[blur]`      | `radius`       | Gaussian bloom spread, in pixels, over block/border surfaces.        |
|               | `strength`     | `0..1` mix of the blurred result.                                    |
| `[cursor]`    | `shape`        | At-rest outline: `block` (default), `bar` (I-beam on the cell's left edge), or `underline`. The speed-reactive ball/tail morph layers on top of whichever is picked. |
|               | `radius`       | `0..1` at-rest corner rounding, as a fraction of the shape's own short half-dimension. |
|               | `glow`         | Cursor edge anti-aliasing width, in pixels.                          |
|               | `blink_style`  | How brightness pulses over time: `ease` (default, sine breathing), `static` (always fully lit), or `hard` (on/off toggle each half-period). Forced to `static` whenever `cursor.glass.enabled` is on. |
|               | `pulse_period` | Cursor breathing/toggle period, in seconds.                          |
| `[cursor.glass]` (experimental) | `enabled` | macOS-style frosted panel that refracts/blurs the scene behind the cursor instead of glowing over it, tinted toward the accent color. Forces `blink_style` to `static` and disables the ball/tail morph. |
|               | `tint`         | `0..1` accent strength mixed into the refracted sample.              |
|               | `blur`         | Refraction sample blur spread, in pixels.                            |
|               | `refract`      | Outward bend of the sampled scene, in pixels.                        |
|               | `opacity`      | `0..1` core opacity of the glass panel.                              |
| `[face]`      | `bg_tint`      | `0..1` faint phosphor-tinted background (the CRT "tube face" look).  |
|               | `inset_shadow` | `0..1` radial falloff toward the screen corners.                     |
| `[contrast]`  | `min_delta`    | Minimum enforced fg/bg brightness gap on the monochrome ramp, so isoluminant color pairs stay readable. |

### `[crt]`

Every field defaults to off (`0`) under the `modern` preset.

| Table            | Key            | Meaning                                                              |
| ---------------- | -------------- | ------------------------------------------------------------------------ |
| `[crt.curvature]`     | `amount`        | Barrel distortion toward a curved tube face. `0` flat, `~0.15` subtle, `~0.4` strong. |
| `[crt.scanlines]`     | `intensity`     | Darkening of alternating horizontal lines.                           |
|                       | `period`        | Device pixels per line-pair.                                         |
| `[crt.aberration]`    | `amount`        | Red/blue channel offset from center, in UV units (lens chromatic aberration). |
| `[crt.shadow_mask]`   | `intensity`     | Procedural RGB triad overlay (shadow-mask subpixel structure).       |
|                       | `cell_size`     | Device pixels per triad column.                                      |
| `[crt.noise]`         | `intensity`     | Per-pixel, per-frame brightness jitter (analog signal noise).        |
| `[crt.flicker]`       | `amount`        | Whole-screen brightness variation over time (unstable power supply). |
|                       | `speed`         | Flicker rate, roughly in Hz.                                         |
| `[crt.phosphor_decay]`| `decay_seconds` | Afterglow trail length behind recently-changed content.              |
| `[crt.aspect_ratio]`  | `width`/`height`| Letterboxed tube aspect ratio (e.g. `4`/`3`) — the physical tube shape, not the raw pixel-count ratio of the mode it displayed. |

## Themes vs. presets vs. profiles

- **Theme** and **preset** are the two axes above — always exactly one
  named built-in (or `"custom"`) each.
- **Profiles** are complete, named snapshots of the whole config,
  saved under `profiles/` next to `config.toml` (`tubeless config`'s
  `s`/`p`/`o` keys: save, profile-save, profile-load). Useful for
  switching between a few fully different setups rather than one
  theme/preset combination.

## Custom theme editor

Picking `theme = "custom"` (or hand-editing any `[colors]`/`[phosphor]`
field after selecting a built-in theme) leaves the current color
values in place instead of reseeding them — this is how the config
TUI's custom theme editor works: start from a built-in that's close,
then tweak.
