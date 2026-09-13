package config

// Effects is the non-color, non-font visual-effect axis of a look — see
// Config's own doc comment for how this differs from ThemeColors.
// EffectsPreset seeds it from a named preset; the config TUI is what
// flips Config.Preset to "custom" the instant any of these fields is
// edited by hand, so this package only ever hands back a preset's
// *starting* values.
type Effects struct {
	Blur     Blur
	Rounding Rounding
	Cursor   Cursor
	Face     Face
	Contrast Contrast
	CRT      CRT
}

// EffectsPresetNames lists every registered effects preset, in the order
// the config TUI's preset cycler steps through them — modern (the
// CRT-off default) followed by six 80s monitors spanning the look's
// range: a sharp monochrome data display, a shadow-mask digital RGB
// monitor, a soft composite monochrome monitor, a long-persistence white
// monochrome monitor, and two shadow-mask analog RGB monitors at
// different quality tiers.
func EffectsPresetNames() []string {
	return []string{
		"modern", "ibm-5151", "ibm-5153", "zenith-zvm-1220",
		"apple-monitor-iii", "commodore-1084s", "princeton-hx12",
	}
}

// EffectsPreset returns the effect values a named preset seeds. Unknown
// or empty names fall back to "modern".
func EffectsPreset(name string) Effects {
	switch name {
	case "ibm-5151":
		return ibm5151Effects()
	case "ibm-5153":
		return ibm5153Effects()
	case "zenith-zvm-1220":
		return zenithZVM1220Effects()
	case "apple-monitor-iii":
		return appleMonitorIIIEffects()
	case "commodore-1084s":
		return commodore1084SEffects()
	case "princeton-hx12":
		return princetonHX12Effects()
	default:
		return modernEffects()
	}
}

// MonitorTheme reports the color theme a named effects preset is
// authentic with, when one exists — a monochrome monitor's phosphor
// color (or a fixed-palette digital monitor's native palette) is as much
// a part of "being that monitor" as its scanlines, so selecting one of
// these in the config TUI switches Theme the same moment it switches
// Preset. An analog RGB monitor with no fixed native palette
// (commodore-1084s, princeton-hx12) reports ok=false and leaves the
// active theme alone — it rendered whatever the host computer sent it,
// same as any TrueColor theme does today.
func MonitorTheme(presetName string) (theme string, ok bool) {
	switch presetName {
	case "ibm-5151":
		return "green-p39", true
	case "ibm-5153":
		return "cga", true
	case "zenith-zvm-1220":
		return "amber", true
	case "apple-monitor-iii":
		return "white-p4", true
	default:
		return "", false
	}
}

// modernEffects is the default look: every CRT emulation effect off,
// the same hand-tuned chrome baseline every TrueColor theme originally
// shipped with.
func modernEffects() Effects {
	return Effects{
		Blur:     Blur{Radius: 2.0, Strength: 0.5},
		Rounding: Rounding{Radius: 2.0},
		Cursor:   Cursor{Glow: 1.5, PulsePeriod: 0.9},
		Face:     Face{BgTint: 0.04, InsetShadow: 0.3},
		Contrast: Contrast{MinDelta: 0.35},
	}
}

