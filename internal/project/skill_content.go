package project

// skillContent is the managed Skill body after the marker line.
// It only explains Supervisor MCP tools and the plan/wait/resume/TUI flow.
const skillContent = `
# Grok Supervisor 工具说明

本 Skill 只说明如何通过 Grok Supervisor MCP 与已有 Grok session 协作。
不强制把工作委派给 Grok，不划定具体任务边界，不修改模型配置。模型由用户自己选择。

## 协作方式

- 适合交给 Grok 的重活可以用下面的工具创建任务。
- Codex 审计划和验收。
- 用 ` + "`grok_wait`" + ` 等待边界状态，不要读取或等待过程流。
- 断连后继续原来的 Grok session，不要创建替换会话。

## 工具

### grok_dispatch

批量创建任务，默认无头，返回 job 与 session 映射。可选 ` + "`project_id`" + `；只有 cwd 时 Supervisor 会匹配或登记已发现项目。不要另开替换会话。

### grok_wait

等待 any/all 任务到达关键边界。不返回过程输出。不要轮询过程日志。

### grok_plan_decide

批准、退回或取消 Plan。批准后再让 Grok 在同一 session 实施。

### grok_followup

向同一 session 追加返工、继续或验收意见。

### grok_cancel_turn

取消当前 turn，保留 session 和排队状态。

### grok_set_view

在 headless 与 headed 之间切换，不重建任务。

### grok_status

返回阶段、显示状态、摘要和最近边界事件。

### grok_open_terminal

打开单任务 TUI 或集中 Grok Dashboard。

## 流程

1. 需要 Grok 执行时，用 grok_dispatch 创建任务。Grok 先进入 Plan。
2. 用 grok_wait 等到 plan_ready，审计划，再用 grok_plan_decide 批准、退回或取消。
3. 实施中继续用 grok_wait 等边界，不要看过程流。
4. 若断连，继续原来的 session。
5. 需要人看终端时用 grok_set_view 或 grok_open_terminal。

调试工具（grok_debug_*）默认不要用来代替 grok_wait。过程日志默认不发给 Codex。
`
