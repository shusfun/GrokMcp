<!-- grok-supervisor-tools managed version=3 -->

# Grok Supervisor 工具说明

本 Skill 只说明如何通过 Grok Supervisor MCP 与已有 Grok session 协作。
不强制把工作委派给 Grok，不划定具体任务边界，不修改模型配置。模型由用户自己选择。

## 协作方式

- 适合交给 Grok 的重活可以用下面的工具创建任务。
- Codex 审计划和验收。
- 用 `grok_wait` 等待边界状态，不要读取或等待过程流。
- 断连后继续原来的 Grok session，不要创建替换会话。

## 工具

### grok_dispatch

批量创建任务，默认无头，返回 job 与 session 映射。每个任务的 `planning` 取 skip 或 required，未填写为 skip。可选 `project_id`；只有 cwd 时 Supervisor 会匹配或登记已发现项目。不要另开替换会话。

### grok_wait

默认等待 300 秒。返回 reason=boundary 表示新的审批、输入、完成、失败、断连或调度异常；reason=report_due 是内部保活，不是错误，也不要向用户发送进度。
每次把返回的 cursors 原样传入下一次 grok_wait。any 等任一任务的新边界，all 等所有目标的新边界；all 保活不会消费尚未凑齐的边界。不要轮询过程日志。
收到 report_due 时静默继续等待。不要发送 followup 催进度，也不要让模型手写 plan_ready 或通过格式修复消息纠正工具状态；取消等待不取消任务。
没有活动仅说明暂未收到活动，不能据此断言卡死。本工具不保证结束 Codex 任务后自动唤醒。

### grok_plan_decide

批准、退回或取消 Plan。传入当前 approval_id、request_id、plan_turn_id 对应的 turn_id 和 plan_version，拒绝旧方案审批。批准挂起调用后原调用继续，不追加实施消息。备注不受支持时会在放行前报错。

### grok_followup

向同一 session 追加新工作。planning 取 skip 或 required，未填写为 skip；replan=true 强制 required。忙或 TUI 活跃时立即返回 accepted_request_id，并按原 session 顺序排队。不要写入 TUI 输入，不要新建 session。不要用于催进度。新工作有独立 request_id，旧结果保留在历史中。

### grok_cancel_turn

取消一条尚未发送的排队请求，或当前活动 turn。传入 request_id、turn_id，或两者同时指向同一目标。保留 session 和其他排队项。迟到结果不能覆盖其他请求。

### grok_set_view

在 headless 与 headed 之间切换，不重建任务。

### grok_status

返回阶段、显示状态、摘要和最近边界事件。include_result=true 按 request_id 读取最终回答；分页使用 offset/limit（Unicode 字符），后续页固定第一次返回的 turn_id。
needs_input 且 pause_reason=review_required 表示本轮最终回答待验收，读取结果后判断，不发送索要状态格式的 followup。completed 才是显式完成，ACP end_turn 本身不能代替任务完成。审批交付 unknown 时不能自动重发或假定已经实施。

### grok_open_terminal

打开单任务 TUI 或集中 Grok Dashboard。

## 流程

1. 需要 Grok 执行时，用 grok_dispatch 创建任务。只有 planning=required 才进入 Plan。
2. 需要审计划时用 grok_wait 等到 plan_ready，再用 grok_plan_decide 批准、退回或取消。
3. 实施中继续用 grok_wait 等边界。report_due 静默续等，不要看过程流，不要向用户发送进度。
4. 若断连，继续原来的 session。TUI 关闭或重开后，已接受的请求仍按原顺序执行。
5. 需要人看终端时用 grok_set_view 或 grok_open_terminal。Codex 请求不会占用 TUI 输入。

调试工具（grok_debug_*）默认不要用来代替 grok_wait。过程日志默认不发给 Codex。
