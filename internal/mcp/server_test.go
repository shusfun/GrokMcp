package mcp

import (
	"context"
	"slices"
	"strings"
	"sync"
	"testing"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"grokmcp/internal/core"
	"grokmcp/internal/protocol"
	"grokmcp/internal/version"
)

type stub struct{ core.Backend }

func (stub) Dispatch(context.Context, protocol.DispatchRequest) (protocol.DispatchResult, error) {
	return protocol.DispatchResult{Jobs: []protocol.Job{{JobID: "j1", State: protocol.StateStarting, ViewMode: protocol.ViewHeadless}}}, nil
}
func (stub) Wait(context.Context, protocol.WaitRequest) (protocol.WaitResult, error) {
	return protocol.WaitResult{}, nil
}
func (stub) PlanDecide(context.Context, protocol.PlanDecideRequest) (protocol.Job, error) {
	return protocol.Job{}, nil
}
func (stub) Followup(context.Context, protocol.FollowupRequest) (protocol.Job, error) {
	return protocol.Job{}, nil
}
func (stub) CancelTurn(context.Context, string) (protocol.Job, error) { return protocol.Job{}, nil }
func (stub) SetView(context.Context, protocol.SetViewRequest) (protocol.Job, error) {
	return protocol.Job{}, nil
}
func (stub) Status(context.Context, string) (protocol.Job, error) { return protocol.Job{}, nil }
func (stub) ListJobs(context.Context) ([]protocol.Job, error)     { return nil, nil }
func (stub) ListJobsPage(context.Context, protocol.ListJobsQuery) (protocol.JobPage, error) {
	return protocol.JobPage{}, nil
}
func (stub) ImportProject(context.Context, string, bool) (protocol.Project, error) {
	return protocol.Project{}, nil
}
func (stub) ListProjects(context.Context) ([]protocol.Project, error) { return nil, nil }
func (stub) GetProject(context.Context, string) (protocol.Project, error) {
	return protocol.Project{}, nil
}
func (stub) RemoveProject(context.Context, string) (protocol.Project, error) {
	return protocol.Project{}, nil
}
func (stub) SkillStatus(context.Context, string) (protocol.Project, error) {
	return protocol.Project{}, nil
}
func (stub) SkillInstall(context.Context, string) (protocol.Project, error) {
	return protocol.Project{}, nil
}
func (stub) SkillUpdate(context.Context, string) (protocol.Project, error) {
	return protocol.Project{}, nil
}
func (stub) SkillRemove(context.Context, string) (protocol.Project, error) {
	return protocol.Project{}, nil
}
func (stub) GeneratePrompt(context.Context, protocol.GeneratePromptRequest) (protocol.PromptResult, error) {
	return protocol.PromptResult{}, nil
}
func (stub) SavePrompt(context.Context, protocol.SavePromptRequest) (protocol.Project, error) {
	return protocol.Project{}, nil
}
func (stub) OpenProjectDir(context.Context, string) error { return nil }
func (stub) ArchiveJob(context.Context, string) (protocol.Job, error) {
	return protocol.Job{}, nil
}
func (stub) UnarchiveJob(context.Context, string) (protocol.Job, error) {
	return protocol.Job{}, nil
}
func (stub) DeleteJob(context.Context, string) error { return nil }
func (stub) OpenTerminal(context.Context, protocol.OpenTerminalRequest) error {
	return nil
}
func (stub) OpenProject(context.Context, string) error                { return nil }
func (stub) Continue(context.Context, string) (protocol.Job, error)   { return protocol.Job{}, nil }
func (stub) DetachView(context.Context, string) (protocol.Job, error) { return protocol.Job{}, nil }
func (stub) Settings(context.Context) (protocol.Settings, error)      { return protocol.Settings{}, nil }
func (stub) SaveSettings(context.Context, protocol.Settings) error    { return nil }
func (stub) Diagnose(context.Context) (protocol.DiagnoseResult, error) {
	return protocol.DiagnoseResult{}, nil
}
func (stub) StatusBar(context.Context) (protocol.StatusBar, error) { return protocol.StatusBar{}, nil }
func (stub) Events(context.Context, string) ([]protocol.BoundaryEvent, error) {
	return nil, nil
}
func (stub) TestTerminal(context.Context, string) error { return nil }
func (stub) DebugSet(context.Context, protocol.DebugSetRequest) (protocol.Job, error) {
	return protocol.Job{}, nil
}
func (stub) DebugSnapshot(context.Context, protocol.DebugSnapshotRequest) (protocol.DebugSnapshot, error) {
	return protocol.DebugSnapshot{}, nil
}
func (stub) DebugWait(context.Context, protocol.DebugWaitRequest) (protocol.DebugSnapshot, error) {
	return protocol.DebugSnapshot{}, nil
}
func (stub) DebugExport(context.Context, string) (protocol.DebugExportResult, error) {
	return protocol.DebugExportResult{}, nil
}
func (stub) Subscribe(func(protocol.Event)) func() { return func() {} }
func (stub) Close() error                          { return nil }

