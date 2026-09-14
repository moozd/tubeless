package upgrade

import "testing"

func TestSelectAsset(t *testing.T) {
	want := "tubeless-v1.2.3" + assetSuffix()
	// Decoys must be assets no platform's suffix can match — a
	// hardcoded "...-darwin-arm64.zip" here is the wanted asset when the
	// tests themselves run on darwin/arm64, so selectAsset rightly
	// returned it and the test failed on exactly the platform it was
	// meant to cover.
	rel := Release{Tag: "v1.2.3", Assets: []Asset{
		{Name: "tubeless_v1.2.3_amd64.deb", URL: "u1"},
		{Name: "tubeless-v1.2.3-linux-amd64.rpm", URL: "u2"},
		{Name: "tubeless-v1.2.3-checksums.txt", URL: "u3"},
		{Name: want, URL: "u4"},
	}}
	got, err := selectAsset(rel)
	if err != nil {
		t.Fatalf("selectAsset: %v", err)
	}
	if got.Name != want || got.URL != "u4" {
		t.Fatalf("selectAsset = %+v, want name %q", got, want)
	}
}

func TestSelectAssetNoMatch(t *testing.T) {
	rel := Release{Tag: "v1.2.3", Assets: []Asset{
		{Name: "tubeless_v1.2.3_amd64.deb", URL: "u1"},
	}}
	if _, err := selectAsset(rel); err == nil {
		t.Fatal("selectAsset with no matching asset: want error, got nil")
	}
}
