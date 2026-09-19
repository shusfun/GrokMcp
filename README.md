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

## Codex MCP

Keep the desktop Supervisor running as the owner of jobs, SQLite, and the Grok Leader. The `mcp` process is a local stdio bridge: it connects over the existing local IPC socket (macOS) or `127.0.0.1` port file (Windows). If Supervisor is not up, `mcp` starts an independent `desktop` process from the same executable and retries; disconnecting Codex does not stop running tasks or sessions.

The stable MCP id is `grok_supervisor`. In **Settings → 集成 / MCP 配置** pick **one** path:

1. **CC-Switch** (if you use CC-Switch as SSOT): copy STDIO JSON, or click **用 CC-Switch 快速导入** to open `ccswitch://v1/import?resource=mcp&apps=codex&config=...`. Grok Supervisor does **not** write `~/.cc-switch/cc-switch.db`. The app only opens the public deep link; confirm the import inside CC-Switch, then **重新检测**. Deep link will not update an existing server’s command; if a `Grok Supervisor` / `grok_supervisor` row already exists and needs a change, copy the update JSON and paste it in CC-Switch’s MCP editor.
2. **Codex Direct** (only if you do **not** use CC-Switch for MCP): click **添加/更新到 Codex** to run `codex mcp add`, or copy the command / TOML. This does not change model settings and does not edit `~/.codex/config.toml` by hand. Do not add `grok_supervisor` if `Grok Supervisor` is already registered.

Do not use both paths to add the same server twice. Existing entries are updated in place (or you are asked to paste), not duplicated.

CLI still prints the same copy-paste config (this does not write `~/.codex/config.toml` or change model settings):

```sh
# macOS (app bundle inner binary)
"/Applications/Grok Supervisor.app/Contents/MacOS/GrokMcp" mcp-config

# Windows (unzipped release)
GrokMcp.exe mcp-config
```

The macOS command must be the inner executable `Grok Supervisor.app/Contents/MacOS/GrokMcp`, not the `.app` bundle. Windows `mcp-config` quotes the add line for PowerShell (backslashes stay literal); paste the TOML into `config.toml` if you are not using PowerShell. Windows runs `GrokMcp.exe mcp`.

Diagnose without becoming the Supervisor owner:

```sh
GrokMcp doctor
```

`doctor` reports the executable path, suggested MCP command, and whether Supervisor IPC is reachable.

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
