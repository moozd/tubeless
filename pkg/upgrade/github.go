package upgrade

import (
	"encoding/json"
	"fmt"
	"net/http"
	"runtime"
)

const releasesAPI = "https://api.github.com/repos/moozd/tubeless/releases/latest"

// Asset is one downloadable file attached to a GitHub release.
type Asset struct {
	Name string `json:"name"`
	URL  string `json:"browser_download_url"`
}

// Release is the subset of GitHub's release JSON this package needs.
type Release struct {
	Tag    string  `json:"tag_name"`
	Assets []Asset `json:"assets"`
}

// latestRelease fetches the newest published GitHub release.
func latestRelease() (Release, error) {
	resp, err := http.Get(releasesAPI)
	if err != nil {
		return Release{}, fmt.Errorf("fetch latest release: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return Release{}, fmt.Errorf("fetch latest release: unexpected status %s", resp.Status)
	}
	var rel Release
	if err := json.NewDecoder(resp.Body).Decode(&rel); err != nil {
		return Release{}, fmt.Errorf("decode latest release: %w", err)
	}
	return rel, nil
}

// assetSuffix is the filename ending this platform/arch's release asset
// always has (see the Makefile's package-linux/package-darwin targets).
func assetSuffix() string {
	if runtime.GOOS == "darwin" {
		return fmt.Sprintf("-darwin-%s.zip", runtime.GOARCH)
	}
	return fmt.Sprintf("-linux-%s.tar.gz", runtime.GOARCH)
}

// selectAsset finds this platform/arch's asset among a release's files.
func selectAsset(rel Release) (Asset, error) {
	suffix := assetSuffix()
	for _, a := range rel.Assets {
		if len(a.Name) > len(suffix) && a.Name[len(a.Name)-len(suffix):] == suffix {
			return a, nil
		}
	}
	return Asset{}, fmt.Errorf("no release asset ending in %q for %s/%s", suffix, runtime.GOOS, runtime.GOARCH)
}
