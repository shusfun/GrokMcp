package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	acp "github.com/coder/acp-go-sdk"
	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"grokmcp/internal/agent"
	"grokmcp/internal/core"
	"grokmcp/internal/ipc"
	"grokmcp/internal/protocol"
	"grokmcp/internal/store"
	"grokmcp/internal/supervisor"
	"grokmcp/internal/terminal"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

type protocolPeer struct {
	genericPermission bool
	afterApproval     func()
	acp.Agent
	conn      *acp.AgentSideConnection
	cwd       string
	turns     atomic.Int32
	sessions  atomic.Int32
	approvals atomic.Int32
	plain     bool
}

func (p *protocolPeer) Initialize(_ context.Context, r acp.InitializeRequest) (acp.InitializeResponse, error) {
	if !r.ClientCapabilities.Fs.WriteTextFile || r.ClientCapabilities.Terminal {
		return acp.InitializeResponse{}, fmt.Errorf("incorrect advertised client capabilities")
	}
	return acp.InitializeResponse{ProtocolVersion: acp.ProtocolVersionNumber, AgentCapabilities: acp.AgentCapabilities{LoadSession: true}}, nil
}
func (p *protocolPeer) NewSession(_ context.Context, r acp.NewSessionRequest) (acp.NewSessionResponse, error) {
	p.sessions.Add(1)
	p.cwd = r.Cwd
	return acp.NewSessionResponse{SessionId: "wire-session"}, nil
}
func (p *protocolPeer) LoadSession(_ context.Context, r acp.LoadSessionRequest) (acp.LoadSessionResponse, error) {
	if r.SessionId != "wire-session" {
		return acp.LoadSessionResponse{}, fmt.Errorf("replacement session")
	}
	return acp.LoadSessionResponse{}, nil
}
func (p *protocolPeer) SetSessionMode(context.Context, acp.SetSessionModeRequest) (acp.SetSessionModeResponse, error) {
	return acp.SetSessionModeResponse{}, nil
}
func (p *protocolPeer) Cancel(context.Context, acp.CancelNotification) error { return nil }
func (p *protocolPeer) Prompt(ctx context.Context, r acp.PromptRequest) (acp.PromptResponse, error) {
	n := p.turns.Add(1)
	if n > 1 {
		if p.genericPermission {
			title := "exit_plan_mode"
			decision, err := p.conn.RequestPermission(ctx, acp.RequestPermissionRequest{SessionId: r.SessionId, ToolCall: acp.ToolCallUpdate{Title: &title, ToolCallId: "native-permission"}, Options: []acp.PermissionOption{{OptionId: "allow", Kind: acp.PermissionOptionKindAllowOnce, Name: "Allow"}, {OptionId: "reject", Kind: acp.PermissionOptionKindRejectOnce, Name: "Reject"}}})
			if err != nil {
				return acp.PromptResponse{}, err
			}
			if decision.Outcome.Selected == nil || decision.Outcome.Selected.OptionId != "allow" {
				return acp.PromptResponse{StopReason: "cancelled"}, nil
			}
		} else {
			raw, err := p.conn.CallExtension(ctx, "_x.ai/exit_plan_mode", map[string]any{"sessionId": r.SessionId, "toolCallId": fmt.Sprint(n), "planContent": fmt.Sprintf("current plan %d: write verified.txt and check it", n)})
			if err != nil {
				return acp.PromptResponse{}, err
			}
			var decision struct{ Outcome, Comment string }
			if err := json.Unmarshal(raw, &decision); err != nil {
				return acp.PromptResponse{}, err
			}
			if decision.Outcome != "approved" {
				return acp.PromptResponse{}, fmt.Errorf("unexpected decision %s", raw)
			}
			p.approvals.Add(1)
		}
		if p.afterApproval != nil {
			p.afterApproval()
			return acp.PromptResponse{}, fmt.Errorf("injected disconnect after decision")
		}

		content := fmt.Sprintf("verified turn %d", n)
		if _, err := p.conn.WriteTextFile(ctx, acp.WriteTextFileRequest{SessionId: r.SessionId, Path: filepath.Join(p.cwd, "verified.txt"), Content: content}); err != nil {
			return acp.PromptResponse{}, err
		}
		got, err := p.conn.ReadTextFile(ctx, acp.ReadTextFileRequest{SessionId: r.SessionId, Path: filepath.Join(p.cwd, "verified.txt")})
		if err != nil || got.Content != content {
			return acp.PromptResponse{}, fmt.Errorf("file roundtrip: %v", err)
		}
	}
	text := protocol.RenderTaskState(protocol.TaskState{State: protocol.MarkerCompleted, Summary: "GROK_OK"})
	if n > 1 {
		text = "文件已修改并通过读取校验。" + strings.Repeat("结果🙂", 20)
		if !p.plain {
			text += "\n" + protocol.RenderTaskState(protocol.TaskState{State: protocol.MarkerCompleted, Summary: "verified"})
		}
	}
	if err := p.conn.SessionUpdate(ctx, acp.SessionNotification{SessionId: r.SessionId, Update: acp.SessionUpdate{AgentMessageChunk: &acp.SessionUpdateAgentMessageChunk{Content: acp.TextBlock(text)}}}); err != nil {
		return acp.PromptResponse{}, err
	}
	return acp.PromptResponse{StopReason: "end_turn"}, nil
}

