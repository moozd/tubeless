package screen

import "testing"

// TestRgbValueIsPhotometricLuminance locks in rgbValue's real-device
// behavior: a monochrome CRT decoding a color composite signal sees only
// its Y' luma, computed from the gamma-encoded R'G'B' by the standard
// Rec.709 weights — not each color's own peak channel. Blue reading
// noticeably dimmer than yellow at equal channel intensity is the
// specific, previously-wrong behavior this pins down (see color.go's own
// doc comment): dense syntax-highlighted text under a monochrome theme
// used to compress almost every accent color into the same near-maximum
// brightness band regardless of hue, washing distinct tokens into one
// indistinguishable, bloom-smeared block.
func TestRgbValueIsPhotometricLuminance(t *testing.T) {
	blue := rgbValue(0, 0, 255)
	yellow := rgbValue(255, 255, 0)
	green := rgbValue(0, 255, 0)
	white := rgbValue(255, 255, 255)

	if blue >= yellow {
		t.Errorf("blue (%v) should read dimmer than yellow (%v) — Rec.709 weights blue at 0.0722 vs yellow's 0.2126+0.7152", blue, yellow)
	}
	if green <= blue {
		t.Errorf("green (%v) should read brighter than blue (%v) — Rec.709 weights green at 0.7152", green, blue)
	}
	if white != 1 {
		t.Errorf("white = %v, want 1 (weights must sum to 1)", white)
	}

	// The old max-channel formula gave blue, yellow, and green all the
	// exact same value (1.0, since each has some channel at 255) — this
	// is the specific clustering the fix breaks apart.
	if blue == yellow || blue == green || yellow == green {
		t.Errorf("blue/yellow/green should each read as distinct brightness, got blue=%v yellow=%v green=%v", blue, yellow, green)
	}
}
