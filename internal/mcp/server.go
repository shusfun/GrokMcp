package mcp

import (
	"context"

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

func newServer(backend core.Backend) *mcp.Server {
	server := mcp.NewServer(&mcp.Implementation{Name: "grok_supervisor", Version: version.Version}, nil)
	addTools(server, backend)
	return server
}

func addTools(server *mcp.Server, backend core.Backend) {
	mcp.AddTool(server, &mcp.Tool{Name: "grok_dispatch", Description: "批量创建任务，默认 headless，返回 job 与 session 映射。"},
		func(ctx context.Context, _ *mcp.CallToolRequest, in protocol.DispatchRequest) (*mcp.CallToolResult, protocol.DispatchResult, error) {
			out, err := backend.Dispatch(ctx, in)
			return nil, out, err
		})
	mcp.AddTool(server, &mcp.Tool{Name: "grok_wait", Description: "等待 any/all 任务到达关键边界；不返回过程输出。"},
		func(ctx context.Context, _ *mcp.CallToolRequest, in protocol.WaitRequest) (*mcp.CallToolResult, protocol.WaitResult, error) {
			out, err := backend.Wait(ctx, in)
			return nil, out, err
		})
	mcp.AddTool(server, &mcp.Tool{Name: "grok_plan_decide", Description: "批准、退回或取消 Plan。"},
		func(ctx context.Context, _ *mcp.CallToolRequest, in protocol.PlanDecideRequest) (*mcp.CallToolResult, protocol.Job, error) {
			out, err := backend.PlanDecide(ctx, in)
			return nil, out, err
		})
	mcp.AddTool(server, &mcp.Tool{Name: "grok_followup", Description: "向同一 session 追加返工、继续或验收意见。"},
		func(ctx context.Context, _ *mcp.CallToolRequest, in protocol.FollowupRequest) (*mcp.CallToolResult, protocol.Job, error) {
			out, err := backend.Followup(ctx, in)
			return nil, out, err
		})
	mcp.AddTool(server, &mcp.Tool{Name: "grok_cancel_turn", Description: "取消当前 turn，保留 session 和排队状态。"},
		func(ctx context.Context, _ *mcp.CallToolRequest, in struct {
			JobID string `json:"job_id" jsonschema:"job id"`
		}) (*mcp.CallToolResult, protocol.Job, error) {
			out, err := backend.CancelTurn(ctx, in.JobID)
			return nil, out, err
		})
	mcp.AddTool(server, &mcp.Tool{Name: "grok_set_view", Description: "在 headless 与 headed 之间切换，不重建任务。"},
		func(ctx context.Context, _ *mcp.CallToolRequest, in protocol.SetViewRequest) (*mcp.CallToolResult, protocol.Job, error) {
			out, err := backend.SetView(ctx, in)
			return nil, out, err
		})
	mcp.AddTool(server, &mcp.Tool{Name: "grok_status", Description: "返回阶段、显示状态、摘要和最近边界事件。"},
		func(ctx context.Context, _ *mcp.CallToolRequest, in struct {
			JobID string `json:"job_id,omitempty" jsonschema:"optional job id"`
		}) (*mcp.CallToolResult, any, error) {
			if in.JobID != "" {
				job, err := backend.Status(ctx, in.JobID)
				return nil, job, err
			}
			jobs, err := backend.ListJobs(ctx)
			return nil, jobs, err
		})
	mcp.AddTool(server, &mcp.Tool{Name: "grok_open_terminal", Description: "打开单任务 TUI 或集中 Grok Dashboard。"},
		func(ctx context.Context, _ *mcp.CallToolRequest, in protocol.OpenTerminalRequest) (*mcp.CallToolResult, struct{}, error) {
			return nil, struct{}{}, backend.OpenTerminal(ctx, in)
		})
	mcp.AddTool(server, &mcp.Tool{Name: "grok_debug_set", Description: "打开或关闭调试记录；job_id 为空时为全局调试模式。过程日志默认不发给 Codex。"},
		func(ctx context.Context, _ *mcp.CallToolRequest, in protocol.DebugSetRequest) (*mcp.CallToolResult, protocol.Job, error) {
			out, err := backend.DebugSet(ctx, in)
			return nil, out, err
		})
	mcp.AddTool(server, &mcp.Tool{Name: "grok_debug_snapshot", Description: "按 cursor 增量读取有限条结构化日志，不含完整 transcript。"},
		func(ctx context.Context, _ *mcp.CallToolRequest, in protocol.DebugSnapshotRequest) (*mcp.CallToolResult, protocol.DebugSnapshot, error) {
			out, err := backend.DebugSnapshot(ctx, in)
			return nil, out, err
		})
	mcp.AddTool(server, &mcp.Tool{Name: "grok_debug_wait", Description: "等待下一条 any/warning/error/boundary 调试事件。"},
		func(ctx context.Context, _ *mcp.CallToolRequest, in protocol.DebugWaitRequest) (*mcp.CallToolResult, protocol.DebugSnapshot, error) {
			out, err := backend.DebugWait(ctx, in)
			return nil, out, err
		})
	mcp.AddTool(server, &mcp.Tool{Name: "grok_debug_export", Description: "导出任务 jsonl 诊断包路径。"},
		func(ctx context.Context, _ *mcp.CallToolRequest, in struct {
			JobID string `json:"job_id" jsonschema:"job id"`
		}) (*mcp.CallToolResult, protocol.DebugExportResult, error) {
			out, err := backend.DebugExport(ctx, in.JobID)
			return nil, out, err
		})
	mcp.AddTool(server, &mcp.Tool{Name: "grok_project_import", Description: "登记项目。默认安装工具说明 Skill；冲突不覆盖用户文件，项目仍成功。"},
		func(ctx context.Context, _ *mcp.CallToolRequest, in protocol.ImportProjectRequest) (*mcp.CallToolResult, protocol.Project, error) {
			install := true
			if in.InstallSkill != nil {
				install = *in.InstallSkill
			}
			out, err := backend.ImportProject(ctx, in.Path, install)
			return nil, out, err
		})
	mcp.AddTool(server, &mcp.Tool{Name: "grok_project_list", Description: "列出已导入和已发现项目。"},
		func(ctx context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, []protocol.Project, error) {
			out, err := backend.ListProjects(ctx)
			return nil, out, err
		})
	mcp.AddTool(server, &mcp.Tool{Name: "grok_project_get", Description: "读取项目详情与 Skill 状态。"},
		func(ctx context.Context, _ *mcp.CallToolRequest, in protocol.ProjectIDRequest) (*mcp.CallToolResult, protocol.Project, error) {
			out, err := backend.GetProject(ctx, in.ProjectID)
			return nil, out, err
		})
	mcp.AddTool(server, &mcp.Tool{Name: "grok_project_remove", Description: "将导入项目降级为已发现，不删除源码、job、session 或 Skill。"},
		func(ctx context.Context, _ *mcp.CallToolRequest, in protocol.ProjectIDRequest) (*mcp.CallToolResult, protocol.Project, error) {
			out, err := backend.RemoveProject(ctx, in.ProjectID)
			return nil, out, err
		})
	mcp.AddTool(server, &mcp.Tool{Name: "grok_skill_status", Description: "探测项目工具说明 Skill 状态。"},
		func(ctx context.Context, _ *mcp.CallToolRequest, in protocol.ProjectIDRequest) (*mcp.CallToolResult, protocol.Project, error) {
			out, err := backend.SkillStatus(ctx, in.ProjectID)
			return nil, out, err
		})
	mcp.AddTool(server, &mcp.Tool{Name: "grok_skill_install", Description: "安装项目工具说明 Skill；用户文件冲突时不覆盖。"},
		func(ctx context.Context, _ *mcp.CallToolRequest, in protocol.ProjectIDRequest) (*mcp.CallToolResult, protocol.Project, error) {
			out, err := backend.SkillInstall(ctx, in.ProjectID)
			return nil, out, err
		})
	mcp.AddTool(server, &mcp.Tool{Name: "grok_skill_update", Description: "更新自身管理的项目 Skill。"},
		func(ctx context.Context, _ *mcp.CallToolRequest, in protocol.ProjectIDRequest) (*mcp.CallToolResult, protocol.Project, error) {
			out, err := backend.SkillUpdate(ctx, in.ProjectID)
			return nil, out, err
		})
	mcp.AddTool(server, &mcp.Tool{Name: "grok_skill_remove", Description: "只删除自身管理的 Skill 文件。"},
		func(ctx context.Context, _ *mcp.CallToolRequest, in protocol.ProjectIDRequest) (*mcp.CallToolResult, protocol.Project, error) {
			out, err := backend.SkillRemove(ctx, in.ProjectID)
			return nil, out, err
		})
	mcp.AddTool(server, &mcp.Tool{Name: "grok_prompt_generate", Description: "生成可复制的 Codex 提示词，不发送、不注入会话。"},
		func(ctx context.Context, _ *mcp.CallToolRequest, in protocol.GeneratePromptRequest) (*mcp.CallToolResult, protocol.PromptResult, error) {
			out, err := backend.GeneratePrompt(ctx, in)
			return nil, out, err
		})
	mcp.AddTool(server, &mcp.Tool{Name: "grok_prompt_save", Description: "保存项目默认 Codex 提示词。"},
		func(ctx context.Context, _ *mcp.CallToolRequest, in protocol.SavePromptRequest) (*mcp.CallToolResult, protocol.Project, error) {
			out, err := backend.SavePrompt(ctx, in)
			return nil, out, err
		})
}
