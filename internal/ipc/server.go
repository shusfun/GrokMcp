package ipc

import (
	"bufio"
	"context"
	"encoding/json"
	"net"
	"os"
	"sync"

	"grokmcp/internal/core"
	"grokmcp/internal/protocol"
)

type Server struct {
	ln   net.Listener
	svc  core.Backend
	mu   sync.Mutex
	cons []net.Conn
}

func Serve(ln net.Listener, svc core.Backend) *Server {
	s := &Server{ln: ln, svc: svc}
	go s.loop()
	return s
}

func (s *Server) Close() error {
	s.mu.Lock()
	for _, c := range s.cons {
		_ = c.Close()
	}
	s.mu.Unlock()
	return s.ln.Close()
}

func (s *Server) loop() {
	for {
		c, err := s.ln.Accept()
		if err != nil {
			return
		}
		s.mu.Lock()
		s.cons = append(s.cons, c)
		s.mu.Unlock()
		go s.handle(c)
	}
}

func (s *Server) handle(c net.Conn) {
	defer c.Close()
	var wmu sync.Mutex
	write := func(v Response) {
		b, _ := json.Marshal(v)
		wmu.Lock()
		_, _ = c.Write(append(b, '\n'))
		wmu.Unlock()
	}
	off := s.svc.Subscribe(func(ev protocol.Event) {
		write(Response{Method: "event", Params: mustJSON(ev)})
	})
	defer off()
	sc := bufio.NewScanner(c)
	sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	for sc.Scan() {
		var req Request
		if err := json.Unmarshal(sc.Bytes(), &req); err != nil {
			continue
		}
		// 响应按 ID 匹配，握手/诊断不能阻塞同连接的状态与取消请求。
		go func(req Request) {
			res, err := s.dispatch(ctx, req)
			out := Response{ID: req.ID}
			if err != nil {
				out.Error = err.Error()
			} else {
				out.Result = res
			}
			write(out)
		}(req)
	}
}

