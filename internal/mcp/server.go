package mcp

import (
	"context"
	"errors"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"grokmcp/internal/core"
	"grokmcp/internal/protocol"
	"grokmcp/internal/version"
)

func Run(ctx context.Context, backend core.Backend) error {
	if ls, ok := backend.(interface{ SetMCPConnected(bool) }); ok {
		ls.SetMCPConnected(true)
		defer ls.SetMCPConnected(false)
	}
	server := newServer(backend)
	return server.Run(ctx, &mcp.StdioTransport{})
}

const instructions = `可委派工作默认交给 Grok。是否进入 Plan 由调用方用 planning=required 显式选择，未填写则 skip。Codex 只在调用方要求规划时审计划，并验收结果。
用 grok_wait 等待关键边界，默认 300 秒。boundary 才处理审批、输入、失败或验收。report_due 是内部保活，不要向用户发送进度消息，携带返回 cursors 继续等待。不要用 followup 催进度，不读取过程流；停止等待不会取消任务。
TUI 查看与 Codex 请求共享同一 session。TUI 活跃时追加请求会立即返回 request_id 并排队，不要写入 TUI 输入，不要另开 session。
断连或 Codex 重启后继续原来的 Grok session，不要创建替换会话。
不要修改用户的模型或推理设置。`

func newServer(backend core.Backend) *mcp.Server {
	server := mcp.NewServer(&mcp.Implementation{Name: "grok_supervisor", Version: version.Version}, &mcp.ServerOptions{Instructions: instructions})
	addTools(server, backend)
	return server
}