// ibm5151Effects models the IBM 5151 Monochrome Display (1981): a 12"
// TTL digital monitor driven directly by the MDA card's own 18.432kHz
// horizontal/50Hz vertical drive (no composite decoding, so no signal
// noise), 720x350 text-mode resolution, and no shadow mask — a single
// electron gun needs no color convergence, so aberration/shadow-mask
// both stay at 0. Its P39 phosphor is documented as long-persistence
// ("high persistence... causes smearing when the image changes") and
// very saturated, modeled here as a longer decay than every other
// preset. InsetShadow is pushed well past the modern baseline (0.3)
// to sell the barrel curvature visually — a flatter falloff reads as a
// framed rectangle even with Curvature bowing the geometry. Sources:
// Wikipedia "IBM 5151", radiomuseum.org's 5151 entry (350 scanlines,
// P39, 50Hz).
func ibm5151Effects() Effects {
	e := modernEffects()
	e.Face = Face{BgTint: 0.045, InsetShadow: 0.38}
	e.Blur = Blur{Radius: 1.5, Strength: 0.4}
	e.CRT = CRT{
		Curvature:     Curvature{Amount: 0.10},
		Scanlines:     Scanlines{Intensity: 0.30, Period: 2.0},
		Noise:         Noise{Intensity: 0.02},
		Flicker:       Flicker{Amount: 0.08, Speed: 50},
		PhosphorDecay: PhosphorDecay{DecaySeconds: 0.35},
		// 4:3 — the standard CRT tube shape of the era, not the raw
		// 720x350 text-mode pixel grid: MDA's pixels were deliberately
		// non-square (tall and thin) so that grid would *display* as 4:3
		// on the actual tube, not the wide rectangle a naive pixel-count
		// ratio would draw.
		AspectRatio: AspectRatio{Width: 4, Height: 3},
	}
	return e
}

// ibm5153Effects models the IBM 5153 Color Display (1983): a 13" shadow-
// mask CGA monitor, 0.39mm dot pitch, digital RGBI (TTL, not composite —
// clean signal, low noise), 640x200. Its shadow mask brings a little
// convergence-driven aberration that no monochrome monitor here has.
// InsetShadow raised well past modern's 0.3 to read as a genuinely
// curved 13" consumer tube. Sources: dfarq.homeip.net's "First
// generation IBM PC monitors", int10h.org's IBM 5153 true-palette
// writeup (dot pitch, resolution).
func ibm5153Effects() Effects {
	e := modernEffects()
	e.Face = Face{BgTint: 0.05, InsetShadow: 0.42}
	e.Blur = Blur{Radius: 1.8, Strength: 0.45}
	e.CRT = CRT{
		Curvature:     Curvature{Amount: 0.13},
		Scanlines:     Scanlines{Intensity: 0.40, Period: 3.0},
		Aberration:    Aberration{Amount: 0.004},
		ShadowMask:    ShadowMask{Intensity: 0.35, CellSize: 3.0},
		Noise:         Noise{Intensity: 0.02},
		Flicker:       Flicker{Amount: 0.05, Speed: 60},
		PhosphorDecay: PhosphorDecay{DecaySeconds: 0.05},
		// 4:3 tube shape — see ibm5151Effects' AspectRatio comment; CGA's
		// 640x200 pixel grid is non-square for the same reason MDA's is.
		AspectRatio: AspectRatio{Width: 4, Height: 3},
	}
	return e
}

// zenithZVM1220Effects models the Zenith ZVM-1220 (1985 service manual):
// a 12" amber composite monitor, NTSC composite sync input (unlike the
// IBM monitors' direct digital drive — a composite signal carries more
// analog noise and is bandwidth-limited to the monitor's documented
// 18MHz/50ns rise time, modeled as extra blur), no shadow mask (single
// beam). Amber phosphors of this era are medium-persistence — shorter
// than IBM's long-persistence green P39, longer than a color shadow-mask
// tube's P22. InsetShadow pushed the highest of any preset here — the
// most curved and least corrected of these tubes. Source: Zenith's
// ZVM-1220/1230 service manual (bitsavers).
func zenithZVM1220Effects() Effects {
	e := modernEffects()
	e.Face = Face{BgTint: 0.06, InsetShadow: 0.46}
	e.Blur = Blur{Radius: 2.5, Strength: 0.6}
	e.CRT = CRT{
		Curvature:     Curvature{Amount: 0.14},
		Scanlines:     Scanlines{Intensity: 0.45, Period: 3.5},
		Noise:         Noise{Intensity: 0.05},
		Flicker:       Flicker{Amount: 0.10, Speed: 60},
		PhosphorDecay: PhosphorDecay{DecaySeconds: 0.18},
		// 4:3 — no pixel resolution is documented for this composite
		// monitor, but the tube shape itself needs no per-monitor spec:
		// every CRT monitor and TV of this era, composite or digital,
		// was 4:3.
		AspectRatio: AspectRatio{Width: 4, Height: 3},
	}
	return e
}

