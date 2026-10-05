package config

import (
	"fmt"
	"strings"
)

// The paper set: light themes that mimic a physical surface instead of
// plain white — vellum, newsprint, an LCD, a legal pad. Each pairs a
// tinted ground with an ink color and a muted, dark-enough palette, so
// every hue reads on its own light background. Slot 0 is the ink and
// slots 7/15 are dark grays, not whites, as in cleanRoomTheme.

// hex3 reads a "rrggbb" literal as a linear-RGB triple.
func hex3(h string) [3]float32 {
	var v [3]int
	fmt.Sscanf(h, "%02x%02x%02x", &v[0], &v[1], &v[2])
	return srgb3(v[0], v[1], v[2])
}

// paperTheme builds a light TrueColor theme from "rrggbb" literals:
// ground, ink, accent, and the 16 palette slots (normals then brights,
// space-separated).
func paperTheme(bg, fg, accent, slots string) ThemeColors {
	var palette [16][3]float32
	for i, h := range strings.Fields(slots) {
		palette[i] = hex3(h)
	}
	return trueColorTheme(hex3(bg), hex3(fg), hex3(accent), palette)
}

// eInkTheme is a paper-white reader: a flat gray-green panel, soft black ink, and muted print hues that stay dull the way electrophoretic color does.
func eInkTheme() ThemeColors {
	return paperTheme("c9cdc2", "1c1e1a", "3a4a3a",
		"2a2c28 9a3a34 3e6a3a 8a6a1e 3a5a8a 6a4a7a 2e6a6a 4a4c46 "+
			"5a5c56 b04a42 4e7e4a a07a2a 4a6e9e 7e5a92 3e7e7e 1c1e1a")
}

// parchmentTheme is an old scroll: a tanned vellum ground, iron-gall brown ink, and faded illuminated reds and lapis blues.
func parchmentTheme() ThemeColors {
	return paperTheme("e6d4a8", "3a2614", "8a2a1a",
		"3a2614 9a2a1e 4a6a2a 9a6a14 2a4a8a 7a3a6a 2a6a6a 5a4630 "+
			"6a5238 b4382a 5e8238 b4801e 3e62a8 94507e 3a8282 24160a")
}

// newsprintTheme is a morning paper: a dingy gray-cream stock, black letterpress ink, and the single spot red of a masthead.
func newsprintTheme() ThemeColors {
	return paperTheme("d6d2c4", "15140f", "c8201a",
		"1a1812 b01c16 2e5a2a 7a5a10 24427a 5a2a6a 1e5a5a 4a4840 "+
			"6a675a c8281e 3e7238 946e18 36549a 723a82 2e7272 0a0a06")
}

// legalPadTheme is a yellow legal pad: canary paper, blue ballpoint ink, and a red margin-rule accent.
func legalPadTheme() ThemeColors {
	return paperTheme("f3e58a", "1a2f7a", "d02a2a",
		"1a1a3a c02a2a 2a6a2a 8a6a00 1a3aaa 6a2a8a 1a6a7a 4a4a5a "+
			"5a5a6a d84040 3a823a a48200 2a4ec8 8a3ea8 2a828e 0a1450")
}

// sakuraTheme is cherry blossom paper: a pale pink wash, plum ink and branch-brown, with a deep rose accent.
func sakuraTheme() ThemeColors {
	return paperTheme("f6dde4", "3a1a2a", "c0306a",
		"3a1a2a b02a4a 4a6a3a 9a6a2a 4a4a8a 8a2a7a 2a6a6a 6a4a58 "+
			"8a6a78 cc3a5a 5a823e b4822e 5e5ea4 a4389a 3a8282 22081a")
}

// mintLcdTheme is a calculator display: a gray-green reflective LCD with dark olive segments, where nothing is ever truly black.
func mintLcdTheme() ThemeColors {
	return paperTheme("b4c49a", "1e2a12", "2e4a1a",
		"1e2a12 6a2a1a 2e5a1a 5a5a10 1a4a5a 4a3a5a 1a5a4a 3a4a2a "+
			"4e5e3a 823a28 3e701e 6e6e16 28607a 5e4a74 28705a 0e180a")
}

// sandstoneTheme is desert rock: a warm ochre-buff ground, deep umber text, and terracotta, sage and dusk-blue strata.
func sandstoneTheme() ThemeColors {
	return paperTheme("e8cfa4", "3a2410", "b0502a",
		"3a2410 a8381e 5a6a2a 9a6212 3a5a7a 7a4a62 3a6a62 5e4630 "+
			"7a5e42 c04a2a 6e823a b47a1e 4a6e92 92587a 4a8278 241408")
}

// lavenderPaperTheme is a violet-tinted stationery sheet: a pale lilac ground with aubergine ink and berry accents.
func lavenderPaperTheme() ThemeColors {
	return paperTheme("e2daf4", "2a1e4a", "6a3ac8",
		"2a1e4a b02a5a 2e6a4a 8a6a1e 3a4ac0 7a2ab0 2a6a8a 5a4a78 "+
			"7e6e9e cc3a72 3e8260 a4821e 4e5ee0 923ac8 3a82a4 140a30")
}

