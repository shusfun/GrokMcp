//go:build windows

package terminal

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"net"
	"os"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Microsoft/go-winio"
	"github.com/google/uuid"
	"golang.org/x/sys/windows"
	"grokmcp/internal/ownedprocess"
	"grokmcp/internal/paths"
)

type terminalView struct {
	mu               sync.Mutex
	worker           *terminalWorker
	listener         net.Listener
	conn             net.Conn
	launcher         *ownedprocess.Group
	process          *ownedprocess.Process
	pid              atomic.Int64
	pipe, generation string
	done             chan struct{}
	once             sync.Once
}

func (v *terminalView) PID() int {
	select {
	case <-v.done:
		return 0
	default:
		return int(v.pid.Load())
	}
}
func (v *terminalView) Pending() bool {
	select {
	case <-v.done:
		return false
	default:
		return v.pid.Load() == 0
	}
}
func (v *terminalView) Wait() error      { <-v.done; return nil }
func (v *terminalView) WindowID() string { return "" }
func (v *terminalView) TTY() string      { return "" }
func (v *terminalView) Close() error {
	v.once.Do(func() {
		close(v.done)
		_ = v.listener.Close()
		v.mu.Lock()
		c := v.conn
		g := v.launcher
		p := v.process
		v.mu.Unlock()
		if c != nil {
			_ = c.Close()
		}
		if g != nil {
			// 先让查看客户端正常退出，避免 Windows Terminal 保留“异常退出”的空窗口。
			if c != nil && p != nil {
				select {
				case <-p.Done():
				case <-time.After(2 * time.Second):
				}
			}
			_ = g.Close()
		}
	})
	return nil
}

func (e Exec) OpenPending(ctx context.Context, bin, sid, cwd string) (Handle, error) {
	return e.openViewer(ctx, bin, sid, cwd, true)
}
func (e Exec) ActivateSession(ctx context.Context, bin, sid, cwd string) error {
	if err := e.ensure(ctx); err != nil {
		return err
	}
	e.managed.mu.Lock()
	defer e.managed.mu.Unlock()
	w := e.managed.workers[sid]
	if w == nil || e.managed.closed {
		return errors.New("查看请求已关闭")
	}
	return e.activateWorker(w, bin, sid, cwd)
}

func (e Exec) activateWorker(w *terminalWorker, bin, sid, cwd string) error {
	w.mu.Lock()
	started := w.process != nil
	w.mu.Unlock()
	if started {
		if w.alive() {
			return nil
		}
		return errors.New("后台交互进程已经退出，请重新打开终端")
	}
	args := []string{"--resume", sid}
	if sid == "" {
		args = []string{"dashboard"}
	}
	args, err := paths.GrokArgs(args...)
	if err != nil {
		return err
	}
	g, err := ownedprocess.New()
	if err != nil {
		return err
	}
	p, err := g.Start(ownedprocess.Spec{Path: bin, Args: args, Dir: cwd, Console: w.console})
	if err != nil {
		_ = g.Close()
		return err
	}
	w.mu.Lock()
	w.group = g
	w.process = p
	w.mu.Unlock()
	close(w.ready)
	go func() {
		_ = p.Wait()
		w.close()
		e.managed.mu.Lock()
		notify := e.managed.workerExit
		if e.managed.workers[sid] == w {
			delete(e.managed.workers, sid)
		}
		e.managed.mu.Unlock()
		if notify != nil {
			notify(sid, w.generation)
		}
	}()
	return nil
}

func (e Exec) openViewer(ctx context.Context, bin, sid, cwd string, pending bool) (Handle, error) {
	if !pending {
		if err := e.ensure(ctx); err != nil {
			return nil, err
		}
	}
	e.managed.mu.Lock()
	defer e.managed.mu.Unlock()
	if e.managed.closed {
		return nil, errors.New("终端管理器已关闭")
	}
	if h := e.managed.handles[sid]; h != nil {
		if v, ok := h.(*terminalView); ok {
			select {
			case <-v.done:
			default:
				return h, nil
			}
		} else if h.PID() > 0 {
			return h, nil
		}
	}
	w := e.managed.workers[sid]
	created := false
	success := false
	defer func() {
		if created && !success {
			w.close()
			if e.managed.workers[sid] == w {
				delete(e.managed.workers, sid)
			}
		}
	}()
	if w != nil && w.jobID != viewJob(ctx, sid) {
		return nil, errors.New("会话已绑定其他任务")
	}
	if w == nil || !w.alive() {
		if w != nil {
			w.close()
		}
		c, err := ownedprocess.NewConsole(120, 30)
		if err != nil {
			return nil, err
		}
		w = &terminalWorker{console: c, jobID: viewJob(ctx, sid), sessionID: sid, generation: uuid.NewString(), ready: make(chan struct{})}
		e.managed.workers[sid] = w
		created = true
	}
	if !pending {
		if err := e.activateWorker(w, bin, sid, cwd); err != nil {
			w.close()
			delete(e.managed.workers, sid)
			return nil, err
		}
	}
	v, err := newTerminalView(w)
	if err != nil {
		return nil, err
	}
	exe, err := os.Executable()
	if err != nil {
		_ = v.Close()
		return nil, err
	}
	args := []string{"terminal-view", "--pipe", v.pipe, "--job", w.jobID, "--session", sid, "--generation", v.generation}
	spec, err := viewerSpec(exe, args, e.Template, cwd, sid)
	if err != nil {
		_ = v.Close()
		return nil, err
	}
	g, err := ownedprocess.New()
	if err != nil {
		_ = v.Close()
		return nil, err
	}
	v.launcher = g
	p, err := g.Start(spec)
	if err != nil {
		_ = v.Close()
		return nil, err
	}
	v.process = p
	e.managed.handles[sid] = v
	go v.serve()
	go func() {
		timer := time.NewTimer(8 * time.Second)
		defer timer.Stop()
		select {
		case <-v.done:
			return
		case <-timer.C:
			if v.pid.Load() == 0 {
				_ = v.Close()
			}
		}
	}()
	go func() {
		<-v.done
		e.managed.mu.Lock()
		if e.managed.handles[sid] == v {
			delete(e.managed.handles, sid)
		}
		if !w.alive() && e.managed.workers[sid] == w {
			delete(e.managed.workers, sid)
			w.close()
		}
		e.managed.mu.Unlock()
	}()
	success = true
	return v, nil
}

