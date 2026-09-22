package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	acp "github.com/coder/acp-go-sdk"
)

var _ acp.ExtensionMethodHandler = (*acpClient)(nil)

type toolCallMeta struct {
	origin   Activity
	title    string
	rawInput any
	kind     string
}

type acpClient struct {
	origin       func(string) Activity
	mu           sync.Mutex
	texts        map[string]*strings.Builder
	calls        map[string]toolCallMeta
	onActivity   func(string, string)
	onUpdate     func(sessionID, lastAction string, planReady bool)
	onPermission func(ctx context.Context, req acp.RequestPermissionRequest) (acp.RequestPermissionResponse, error)
	onExitPlan   func(ctx context.Context, params json.RawMessage) (any, error)
}

func newACPClient() *acpClient {
	return &acpClient{texts: map[string]*strings.Builder{}, calls: map[string]toolCallMeta{}}
}

func (c *acpClient) resetText(sessionID string) {
	c.mu.Lock()
	c.texts[sessionID] = &strings.Builder{}
	c.mu.Unlock()
}

func (c *acpClient) takeText(sessionID string) string {
	c.mu.Lock()
	defer c.mu.Unlock()
	b := c.texts[sessionID]
	if b == nil {
		return ""
	}
	return b.String()
}

func (c *acpClient) ReadTextFile(ctx context.Context, params acp.ReadTextFileRequest) (acp.ReadTextFileResponse, error) {
	if err := ctx.Err(); err != nil {
		return acp.ReadTextFileResponse{}, err
	}
	if !filepath.IsAbs(params.Path) {
		return acp.ReadTextFileResponse{}, fmt.Errorf("file path must be absolute")
	}
	if (params.Line != nil && *params.Line < 1) || (params.Limit != nil && *params.Limit < 0) {
		return acp.ReadTextFileResponse{}, fmt.Errorf("invalid file line range")
	}
	b, err := os.ReadFile(params.Path)
	if err != nil {
		return acp.ReadTextFileResponse{}, err
	}
	text := string(b)
	if params.Line != nil || params.Limit != nil {
		lines := strings.SplitAfter(text, "\n")
		start := 0
		if params.Line != nil {
			start = min(*params.Line-1, len(lines))
		}
		end := len(lines)
		if params.Limit != nil {
			end = start + min(*params.Limit, len(lines)-start)
		}
		text = strings.Join(lines[start:end], "")
	}
	return acp.ReadTextFileResponse{Content: text}, nil
}

func (c *acpClient) WriteTextFile(ctx context.Context, params acp.WriteTextFileRequest) (acp.WriteTextFileResponse, error) {
	if err := ctx.Err(); err != nil {
		return acp.WriteTextFileResponse{}, err
	}
	if !filepath.IsAbs(params.Path) {
		return acp.WriteTextFileResponse{}, fmt.Errorf("file path must be absolute")
	}
	return acp.WriteTextFileResponse{}, os.WriteFile(params.Path, []byte(params.Content), 0o644)
}

func (c *acpClient) HandleExtensionMethod(ctx context.Context, method string, params json.RawMessage) (any, error) {
	if method == exitPlanMethod && c.onExitPlan != nil {
		var req exitPlanExtReq
		if err := json.Unmarshal(params, &req); err != nil {
			return nil, err
		}
		ctx = c.bindOrigin(ctx, req.SessionID, req.ToolCallID)
		c.resetText(req.SessionID)
		return c.onExitPlan(ctx, params)
	}
	return nil, acp.NewMethodNotFound(method)
}

func (c *acpClient) RequestPermission(ctx context.Context, params acp.RequestPermissionRequest) (acp.RequestPermissionResponse, error) {
	sid := string(params.SessionId)
	params.ToolCall = c.mergeToolCall(sid, params.ToolCall)
	ctx = c.bindOrigin(ctx, sid, string(params.ToolCall.ToolCallId))
	var resp acp.RequestPermissionResponse
	var err error
	if c.onPermission != nil {
		resp, err = c.onPermission(ctx, params)
	} else {
		resp = allowPermission(params)
	}
	c.forgetToolCall(sid, params.ToolCall.ToolCallId)
	return resp, err
}

func (c *acpClient) SessionUpdate(_ context.Context, params acp.SessionNotification) error {
	sid := string(params.SessionId)
	action := ""
	u := params.Update
	kind := "session_update"
	switch {
	case u.AgentMessageChunk != nil:
		kind = "message"
	case u.AgentThoughtChunk != nil:
		kind = "thought"
	case u.ToolCall != nil:
		c.resetText(sid)
		kind = "tool_call"
	case u.ToolCallUpdate != nil:
		kind = "tool_update"
	case u.Plan != nil:
		kind = "plan_update"
	}
	if c.onActivity != nil {
		c.onActivity(sid, kind)
	}
	switch {
	case u.AgentMessageChunk != nil:
		c.mu.Lock()
		if c.texts[sid] == nil {
			c.texts[sid] = &strings.Builder{}
		}
		if u.AgentMessageChunk.Content.Text != nil {
			c.texts[sid].WriteString(u.AgentMessageChunk.Content.Text.Text)
		}
		c.mu.Unlock()
	case u.ToolCall != nil:
		action = u.ToolCall.Title
		c.rememberToolCall(sid, string(u.ToolCall.ToolCallId), u.ToolCall.Title, u.ToolCall.RawInput, string(u.ToolCall.Kind), u.ToolCall.Status)
	case u.ToolCallUpdate != nil:
		if u.ToolCallUpdate.Title != nil {
			action = *u.ToolCallUpdate.Title
		}
		kind := ""
		if u.ToolCallUpdate.Kind != nil {
			kind = string(*u.ToolCallUpdate.Kind)
		}
		status := acp.ToolCallStatus("")
		if u.ToolCallUpdate.Status != nil {
			status = *u.ToolCallUpdate.Status
		}
		c.rememberToolCall(sid, string(u.ToolCallUpdate.ToolCallId), action, u.ToolCallUpdate.RawInput, kind, status)
	case u.Plan != nil:
		action = "Plan ready"
	}
	if c.onUpdate != nil && action != "" {
		c.onUpdate(sid, action, false)
	}
	return nil
}

