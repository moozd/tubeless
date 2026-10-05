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
  `text_glow`, `cursor`, `face`, `contrast`, and `crt`.

Setting either one seeds the fields it governs; hand-editing any of
those fields afterward (or through the config TUI) flips that axis's
name to `"custom"` so it's never silently reseeded later.

The axes never affect each other: picking a preset leaves your theme
alone, and picking a theme leaves your effects alone. A `preset` or
`theme` name that no longer exists (for example `preset = "nixie"` from
an older version) is logged and treated as `"custom"`: your own values
are kept and nothing is reseeded.

```toml
theme = "rosepine"
preset = "modern"
```

### Built-in themes

`rosepine` (default) · `rosepine-moon` · `gruvbox-dark-hard` · `nord` ·
`dracula` · `catppuccin-mocha` · `tokyo-night` · `one-dark` · `green` ·
`amber` · `green-p39` · `white-p4` · `cga` · `cyberpunk` · `neon` ·
`nixie` · `vfd` · `dmg` · `scope` · `ember` · `sepia` · `ultraviolet` ·
`rose-gold` · `seafoam` · `lavender-haze` · `sunset` · `sage` · `glacier` ·
`mulberry` · `gold-leaf` · `green-night` · `moss` · `emerald-noir` ·
`amber-night` · `bronze-night` · `ice-night` · `p4-night` · `crimson-night` ·
`msdos-blue` · `msdos-black` · `turbo-blue` · `pipboy` · `nostromo` · `wopr` ·
`matrix` · `replicant` · `tron` · `synthwave` · `c64` · `terminator` ·
`vertigo` · `ibm-3278` · `arcade` · `radar-p7`

The monochrome and fixed-palette themes (`green`, `amber`, `green-p39`,
`white-p4`, `cga`, `cyberpunk`) pair naturally with the monitor presets
below but never depend on them. `green-p39`, `white-p4`, `cga` and
`amber` are period-accurate phosphor and palette colors; `cyberpunk`
(neon cyan) is fictional. `neon` is a true-color rainbow theme: a
low-chroma default foreground and grays, with every hue slot a saturated
neon, so the `neon` preset's selective text glow lights up only colored
output.

**Palette themes** — chosen for their color. Each is a duotone: the dim
and bright ends of the ramp are two different, paired hues, so text
shades through a gradient (dusty rose into warm gold) instead of one
color at varying brightness: `rose-gold`, `seafoam`, `lavender-haze`,
`sunset`, `sage`, `glacier`, `mulberry`, `gold-leaf`.

**Night themes** — darker takes for late hours, with a bright end at or
under 80% so nothing glares: `green-night`, `moss`, `emerald-noir`,
`amber-night`, `bronze-night`, `ice-night`, `p4-night`, `crimson-night`.

**Hardware themes** — `nixie`, `vfd`, `dmg`, `scope`, `ember`, `sepia`,
`ultraviolet`: the glow colors of a nixie tube, vacuum-fluorescent
display, handheld LCD, scope tube, red LED readout, thermal paper and a
fictional blacklight tube.

**MS-DOS themes** — true-color themes on the real 16-color VGA text
palette: `msdos-blue` (light gray on VGA blue), `msdos-black` (light
gray on black) and `turbo-blue` (yellow on blue with a cyan accent, a
Borland-IDE look). Being true-color themes they ignore `[monochrome]`
and `true_color` is switched on when you pick them.

**Screen themes** — inspired by famous screens and other retro terminal
emulators (cool-retro-term's community themes were the color
reference): `pipboy`, `nostromo`, `wopr`, `matrix`, `replicant`,
`tron`, `synthwave`, `c64`, `terminator`, `vertigo`, `ibm-3278`,
`arcade`, `radar-p7`.

All of the monochrome themes render under whatever `[monochrome]` mode
is set (it is outside both axes) — with `spectrum`, part of each cell's
real color is mixed back in, so e.g. directory blues show through a red
or orange theme.

### Built-in presets