// appleMonitorIIIEffects models the Apple Monitor III's white-phosphor
// variant (A3M0006, 1983): a 12" monochrome monitor with a fine anti-
// glare mesh over the tube (modeled as a touch more face tint/inset
// shadow than the baseline) and a documented "very slow phosphor
// refresh" that "adversely created a ghosting effect with any video
// movement" — the longest persistence of any preset here, longer even
// than the 5151's already-long P39. No shadow mask (single beam). Its
// viewable area is documented as 560x162 pixels.
// Source: Wikipedia "Apple Monitor III".
func appleMonitorIIIEffects() Effects {
	e := modernEffects()
	e.Face = Face{BgTint: 0.06, InsetShadow: 0.40}
	e.Blur = Blur{Radius: 1.6, Strength: 0.35}
	e.CRT = CRT{
		Curvature:     Curvature{Amount: 0.09},
		Scanlines:     Scanlines{Intensity: 0.30, Period: 2.2},
		Noise:         Noise{Intensity: 0.015},
		Flicker:       Flicker{Amount: 0.06, Speed: 50},
		PhosphorDecay: PhosphorDecay{DecaySeconds: 0.45},
		// 4:3 tube shape — see ibm5151Effects' AspectRatio comment.
		AspectRatio: AspectRatio{Width: 4, Height: 3},
	}
	return e
}

// commodore1084SEffects models the Commodore 1084S (1985): a 14" (13"
// viewable) shadow-mask RGB/composite monitor with a 0.42mm slotted
// triplet dot pitch — coarser than the IBM 5153's 0.39mm, so its
// shadow-mask cell reads a little larger here — and both RGB/RGBI and
// composite inputs (Amiga/C64 owners frequently ran it over composite,
// hence a bit more signal noise than the digital-only IBM monitors).
// Source: bigbookofamigahardware.com's 1084S entry (640x200/640x400i,
// used here in its primary non-interlaced 640x200 mode).
func commodore1084SEffects() Effects {
	e := modernEffects()
	e.Face = Face{BgTint: 0.05, InsetShadow: 0.40}
	e.Blur = Blur{Radius: 2.0, Strength: 0.5}
	e.CRT = CRT{
		Curvature:     Curvature{Amount: 0.12},
		Scanlines:     Scanlines{Intensity: 0.35, Period: 3.0},
		Aberration:    Aberration{Amount: 0.005},
		ShadowMask:    ShadowMask{Intensity: 0.30, CellSize: 3.4},
		Noise:         Noise{Intensity: 0.04},
		Flicker:       Flicker{Amount: 0.07, Speed: 50},
		PhosphorDecay: PhosphorDecay{DecaySeconds: 0.05},
		// 4:3 tube shape — see ibm5151Effects' AspectRatio comment.
		AspectRatio: AspectRatio{Width: 4, Height: 3},
	}
	return e
}

// princetonHX12Effects models the Princeton Graphic Systems HX-12
// (1985): a CGA/EGA-compatible analog RGB monitor built around an NEC
// tube with a notably fine 0.31mm dot pitch and 15MHz bandwidth — the
// sharpest, cleanest picture of any color monitor here, so the finest
// shadow-mask cell and the least blur among the shadow-mask presets.
// Source: minuszerodegrees.net's HX-12 service manual, vcfed.org forum
// listings for the HX-12/HX-12E (690x240 interlaced).
func princetonHX12Effects() Effects {
	e := modernEffects()
	e.Face = Face{BgTint: 0.04, InsetShadow: 0.35}
	e.Blur = Blur{Radius: 1.4, Strength: 0.35}
	e.CRT = CRT{
		Curvature:     Curvature{Amount: 0.10},
		Scanlines:     Scanlines{Intensity: 0.30, Period: 2.0},
		Aberration:    Aberration{Amount: 0.003},
		ShadowMask:    ShadowMask{Intensity: 0.25, CellSize: 2.4},
		Noise:         Noise{Intensity: 0.02},
		Flicker:       Flicker{Amount: 0.05, Speed: 60},
		PhosphorDecay: PhosphorDecay{DecaySeconds: 0.04},
		// 4:3 tube shape — see ibm5151Effects' AspectRatio comment.
		AspectRatio: AspectRatio{Width: 4, Height: 3},
	}
	return e
}

