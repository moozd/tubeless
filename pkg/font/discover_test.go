package font

import "testing"

// TestParseStylePrimaryMatchSkipsSecondaryAliasWeights reproduces a real
// user report: a multi-weight family's italic cuts (ExtraLight Italic,
// Light Italic, Medium Italic, plain Italic, ...) each carry the generic
// "Italic" as a secondary style alias so style-matching tools still find
// *an* italic at all, alongside their own specific primary style name.
// fc-list's own "family:style=Italic" query matches on any alias, and
// enumerates files in filesystem order rather than by relevance — so the
// first hit was routinely a thin weight's cut, not the actual Italic cut
// in the same weight as Regular/Bold. At a small font size that reads as
// the italic barely rendering at all next to a heavier upright weight.
// The primary-style-only match here must land on the actual Italic file
// regardless of enumeration order.
func TestParseStylePrimaryMatchSkipsSecondaryAliasWeights(t *testing.T) {
	// Modeled directly on real fc-list output for a Nerd Font family
	// with several weights, each italic cut tagging "Italic" as a
	// secondary alias — deliberately listing the true Italic cut last,
	// so a naive "first match" would pick a wrong weight instead.
	const fcListOutput = `/fonts/Family-ExtraLight.otf: :style=ExtraLight,Regular
/fonts/Family-Light.otf: :style=Light,Regular
/fonts/Family-ExtraLightItalic.otf: :style=ExtraLight Italic,Italic
/fonts/Family-MediumItalic.otf: :style=Medium Italic,Italic
/fonts/Family-Regular.otf: :style=Regular
/fonts/Family-LightItalic.otf: :style=Light Italic,Italic
/fonts/Family-Medium.otf: :style=Medium,Regular
/fonts/Family-BoldItalic.otf: :style=Bold Italic
/fonts/Family-Bold.otf: :style=Bold
/fonts/Family-Italic.otf: :style=Italic
`
	path, ok := parseStylePrimaryMatch(fcListOutput, "Italic")
	if !ok {
		t.Fatal("expected a match for style=Italic")
	}
	if path != "/fonts/Family-Italic.otf" {
		t.Fatalf("path = %q, want /fonts/Family-Italic.otf (the actual Italic cut, not a weight variant matching only by secondary alias)", path)
	}
}

// TestParseStylePrimaryMatchFindsBoldItalic covers the two-word style
// case (space inside the style name) still matches as a whole primary
// token, not split on the space.
func TestParseStylePrimaryMatchFindsBoldItalic(t *testing.T) {
	const fcListOutput = `/fonts/Family-Bold.otf: :style=Bold
/fonts/Family-BoldItalic.otf: :style=Bold Italic
/fonts/Family-Italic.otf: :style=Italic
`
	path, ok := parseStylePrimaryMatch(fcListOutput, "Bold Italic")
	if !ok {
		t.Fatal("expected a match for style=Bold Italic")
	}
	if path != "/fonts/Family-BoldItalic.otf" {
		t.Fatalf("path = %q, want /fonts/Family-BoldItalic.otf", path)
	}
}

// TestParseStylePrimaryMatchNoMatchReturnsFalse covers a family with
// genuinely no cut in the requested style — callers (loadFontFaces) fall
// back to a synthetic slant/weight rather than trusting a substituted
// file, so this must report ok=false, not a coincidental alias match.
func TestParseStylePrimaryMatchNoMatchReturnsFalse(t *testing.T) {
	const fcListOutput = `/fonts/Family-Regular.otf: :style=Regular
/fonts/Family-Bold.otf: :style=Bold
`
	if _, ok := parseStylePrimaryMatch(fcListOutput, "Italic"); ok {
		t.Fatal("expected no match: this family has no Italic cut at all")
	}
}