Presets are effects only. Every one of them works with every theme.

| Preset              | Look |
| ------------------- | ---- |
| `modern`            | Every CRT effect off (the default). |
| `daily-driver`      | The author's own look: soft bloom, medium scanlines, deep inset shadow. |
| `ibm-5151`          | IBM 5151 monochrome data display: sharp, long persistence. Pairs with `green-p39`. |
| `ibm-5153`          | IBM 5153 digital RGB monitor: shadow mask. Pairs with `cga`. |
| `zenith-zvm-1220`   | Zenith ZVM-1220 amber monitor. Pairs with `amber`. |
| `apple-monitor-iii` | Apple Monitor III soft composite white. Pairs with `white-p4`. |
| `commodore-1084s`   | Commodore 1084S analog RGB: shadow mask, soft. |
| `princeton-hx12`    | Princeton HX-12 analog RGB, a higher quality tier. |
| `cyberpunk`         | A sharp, modern digital feel, not a real monitor. Pairs with `cyberpunk`. |
| `gba`               | Game Boy Advance LCD: pixel-grid mesh, flat reflective face, slight ghosting. Pairs with `dmg`. |
| `neon`              | A neon-sign look: selective text glow, low block bloom, a faint RGB fringe and scanlines. Pairs with `neon`. |

Each `ibm-*`/`zenith-*`/`apple-*`/`commodore-*`/`princeton-*` preset
models one real 1980s monitor's curvature, phosphor persistence, noise,
and (where applicable) shadow-mask convergence error, tuned from that
display's own service manual. "Pairs with" is only a suggestion: pick
the theme yourself.

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

### `[shell]`

| Key       | Type   | Default | Meaning                                                                 |
| --------- | ------ | ------- | ------------------------------------------------------------------------ |
| `program` | string | `""`    | Which installed shell to launch into. Empty (`"auto"` in the config TUI) follows `$SHELL`. `"tmux"` attaches to (or creates) a fixed session named `home` instead of a plain login shell, only when tmux is on `PATH`. Any other value names a shell resolved via `PATH` — the config TUI's shell row lists what's actually installed on this machine. Only applies on the `$SHELL` auto-detect path (an explicit `--shell` always wins). |

### Theme axis: `[colors]` and `[phosphor]`

| Key                 | Meaning                                                                 |
| ------------------- | ------------------------------------------------------------------------ |
| `true_color`        | Whether cells use `[colors]` (true color) or the monochrome `[phosphor]` ramp. |
| `colors.default_fg` / `colors.default_bg` | Color for a cell with no explicit SGR color. |
| `colors.palette`    | The 16-entry ANSI table (0-7 normal, 8-15 bright) indexed SGR colors resolve against. |
| `phosphor.low` / `phosphor.high` | The monochrome intensity ramp's dim/bright ends. On a true-color theme, still drives the CRT chrome tint and cursor accent even though cell colors come from `[colors]`. |

### `[monochrome]`

Only affects cells when `true_color = false`. Sits outside both the theme
and preset axes — a rendering preference the user sets once, not reseeded
by switching theme or preset.