func (s *Server) dispatch(ctx context.Context, req Request) (json.RawMessage, error) {
	switch req.Method {
	case "dispatch":
		var in protocol.DispatchRequest
		if err := json.Unmarshal(req.Params, &in); err != nil {
			return nil, err
		}
		out, err := s.svc.Dispatch(ctx, in)
		return marshal(out, err)
	case "wait":
		var in protocol.WaitRequest
		if err := json.Unmarshal(req.Params, &in); err != nil {
			return nil, err
		}
		out, err := s.svc.Wait(ctx, in)
		return marshal(out, err)
	case "planDecide":
		var in protocol.PlanDecideRequest
		if err := json.Unmarshal(req.Params, &in); err != nil {
			return nil, err
		}
		out, err := s.svc.PlanDecide(ctx, in)
		return marshal(out, err)
	case "followup":
		var in protocol.FollowupRequest
		if err := json.Unmarshal(req.Params, &in); err != nil {
			return nil, err
		}
		out, err := s.svc.Followup(ctx, in)
		return marshal(out, err)
	case "cancelTurn":
		var in struct {
			JobID string `json:"job_id"`
		}
		if err := json.Unmarshal(req.Params, &in); err != nil {
			return nil, err
		}
		out, err := s.svc.CancelTurn(ctx, in.JobID)
		return marshal(out, err)
	case "setView":
		var in protocol.SetViewRequest
		if err := json.Unmarshal(req.Params, &in); err != nil {
			return nil, err
		}
		out, err := s.svc.SetView(ctx, in)
		return marshal(out, err)
	case "status":
		var in struct {
			JobID string `json:"job_id"`
		}
		_ = json.Unmarshal(req.Params, &in)
		out, err := s.svc.Status(ctx, in.JobID)
		return marshal(out, err)
	case "listJobs":
		out, err := s.svc.ListJobs(ctx)
		return marshal(out, err)
	case "listJobsPage":
		var in protocol.ListJobsQuery
		if len(req.Params) > 0 {
			if err := json.Unmarshal(req.Params, &in); err != nil {
				return nil, err
			}
		}
		out, err := s.svc.ListJobsPage(ctx, in)
		return marshal(out, err)
	case "importProject":
		var in protocol.ImportProjectRequest
		if err := json.Unmarshal(req.Params, &in); err != nil {
			return nil, err
		}
		install := true
		if in.InstallSkill != nil {
			install = *in.InstallSkill
		}
		out, err := s.svc.ImportProject(ctx, in.Path, install)
		return marshal(out, err)
	case "listProjects":
		out, err := s.svc.ListProjects(ctx)
		return marshal(out, err)
	case "getProject":
		var in protocol.ProjectIDRequest
		if err := json.Unmarshal(req.Params, &in); err != nil {
			return nil, err
		}
		out, err := s.svc.GetProject(ctx, in.ProjectID)
		return marshal(out, err)
	case "removeProject":
		var in protocol.ProjectIDRequest
		if err := json.Unmarshal(req.Params, &in); err != nil {
			return nil, err
		}
		out, err := s.svc.RemoveProject(ctx, in.ProjectID)
		return marshal(out, err)
	case "skillStatus":
		var in protocol.ProjectIDRequest
		if err := json.Unmarshal(req.Params, &in); err != nil {
			return nil, err
		}
		out, err := s.svc.SkillStatus(ctx, in.ProjectID)
		return marshal(out, err)
	case "skillInstall":
		var in protocol.ProjectIDRequest
		if err := json.Unmarshal(req.Params, &in); err != nil {
			return nil, err
		}
		out, err := s.svc.SkillInstall(ctx, in.ProjectID)
		return marshal(out, err)
	case "skillUpdate":
		var in protocol.ProjectIDRequest
		if err := json.Unmarshal(req.Params, &in); err != nil {
			return nil, err
		}
		out, err := s.svc.SkillUpdate(ctx, in.ProjectID)
		return marshal(out, err)
	case "skillRemove":
		var in protocol.ProjectIDRequest
		if err := json.Unmarshal(req.Params, &in); err != nil {
			return nil, err
		}
		out, err := s.svc.SkillRemove(ctx, in.ProjectID)
		return marshal(out, err)
	case "generatePrompt":
		var in protocol.GeneratePromptRequest
		if err := json.Unmarshal(req.Params, &in); err != nil {
			return nil, err
		}
		out, err := s.svc.GeneratePrompt(ctx, in)
		return marshal(out, err)
	case "savePrompt":
		var in protocol.SavePromptRequest
		if err := json.Unmarshal(req.Params, &in); err != nil {
			return nil, err
		}
		out, err := s.svc.SavePrompt(ctx, in)
		return marshal(out, err)
	case "openProjectDir":
		var in protocol.ProjectIDRequest
		_ = json.Unmarshal(req.Params, &in)
		return marshal(struct{}{}, s.svc.OpenProjectDir(ctx, in.ProjectID))
	case "archiveJob":
		var in struct {
			JobID string `json:"job_id"`
		}
		if err := json.Unmarshal(req.Params, &in); err != nil {
			return nil, err
		}
		out, err := s.svc.ArchiveJob(ctx, in.JobID)
		return marshal(out, err)
	case "unarchiveJob":
		var in struct {
			JobID string `json:"job_id"`
		}
		if err := json.Unmarshal(req.Params, &in); err != nil {
			return nil, err
		}
		out, err := s.svc.UnarchiveJob(ctx, in.JobID)
		return marshal(out, err)
	case "deleteJob":
		var in struct {
			JobID string `json:"job_id"`
		}
		if err := json.Unmarshal(req.Params, &in); err != nil {
			return nil, err
		}
		return marshal(struct{}{}, s.svc.DeleteJob(ctx, in.JobID))
	case "openTerminal":
		var in protocol.OpenTerminalRequest
		if err := json.Unmarshal(req.Params, &in); err != nil {
			return nil, err
		}
		return marshal(struct{}{}, s.svc.OpenTerminal(ctx, in))
	case "openProject":
		var in struct {
			JobID string `json:"job_id"`
		}
		_ = json.Unmarshal(req.Params, &in)
		return marshal(struct{}{}, s.svc.OpenProject(ctx, in.JobID))
	case "continue":
		var in struct {
			JobID string `json:"job_id"`
		}
		_ = json.Unmarshal(req.Params, &in)
		out, err := s.svc.Continue(ctx, in.JobID)
		return marshal(out, err)
	case "detachView":
		var in struct {
			JobID string `json:"job_id"`
		}
		_ = json.Unmarshal(req.Params, &in)
		out, err := s.svc.DetachView(ctx, in.JobID)
		return marshal(out, err)
	case "settings":
		out, err := s.svc.Settings(ctx)
		return marshal(out, err)
	case "saveSettings":
		var in protocol.Settings
		if err := json.Unmarshal(req.Params, &in); err != nil {
			return nil, err
		}
		return marshal(struct{}{}, s.svc.SaveSettings(ctx, in))
	case "diagnose":
		out, err := s.svc.Diagnose(ctx)
		return marshal(out, err)
	case "mcpConfig":
		out, err := s.svc.MCPConfig(ctx)
		return marshal(out, err)
	case "mcpStatus":
		out, err := s.svc.MCPStatus(ctx)
		return marshal(out, err)
	case "openCCSwitchMCPImport":
		out, err := s.svc.OpenCCSwitchMCPImport(ctx)
		return marshal(out, err)
	case "openCCSwitchApp":
		out, err := s.svc.OpenCCSwitchApp(ctx)
		return marshal(out, err)
	case "addMCPToCodex":
		out, err := s.svc.AddMCPToCodex(ctx)
		return marshal(out, err)
	case "statusBar":
		out, err := s.svc.StatusBar(ctx)
		return marshal(out, err)
	case "events":
		var in struct {
			JobID string `json:"job_id"`
		}
		_ = json.Unmarshal(req.Params, &in)
		out, err := s.svc.Events(ctx, in.JobID)
		return marshal(out, err)
	case "testTerminal":
		var in struct {
			Template string `json:"template"`
		}
		_ = json.Unmarshal(req.Params, &in)
		return marshal(struct{}{}, s.svc.TestTerminal(ctx, in.Template))
	case "debugSet":
		var in protocol.DebugSetRequest
		if err := json.Unmarshal(req.Params, &in); err != nil {
			return nil, err
		}
		out, err := s.svc.DebugSet(ctx, in)
		return marshal(out, err)
	case "debugSnapshot":
		var in protocol.DebugSnapshotRequest
		if err := json.Unmarshal(req.Params, &in); err != nil {
			return nil, err
		}
		out, err := s.svc.DebugSnapshot(ctx, in)
		return marshal(out, err)
	case "debugWait":
		var in protocol.DebugWaitRequest
		if err := json.Unmarshal(req.Params, &in); err != nil {
			return nil, err
		}
		out, err := s.svc.DebugWait(ctx, in)
		return marshal(out, err)
	case "debugExport":
		var in struct {
			JobID string `json:"job_id"`
		}
		_ = json.Unmarshal(req.Params, &in)
		out, err := s.svc.DebugExport(ctx, in.JobID)
		return marshal(out, err)
	case "setMCP":
		var in struct {
			Live bool `json:"live"`
		}
		_ = json.Unmarshal(req.Params, &in)
		if ls, ok := s.svc.(interface{ SetMCPConnected(bool) }); ok {
			ls.SetMCPConnected(in.Live)
		}
		return marshal(struct{}{}, nil)
	default:
		return nil, os.ErrInvalid
	}
}

func marshal(v any, err error) (json.RawMessage, error) {
	if err != nil {
		return nil, err
	}
	b, err := json.Marshal(v)
	return b, err
}

func mustJSON(v any) json.RawMessage {
	b, _ := json.Marshal(v)
	return b
}
