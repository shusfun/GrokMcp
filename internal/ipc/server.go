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
		if req.Method == "wait" || req.Method == "debugWait" {
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
			continue
		}
		res, err := s.dispatch(ctx, req)
		out := Response{ID: req.ID}
		if err != nil {
			out.Error = err.Error()
		} else {
			out.Result = res
		}
		write(out)
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