| Key          | Type   | Default    | Meaning                                                              |
| ------------ | ------ | ---------- | ------------------------------------------------------------------------ |
| `mode`       | string | `"binary"` | `"binary"`: a cell whose real fg/bg colors land too close together once collapsed onto the ramp (see `[contrast].min_delta`) hard-inverts to solid black/phosphor-peak — always legible, but flattens syntax highlighting to on/off blocks (the common case under an app like Neovim with `termguicolors`, which paints an explicit background on nearly every cell). `"shades"`: real colors quantize onto a small number of discrete accent-color brightness levels instead (see `steps`/`hue_weight`), so syntax highlighting keeps reading as relative brightness while any two adjacent levels stay legible stacked as fg-on-bg. `"spectrum"`: `"shades"`'s brightness ladder linearly mixed toward each cell's own real color (see `amount`) — `0` is the plain ramp (identical to `"shades"` with `hue_weight` pinned to `0`), `1` is the real color unmodified (the same passthrough `true_color` itself uses), so different colors read as genuinely different colors instead of only different brightnesses. |
| `steps`      | int    | `0` (→ 16) | `"shades"`/`"spectrum"` only: how many discrete brightness levels real colors quantize onto, evenly spaced in perceived lightness across the ramp. `0` uses the built-in default (16 — see pkg/render's `defaultShadeSteps` for why this many). |
| `hue_weight` | float  | `-1` (→ 0.6) | `"shades"` only (ignored by `"spectrum"`, which mixes in real color directly via `amount` instead): how much brightness leans on a color's closeness to the theme's own accent hue, on top of its real luminance — `0` is luminance only, `1` is hue-closeness only. Negative uses the built-in default (0.6 — luminance alone is usually too compressed a range to separate real syntax-highlight colors on its own, but a too-high weight can override a genuine luminance difference like a comment being dimmer than body text; see pkg/render's `defaultShadeHueWeight`); `0` is itself a valid explicit choice. |
| `amount`     | float  | `-1` (→ 0.4) | `"spectrum"` only: how far a cell's rendered color mixes from the plain phosphor ramp (`0`) toward its own real color (`1`). Negative uses the built-in default (0.4 — an untuned first estimate, unlike the other constants above; see pkg/render's `defaultSpectrumAmount`); `0` is itself a valid explicit choice. |

### Preset axis: effects

| Table         | Key            | Meaning                                                              |
| ------------- | -------------- | ------------------------------------------------------------------------ |
| `[surface]`   | `radius`       | Block/border corner radius, in pixels.                               |
|               | `gradient`     | `0..1` top-lit/bottom-shaded sheen on block surfaces.                |
|               | `shadow`       | `0..1` soft drop shadow cast by block surfaces.                      |
| `[blur]`      | `radius`       | Gaussian bloom spread, in pixels, over block/border surfaces.        |
|               | `strength`     | `0..1` mix of the blurred result.                                    |
| `[text_glow]` | `radius`       | Gaussian spread, in pixels, of a halo around text in each glyph's own color. |
|               | `strength`     | `0..1` halo intensity. `0` (every preset but `neon`) is off.         |
|               | `threshold`    | How colorful a glyph's foreground must be to glow (linear RGB max-min, `0` gray .. `1` pure primary; ramps to full over the next `0.25`). `0` glows all text; higher keeps neutral text crisp. |
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
| `[crt.pixel_grid]`    | `intensity`     | Dark gaps between pixel cells on both axes (LCD screen-door look). |
|                       | `cell_size`     | Device pixels per pixel cell.                                        |
|                       | `gap`           | Border width as a fraction of a cell, `0..0.5`.                      |
| `[crt.noise]`         | `intensity`     | Per-pixel, per-frame brightness jitter (analog signal noise).        |
| `[crt.flicker]`       | `amount`        | Whole-screen brightness variation over time (unstable power supply). |
|                       | `speed`         | Flicker rate, roughly in Hz.                                         |
| `[crt.phosphor_decay]`| `decay_seconds` | Afterglow trail length behind recently-changed content.              |
| `[crt.aspect_ratio]`  | `width`/`height`| Letterboxed tube aspect ratio (e.g. `4`/`3`) — the physical tube shape, not the raw pixel-count ratio of the mode it displayed. |

## Themes vs. presets

**Theme** and **preset** are the two axes above — always exactly one
named built-in (or `"custom"`) each. `config.toml` is the single source
of truth: `tubeless config`'s `s` key writes straight to it, and the
running `tubeless` host watches that file and picks up any change live —
there's no separate profile/switch concept to manage on top of it.

## Custom theme editor

Picking `theme = "custom"` (or hand-editing any `[colors]`/`[phosphor]`
field after selecting a built-in theme) leaves the current color
values in place instead of reseeding them — this is how the config
TUI's custom theme editor works: start from a built-in that's close,
then tweak.
