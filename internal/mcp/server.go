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
}
