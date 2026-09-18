package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"

	acp "github.com/coder/acp-go-sdk"
	"github.com/google/uuid"
	"grokmcp/internal/grokbin"
	"grokmcp/internal/paths"
	"grokmcp/internal/protocol"
	"grokmcp/internal/trace"
)

const exitPlanMethod = "_x.ai/exit_plan_mode"

type planChoice struct {
	decide protocol.PlanDecision
	notes  string
}

type exitPlanExtReq struct {
	SessionID   string `json:"sessionId"`
	ToolCallID  string `json:"toolCallId"`
	PlanContent string `json:"planContent"`
}

type Grok struct {
	Bin     string
	Finder  grokbin.Finder
	mu      sync.Mutex
	cmd     *exec.Cmd
	conn    *acp.ClientSideConnection
	client  *acpClient
	last    map[string]string
	plan    map[string]bool
	pending map[string]chan planChoice
	arm     map[string]planChoice
	attach  string
	planFn  func(string, string)
	trace   trace.Sink
}

func NewGrok(bin string, finder grokbin.Finder) *Grok {
	return &Grok{
		Bin: bin, Finder: finder, last: map[string]string{}, plan: map[string]bool{},
		pending: map[string]chan planChoice{}, arm: map[string]planChoice{}, attach: "boundary",
	}
}

func (g *Grok) SetTrace(s trace.Sink) { g.trace = s }

func (g *Grok) emitTrace(sessionID, level, name, message string, fields map[string]any) {
	if g.trace == nil {
		return
	}
	g.trace.Emit(trace.Event{
		Level:     level,
		Source:    trace.SourceACP,
		Name:      name,
		SessionID: sessionID,
		Message:   message,
		Fields:    fields,
	})
}

func (g *Grok) AttachMode() string {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.attach == "" {
		return "boundary"
	}
	return g.attach
}

func (g *Grok) Diagnose(ctx context.Context) protocol.DiagnoseResult {
	d := g.Finder.Diagnose(ctx, g.Bin)
	d.AttachMode = g.AttachMode()
	g.mu.Lock()
	d.ACPOK = g.conn != nil
	g.mu.Unlock()
	return d
}

func (g *Grok) EnsureLeader(ctx context.Context) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.conn != nil {
		return nil
	}
	bin := g.Bin
	if bin == "" {
		var err error
		bin, err = g.Finder.Resolve("")
		if err != nil {
			return err
		}
		g.Bin = bin
	}
	if err := g.ensureLeaderProcess(bin); err != nil {
		return err
	}
	cmd := exec.CommandContext(ctx, bin, "agent", "--leader", "stdio")
	cmd.Stderr = os.Stderr
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	client := newACPClient()
	client.onUpdate = func(sessionID, lastAction string, planReady bool) {
		g.mu.Lock()
		if lastAction != "" {
			g.last[sessionID] = lastAction
		}
		if planReady {
			g.plan[sessionID] = true
		}
		g.mu.Unlock()
		if lastAction != "" {
			g.emitTrace(sessionID, trace.LevelDebug, "acp.session_update", lastAction, map[string]any{"action": lastAction, "plan_ready": planReady})
		}
	}
	client.onPermission = g.handlePermission
	client.onExitPlan = g.handleExitPlanMode
	conn := acp.NewClientSideConnection(client, stdin, stdout)
	if _, err := conn.Initialize(ctx, acp.InitializeRequest{
		ProtocolVersion: acp.ProtocolVersionNumber,
		ClientCapabilities: acp.ClientCapabilities{
			Fs: acp.FileSystemCapabilities{ReadTextFile: true, WriteTextFile: true},
		},
		ClientInfo: &acp.Implementation{Name: "Grok Supervisor", Version: "0.1.0"},
	}); err != nil {
		_ = cmd.Process.Kill()
		return err
	}
	g.cmd = cmd
	g.conn = conn
	g.client = client
	g.mu.Unlock()
	g.emitTrace("", trace.LevelInfo, "leader.connected", "ACP leader connected", nil)
	go func() {
		_ = cmd.Wait()
		g.mu.Lock()
		if g.cmd == cmd {
			g.conn = nil
			g.cmd = nil
		}
		g.mu.Unlock()
		g.emitTrace("", trace.LevelWarn, "leader.disconnected", "ACP leader disconnected", nil)
	}()
	g.probeAttach(ctx)
	g.mu.Lock()
	return nil
}

