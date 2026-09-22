# Windows 隔离真实验收

## 当前门禁

执行用户要求的真实启动确认之前，只运行 `pwsh -NoProfile -File scripts/accept-protocol.ps1`。默认操作仅编译 GUI 子系统的验收程序，不启动 Grok。

确认之后才运行 `pwsh -NoProfile -File scripts/accept-protocol.ps1 -Run`。脚本隐藏启动验收进程，进程级环境开关会在脚本结束时恢复；模型、推理、认证和设备配置保持原样。

## 具体对象与行为

- Grok 使用 Finder 解析出的同一本机可执行文件，记录路径和 SHA256；直接 CLI 基线使用其内置说明支持的 `-p` 单次模式。
- 仅使用本仓库 `dist/acp-acceptance/home`（数据库、锁、专用 socket）、`work`（测试文件）、`decisions`（审核决定）、`plans`（逐份方案快照）。子目录解析后必须仍在隔离根内。
- 先比较 CLI 与 MCP 的 GROK_OK 连通性结果，再在一个 MCP session 内生成 calc.py、test_calc.py，运行 unittest；同 session 返工增加乘法及零值/负值用例。宿主独立重跑测试和已知输入断言。
- 失败保留原 job/session 与结果；重新运行发现未完成任务时，只恢复这个原 session，不另建替代任务。恢复轮次与初次健康流程不能混称一次无故障通过。
- WinEventHook 记录新出现的 ConsoleWindowClass / CASCADIA_HOSTING_WINDOW_CLASS 窗口。监测异常会使验收失败，不结束任何无关进程。

## 方案审核

真实审批到达后，程序写入 `pending-plan.json`，保持原 ACP 权限调用等待。Codex 读取并核验方案只涉及 work 中的两个测试文件和本地测试，再写入 `decisions/<approval_id>.json`：

```json
{
  "job_id": "来自 pending-plan.json",
  "approval_id": "来自 pending-plan.json",
  "request_id": "来自 pending-plan.json",
  "turn_id": "来自 plan_turn_id",
  "plan_version": 1,
  "decide": "approve",
  "notes": "仅执行已审核的隔离测试方案"
}
```

不支持备注时 notes 必须为空。验收程序校验全部绑定后，通过正常 grok_plan_decide MCP 调用交付决定；文件只是测试审核输入，不修改 Supervisor 数据库、任务状态或 Grok 会话内容，也不用于伪造 plan_ready。

## 记录与通过条件

report.json 指向最新结果，每次尝试另存独立 report-时间戳.json，保留失败记录。记录包含二进制身份、job/session、每轮请求/turn、独立验证结果和可见终端事件。程序日志在 dist/protocol-chain-candidate/acceptance-时间戳.stdout.log 与对应 stderr.log。每五分钟打印当前真实状态；不读过程日志催促模型，不因安静而取消并重试。

通过要求：原生审批和可读方案真实到达；一份决定只恢复一次原调用；文件功能与测试通过；结果可按请求读取；没有残留活动 turn 或队列；没有新可见终端；原 session 始终保持。普通最终回答未带状态标记时允许 review_required，但必须读取并独立验收真实结果，不宣称模型已发送 completed。

不发布、不安装、不访问任何业务服务器。真实验收未运行或失败时，报告必须明确写为未验证或失败，不能用协议测试替代。
