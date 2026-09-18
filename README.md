# GrokMcp

Desktop supervisor for Grok: tray app, job list, and MCP server.

## Install

Download the asset for your machine from [Releases](https://github.com/shusfun/GrokMcp/releases):

| File | Platform |
| --- | --- |
| `GrokMcp-*-darwin-amd64.dmg` | macOS Intel |
| `GrokMcp-*-darwin-arm64.dmg` | macOS Apple Silicon |
| `GrokMcp-*-windows-amd64.zip` | Windows x64 |

macOS builds are ad-hoc signed and not notarized (no Apple Developer ID). Gatekeeper will warn on the first open after a download. Drag the app to Applications, then use **System Settings → Privacy & Security → Open Anyway**, or run `xattr -cr "/Applications/Grok Supervisor.app"`. The disk image also has `打开说明.txt` and `清除隔离属性.command`.

Verify downloads with `SHA256SUMS.txt` on the same release:

```sh
shasum -a 256 -c SHA256SUMS.txt
```

Windows needs the [WebView2 Runtime](https://developer.microsoft.com/microsoft-edge/webview2/).

## Development

```sh
pnpm install
pnpm build
go test ./...
sh scripts/dev.sh
```

Production packages (macOS DMGs + Windows zip):

```sh
sh scripts/package.sh
```