func (g *Grok) ensureLeaderProcess(bin string) error {
	sock := paths.LeaderSocket()
	if sock != "" {
		if st, err := os.Stat(sock); err == nil && st.Mode()&os.ModeSocket != 0 {
			return nil
		}
	}
	cmd := exec.Command(bin, "agent", "leader", "--no-exit-on-disconnect")
	cmd.Stdout = nil
	cmd.Stderr = nil
	if err := cmd.Start(); err != nil {
		return err
	}
	go func() { _ = cmd.Wait() }()
	return nil
}

func (g *Grok) NewSession(ctx context.Context, cwd string, worktree bool) (string, string, error) {
	if err := g.EnsureLeader(ctx); err != nil {
		return "", "", err
	}
	if worktree {
		wt, err := gitWorktree(cwd)
		if err != nil {
			return "", "", err
		}
		cwd = wt
	}
	g.mu.Lock()
	conn := g.conn
	g.mu.Unlock()
	resp, err := conn.NewSession(ctx, acp.NewSessionRequest{
		Cwd:        cwd,
		McpServers: []acp.McpServer{},
		Meta:       map[string]any{"yoloMode": false},
	})
	if err != nil {
		return "", "", err
	}
	id := string(resp.SessionId)
	if err := setPlanMode(ctx, conn.SetSessionMode, resp.SessionId, resp.Modes); err != nil {
		return "", "", err
	}
	return id, cwd, nil
}

func (g *Grok) LoadSession(ctx context.Context, sessionID, cwd string) error {
	if err := g.EnsureLeader(ctx); err != nil {
		return err
	}
	g.mu.Lock()
	conn := g.conn
	g.mu.Unlock()
	g.emitTrace(sessionID, trace.LevelInfo, "session.load.started", "load session", nil)
	_, err := conn.LoadSession(ctx, acp.LoadSessionRequest{
		SessionId:  acp.SessionId(sessionID),
		Cwd:        cwd,
		McpServers: []acp.McpServer{},
	})
	if err != nil {
		g.emitTrace(sessionID, trace.LevelError, "session.load.failed", err.Error(), map[string]any{"error": err.Error()})
		return err
	}
	g.emitTrace(sessionID, trace.LevelInfo, "session.load.completed", "load session", nil)
	return nil
}

func (g *Grok) Prompt(ctx context.Context, sessionID, text string) (PromptResult, error) {
	if err := g.EnsureLeader(ctx); err != nil {
		return PromptResult{}, err
	}
	g.mu.Lock()
	conn := g.conn
	client := g.client
	g.plan[sessionID] = false
	g.mu.Unlock()
	if client != nil {
		client.resetText(sessionID)
	}
	resp, err := conn.Prompt(ctx, acp.PromptRequest{
		SessionId: acp.SessionId(sessionID),
		Prompt:    []acp.ContentBlock{acp.TextBlock(text)},
	})
	if err != nil {
		if ctx.Err() != nil {
			return PromptResult{StopReason: "cancelled"}, err
		}
		return PromptResult{}, err
	}
	out := ""
	if client != nil {
		out = client.takeText(sessionID)
	}
	g.mu.Lock()
	res := PromptResult{
		Text:       out,
		StopReason: string(resp.StopReason),
		LastAction: g.last[sessionID],
		PlanReady:  g.plan[sessionID],
	}
	g.mu.Unlock()
	return res, nil
}

func (g *Grok) Cancel(ctx context.Context, sessionID string) error {
	g.mu.Lock()
	conn := g.conn
	g.mu.Unlock()
	if conn == nil {
		return nil
	}
	return conn.Cancel(ctx, acp.CancelNotification{SessionId: acp.SessionId(sessionID)})
}