// greenP39Theme is the color companion to ibm-5151 (see MonitorTheme):
// IBM's own documentation names the phosphor P39 (Zn2SiO4:Mn,As) and
// describes it as "very saturated" and "dark green" — distinctly more
// saturated than the P1 scope-green greenTheme already models — but its
// JEDEC-registered CIE xy sits behind the paywalled TEP116 standard, so
// (following the same defensible-construction approach as amberTheme)
// this is built from a dominant wavelength placed slightly further from
// P1's cyan-leaning 525-528nm toward true green, at high purity for the
// documented saturation.
func greenP39Theme() ThemeColors {
	x, y := dominantWavelengthChromaticity(530, 0.88)
	peak := phosphorColor(x, y)
	return ThemeColors{Phosphor: Phosphor{Low: scale3(peak, 0.38), High: peak}}
}

// whiteP4Theme is the color companion to apple-monitor-iii (see
// MonitorTheme). P4 (the standard black-and-white TV/data-monitor
// phosphor blend) is widely documented in CRT restoration references as
// a cool, blue-leaning white rather than a neutral D65 white — no public
// source gives its JEDEC-registered xy, so this uses a commonly cited
// ~9300K cool-white point, distinctly bluer than D65's (0.3127, 0.3290),
// as a defensible approximation of that description.
func whiteP4Theme() ThemeColors {
	peak := phosphorColor(0.283, 0.297)
	return ThemeColors{Phosphor: Phosphor{Low: scale3(peak, 0.45), High: peak}}
}

// cgaTheme is the color companion to ibm-5153 (see MonitorTheme): the
// real measured 16-color CGA output of an actual IBM 5153, from
// engineer Dr. Hugo Holden's gun-amplifier voltage measurements
// (int10h.org, "The IBM 5153's True CGA Palette and Color Output") —
// not the naive idealized 0/0xAA/0xFF palette most emulators use, which
// that article documents as measurably wrong (most visibly on color 6,
// brown, a 36% rather than 50% green reduction). Default fg/bg (light
// gray on black) match real CGA text mode's default attribute; the
// accent is CGA's bright cyan, index 11.
func cgaTheme() ThemeColors {
	return trueColorTheme(
		srgb3(0x00, 0x00, 0x00), // bg: black
		srgb3(0xc4, 0xc4, 0xc4), // fg: light gray (CGA text default)
		srgb3(0x4e, 0xf3, 0xf3), // accent: bright cyan
		[16][3]float32{
			srgb3(0x00, 0x00, 0x00), srgb3(0x00, 0x00, 0xc4), srgb3(0x00, 0xc4, 0x00), srgb3(0x00, 0xc4, 0xc4),
			srgb3(0xc4, 0x00, 0x00), srgb3(0xc4, 0x00, 0xc4), srgb3(0xc4, 0x7e, 0x00), srgb3(0xc4, 0xc4, 0xc4),
			srgb3(0x4e, 0x4e, 0x4e), srgb3(0x4e, 0x4e, 0xdc), srgb3(0x4e, 0xdc, 0x4e), srgb3(0x4e, 0xf3, 0xf3),
			srgb3(0xdc, 0x4e, 0x4e), srgb3(0xf3, 0x4e, 0xf3), srgb3(0xf3, 0xf3, 0x4e), srgb3(0xff, 0xff, 0xff),
		})
}
