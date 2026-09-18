package app

import (
	"testing"

	"github.com/wailsapp/wails/v3/pkg/updater"
	"github.com/wailsapp/wails/v3/pkg/updater/providers/github"
)

func TestMatchReleaseAssetSkipsDMG(t *testing.T) {
	assets := []github.ReleaseAsset{
		{Name: "SHA256SUMS.txt"},
		{Name: "GrokMcp-0.1.2-darwin-amd64.dmg"},
		{Name: "GrokMcp-0.1.2-darwin-amd64.zip"},
		{Name: "GrokMcp-0.1.2-darwin-arm64.zip"},
		{Name: "GrokMcp-0.1.2-windows-amd64.zip"},
	}
	idx := matchReleaseAsset(updater.CheckRequest{Platform: "darwin", Arch: "amd64"}, assets)
	if idx != 2 {
		t.Fatalf("darwin/amd64 got %d, want zip at 2", idx)
	}
	idx = matchReleaseAsset(updater.CheckRequest{Platform: "darwin", Arch: "arm64"}, assets)
	if idx != 3 {
		t.Fatalf("darwin/arm64 got %d, want zip at 3", idx)
	}
	idx = matchReleaseAsset(updater.CheckRequest{Platform: "windows", Arch: "amd64"}, assets)
	if idx != 4 {
		t.Fatalf("windows/amd64 got %d, want zip at 4", idx)
	}
}
