package config

// The retro palettes (themes) and the daily-driver effects preset.
// Palettes are colors only: any of them pairs with any effects preset,
// and none of them turns on an effect by itself.

// retroEffects is the daily driver's starting point: modern's chrome
// with a slimmer cursor.
func retroEffects() Effects {
	e := modernEffects()
	e.Cursor.Radius = 0.2
	return e
}

// dailyDriverEffects is the author's own everyday look, exactly: a soft
// amber-monitor-style bloom, medium scanlines and a deep inset shadow,
// with every other CRT effect off.
func dailyDriverEffects() Effects {
	e := retroEffects()
	e.Face = Face{BgTint: 0.06, InsetShadow: 0.48}
	e.Blur = Blur{Radius: 2.5, Strength: 0.6}
	e.CRT = CRT{Scanlines: Scanlines{Intensity: 0.45, Period: 3.5}}
	return e
}

// nixieTheme is neon-orange: a nixie tube's glow is excited neon, a
// saturated orange-red near 598nm — more red than amberTheme's gold.
func nixieTheme() ThemeColors {
	x, y := dominantWavelengthChromaticity(598, 0.92)
	peak := phosphorColor(x, y)
	return ThemeColors{Phosphor: Phosphor{Low: scale3(peak, 0.4), High: peak}}
}

// vfdTheme is the cool aqua-green of a vacuum-fluorescent display, a
// blue-green near 497nm at moderate purity (VFD glass is pale, not a
// laser).
func vfdTheme() ThemeColors {
	x, y := dominantWavelengthChromaticity(497, 0.55)
	peak := phosphorColor(x, y)
	return ThemeColors{Phosphor: Phosphor{Low: scale3(peak, 0.4), High: peak}}
}

// dmgTheme is the olive-to-lime ramp of a handheld LCD: not a phosphor
// but a reflective panel, so the dim end is a dark olive and the bright
// end a pale yellow-green (hand-picked from the original Game Boy's
// four-shade palette, stretched so the peak channel is full).
func dmgTheme() ThemeColors {
	return ThemeColors{Phosphor: Phosphor{
		Low:  srgb3(0x4a, 0x78, 0x1e),
		High: srgb3(0xc8, 0xf0, 0x30),
	}}
}

// scopeTheme is a radar or oscilloscope tube's cyan-blue, near 480nm.
func scopeTheme() ThemeColors {
	x, y := dominantWavelengthChromaticity(480, 0.8)
	peak := phosphorColor(x, y)
	return ThemeColors{Phosphor: Phosphor{Low: scale3(peak, 0.4), High: peak}}
}

// emberTheme is a red LED's deep, nearly pure red near 628nm.
func emberTheme() ThemeColors {
	x, y := dominantWavelengthChromaticity(628, 0.97)
	peak := phosphorColor(x, y)
	return ThemeColors{Phosphor: Phosphor{Low: scale3(peak, 0.45), High: peak}}
}

// sepiaTheme is a warm cream-white, around 3500K — thermal paper or a
// teletype page, not a cool P4 white (compare whiteP4Theme).
func sepiaTheme() ThemeColors {
	peak := phosphorColor(0.405, 0.385)
	return ThemeColors{Phosphor: Phosphor{Low: scale3(peak, 0.42), High: peak}}
}

// ultravioletTheme is a violet-magenta from the purple line (no single
// wavelength makes it, so it is a chromaticity picked directly), the
// retro-futurist neon of a blacklight poster.
func ultravioletTheme() ThemeColors {
	peak := phosphorColor(0.27, 0.12)
	return ThemeColors{Phosphor: Phosphor{Low: scale3(peak, 0.4), High: peak}}
}

// The palette set: themes chosen for their color. Each is a duotone —
// the dim end and the bright end of the ramp are two different,
// carefully paired hues (picked by eye, not derived from any phosphor),
// so text shades through a gradient (a dusty rose into warm gold, say)
// instead of one color at varying brightness.

// duotone builds a monochrome theme from two sRGB colors: the dim end
// and the bright end of the ramp.
func duotone(low, high [3]int) ThemeColors {
	return ThemeColors{Phosphor: Phosphor{
		Low:  srgb3(low[0], low[1], low[2]),
		High: srgb3(high[0], high[1], high[2]),
	}}
}

// roseGoldTheme runs dusty mauve into warm peach-gold.
func roseGoldTheme() ThemeColors { return duotone([3]int{0x8a, 0x4f, 0x5a}, [3]int{0xff, 0xd2, 0xb8}) }

// seafoamTheme runs deep teal into pale mint, a 1960s aqua.
func seafoamTheme() ThemeColors { return duotone([3]int{0x1f, 0x7a, 0x6e}, [3]int{0xb8, 0xff, 0xe3}) }

