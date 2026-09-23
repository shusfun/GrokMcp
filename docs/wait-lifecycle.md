# GrokMcp 任务、审批与事件等待协议

## 生命周期依据

工作请求具有稳定 request_id，实际调用具有 turn_id。Agent 接口明确提供带身份的审批、待决权限查询、决定交付、活动和断连通知。生产路径不再通过可选接口或无 turn 的回调猜测审批能力。

原生权限请求生成 approval_id，并绑定连接代次、session、request、turn 和 Supervisor 方案版本。任何活动阶段都能进入 plan_ready。普通文本中的 plan_ready 不改变审批状态，未声明支持的 ACP terminal 方法返回 MethodNotFound，不返回虚构 ID 或空成功。

每任务协调器串行提交状态。外部 ACP Prompt、权限交付和取消不持有协调锁；取消交付期间禁止启动下一 turn，避免取消请求误中后续工作。旧工具调用保留其请求/turn 来源，不能在迟到时重新绑定到当前调用。

## 方案与批准

优先使用本次原生请求的方案正文。正文缺失时，只在原工作目录的等价 Windows 路径编码中定位同一个 session，并校验文件属于本次规划轮次；多目录歧义或旧文件不得静默回退。Grok 的编码包括盘符冒号 `%3A`，不能直接使用保留冒号的 PathEscape。

正文缺失仍保留真实待决请求，pause_reason=plan_content_missing，禁止批准没有核验正文的方案。重启或断连后的审批记录会标为过期，人工继续原 session 才能获取新原生审批。

`grok_plan_decide` 要求 approval_id、request_id、turn_id（取 plan_turn_id）和 plan_version。先记录决定，再恢复准确的权限调用，不追加实施消息。支持备注的 exit_plan_mode 扩展使用 comment；通用 permission 不支持备注，带备注时在放行前报错。

approval_delivery 区分 pending、submitted、confirmed、unknown、expired。放入本地 channel 只代表 submitted；原调用成功返回后才 confirmed。交付期间断连或失败记为 unknown，撤销“已确认批准”的推断，不自动重发。`peer disconnected before response` 明确归类为断连。

## 最终回答与恢复

ACP turn 结束不等于工作完成。兼容 working / needs_input / completed / blocked 四种标记；缺标记时保存最终回答，返回 needs_input + review_required，不再自动发送格式修复提示。

`grok_status(job_id, include_result=true, request_id)` 读取回答。结果包含 turn_id、offset、next_offset、total、has_more；分页以 Unicode 字符计数，默认 16384、最大 65536。后续页必须固定第一次返回的 turn_id，不跟随更新中的“最新结果”。普通状态和 report_due 保活不携带完整回答。

SQLite 在同一事务中保存任务、接受的请求、结果和边界通知。work_requests.phase 区分 queued、preparing、sent、returned；旧数据无法证明发送情况时保留 unknown。人工恢复时，尚未发送的请求可按已保存提示执行；已发送或结果未知的请求在原 session 核对已有工作，不盲目重发原始任务。启动不自动恢复。

最终 completed 在活动 turn 释放、队列检查及结果提交之后发布。排队请求尚未处理时不发布整个任务完成；待验收、待审批和交付未确认都是明确的暂停原因。

## 等待与消费者

`grok_wait` 默认 300 秒，关键边界立即返回 boundary。正常到期返回 report_due，这是内部保活，不生成用户可见进度，也不入队催促。调用方使用返回 cursors 继续等待，不通过 followup 催促或要求模型纠正协议格式。

状态、事件上界来自同一数据库读事务，并与运行态在同一协调器内取快照。当前请求已经被后续状态取代的审批/输入不会再次要求处理。any 等任一目标，all 等所有目标；all 简报不消费部分到达的边界。游标不得超出本次已读取上界，任务删除或持久化失败明确返回错误。

普通 ACP 活动只更新类别和时间，不采集思考或工具输出，不打断等待。安静不等于卡死。Codex 请求与 TUI 查看是共享同一 Grok session 的两条逻辑通道，写入 ACP 前串行调度。用户打开 TUI 时，若 ACP 写入尚未停在原生审批，则等这次写入返回后再附着，不取消该 turn。若 turn 正停在原生权限调用，只释放这一次调用并记为 execution_unknown，不把 TUI 文本写成该请求的结果。TUI 活跃时新的 Codex 请求立即接受并持久化排队，返回 request_id，不写入 TUI 输入，不创建第二 session。关闭或重开查看窗口不删除、不重排队列；worker 仍占着 session 时不开始 ACP 写入，worker 退出后队列按原顺序继续。ACP 活动 turn 的结果仍按其 request_id 回写；用户在独立 TUI 中输入形成的回答只进入 TUI 输出，现有 ACP 协议不把它映射为原 ACP 请求的最终回答，grok_status 不得据此伪报完成。取消必须绑定排队 request_id 或活动 turn_id，迟到回调不能覆盖其他请求。IPC EOF 释放挂起调用，事件回调在读循环之外执行；关闭等待连接不取消后台工作。

## 验证边界

确定性测试已使用真实 MCP、IPC 和 ACP 编解码，覆盖连通性检查、同 session 重新规划、原生审批、文件写读、返工、完整回答分页和断连。虚拟时钟用例推进完整 300 秒验证正常简报；普通测试不启动真实 Grok。

真实 Windows 对照验收由 scripts/accept-protocol.ps1 准备，明确确认后才使用 -Run。其状态、方案与结果保存在 dist/acp-acceptance，流程见 protocol-chain-acceptance.md。本地测试通过不代表真实 Grok 或当前安装已经验收；不操作 Pre/Production，不自动发布或替换安装，不承诺关闭 Codex 任务后自动唤醒。