func viewerSpec(exe string, args []string, template, cwd, sid string) (ownedprocess.Spec, error) {
	spec := ownedprocess.Spec{Path: exe, Args: args, Dir: cwd, Visible: true, Title: SessionTitle(sid)}
	if strings.TrimSpace(template) == "" {
		return spec, nil
	}
	// 旧标准恢复模板仅在执行时改为查看客户端，不改写用户保存的配置。
	template = legacyResumeTemplate.ReplaceAllString(template, "{command}")
	if strings.Count(template, "{command}") != 1 || strings.Contains(template, "{grok}") {
		return spec, errors.New("终端模板必须包含一次 {command}；不再支持直接启动 Grok 的 {grok}")
	}
	command := windows.ComposeCommandLine(append([]string{exe}, args...))
	spec.Path = "cmd"
	spec.Args = []string{"/c", Render(template, "", command, cwd, sid)}
	return spec, nil
}

var legacyResumeTemplate = regexp.MustCompile(`["']?\{grok\}["']?\s+--resume\s+["']?\{session_id\}["']?`)

func newTerminalView(w *terminalWorker) (*terminalView, error) {
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		return nil, err
	}
	pipe := `\\.\pipe\grokmcp-view-` + uuid.NewString()
	ln, err := winio.ListenPipe(pipe, &winio.PipeConfig{SecurityDescriptor: "D:P(A;;GA;;;" + user.User.Sid.String() + ")", InputBufferSize: 65536, OutputBufferSize: 65536})
	if err != nil {
		return nil, err
	}
	return &terminalView{worker: w, listener: ln, pipe: pipe, generation: uuid.NewString(), done: make(chan struct{})}, nil
}

func (v *terminalView) serve() {
	defer v.Close()
	for {
		c, err := v.listener.Accept()
		if err != nil {
			return
		}
		_ = c.SetReadDeadline(time.Now().Add(3 * time.Second))
		scan := bufio.NewScanner(c)
		scan.Buffer(make([]byte, 4096), 65536)
		var hello viewFrame
		if !scan.Scan() || json.Unmarshal(scan.Bytes(), &hello) != nil || hello.Session != v.worker.sessionID || hello.Job != v.worker.jobID || hello.Generation != v.generation || hello.PID <= 0 {
			_ = c.Close()
			continue
		}
		_ = c.SetReadDeadline(time.Time{})
		v.mu.Lock()
		select {
		case <-v.done:
			v.mu.Unlock()
			_ = c.Close()
			return
		default:
		}
		v.conn = c
		v.pid.Store(int64(hello.PID))
		v.mu.Unlock()
		v.serveConnection(c, scan)
		return
	}
}

func (v *terminalView) serveConnection(c net.Conn, scan *bufio.Scanner) {
	var writeMu sync.Mutex
	send := func(f viewFrame) error {
		writeMu.Lock()
		defer writeMu.Unlock()
		_ = c.SetWriteDeadline(time.Now().Add(2 * time.Second))
		return json.NewEncoder(c).Encode(f)
	}
	_, ch, off := v.worker.console.Subscribe()
	defer off()
	readerDone := make(chan struct{})
	go func() {
		defer close(readerDone)
		defer c.Close()
		for scan.Scan() {
			var f viewFrame
			if json.Unmarshal(scan.Bytes(), &f) != nil {
				return
			}
			if f.Cols > 0 {
				if err := v.worker.console.Resize(f.Cols, f.Rows); err != nil {
					return
				}
			}
			if len(f.Input) > 4096 {
				return
			}
			if len(f.Input) > 0 {
				select {
				case <-v.worker.ready:
					if _, err := v.worker.console.Write(f.Input); err != nil {
						return
					}
				default:
				}
			}
		}
	}()
	ready := v.worker.ready
	if !v.worker.alive() {
		_ = send(viewFrame{Output: []byte("[当前任务继续在后台执行，正在等待安全的交互边界。]\r\n")})
	}
	for {
		select {
		case <-v.done:
			return
		case <-readerDone:
			return
		case <-ready:
			ready = nil
			reset := append([]byte("\x1b[0m\x1b[2J\x1b[H"), v.worker.console.Modes()...)
			if send(viewFrame{Ready: true, Output: reset}) != nil {
				return
			}
			if v.worker.console.Refresh() != nil {
				return
			}
		case part, ok := <-ch:
			if !ok {
				return
			}
			if ready != nil {
				continue
			}
			if send(viewFrame{Output: part}) != nil {
				return
			}
		}
	}
}

func (e Exec) testViewerTemplate(_ context.Context, template string) error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	spec, err := viewerSpec(exe, []string{"terminal-view", "--demo"}, template, "", "template-test")
	if err != nil {
		return err
	}
	g, err := ownedprocess.New()
	if err != nil {
		return err
	}
	p, err := g.Start(spec)
	if err != nil {
		g.Close()
		return err
	}
	go func() { _ = p.Wait(); _ = g.Close() }()
	return nil
}
