# GrokMcp

Desktop supervisor for Grok: tray app, job list, and MCP server.

## Install

Download the asset for your machine from [Releases](https://github.com/shusfun/GrokMcp/releases):

| File | Platform |
| --- | --- |
| `GrokMcp-*-darwin-amd64.dmg` | macOS Intel |
| `GrokMcp-*-darwin-arm64.dmg` | macOS Apple Silicon |
| `GrokMcp-*-windows-amd64-installer.exe` | Windows x64 installer |
| `GrokMcp-*-windows-amd64.zip` | Windows x64 portable ZIP |

macOS builds are ad-hoc signed and not notarized (no Apple Developer ID). Gatekeeper will warn on the first open after a download. Drag the app to Applications, then use **System Settings → Privacy & Security → Open Anyway**, or run `xattr -cr "/Applications/Grok Supervisor.app"`. The disk image also has `打开说明.txt` and `清除隔离属性.command`.

On Windows, run the EXE installer to install the desktop app and shortcuts. Running the installer again upgrades the existing installation in place and keeps its directory and user data. The ZIP remains available as a portable build.

Verify downloads with `SHA256SUMS.txt` on the same release:

```sh
shasum -a 256 -c SHA256SUMS.txt
```

Windows needs the [WebView2 Runtime](https://developer.microsoft.com/microsoft-edge/webview2/).

## Codex MCP

桌面启动只打开界面和本机 IPC，不启动 Grok，也不自动恢复历史任务。新建任务、点击“继续”或审批方案后才按需连接；重启后的任务显示“待手动恢复”，继续使用原 session ID。状态栏、托盘及 IPC 健康检查只读内存状态；设置页的完整诊断需要主动点击。

Windows 使用 Supervisor 数据目录中的 `grok-leader.sock`，与用户终端的共享 leader 隔离。后台 leader 与交互工作进程使用 ConPTY，不创建可见终端；ACP 的 JSON-RPC 使用独立标准流管道。状态查询不启动任何 Grok 进程，完整诊断需要主动点击。

任务始终从无头状态开始，旧“默认有头”设置迁移为无头。只有主动点击“显示 TUI / 打开终端”或明确调用打开命令才创建查看窗口。同一任务重复打开会复用窗口。执行中请求查看会先显示等待提示，在安全边界接入交互。

Windows 查看窗口是独立的 `GrokMcp terminal-view` 客户端。键盘、尺寸和屏幕数据通过只允许当前用户的本地命名管道转发，连接绑定任务、原 session 和本次窗口代次。关闭窗口或切回无头不会取消任务或结束后台 TUI；再次打开仍连接同一个工作进程。后台 TUI 保留输入所有权，退出 Grok TUI 才归还给 Supervisor 并处理排队请求，避免用窗口关闭猜测任务是否空闲。

专用进程由 Windows Job Object 管理；退出应用或崩溃时回收自有进程，不接管其他 Grok/Codex 或开发服务。无执行、排队、当前待审批或关联交互工作进程时，30 秒后回收后台进程。握手超时 15 秒，失败后提示人工重试，不循环重启。

Windows 自定义终端模板应使用 `{command}` 启动完整查看命令。旧标准 `{grok} --resume {session_id}` 写法在运行时转换为查看命令，保存的配置不改写；不允许模板绕过查看客户端直接建立第二份执行会话。

`mcp` 仍是 stdio 到本机 IPC 的桥接器；MCP 客户端断开不停止 Supervisor 的任务。找不到宿主时可启动桌面界面，但不会因此运行 Grok。

The stable MCP id is `grok_supervisor`. In **Settings → 集成 / MCP 配置** pick **one** path:

1. **CC-Switch** (if you use CC-Switch as SSOT): copy STDIO JSON, or click **用 CC-Switch 快速导入** to open `ccswitch://v1/import?resource=mcp&apps=codex&config=...`. Grok Supervisor does **not** write `~/.cc-switch/cc-switch.db`. The app only opens the public deep link; confirm the import inside CC-Switch, then **重新检测**. MCP server ids must not contain spaces. If an old `Grok Supervisor` row exists, import `grok_supervisor`, delete the old row, then restart MCP servers in Codex settings.
2. **Codex Direct** (only if you do **not** use CC-Switch for MCP): click **添加/更新到 Codex** to run `codex mcp add`, or copy the command / TOML. This does not change model settings and does not edit `~/.codex/config.toml` by hand. An old `Grok Supervisor` entry is invalid in Codex Desktop and must be migrated to `grok_supervisor`.

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

## Release validation

Release CI creates a **draft** first. Windows assets include `*-windows-amd64-installer.exe` for installation and `*-windows-amd64.zip` for the built-in updater; the installer must never be selected as an in-place application binary. CI tests an isolated installation using the v0.1.22 payload, verifies an in-place upgrade and retained data, and repeats installation without launching the app.

Before promoting a draft to stable, complete the controlled, single-instance Windows Grok check: private socket, interactive attach, closing/reopening the viewer during work, and zero unrequested visible terminals. Fake ACP and ConPTY fixture tests do not replace that check. Never overwrite a published release or force-move a tag. Updating this computer's installation is separate from publishing the release.
