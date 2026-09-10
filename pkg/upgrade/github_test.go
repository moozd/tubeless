package upgrade

import "testing"

func TestSelectAsset(t *testing.T) {
	want := "tubeless-v1.2.3" + assetSuffix()
	rel := Release{Tag: "v1.2.3", Assets: []Asset{
		{Name: "tubeless-v1.2.3-darwin-arm64.zip", URL: "u1"},
		{Name: "tubeless-v1.2.3-darwin-amd64.zip", URL: "u2"},
		{Name: "tubeless_v1.2.3_amd64.deb", URL: "u3"},
		{Name: want, URL: "u4"},
		{Name: "tubeless-v1.2.3-linux-arm64.tar.gz", URL: "u5"},
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
