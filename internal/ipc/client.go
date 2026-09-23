package ipc

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"net"
	"sync"
	"sync/atomic"

	"grokmcp/internal/protocol"
)

type Client struct {
	conn   net.Conn
	wmu    sync.Mutex
	id     atomic.Uint64
	pend   sync.Map
	subs   map[int]func(protocol.Event)
	smu    sync.Mutex
	seq    int
	done   chan struct{}
	events chan protocol.Event
}

func Dial(ctx context.Context, network, addr string) (*Client, error) {
	var d net.Dialer
	c, err := d.DialContext(ctx, network, addr)
	if err != nil {
		return nil, err
	}
	cl := &Client{conn: c, subs: map[int]func(protocol.Event){}, done: make(chan struct{}), events: make(chan protocol.Event, 64)}
	go cl.dispatchEvents()
	go cl.read()
	return cl, nil
}

func (c *Client) read() {
	defer close(c.done)
	sc := bufio.NewScanner(c.conn)
	sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for sc.Scan() {
		var resp Response
		if err := json.Unmarshal(sc.Bytes(), &resp); err != nil {
			continue
		}
		if resp.Method == "event" {
			var ev protocol.Event
			_ = json.Unmarshal(resp.Params, &ev)
			// UI 通知可合并/丢弃；权威状态与等待边界从服务端补读。
			select {
			case c.events <- ev:
			default:
			}
			continue
		}
		if ch, ok := c.pend.LoadAndDelete(resp.ID); ok {
			ch.(chan Response) <- resp
		}
	}
}

func (c *Client) call(ctx context.Context, method string, params any) (json.RawMessage, error) {
	id := c.id.Add(1)
	b, _ := json.Marshal(params)
	req := Request{ID: id, Method: method, Params: b}
	raw, _ := json.Marshal(req)
	ch := make(chan Response, 1)
	c.pend.Store(id, ch)
	defer c.pend.Delete(id)
	c.wmu.Lock()
	_, err := c.conn.Write(append(raw, '\n'))
	c.wmu.Unlock()
	if err != nil {
		return nil, err
	}
	select {
	case <-c.done:
		return nil, errors.New("IPC disconnected")
	case <-ctx.Done():
		c.pend.Delete(id)
		return nil, ctx.Err()
	case resp := <-ch:
		if resp.Error != "" {
			return nil, errors.New(resp.Error)
		}
		return resp.Result, nil
	}
}

func decode[T any](raw json.RawMessage, err error) (T, error) {
	var v T
	if err != nil {
		return v, err
	}
	if len(raw) == 0 {
		return v, nil
	}
	return v, json.Unmarshal(raw, &v)
}