type recorder struct {
	stub
	mu       sync.Mutex
	listJobs int
	status   []string
	dispatch []protocol.DispatchRequest
	wait     []protocol.WaitRequest
	plan     []protocol.PlanDecideRequest
	open     []protocol.OpenTerminalRequest
}

func (r *recorder) Dispatch(_ context.Context, req protocol.DispatchRequest) (protocol.DispatchResult, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.dispatch = append(r.dispatch, req)
	return protocol.DispatchResult{}, nil
}

func (r *recorder) Wait(_ context.Context, req protocol.WaitRequest) (protocol.WaitResult, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.wait = append(r.wait, req)
	return protocol.WaitResult{}, nil
}

func (r *recorder) PlanDecide(_ context.Context, req protocol.PlanDecideRequest) (protocol.Job, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.plan = append(r.plan, req)
	return protocol.Job{}, nil
}

func (r *recorder) Status(_ context.Context, jobID string) (protocol.Job, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.status = append(r.status, jobID)
	return protocol.Job{}, nil
}

func (r *recorder) ListJobs(context.Context) ([]protocol.Job, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.listJobs++
	return nil, nil
}

func (r *recorder) OpenTerminal(_ context.Context, req protocol.OpenTerminalRequest) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.open = append(r.open, req)
	return nil
}

func connectMCP(t *testing.T, backend core.Backend) *sdk.ClientSession {
	t.Helper()
	ctx := context.Background()
	server := newServer(backend)
	client := sdk.NewClient(&sdk.Implementation{Name: "t", Version: "0"}, nil)
	st, ct := sdk.NewInMemoryTransports()
	ss, err := server.Connect(ctx, st, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ss.Close() })
	cs, err := client.Connect(ctx, ct, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cs.Close() })
	return cs
}

func schemaMap(v any) map[string]any {
	m, _ := v.(map[string]any)
	return m
}

func requiredProps(schema any) []string {
	raw, _ := schemaMap(schema)["required"].([]any)
	if len(raw) == 0 {
		return nil
	}
	out := make([]string, 0, len(raw))
	for _, x := range raw {
		s, ok := x.(string)
		if !ok {
			continue
		}
		out = append(out, s)
	}
	return out
}

func nestedSchema(schema any, keys ...string) any {
	cur := schema
	for _, key := range keys {
		cur = schemaMap(cur)[key]
	}
	return cur
}

func callTool(t *testing.T, cs *sdk.ClientSession, name string, args map[string]any) {
	t.Helper()
	res, err := cs.CallTool(context.Background(), &sdk.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	if res.IsError {
		t.Fatalf("%s rejected optional args: %v", name, res.Content)
	}
}

func TestToolInputSchemaRequired(t *testing.T) {
	cs := connectMCP(t, stub{})
	init := cs.InitializeResult()
	if init == nil || init.ServerInfo == nil {
		t.Fatal("missing serverInfo")
	}
	if init.ServerInfo.Version != version.Version {
		t.Fatalf("serverInfo.version = %q, want %q", init.ServerInfo.Version, version.Version)
	}

	listed, err := cs.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string][]string{
		"grok_dispatch":        {"tasks"},
		"grok_wait":            {"job_ids"},
		"grok_plan_decide":     {"job_id", "decide"},
		"grok_followup":        {"job_id", "prompt"},
		"grok_cancel_turn":     {"job_id"},
		"grok_set_view":        {"job_id", "view"},
		"grok_status":          nil,
		"grok_open_terminal":   nil,
		"grok_debug_set":       {"enabled"},
		"grok_debug_snapshot":  {"job_id"},
		"grok_debug_wait":      {"job_id", "cursor"},
		"grok_debug_export":    {"job_id"},
		"grok_project_import":  {"path"},
		"grok_project_list":    nil,
		"grok_project_get":     {"project_id"},
		"grok_project_remove":  {"project_id"},
		"grok_skill_status":    {"project_id"},
		"grok_skill_install":   {"project_id"},
		"grok_skill_update":    {"project_id"},
		"grok_skill_remove":    {"project_id"},
		"grok_prompt_generate": {"project_id"},
		"grok_prompt_save":     {"project_id", "template"},
	}
	got := map[string][]string{}
	for _, tool := range listed.Tools {
		got[tool.Name] = requiredProps(tool.InputSchema)
		if tool.Name == "grok_dispatch" {
			items := nestedSchema(tool.InputSchema, "properties", "tasks", "items")
			if req := requiredProps(items); !slices.Equal(req, []string{"prompt"}) {
				t.Errorf("grok_dispatch tasks[].required = %v, want [prompt]", req)
			}
		}
	}
	if len(got) != len(want) {
		t.Fatalf("tools = %v, want %v", keys(got), keys(want))
	}
	for name, req := range want {
		if !slices.Equal(got[name], req) {
			t.Errorf("%s required = %v, want %v", name, got[name], req)
		}
	}
}