// lavenderHazeTheme runs soft violet into pale lilac.
func lavenderHazeTheme() ThemeColors {
	return duotone([3]int{0x6a, 0x55, 0xa8}, [3]int{0xe6, 0xd2, 0xff})
}

// sunsetTheme runs crimson-coral into golden yellow.
func sunsetTheme() ThemeColors { return duotone([3]int{0xb0, 0x3a, 0x48}, [3]int{0xff, 0xc8, 0x57}) }

// sageTheme runs muted sage green into warm cream.
func sageTheme() ThemeColors { return duotone([3]int{0x5b, 0x7a, 0x5a}, [3]int{0xf0, 0xf5, 0xd8}) }

// glacierTheme runs deep cobalt into ice white-blue.
func glacierTheme() ThemeColors { return duotone([3]int{0x2f, 0x4f, 0xa0}, [3]int{0xcf, 0xe8, 0xff}) }

// mulberryTheme runs plum into bubblegum pink.
func mulberryTheme() ThemeColors { return duotone([3]int{0x7a, 0x2d, 0x6e}, [3]int{0xff, 0xa8, 0xe0}) }

// goldLeafTheme runs dark bronze into bright gold.
func goldLeafTheme() ThemeColors { return duotone([3]int{0x7a, 0x5a, 0x14}, [3]int{0xff, 0xe0, 0x7a}) }

// The night set: darker takes on the retro palettes. Dimmer text (the
// bright end of the ramp stays at or under 80% so nothing glares) —
// the screen you want at 2am.

// nightPhosphor is a phosphorColor-style theme dimmed for night use.
func nightPhosphor(peak [3]float32) ThemeColors {
	return ThemeColors{Phosphor: Phosphor{Low: scale3(peak, 0.24), High: scale3(peak, 0.8)}}
}

// greenNightTheme is the classic P1 green, dimmed.
func greenNightTheme() ThemeColors { return nightPhosphor(phosphorColor(0.21, 0.71)) }

// mossTheme runs deep forest green into soft mint.
func mossTheme() ThemeColors { return duotone([3]int{0x16, 0x3a, 0x1c}, [3]int{0x7a, 0xd6, 0x8a}) }

// emeraldNoirTheme runs deep pine into emerald.
func emeraldNoirTheme() ThemeColors {
	return duotone([3]int{0x0b, 0x3d, 0x2e}, [3]int{0x52, 0xe0, 0xa4})
}

// amberNightTheme is amber, dimmed to a deep, warm orange-gold.
func amberNightTheme() ThemeColors {
	x, y := dominantWavelengthChromaticity(590, 0.8)
	return nightPhosphor(phosphorColor(x, y))
}

// bronzeNightTheme runs dark brown into burnt orange.
func bronzeNightTheme() ThemeColors {
	return duotone([3]int{0x3a, 0x25, 0x10}, [3]int{0xe0, 0x9a, 0x4a})
}

// iceNightTheme runs midnight blue into pale sky.
func iceNightTheme() ThemeColors { return duotone([3]int{0x10, 0x2a, 0x48}, [3]int{0x68, 0xb4, 0xe0}) }

// p4NightTheme is the cool monochrome TV white, dimmed to a soft gray.
func p4NightTheme() ThemeColors { return nightPhosphor(phosphorColor(0.283, 0.297)) }

// crimsonNightTheme runs oxblood into a muted red.
func crimsonNightTheme() ThemeColors {
	return duotone([3]int{0x3a, 0x0e, 0x18}, [3]int{0xe0, 0x50, 0x60})
}

// The MS-DOS set: true-color themes on the real VGA text palette (the
// standard 16 colors every PC's text mode drew with).

// vgaPalette is the standard VGA text-mode 16-color palette.
func vgaPalette() [16][3]float32 {
	return [16][3]float32{
		srgb3(0x00, 0x00, 0x00), srgb3(0x00, 0x00, 0xaa), srgb3(0x00, 0xaa, 0x00), srgb3(0x00, 0xaa, 0xaa),
		srgb3(0xaa, 0x00, 0x00), srgb3(0xaa, 0x00, 0xaa), srgb3(0xaa, 0x55, 0x00), srgb3(0xaa, 0xaa, 0xaa),
		srgb3(0x55, 0x55, 0x55), srgb3(0x55, 0x55, 0xff), srgb3(0x55, 0xff, 0x55), srgb3(0x55, 0xff, 0xff),
		srgb3(0xff, 0x55, 0x55), srgb3(0xff, 0x55, 0xff), srgb3(0xff, 0xff, 0x55), srgb3(0xff, 0xff, 0xff),
	}
}