type protocolBackend struct {
	core.Backend
	svc *supervisor.Service
}

func (b protocolBackend) Dispatch(c context.Context, r protocol.DispatchRequest) (protocol.DispatchResult, error) {
	return b.svc.Dispatch(c, r)
}
func (b protocolBackend) Followup(c context.Context, r protocol.FollowupRequest) (protocol.Job, error) {
	return b.svc.Followup(c, r)
}
func (b protocolBackend) Wait(c context.Context, r protocol.WaitRequest) (protocol.WaitResult, error) {
	return b.svc.Wait(c, r)
}
func (b protocolBackend) PlanDecide(c context.Context, r protocol.PlanDecideRequest) (protocol.Job, error) {
	return b.svc.PlanDecide(c, r)
}
func (b protocolBackend) Status(c context.Context, id string, q ...protocol.ResultQuery) (protocol.Job, error) {
	return b.svc.Status(c, id, q...)
}
func (b protocolBackend) Subscribe(fn func(protocol.Event)) func() { return b.svc.Subscribe(fn) }

func flowTool[T any](t *testing.T, c *sdk.ClientSession, name string, args any) T {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	res, err := c.CallTool(ctx, &sdk.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		t.Fatal(name, err)
	}
	if res.IsError {
		t.Fatalf("%s: %+v", name, res.Content)
	}
	b, err := json.Marshal(res.StructuredContent)
	if err != nil {
		t.Fatal(err)
	}
	var out T
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatalf("%s %s %v", name, b, err)
	}
	return out
}
func TestMCPIPCSupervisorACPCompleteLifecycle(t *testing.T) {
	for _, plain := range []bool{false, true} {
		t.Run(fmt.Sprint("plain=", plain), func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
			defer cancel()
			clientWire, serverWire := net.Pipe()
			defer serverWire.Close()
			peer := &protocolPeer{plain: plain}
			peer.conn = acp.NewAgentSideConnection(peer, serverWire, serverWire)
			grok, err := agent.NewTransport(ctx, clientWire, clientWire)
			if err != nil {
				t.Fatal(err)
			}
			st, err := store.Open(filepath.Join(t.TempDir(), "flow.db"))
			if err != nil {
				t.Fatal(err)
			}
			defer st.Close()
			svc := supervisor.New(st, grok, terminal.NewFake(), nil, nil)
			defer svc.Close()
			ln, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			server := ipc.Serve(ln, protocolBackend{svc: svc})
			defer server.Close()
			ipcClient, err := ipc.Dial(ctx, "tcp", ln.Addr().String())
			if err != nil {
				t.Fatal(err)
			}
			defer ipcClient.Close()
			mcpClient := connectMCP(t, ipcClient)
			cwd := t.TempDir()
			dispatched := flowTool[protocol.DispatchResult](t, mcpClient, "grok_dispatch", protocol.DispatchRequest{Cwd: cwd, Tasks: []protocol.DispatchTask{{Prompt: "connectivity"}}})
			id := dispatched.Jobs[0].JobID
			first := flowTool[protocol.WaitResult](t, mcpClient, "grok_wait", protocol.WaitRequest{JobIDs: []string{id}})
			if first.Jobs[0].State != protocol.StateCompleted {
				t.Fatalf("initial %+v", first)
			}
			cursor := first.Cursors
			for round := 0; round < 2; round++ {
				request := flowTool[protocol.Job](t, mcpClient, "grok_followup", protocol.FollowupRequest{JobID: id, Prompt: "write a file and verify it", Replan: true})
				plan := flowTool[protocol.WaitResult](t, mcpClient, "grok_wait", protocol.WaitRequest{JobIDs: []string{id}, Cursors: cursor})
				j := plan.Jobs[0]
				if j.State != protocol.StatePlanReady || !j.Busy || j.ApprovalID == "" || j.PlanSummary == "" || j.RequestID != request.AcceptedRequestID {
					t.Fatalf("lost real ACP approval %+v", j)
				}
				decision := protocol.PlanDecideRequest{JobID: id, ApprovalID: j.ApprovalID, RequestID: j.RequestID, TurnID: j.PlanTurnID, PlanVersion: j.PlanVersion, Decide: protocol.PlanApprove, Notes: "write only the test file"}
				flowTool[protocol.Job](t, mcpClient, "grok_plan_decide", decision)
				done := flowTool[protocol.WaitResult](t, mcpClient, "grok_wait", protocol.WaitRequest{JobIDs: []string{id}, Cursors: plan.Cursors})
				final := done.Jobs[0]
				want := protocol.StateCompleted
				if plain {
					want = protocol.StateNeedsInput
				}
				if final.State != want || final.Busy || final.QueueLength != 0 || final.ActiveTurnID != "" || final.ApprovalDelivery != "confirmed" {
					t.Fatalf("incomplete result %+v", final)
				}
				result := flowTool[protocol.Job](t, mcpClient, "grok_status", map[string]any{"job_id": id, "include_result": true, "request_id": j.RequestID, "limit": 7})
				if result.Result == nil || !result.Result.HasMore || len([]rune(result.Result.Text)) != 7 {
					t.Fatalf("missing paged answer %+v", result)
				}
				tail := flowTool[protocol.Job](t, mcpClient, "grok_status", map[string]any{"job_id": id, "include_result": true, "request_id": j.RequestID, "turn_id": result.Result.TurnID, "offset": result.Result.NextOffset})
				if !strings.Contains(result.Result.Text+tail.Result.Text, "结果🙂") {
					t.Fatal("UTF-8 answer corrupted")
				}
				cursor = done.Cursors
			}
			if peer.turns.Load() != 3 || peer.sessions.Load() != 1 || peer.approvals.Load() != 2 {
				t.Fatalf("duplicate prompt/session: turns=%d sessions=%d approvals=%d", peer.turns.Load(), peer.sessions.Load(), peer.approvals.Load())
			}
			if b, err := os.ReadFile(filepath.Join(cwd, "verified.txt")); err != nil || string(b) != "verified turn 3" {
				t.Fatalf("file %s %v", b, err)
			}
		})
	}
}

