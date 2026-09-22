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

func TestWindowsUpdaterNeverChoosesInstallerEvenWhenListedFirst(t *testing.T) {
	assets := []github.ReleaseAsset{{Name: "GrokMcp-0.1.23-windows-amd64-installer.exe"}, {Name: "GrokMcp-0.1.22-windows-amd64.exe"}, {Name: "GrokMcp-0.1.23-windows-amd64.zip"}}
	req := updater.CheckRequest{Platform: "windows", Arch: "amd64"}
	if got := matchReleaseAsset(req, assets); got != 2 {
		t.Fatalf("picked installer: %d", got)
	}
	// v0.1.22 中使用的默认 matcher 也会排除新安装器命名。
	if got := github.DefaultAssetMatcher(req, []github.ReleaseAsset{assets[0], assets[2]}); got != 1 {
		t.Fatalf("legacy matcher picked new installer: %d", got)
	}
}
