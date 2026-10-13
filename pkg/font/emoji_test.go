package font

import "testing"

func openTestEmojiFace(t *testing.T) *EmojiFace {
	t.Helper()
	path, err := FindEmojiFont()
	if err != nil {
		t.Skipf("no color emoji font on this machine: %v", err)
	}
	face, err := OpenEmojiFace(path, 64)
	if err != nil {
		t.Fatalf("OpenEmojiFace: %v", err)
	}
	t.Cleanup(face.Close)
	return face
}

func TestEmojiFaceRendersColorClusters(t *testing.T) {
	face := openTestEmojiFace(t)
	for _, cluster := range []string{"🚀", "❤\uFE0F", "👍🏽", "🇳🇱", "👨\u200D👩\u200D👧"} {
		img, ok := face.Render(cluster)
		if !ok {
			t.Errorf("Render(%q) found no glyph", cluster)
			continue
		}
		b := img.Bounds()
		if b.Dx() < 32 || b.Dy() < 32 {
			t.Errorf("Render(%q) = %dx%d, want a strike of at least 32px", cluster, b.Dx(), b.Dy())
		}
		if !hasColor(img.Pix) {
			t.Errorf("Render(%q) has no colored pixel", cluster)
		}
	}
}

func TestEmojiFaceMergesSequences(t *testing.T) {
	face := openTestEmojiFace(t)
	coder := face.clusterGlyphs("👩\u200D💻")
	woman := face.clusterGlyphs("👩")
	if len(coder) != 2 || coder[0] == woman[0] || coder[1] != woman[0] {
		t.Errorf("ZWJ glyphs = %v, woman = %v: want [merged, woman]", coder, woman)
	}
	if face.clusterGlyphs("👍🏽")[0] == face.clusterGlyphs("👍")[0] {
		t.Errorf("skin tone sequence did not merge into its own glyph")
	}
}

func TestEmojiFaceMissingGlyph(t *testing.T) {
	face := openTestEmojiFace(t)
	if _, ok := face.Render("a\u0301"); ok {
		t.Errorf("Render of a non-emoji cluster reported a glyph")
	}
}

func TestIsBetterStrike(t *testing.T) {
	cases := []struct {
		h, cur, target int
		want           bool
	}{
		{h: 64, cur: 40, target: 48, want: true},
		{h: 96, cur: 64, target: 48, want: false},
		{h: 40, cur: 32, target: 48, want: true},
		{h: 32, cur: 64, target: 48, want: false},
	}
	for _, tc := range cases {
		if got := isBetterStrike(tc.h, tc.cur, tc.target); got != tc.want {
			t.Errorf("isBetterStrike(%d, %d, %d) = %v, want %v", tc.h, tc.cur, tc.target, got, tc.want)
		}
	}
}

// hasColor reports whether any RGBA pixel has distinct channels — a
// grayscale coverage bitmap smuggled into RGBA would not.
func hasColor(pix []byte) bool {
	for i := 0; i+3 < len(pix); i += 4 {
		if pix[i] != pix[i+1] || pix[i+1] != pix[i+2] {
			return true
		}
	}
	return false
}