func callKey(sessionID, callID string) string {
	return sessionID + "\x00" + callID
}

func (c *acpClient) rememberToolCall(sessionID, callID, title string, raw any, kind string, status acp.ToolCallStatus) {
	if callID == "" {
		return
	}
	key := callKey(sessionID, callID)
	origin := Activity{}
	if c.origin != nil {
		origin = c.origin(sessionID)
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if status == acp.ToolCallStatusCompleted || status == acp.ToolCallStatusFailed {
		delete(c.calls, key)
		return
	}
	meta, existed := c.calls[key]
	if !existed {
		meta.origin = origin
	}
	if title != "" {
		meta.title = title
	}
	if raw != nil {
		meta.rawInput = raw
	}
	if kind != "" {
		meta.kind = kind
	}
	c.calls[key] = meta
}

func (c *acpClient) mergeToolCall(sessionID string, tc acp.ToolCallUpdate) acp.ToolCallUpdate {
	if tc.ToolCallId == "" {
		return tc
	}
	c.mu.Lock()
	meta, ok := c.calls[callKey(sessionID, string(tc.ToolCallId))]
	c.mu.Unlock()
	if !ok {
		return tc
	}
	if tc.Title == nil && meta.title != "" {
		title := meta.title
		tc.Title = &title
	}
	if tc.RawInput == nil && meta.rawInput != nil {
		tc.RawInput = meta.rawInput
	}
	if tc.Kind == nil && meta.kind != "" {
		k := acp.ToolKind(meta.kind)
		tc.Kind = &k
	}
	return tc
}

func (c *acpClient) forgetToolCall(sessionID string, id acp.ToolCallId) {
	if id == "" {
		return
	}
	c.mu.Lock()
	delete(c.calls, callKey(sessionID, string(id)))
	c.mu.Unlock()
}

func (c *acpClient) CreateTerminal(context.Context, acp.CreateTerminalRequest) (acp.CreateTerminalResponse, error) {
	return acp.CreateTerminalResponse{}, acp.NewMethodNotFound("terminal/create")
}
func (c *acpClient) KillTerminal(context.Context, acp.KillTerminalRequest) (acp.KillTerminalResponse, error) {
	return acp.KillTerminalResponse{}, acp.NewMethodNotFound("terminal/kill")
}
func (c *acpClient) TerminalOutput(context.Context, acp.TerminalOutputRequest) (acp.TerminalOutputResponse, error) {
	return acp.TerminalOutputResponse{}, acp.NewMethodNotFound("terminal/output")
}
func (c *acpClient) ReleaseTerminal(context.Context, acp.ReleaseTerminalRequest) (acp.ReleaseTerminalResponse, error) {
	return acp.ReleaseTerminalResponse{}, acp.NewMethodNotFound("terminal/release")
}
func (c *acpClient) WaitForTerminalExit(context.Context, acp.WaitForTerminalExitRequest) (acp.WaitForTerminalExitResponse, error) {
	return acp.WaitForTerminalExitResponse{}, acp.NewMethodNotFound("terminal/wait_for_exit")
}

func allowPermission(params acp.RequestPermissionRequest) acp.RequestPermissionResponse {
	for _, opt := range params.Options {
		if opt.Kind == "allow_once" || opt.Kind == "allow_always" || opt.Kind == "allow" {
			return acp.RequestPermissionResponse{
				Outcome: acp.RequestPermissionOutcome{Selected: &acp.RequestPermissionOutcomeSelected{OptionId: opt.OptionId}},
			}
		}
	}
	if len(params.Options) > 0 {
		return acp.RequestPermissionResponse{
			Outcome: acp.RequestPermissionOutcome{Selected: &acp.RequestPermissionOutcomeSelected{OptionId: params.Options[0].OptionId}},
		}
	}
	return acp.RequestPermissionResponse{}
}

func (c *acpClient) bindOrigin(ctx context.Context, sid, callID string) context.Context {
	c.mu.Lock()
	meta, ok := c.calls[callKey(sid, callID)]
	c.mu.Unlock()
	origin := meta.origin
	if !ok && c.origin != nil {
		origin = c.origin(sid)
	}
	if origin.TurnID != "" {
		ctx = WithTurn(ctx, origin.TurnID)
		ctx = WithRequest(ctx, origin.RequestID)
	}
	if origin.ConnectionID != "" {
		ctx = withConnection(ctx, origin.ConnectionID)
	}
	return ctx
}