func addTools(server *mcp.Server, backend core.Backend) {
	mcp.AddTool(server, &mcp.Tool{Name: "grok_dispatch", Description: "批量创建任务，默认 headless，返回 job 与 session 映射。tasks[].planning 为 skip 或 required，未填写为 skip。", Annotations: annExec},
		func(ctx context.Context, _ *mcp.CallToolRequest, in protocol.DispatchRequest) (*mcp.CallToolResult, protocol.DispatchResult, error) {
			out, err := backend.Dispatch(ctx, in)
			return nil, out, err
		})
	mcp.AddTool(server, &mcp.Tool{Name: "grok_wait", Description: "等待新的 any/all 边界，默认 300 秒。关键边界立即返回 boundary。report_due 是内部保活，不要据此向用户发送进度。下一次原样携带 cursors。不返回过程输出。", Annotations: annWait},
		func(ctx context.Context, _ *mcp.CallToolRequest, in protocol.WaitRequest) (*mcp.CallToolResult, protocol.WaitResult, error) {
			out, err := backend.Wait(ctx, in)
			return nil, out, err
		})
	mcp.AddTool(server, &mcp.Tool{Name: "grok_plan_decide", Description: "批准、退回或取消真实原生审批；携带 approval_id 和请求/turn/方案版本。submitted 不等于 Grok 已确认执行，unknown 不自动重发。", Annotations: annExec},
		func(ctx context.Context, _ *mcp.CallToolRequest, in protocol.PlanDecideRequest) (*mcp.CallToolResult, protocol.Job, error) {
			out, err := backend.PlanDecide(ctx, in)
			return nil, out, err
		})
	mcp.AddTool(server, &mcp.Tool{Name: "grok_followup", Description: "向同一 session 追加新工作。planning 为 skip 或 required，未填写为 skip；replan=true 强制 required。忙或 TUI 活跃时立即返回 accepted_request_id 并排队，不写入 TUI，不新建 session。不要用于催进度。", Annotations: annExec},
		func(ctx context.Context, _ *mcp.CallToolRequest, in protocol.FollowupRequest) (*mcp.CallToolResult, protocol.Job, error) {
			out, err := backend.Followup(ctx, in)
			return nil, out, err
		})
	mcp.AddTool(server, &mcp.Tool{Name: "grok_cancel_turn", Description: "取消一条排队请求或当前 turn。request_id 取消尚未发送的请求；turn_id 取消活动 turn。两者都传时必须指向同一目标。保留 session 和其他排队项。", Annotations: annCancelTurn},
		func(ctx context.Context, _ *mcp.CallToolRequest, in struct {
			TurnID    string `json:"turn_id,omitempty" jsonschema:"active turn id; rejects a stale turn"`
			RequestID string `json:"request_id,omitempty" jsonschema:"queued or active request id"`
			JobID     string `json:"job_id" jsonschema:"job id"`
		}) (*mcp.CallToolResult, protocol.Job, error) {
			out, err := backend.CancelRequest(ctx, in.JobID, in.RequestID, in.TurnID)
			return nil, out, err
		})
	mcp.AddTool(server, &mcp.Tool{Name: "grok_set_view", Description: "在 headless 与 headed 之间切换，不重建任务。", Annotations: annSetView},
		func(ctx context.Context, _ *mcp.CallToolRequest, in protocol.SetViewRequest) (*mcp.CallToolResult, protocol.Job, error) {
			out, err := backend.SetView(ctx, in)
			return nil, out, err
		})
	mcp.AddTool(server, &mcp.Tool{Name: "grok_status", Description: "返回当前状态；include_result=true 按 request_id 读取最终回答，分页 offset/limit 按 Unicode 字符计数，后续页固定返回的 turn_id。review_required 需要验收回答，不追加格式修复消息。", Annotations: annStatus},
		func(ctx context.Context, _ *mcp.CallToolRequest, in struct {
			protocol.ResultQuery
			JobID string `json:"job_id,omitempty" jsonschema:"optional job id"`
		}) (*mcp.CallToolResult, any, error) {
			if in.JobID != "" {
				job, err := backend.Status(ctx, in.JobID, in.ResultQuery)
				return nil, job, err
			}
			if in.IncludeResult || in.RequestID != "" || in.TurnID != "" {
				return nil, nil, errors.New("job_id is required for result lookup")
			}
			jobs, err := backend.ListJobs(ctx)
			return nil, jobs, err
		})
	mcp.AddTool(server, &mcp.Tool{Name: "grok_open_terminal", Description: "打开单任务 TUI 或集中 Grok Dashboard。", Annotations: annOpenTerm},
		func(ctx context.Context, _ *mcp.CallToolRequest, in protocol.OpenTerminalRequest) (*mcp.CallToolResult, struct{}, error) {
			return nil, struct{}{}, backend.OpenTerminal(ctx, in)
		})
	mcp.AddTool(server, &mcp.Tool{Name: "grok_debug_set", Description: "打开或关闭调试记录；job_id 为空时为全局调试模式。过程日志默认不发给 Codex。", Annotations: annDebugSet},
		func(ctx context.Context, _ *mcp.CallToolRequest, in protocol.DebugSetRequest) (*mcp.CallToolResult, protocol.Job, error) {
			out, err := backend.DebugSet(ctx, in)
			return nil, out, err
		})
	mcp.AddTool(server, &mcp.Tool{Name: "grok_debug_snapshot", Description: "按 cursor 增量读取有限条结构化日志，不含完整 transcript。", Annotations: annDebugRead},
		func(ctx context.Context, _ *mcp.CallToolRequest, in protocol.DebugSnapshotRequest) (*mcp.CallToolResult, protocol.DebugSnapshot, error) {
			out, err := backend.DebugSnapshot(ctx, in)
			return nil, out, err
		})
	mcp.AddTool(server, &mcp.Tool{Name: "grok_debug_wait", Description: "等待下一条 any/warning/error/boundary 调试事件。", Annotations: annDebugRead},
		func(ctx context.Context, _ *mcp.CallToolRequest, in protocol.DebugWaitRequest) (*mcp.CallToolResult, protocol.DebugSnapshot, error) {
			out, err := backend.DebugWait(ctx, in)
			return nil, out, err
		})
	mcp.AddTool(server, &mcp.Tool{Name: "grok_debug_export", Description: "导出任务 jsonl 诊断包路径。", Annotations: annDebugExport},
		func(ctx context.Context, _ *mcp.CallToolRequest, in struct {
			JobID string `json:"job_id" jsonschema:"job id"`
		}) (*mcp.CallToolResult, protocol.DebugExportResult, error) {
			out, err := backend.DebugExport(ctx, in.JobID)
			return nil, out, err
		})
	mcp.AddTool(server, &mcp.Tool{Name: "grok_project_import", Description: "登记项目。默认安装工具说明 Skill；冲突不覆盖用户文件，项目仍成功。", Annotations: annProjWrite},
		func(ctx context.Context, _ *mcp.CallToolRequest, in protocol.ImportProjectRequest) (*mcp.CallToolResult, protocol.Project, error) {
			install := true
			if in.InstallSkill != nil {
				install = *in.InstallSkill
			}
			out, err := backend.ImportProject(ctx, in.Path, install)
			return nil, out, err
		})
	mcp.AddTool(server, &mcp.Tool{Name: "grok_project_list", Description: "列出已导入和已发现项目。", Annotations: annProjRead},
		func(ctx context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, []protocol.Project, error) {
			out, err := backend.ListProjects(ctx)
			return nil, out, err
		})
	mcp.AddTool(server, &mcp.Tool{Name: "grok_project_get", Description: "读取项目详情与 Skill 状态。", Annotations: annProjRead},
		func(ctx context.Context, _ *mcp.CallToolRequest, in protocol.ProjectIDRequest) (*mcp.CallToolResult, protocol.Project, error) {
			out, err := backend.GetProject(ctx, in.ProjectID)
			return nil, out, err
		})
	mcp.AddTool(server, &mcp.Tool{Name: "grok_project_remove", Description: "将导入项目降级为已发现，不删除源码、job、session 或 Skill。", Annotations: annProjWrite},
		func(ctx context.Context, _ *mcp.CallToolRequest, in protocol.ProjectIDRequest) (*mcp.CallToolResult, protocol.Project, error) {
			out, err := backend.RemoveProject(ctx, in.ProjectID)
			return nil, out, err
		})
	mcp.AddTool(server, &mcp.Tool{Name: "grok_skill_status", Description: "探测项目工具说明 Skill 状态。", Annotations: annSkillRead},
		func(ctx context.Context, _ *mcp.CallToolRequest, in protocol.ProjectIDRequest) (*mcp.CallToolResult, protocol.Project, error) {
			out, err := backend.SkillStatus(ctx, in.ProjectID)
			return nil, out, err
		})
	mcp.AddTool(server, &mcp.Tool{Name: "grok_skill_install", Description: "安装项目工具说明 Skill；用户文件冲突时不覆盖。", Annotations: annSkillAdd},
		func(ctx context.Context, _ *mcp.CallToolRequest, in protocol.ProjectIDRequest) (*mcp.CallToolResult, protocol.Project, error) {
			out, err := backend.SkillInstall(ctx, in.ProjectID)
			return nil, out, err
		})
	mcp.AddTool(server, &mcp.Tool{Name: "grok_skill_update", Description: "更新自身管理的项目 Skill。", Annotations: annSkillUpdate},
		func(ctx context.Context, _ *mcp.CallToolRequest, in protocol.ProjectIDRequest) (*mcp.CallToolResult, protocol.Project, error) {
			out, err := backend.SkillUpdate(ctx, in.ProjectID)
			return nil, out, err
		})
	mcp.AddTool(server, &mcp.Tool{Name: "grok_skill_remove", Description: "只删除自身管理的 Skill 文件。", Annotations: annSkillRemove},
		func(ctx context.Context, _ *mcp.CallToolRequest, in protocol.ProjectIDRequest) (*mcp.CallToolResult, protocol.Project, error) {
			out, err := backend.SkillRemove(ctx, in.ProjectID)
			return nil, out, err
		})
	mcp.AddTool(server, &mcp.Tool{Name: "grok_prompt_generate", Description: "生成可复制的 Codex 提示词，不发送、不注入会话。", Annotations: annPromptRead},
		func(ctx context.Context, _ *mcp.CallToolRequest, in protocol.GeneratePromptRequest) (*mcp.CallToolResult, protocol.PromptResult, error) {
			out, err := backend.GeneratePrompt(ctx, in)
			return nil, out, err
		})
	mcp.AddTool(server, &mcp.Tool{Name: "grok_prompt_save", Description: "保存项目默认 Codex 提示词。", Annotations: annPromptSave},
		func(ctx context.Context, _ *mcp.CallToolRequest, in protocol.SavePromptRequest) (*mcp.CallToolResult, protocol.Project, error) {
			out, err := backend.SavePrompt(ctx, in)
			return nil, out, err
		})
}