// receiptTheme is thermal receipt paper: an off-white shop-till stock printed in faded blue-black and a stamp-red total.
func receiptTheme() ThemeColors {
	return paperTheme("eee9d8", "2a2f3a", "c03020",
		"2a2f3a b03020 3a6a3a 8a6a20 2a4a8a 6a3a7a 2a6a72 5a5f6a "+
			"8a8f9a c84030 4a7e4a a4822e 3a5ea8 823e92 3a828e 12151c")
}

// cloudDeckTheme is a window seat above the clouds: a pale sky-blue haze with deep navy text and sunrise coral for emphasis.
func cloudDeckTheme() ThemeColors {
	return paperTheme("d0e2f2", "14283e", "e8603a",
		"14283e c8382e 2a7a52 a07a1a 2a62c0 6a3aa0 1a7a9a 4a6078 "+
			"6e869e e8603a 3a9066 bc9222 3a7ae0 823ec0 2a92b4 081828")
}

// typewriterTheme is an ivory typewriter page: a cream sheet, carbon-black type, and the red half of a two-color ribbon.
func typewriterTheme() ThemeColors {
	return paperTheme("f0e6c8", "16140f", "b8281e",
		"16140f b8281e 3a5a2a 7a5a14 2a3a6a 5a2a5a 2a5a5a 4a463a "+
			"7a7464 d03a2e 4a6e38 947028 3a4e84 723a72 3a7272 080806")
}

// matchaTheme is a matcha latte: a soft green-cream ground, deep tea-leaf text, and roasted brown and persimmon accents.
func matchaTheme() ThemeColors {
	return paperTheme("d2dcae", "1e2e14", "d0602a",
		"1e2e14 a83a22 2e6a22 8a6a16 2a4a72 6a3a5a 2a6a58 4a5a3a "+
			"6a7a52 c4482e 3e8230 a4821e 3a5e8e 824a70 3a8270 0e1a08")
}

// The calculator set: LCD, backlit and LED displays from calculators and
// small devices. The LCDs are light paper themes (a gray-green panel,
// dark pixels); the LED and VFD ones are duotones that glow on black.

// ti83Theme is a graphing calculator's LCD: a greenish gray panel with dark blue-black pixels, like a TI-83 left on a desk.
func ti83Theme() ThemeColors {
	return paperTheme("9fb4a0", "16241e", "1a3a5a",
		"16241e 8a2a2a 1e5a2e 6a5a10 1a3a6a 5a2a62 1a5a5e 3a4a42 "+
			"4a5a52 a03434 2a6e3a 826e18 2a4a82 723a7a 2a6e72 081410")
}

// hp48Theme is a scientific calculator: a pale gray-olive LCD with near-black segments and the red of a shift-key legend.
func hp48Theme() ThemeColors {
	return paperTheme("bbc0a4", "1e2216", "a02a1a",
		"1e2216 a02a1a 2e5a22 7a5e14 24407a 62325a 245a58 464a3a "+
			"6a6e5a b83a28 3e7230 927214 345296 7a427a 347270 0c0e08")
}

// casioFxTheme is a pocket solar calculator: a cool blue-gray LCD under a solar cell strip, with soft black digits.
func casioFxTheme() ThemeColors {
	return paperTheme("b1bfb8", "1a2420", "c87a14",
		"1a2420 a03030 2a6040 7a6218 2a487a 5a3a70 2a6270 3c4a46 "+
			"66726c b84040 3a7650 927a20 3a5a92 724a88 3a7a88 0a1410")
}

// inTheme is a watch's electroluminescent backlight: a glowing pale green-teal panel with deep teal-black ink.
func indigloTheme() ThemeColors {
	return paperTheme("bfe9d3", "0e2a26", "0a7a62",
		"0e2a26 b0303a 12683a 7a6a10 1a4a8a 6a2a82 0a6a7a 3a5a54 "+
			"5e7e76 c84450 1e7e4a 927e1a 2a5ea4 823e9a 1a8294 041612")
}

// backlitAmberTheme is an amber-backlit LCD, like a pager or a car stereo: a glowing orange-gold panel with dark brown pixels.
func backlitAmberTheme() ThemeColors {
	return paperTheme("eba838", "2a1400", "7a2a00",
		"2a1400 8a1e14 2a5a14 6a4a00 1a3a6a 5a1a52 1a5a5a 4a3010 "+
			"7a5a20 a42a1e 3a6e1e 826000 2a4e82 722e6a 2a6e6e 140a00")
}

// led7SegTheme is a calculator's red seven-segment LED: a deep dark-red dim end and a hot red bright end.
func led7SegTheme() ThemeColors {
	return ThemeColors{Phosphor: Phosphor{
		Low:  srgb3(0x5a, 0x08, 0x08),
		High: srgb3(0xff, 0x2a, 0x1a),
	}}
}

// sharpVfdTheme is an 80s desk calculator's vacuum-fluorescent display: a deep blue-teal dim end and an icy blue-white glow, bluer than vfdTheme's aqua-green.
func sharpVfdTheme() ThemeColors {
	return ThemeColors{Phosphor: Phosphor{
		Low:  srgb3(0x10, 0x50, 0x6a),
		High: srgb3(0x9a, 0xf0, 0xff),
	}}
}