func TestServerInstructionsAndAnnotations(t *testing.T) {
	cs := connectMCP(t, stub{})
	init := cs.InitializeResult()
	if init == nil || init.Instructions == "" {
		t.Fatal("missing instructions")
	}
	for _, needle := range []string{"Grok", "grok_wait", "session", "模型"} {
		if !strings.Contains(init.Instructions, needle) {
			t.Errorf("instructions missing %q:\n%s", needle, init.Instructions)
		}
	}

	listed, err := cs.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	byName := map[string]*sdk.Tool{}
	for _, tool := range listed.Tools {
		if tool.Annotations == nil {
			t.Errorf("%s missing annotations", tool.Name)
			continue
		}
		byName[tool.Name] = tool
	}

	assertExec := func(name string) {
		t.Helper()
		tool := byName[name]
		if tool == nil {
			t.Fatalf("missing %s", name)
		}
		a := tool.Annotations
		if a.ReadOnlyHint {
			t.Errorf("%s ReadOnlyHint = true", name)
		}
		if a.DestructiveHint == nil || !*a.DestructiveHint {
			t.Errorf("%s DestructiveHint = %v, want true", name, a.DestructiveHint)
		}
		if a.OpenWorldHint == nil || !*a.OpenWorldHint {
			t.Errorf("%s OpenWorldHint = %v, want true", name, a.OpenWorldHint)
		}
	}
	assertExec("grok_dispatch")
	assertExec("grok_plan_decide")
	assertExec("grok_followup")

	assertRead := func(name string) {
		t.Helper()
		tool := byName[name]
		if tool == nil {
			t.Fatalf("missing %s", name)
		}
		if !tool.Annotations.ReadOnlyHint {
			t.Errorf("%s ReadOnlyHint = false", name)
		}
	}
	assertRead("grok_status")
	assertRead("grok_prompt_generate")

	cancel := byName["grok_cancel_turn"]
	if cancel == nil {
		t.Fatal("missing grok_cancel_turn")
	}
	if cancel.Annotations.ReadOnlyHint || cancel.Annotations.DestructiveHint == nil || !*cancel.Annotations.DestructiveHint || cancel.Annotations.OpenWorldHint == nil || !*cancel.Annotations.OpenWorldHint {
		t.Errorf("grok_cancel_turn annotations = %+v", cancel.Annotations)
	}

	assertClosedDestructive := func(name string) {
		t.Helper()
		tool := byName[name]
		if tool == nil {
			t.Fatalf("missing %s", name)
		}
		a := tool.Annotations
		if a.ReadOnlyHint {
			t.Errorf("%s ReadOnlyHint = true", name)
		}
		if a.DestructiveHint == nil || !*a.DestructiveHint {
			t.Errorf("%s DestructiveHint = %v, want true", name, a.DestructiveHint)
		}
		if a.OpenWorldHint == nil || *a.OpenWorldHint {
			t.Errorf("%s OpenWorldHint = %v, want false", name, a.OpenWorldHint)
		}
	}
	assertClosedDestructive("grok_skill_update")
	assertClosedDestructive("grok_skill_remove")
	assertClosedDestructive("grok_project_remove")
	assertClosedDestructive("grok_prompt_save")
}

func TestOptionalToolArgsReachBackend(t *testing.T) {
	rec := &recorder{}
	cs := connectMCP(t, rec)

	callTool(t, cs, "grok_status", map[string]any{})
	callTool(t, cs, "grok_wait", map[string]any{"job_ids": []string{"j1"}})
	callTool(t, cs, "grok_plan_decide", map[string]any{"job_id": "j1", "decide": "approve"})
	callTool(t, cs, "grok_dispatch", map[string]any{"tasks": []any{map[string]any{"prompt": "do it"}}})
	callTool(t, cs, "grok_open_terminal", map[string]any{"dashboard": true})

	rec.mu.Lock()
	defer rec.mu.Unlock()
	if rec.listJobs != 1 {
		t.Fatalf("ListJobs calls = %d, want 1", rec.listJobs)
	}
	if len(rec.status) != 0 {
		t.Fatalf("Status called with %v, want list-all via empty job_id", rec.status)
	}
	if len(rec.wait) != 1 || rec.wait[0].Mode != "" || rec.wait[0].TimeoutSec != 0 {
		t.Fatalf("Wait = %+v, want job_ids only", rec.wait)
	}
	if len(rec.plan) != 1 || rec.plan[0].Notes != "" {
		t.Fatalf("PlanDecide = %+v, want notes omitted", rec.plan)
	}
	if len(rec.dispatch) != 1 || rec.dispatch[0].Cwd != "" || rec.dispatch[0].Tasks[0].Prompt != "do it" {
		t.Fatalf("Dispatch = %+v, want prompt-only task", rec.dispatch)
	}
	if len(rec.open) != 1 || !rec.open[0].Dashboard || rec.open[0].JobID != "" {
		t.Fatalf("OpenTerminal = %+v, want dashboard only", rec.open)
	}
}

func keys(m map[string][]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	slices.Sort(out)
	return out
}