func TestProtocolDisconnectAfterDecisionIsNotReportedAsExecution(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	a, b := net.Pipe()
	defer b.Close()
	peer := &protocolPeer{afterApproval: func() { b.Close() }}
	peer.turns.Store(1)
	peer.conn = acp.NewAgentSideConnection(peer, b, b)
	grok, err := agent.NewTransport(ctx, a, a)
	if err != nil {
		t.Fatal(err)
	}
	st, err := store.Open(filepath.Join(t.TempDir(), "disconnect.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	svc := supervisor.New(st, grok, terminal.NewFake(), nil, nil)
	defer svc.Close()
	dispatched, err := svc.Dispatch(ctx, protocol.DispatchRequest{Cwd: t.TempDir(), Tasks: []protocol.DispatchTask{{Prompt: "plan"}}})
	if err != nil {
		t.Fatal(err)
	}
	id := dispatched.Jobs[0].JobID
	ready, err := svc.Wait(ctx, protocol.WaitRequest{JobIDs: []string{id}})
	if err != nil {
		t.Fatal(err)
	}
	j := ready.Jobs[0]
	_, _ = svc.PlanDecide(ctx, protocol.PlanDecideRequest{JobID: id, ApprovalID: j.ApprovalID, RequestID: j.RequestID, TurnID: j.PlanTurnID, PlanVersion: j.PlanVersion, Decide: protocol.PlanApprove})
	end, err := svc.Wait(ctx, protocol.WaitRequest{JobIDs: []string{id}, Cursors: ready.Cursors})
	if err != nil {
		t.Fatal(err)
	}
	if got := end.Jobs[0]; got.State == protocol.StateCompleted || got.ApprovalDelivery != "unknown" || got.Approved {
		t.Fatalf("unconfirmed delivery reported as success %+v", got)
	}
	if peer.turns.Load() != 2 {
		t.Fatal("automatically retried failed prompt")
	}
}
func TestNativePermissionRejectsNotesBeforeRelease(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	a, b := net.Pipe()
	defer b.Close()
	peer := &protocolPeer{genericPermission: true}
	peer.turns.Store(1)
	peer.conn = acp.NewAgentSideConnection(peer, b, b)
	grok, err := agent.NewTransport(ctx, a, a)
	if err != nil {
		t.Fatal(err)
	}
	st, err := store.Open(filepath.Join(t.TempDir(), "permission.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	svc := supervisor.New(st, grok, terminal.NewFake(), nil, nil)
	defer svc.Close()
	res, err := svc.Dispatch(ctx, protocol.DispatchRequest{Cwd: t.TempDir(), Tasks: []protocol.DispatchTask{{Prompt: "plan"}}})
	if err != nil {
		t.Fatal(err)
	}
	ready, err := svc.Wait(ctx, protocol.WaitRequest{JobIDs: []string{res.Jobs[0].JobID}})
	if err != nil {
		t.Fatal(err)
	}
	j := ready.Jobs[0]
	d := protocol.PlanDecideRequest{JobID: j.JobID, ApprovalID: j.ApprovalID, RequestID: j.RequestID, TurnID: j.PlanTurnID, PlanVersion: j.PlanVersion, Decide: protocol.PlanRevise, Notes: "cannot silently drop this"}
	if _, err := svc.PlanDecide(ctx, d); err == nil || !strings.Contains(err.Error(), "does not support") {
		t.Fatalf("unsupported notes %v", err)
	}
	if _, err := grok.PendingApproval(j.ApprovalID); err != nil {
		t.Fatal("notes rejection released permission")
	}
	d.Notes = ""
	d.Decide = protocol.PlanCancel
	if _, err := svc.PlanDecide(ctx, d); err != nil {
		t.Fatal(err)
	}
}
