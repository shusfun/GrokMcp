# GrokMcp

Desktop supervisor for Grok: tray app, job list, and MCP server.

## Install

Download the asset for your machine from [Releases](https://github.com/shusfun/GrokMcp/releases):

| File | Platform |
| --- | --- |
| `GrokMcp-*-darwin-amd64.dmg` | macOS Intel |
| `GrokMcp-*-darwin-arm64.dmg` | macOS Apple Silicon |
| `GrokMcp-*-windows-amd64.zip` | Windows x64 |

macOS builds are ad-hoc signed and not notarized. Gatekeeper may block the first open: right-click the app, choose Open, and confirm.

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