// msdosBlueTheme is the blue-screen DOS look: light gray on VGA blue.
func msdosBlueTheme() ThemeColors {
	return trueColorTheme(srgb3(0x00, 0x00, 0xaa), srgb3(0xaa, 0xaa, 0xaa), srgb3(0xaa, 0xaa, 0xaa), vgaPalette())
}

// msdosBlackTheme is the plain DOS prompt: light gray on black.
func msdosBlackTheme() ThemeColors {
	return trueColorTheme(srgb3(0x00, 0x00, 0x00), srgb3(0xaa, 0xaa, 0xaa), srgb3(0xaa, 0xaa, 0xaa), vgaPalette())
}

// turboBlueTheme is a Borland-IDE blue: yellow text on VGA blue with a
// cyan accent.
func turboBlueTheme() ThemeColors {
	return trueColorTheme(srgb3(0x00, 0x00, 0xaa), srgb3(0xff, 0xff, 0x55), srgb3(0x55, 0xff, 0xff), vgaPalette())
}

// The screen set: palettes inspired by famous screens — the terminals
// and HUDs of films, games and other retro terminal emulators
// (cool-retro-term's community themes were the colour reference:
// Pip-Boy #1aff80, Matrix #5efaac, MU/TH/UR #41e64e, WOPR #33ff33, Blade
// Runner #ff6a00, Tron #6fc3df, Synthwave #ff71ce, C64 #7664d9, Vertigo
// #aaff7f, IBM 3278 #3399ff, Atari arcade #ffcc00).

// pipboyTheme is Fallout's wrist computer: spring green.
func pipboyTheme() ThemeColors { return duotone([3]int{0x0b, 0x55, 0x30}, [3]int{0x1a, 0xff, 0x80}) }

// nostromoTheme is the freighter's MU/TH/UR terminal: a dark, dirty
// industrial green.
func nostromoTheme() ThemeColors { return duotone([3]int{0x05, 0x34, 0x14}, [3]int{0x41, 0xe6, 0x4e}) }

// woprTheme is the WarGames mainframe: a vivid 1983 terminal green.
func woprTheme() ThemeColors { return duotone([3]int{0x0c, 0x66, 0x0c}, [3]int{0x33, 0xff, 0x33}) }

// matrixTheme is falling code: a cool mint-emerald.
func matrixTheme() ThemeColors { return duotone([3]int{0x0a, 0x4a, 0x38}, [3]int{0x5e, 0xfa, 0xac}) }

// replicantTheme is a Blade Runner tungsten orange over rust.
func replicantTheme() ThemeColors { return duotone([3]int{0x6a, 0x1a, 0x00}, [3]int{0xff, 0x6a, 0x00}) }

// tronTheme is the Grid's ice-cyan, deep blue into pale glowing cyan.
func tronTheme() ThemeColors { return duotone([3]int{0x0e, 0x3a, 0x5a}, [3]int{0x8f, 0xdf, 0xff}) }

// synthwaveTheme is pink neon over a purple glass: the dim end is
// purple, so the face tint washes the whole screen violet.
func synthwaveTheme() ThemeColors { return duotone([3]int{0x4a, 0x1a, 0x6a}, [3]int{0xff, 0x71, 0xce}) }

// c64Theme is a home computer's periwinkle on blue: the dim end is the
// deep blue the face tint turns into the famous blue screen.
func c64Theme() ThemeColors { return duotone([3]int{0x40, 0x31, 0x8d}, [3]int{0xb8, 0xaf, 0xff}) }

// terminatorTheme is a machine's-eye-view HUD: pale pink-white over a
// deep red wash.
func terminatorTheme() ThemeColors {
	return duotone([3]int{0x80, 0x00, 0x08}, [3]int{0xff, 0xd0, 0xd0})
}

// vertigoTheme is a lime-yellow green, brighter and warmer than P1.
func vertigoTheme() ThemeColors { return duotone([3]int{0x2a, 0x5a, 0x10}, [3]int{0xaa, 0xff, 0x7f}) }

// ibm3278Theme is the mainframe terminal's cool phosphor blue.
func ibm3278Theme() ThemeColors { return duotone([3]int{0x0c, 0x2a, 0x66}, [3]int{0x33, 0x99, 0xff}) }

// arcadeTheme is a cabinet's hot gold-yellow over bronze.
func arcadeTheme() ThemeColors { return duotone([3]int{0x5a, 0x3a, 0x00}, [3]int{0xff, 0xcc, 0x00}) }

// radarP7Theme follows the P7 phosphor of long-persistence radar and
// scope tubes: a blue-white flash fading through yellow-green. The dim
// end is that yellow-green, the bright end the blue-white.
func radarP7Theme() ThemeColors { return duotone([3]int{0x55, 0x7a, 0x10}, [3]int{0xcf, 0xe8, 0xff}) }
