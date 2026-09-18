package app

import (
	"strings"

	"github.com/wailsapp/wails/v3/pkg/updater"
	"github.com/wailsapp/wails/v3/pkg/updater/providers/github"
)

const githubRepo = "shusfun/GrokMcp"

func matchReleaseAsset(req updater.CheckRequest, assets []github.ReleaseAsset) int {
	filtered := make([]github.ReleaseAsset, 0, len(assets))
	orig := make([]int, 0, len(assets))
	for i, a := range assets {
		name := strings.ToLower(a.Name)
		if strings.HasSuffix(name, ".dmg") {
			continue
		}
		filtered = append(filtered, a)
		orig = append(orig, i)
	}
	idx := github.DefaultAssetMatcher(req, filtered)
	if idx < 0 || idx >= len(orig) {
		return -1
	}
	return orig[idx]
}