func (c *Client) Dispatch(ctx context.Context, req protocol.DispatchRequest) (protocol.DispatchResult, error) {
	return decode[protocol.DispatchResult](c.call(ctx, "dispatch", req))
}
func (c *Client) Wait(ctx context.Context, req protocol.WaitRequest) (protocol.WaitResult, error) {
	return decode[protocol.WaitResult](c.call(ctx, "wait", req))
}
func (c *Client) PlanDecide(ctx context.Context, req protocol.PlanDecideRequest) (protocol.Job, error) {
	return decode[protocol.Job](c.call(ctx, "planDecide", req))
}
func (c *Client) Followup(ctx context.Context, req protocol.FollowupRequest) (protocol.Job, error) {
	return decode[protocol.Job](c.call(ctx, "followup", req))
}
func (c *Client) CancelTurn(ctx context.Context, jobID string, expectedTurnID ...string) (protocol.Job, error) {
	turnID := ""
	if len(expectedTurnID) > 0 {
		turnID = expectedTurnID[0]
	}
	return c.CancelRequest(ctx, jobID, "", turnID)
}
func (c *Client) CancelRequest(ctx context.Context, jobID, requestID, turnID string) (protocol.Job, error) {
	return decode[protocol.Job](c.call(ctx, "cancelTurn", map[string]string{"job_id": jobID, "turn_id": turnID, "request_id": requestID}))
}
func (c *Client) SetView(ctx context.Context, req protocol.SetViewRequest) (protocol.Job, error) {
	return decode[protocol.Job](c.call(ctx, "setView", req))
}
func (c *Client) Status(ctx context.Context, jobID string, options ...protocol.ResultQuery) (protocol.Job, error) {
	q := protocol.ResultQuery{}
	if len(options) > 0 {
		q = options[0]
	}
	return decode[protocol.Job](c.call(ctx, "status", struct {
		JobID string `json:"job_id"`
		protocol.ResultQuery
	}{jobID, q}))
}
func (c *Client) ListJobs(ctx context.Context) ([]protocol.Job, error) {
	return decode[[]protocol.Job](c.call(ctx, "listJobs", struct{}{}))
}
func (c *Client) ListJobsPage(ctx context.Context, q protocol.ListJobsQuery) (protocol.JobPage, error) {
	return decode[protocol.JobPage](c.call(ctx, "listJobsPage", q))
}
func (c *Client) ImportProject(ctx context.Context, path string, installSkill bool) (protocol.Project, error) {
	flag := installSkill
	return decode[protocol.Project](c.call(ctx, "importProject", protocol.ImportProjectRequest{Path: path, InstallSkill: &flag}))
}
func (c *Client) ListProjects(ctx context.Context) ([]protocol.Project, error) {
	return decode[[]protocol.Project](c.call(ctx, "listProjects", struct{}{}))
}
func (c *Client) GetProject(ctx context.Context, projectID string) (protocol.Project, error) {
	return decode[protocol.Project](c.call(ctx, "getProject", protocol.ProjectIDRequest{ProjectID: projectID}))
}
func (c *Client) RemoveProject(ctx context.Context, projectID string) (protocol.Project, error) {
	return decode[protocol.Project](c.call(ctx, "removeProject", protocol.ProjectIDRequest{ProjectID: projectID}))
}
func (c *Client) SkillStatus(ctx context.Context, projectID string) (protocol.Project, error) {
	return decode[protocol.Project](c.call(ctx, "skillStatus", protocol.ProjectIDRequest{ProjectID: projectID}))
}
func (c *Client) SkillInstall(ctx context.Context, projectID string) (protocol.Project, error) {
	return decode[protocol.Project](c.call(ctx, "skillInstall", protocol.ProjectIDRequest{ProjectID: projectID}))
}
func (c *Client) SkillUpdate(ctx context.Context, projectID string) (protocol.Project, error) {
	return decode[protocol.Project](c.call(ctx, "skillUpdate", protocol.ProjectIDRequest{ProjectID: projectID}))
}
func (c *Client) SkillRemove(ctx context.Context, projectID string) (protocol.Project, error) {
	return decode[protocol.Project](c.call(ctx, "skillRemove", protocol.ProjectIDRequest{ProjectID: projectID}))
}
func (c *Client) GeneratePrompt(ctx context.Context, req protocol.GeneratePromptRequest) (protocol.PromptResult, error) {
	return decode[protocol.PromptResult](c.call(ctx, "generatePrompt", req))
}
func (c *Client) SavePrompt(ctx context.Context, req protocol.SavePromptRequest) (protocol.Project, error) {
	return decode[protocol.Project](c.call(ctx, "savePrompt", req))
}
func (c *Client) OpenProjectDir(ctx context.Context, projectID string) error {
	_, err := c.call(ctx, "openProjectDir", protocol.ProjectIDRequest{ProjectID: projectID})
	return err
}
func (c *Client) ArchiveJob(ctx context.Context, jobID string) (protocol.Job, error) {
	return decode[protocol.Job](c.call(ctx, "archiveJob", map[string]string{"job_id": jobID}))
}
func (c *Client) UnarchiveJob(ctx context.Context, jobID string) (protocol.Job, error) {
	return decode[protocol.Job](c.call(ctx, "unarchiveJob", map[string]string{"job_id": jobID}))
}
func (c *Client) DeleteJob(ctx context.Context, jobID string) error {
	_, err := c.call(ctx, "deleteJob", map[string]string{"job_id": jobID})
	return err
}
func (c *Client) OpenTerminal(ctx context.Context, req protocol.OpenTerminalRequest) error {
	_, err := c.call(ctx, "openTerminal", req)
	return err
}
func (c *Client) OpenProject(ctx context.Context, jobID string) error {
	_, err := c.call(ctx, "openProject", map[string]string{"job_id": jobID})
	return err
}
func (c *Client) Continue(ctx context.Context, jobID string) (protocol.Job, error) {
	return decode[protocol.Job](c.call(ctx, "continue", map[string]string{"job_id": jobID}))
}
func (c *Client) DetachView(ctx context.Context, jobID string) (protocol.Job, error) {
	return decode[protocol.Job](c.call(ctx, "detachView", map[string]string{"job_id": jobID}))
}
func (c *Client) Settings(ctx context.Context) (protocol.Settings, error) {
	return decode[protocol.Settings](c.call(ctx, "settings", struct{}{}))
}
func (c *Client) SaveSettings(ctx context.Context, s protocol.Settings) error {
	_, err := c.call(ctx, "saveSettings", s)
	return err
}
func (c *Client) Diagnose(ctx context.Context) (protocol.DiagnoseResult, error) {
	return decode[protocol.DiagnoseResult](c.call(ctx, "diagnose", struct{}{}))
}
func (c *Client) MCPConfig(ctx context.Context) (protocol.MCPConfigBundle, error) {
	return decode[protocol.MCPConfigBundle](c.call(ctx, "mcpConfig", struct{}{}))
}
func (c *Client) MCPStatus(ctx context.Context) (protocol.MCPInstallStatus, error) {
	return decode[protocol.MCPInstallStatus](c.call(ctx, "mcpStatus", struct{}{}))
}
func (c *Client) OpenCCSwitchMCPImport(ctx context.Context) (protocol.MCPApplyResult, error) {
	return decode[protocol.MCPApplyResult](c.call(ctx, "openCCSwitchMCPImport", struct{}{}))
}
func (c *Client) OpenCCSwitchApp(ctx context.Context) (protocol.MCPApplyResult, error) {
	return decode[protocol.MCPApplyResult](c.call(ctx, "openCCSwitchApp", struct{}{}))
}
func (c *Client) AddMCPToCodex(ctx context.Context) (protocol.MCPApplyResult, error) {
	return decode[protocol.MCPApplyResult](c.call(ctx, "addMCPToCodex", struct{}{}))
}
func (c *Client) StatusBar(ctx context.Context) (protocol.StatusBar, error) {
	return decode[protocol.StatusBar](c.call(ctx, "statusBar", struct{}{}))
}
func (c *Client) Events(ctx context.Context, jobID string) ([]protocol.BoundaryEvent, error) {
	return decode[[]protocol.BoundaryEvent](c.call(ctx, "events", map[string]string{"job_id": jobID}))
}
func (c *Client) TestTerminal(ctx context.Context, template string) error {
	_, err := c.call(ctx, "testTerminal", map[string]string{"template": template})
	return err
}
func (c *Client) DebugSet(ctx context.Context, req protocol.DebugSetRequest) (protocol.Job, error) {
	return decode[protocol.Job](c.call(ctx, "debugSet", req))
}
func (c *Client) DebugSnapshot(ctx context.Context, req protocol.DebugSnapshotRequest) (protocol.DebugSnapshot, error) {
	return decode[protocol.DebugSnapshot](c.call(ctx, "debugSnapshot", req))
}
func (c *Client) DebugWait(ctx context.Context, req protocol.DebugWaitRequest) (protocol.DebugSnapshot, error) {
	return decode[protocol.DebugSnapshot](c.call(ctx, "debugWait", req))
}
func (c *Client) DebugExport(ctx context.Context, jobID string) (protocol.DebugExportResult, error) {
	return decode[protocol.DebugExportResult](c.call(ctx, "debugExport", map[string]string{"job_id": jobID}))
}
func (c *Client) Subscribe(fn func(protocol.Event)) func() {
	c.smu.Lock()
	c.seq++
	id := c.seq
	c.subs[id] = fn
	c.smu.Unlock()
	return func() {
		c.smu.Lock()
		delete(c.subs, id)
		c.smu.Unlock()
	}
}
func (c *Client) SetMCPConnected(v bool) {
	_, _ = c.call(context.Background(), "setMCP", map[string]bool{"live": v})
}

func (c *Client) Close() error { return c.conn.Close() }

func (c *Client) dispatchEvents() {
	for {
		select {
		case <-c.done:
			return
		case ev := <-c.events:
			c.smu.Lock()
			fns := make([]func(protocol.Event), 0, len(c.subs))
			for _, fn := range c.subs {
				fns = append(fns, fn)
			}
			c.smu.Unlock()
			for _, fn := range fns {
				fn(ev)
			}
		}
	}
}