func (g *Grok) ResolvePlan(_ context.Context, sessionID string, decide protocol.PlanDecision, notes string) error {
	choice := planChoice{decide: decide, notes: notes}
	g.mu.Lock()
	ch := g.pending[sessionID]
	if ch == nil {
		if g.arm == nil {
			g.arm = map[string]planChoice{}
		}
		g.arm[sessionID] = choice
		g.mu.Unlock()
		return ErrNoPlanPermission
	}
	g.mu.Unlock()
	select {
	case ch <- choice:
	default:
	}
	return nil
}

func (g *Grok) consumeArm(sessionID string) (planChoice, bool) {
	g.mu.Lock()
	defer g.mu.Unlock()
	v, ok := g.arm[sessionID]
	if ok {
		delete(g.arm, sessionID)
	}
	return v, ok
}

func (g *Grok) clearPending(sessionID string) {
	g.mu.Lock()
	delete(g.pending, sessionID)
	g.mu.Unlock()
}

func (g *Grok) handlePermission(ctx context.Context, params acp.RequestPermissionRequest) (acp.RequestPermissionResponse, error) {
	sid := string(params.SessionId)
	g.emitTrace(sid, trace.LevelInfo, "acp.permission", "permission request", map[string]any{
		"tool": params.ToolCall.Title,
	})
	if !isExitPlanCall(params.ToolCall) {
		return allowPermission(params), nil
	}
	choice, err := g.awaitPlanChoice(ctx, sid, "")
	if err != nil {
		return cancelPermission(), err
	}
	return g.finishPlanPermission(params, choice.decide == protocol.PlanApprove)
}

func (g *Grok) handleExitPlanMode(ctx context.Context, raw json.RawMessage) (any, error) {
	var req exitPlanExtReq
	if err := json.Unmarshal(raw, &req); err != nil {
		return nil, err
	}
	sid := req.SessionID
	if sid == "" {
		return nil, acp.NewInvalidParams(map[string]any{"error": "sessionId required"})
	}
	choice, err := g.awaitPlanChoice(ctx, sid, req.PlanContent)
	if err != nil {
		return nil, err
	}
	return exitPlanResponse(choice.decide, choice.notes), nil
}

func (g *Grok) awaitPlanChoice(ctx context.Context, sid, excerpt string) (planChoice, error) {
	if choice, armed := g.consumeArm(sid); armed {
		g.setPlanFlag(sid, choice.decide == protocol.PlanApprove)
		return choice, nil
	}
	g.mu.Lock()
	g.plan[sid] = true
	ch := make(chan planChoice, 1)
	g.pending[sid] = ch
	g.last[sid] = "Plan ready"
	hook := g.planFn
	g.mu.Unlock()
	if hook != nil {
		hook(sid, excerpt)
	}
	select {
	case <-ctx.Done():
		g.clearPending(sid)
		g.setPlanFlag(sid, false)
		return planChoice{}, ctx.Err()
	case choice := <-ch:
		g.clearPending(sid)
		g.setPlanFlag(sid, choice.decide == protocol.PlanApprove)
		return choice, nil
	}
}

func (g *Grok) finishPlanPermission(params acp.RequestPermissionRequest, approve bool) (acp.RequestPermissionResponse, error) {
	if approve {
		if resp, ok := pickPermission(params, true); ok {
			return resp, nil
		}
		return cancelPermission(), errors.New("no allow option")
	}
	if resp, ok := pickPermission(params, false); ok {
		return resp, nil
	}
	return cancelPermission(), errors.New("no reject option")
}

func exitPlanResponse(decide protocol.PlanDecision, notes string) map[string]any {
	out := map[string]any{"comment": notes}
	switch decide {
	case protocol.PlanApprove:
		out["outcome"] = "approved"
	case protocol.PlanRevise:
		out["outcome"] = "request_changes"
	default:
		out["outcome"] = "abandoned"
	}
	return out
}

func (g *Grok) setPlanFlag(sessionID string, ready bool) {
	g.mu.Lock()
	g.plan[sessionID] = ready
	g.mu.Unlock()
}

func (g *Grok) SetPlanListener(fn func(string, string)) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.planFn = fn
}

func (g *Grok) Close() error {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.cmd != nil && g.cmd.Process != nil {
		_ = g.cmd.Process.Kill()
	}
	g.conn = nil
	return nil
}

func isExitPlanCall(tc acp.ToolCallUpdate) bool {
	if looksLikeExitPlan(deref(tc.Title)) {
		return true
	}
	if looksLikeExitPlan(string(tc.ToolCallId)) {
		return true
	}
	if tc.RawInput != nil && looksLikeExitPlan(fmt.Sprint(tc.RawInput)) {
		return true
	}
	return false
}

func looksLikeExitPlan(title string) bool {
	n := normalizePlanText(title)
	if strings.Contains(n, "plan exit") || strings.Contains(n, "exit plan") {
		return true
	}
	return strings.Contains(strings.ReplaceAll(n, " ", ""), "exitplanmode")
}

func normalizePlanText(s string) string {
	s = strings.ToLower(s)
	var b strings.Builder
	space := false
	for _, r := range s {
		switch r {
		case ' ', '_', ':', '-', '/', '.', '\t', '\n':
			space = true
		default:
			if space && b.Len() > 0 {
				b.WriteByte(' ')
			}
			b.WriteRune(r)
			space = false
		}
	}
	return b.String()
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func pickPermission(params acp.RequestPermissionRequest, allow bool) (acp.RequestPermissionResponse, bool) {
	for _, opt := range params.Options {
		kind := string(opt.Kind)
		if allow && (kind == "allow_once" || kind == "allow_always" || kind == "allow") {
			return acp.RequestPermissionResponse{
				Outcome: acp.RequestPermissionOutcome{Selected: &acp.RequestPermissionOutcomeSelected{OptionId: opt.OptionId}},
			}, true
		}
		if !allow && (kind == "reject_once" || kind == "reject_always" || kind == "reject") {
			return acp.RequestPermissionResponse{
				Outcome: acp.RequestPermissionOutcome{Selected: &acp.RequestPermissionOutcomeSelected{OptionId: opt.OptionId}},
			}, true
		}
	}
	return acp.RequestPermissionResponse{}, false
}

func cancelPermission() acp.RequestPermissionResponse {
	return acp.RequestPermissionResponse{
		Outcome: acp.RequestPermissionOutcome{Cancelled: &acp.RequestPermissionOutcomeCancelled{}},
	}
}

type sessionModeSetter func(context.Context, acp.SetSessionModeRequest) (acp.SetSessionModeResponse, error)

func setPlanMode(ctx context.Context, set sessionModeSetter, sessionID acp.SessionId, modes *acp.SessionModeState) error {
	mode, err := planModeID(modes)
	if err != nil {
		return err
	}
	if _, err := set(ctx, acp.SetSessionModeRequest{
		SessionId: sessionID,
		ModeId:    mode,
	}); err != nil {
		return fmt.Errorf("session/set_mode plan: %w", err)
	}
	return nil
}

func planModeID(modes *acp.SessionModeState) (acp.SessionModeId, error) {
	if modes == nil || len(modes.AvailableModes) == 0 {
		return "plan", nil
	}
	for _, m := range modes.AvailableModes {
		id := strings.ToLower(string(m.Id) + " " + m.Name)
		if strings.Contains(id, "plan") {
			return m.Id, nil
		}
	}
	return "", errors.New("no plan session mode")
}

func gitWorktree(cwd string) (string, error) {
	root, err := exec.Command("git", "-C", cwd, "rev-parse", "--show-toplevel").Output()
	if err != nil {
		return "", fmt.Errorf("worktree: %w", err)
	}
	base := strings.TrimSpace(string(root))
	dir := filepath.Join(filepath.Dir(base), filepath.Base(base)+"-grok-wt-"+uuid.NewString())
	cmd := exec.Command("git", "-C", base, "worktree", "add", "--detach", dir, "HEAD")
	if out, err := cmd.CombinedOutput(); err != nil {
		return "", fmt.Errorf("worktree add: %s: %w", strings.TrimSpace(string(out)), err)
	}
	return dir, nil
}
