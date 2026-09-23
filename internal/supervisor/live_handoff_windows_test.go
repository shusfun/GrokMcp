//go:build windows

package supervisor

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	goruntime "runtime"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
	"grokmcp/internal/agent"
	"grokmcp/internal/clock"
	"grokmcp/internal/grokbin"
	"grokmcp/internal/ids"
	"grokmcp/internal/paths"
	"grokmcp/internal/protocol"
	"grokmcp/internal/store"
	"grokmcp/internal/terminal"
)

func TestMain(m *testing.M) {
	if len(os.Args) > 1 && os.Args[1] == "terminal-view" {
		if err := terminal.RunViewer(context.Background(), os.Args[2:]); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		os.Exit(0)
	}
	if os.Getenv("GROK_HANDOFF_INJECT") == "1" {
		os.Exit(injectViewerInput())
	}
	os.Exit(m.Run())
}

func TestLiveHandoffDuringACPApproval(t *testing.T) {
	if os.Getenv("GROK_LIVE_HANDOFF") != "1" {
		t.Skip("requires explicit isolated live handoff acceptance")
	}
	home := t.TempDir()
	t.Setenv("GROK_SUPERVISOR_HOME", home)
	socket, err := paths.SupervisorLeaderSocket()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(socket, home) {
		t.Fatalf("leader socket is not private: %s", socket)
	}
	watch, err := startHandoffWatch()
	if err != nil {
		t.Fatal(err)
	}
	defer watch.stop()

	bin, err := grokbin.New().Resolve("")
	if err != nil {
		t.Fatal(err)
	}
	work := filepath.Join(home, "work")
	if err := os.MkdirAll(work, 0o700); err != nil {
		t.Fatal(err)
	}
	st, err := store.Open(filepath.Join(home, "supervisor.db"))
	if err != nil {
		t.Fatal(err)
	}
	ag := agent.NewGrok(bin, grokbin.New())
	term := terminal.NewExec("", "", ag)
	s := New(st, ag, term, clock.Real{}, ids.UUID{})
	s.SetGrokPath(func() string { return bin })
	t.Cleanup(func() {
		_ = s.Close()
		_ = st.Close()
	})

	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Minute)
	defer cancel()
	res, err := s.Dispatch(ctx, protocol.DispatchRequest{
		Cwd: work,
		Tasks: []protocol.DispatchTask{{
			Prompt: "Plan a tiny read-only task: reply HANDOFF_ACP_OK. Request native plan approval before doing anything. Do not modify files.",
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	id := res.Jobs[0].JobID
	requestID := res.Jobs[0].RequestID
	deadline := time.Now().Add(3 * time.Minute)
	var job protocol.Job
	for time.Now().Before(deadline) {
		job, err = s.Status(ctx, id)
		if err == nil && job.State == protocol.StatePlanReady && job.GrokSessionID != "" {
			break
		}
		if err == nil && (job.State == protocol.StateFailed || job.State == protocol.StateBlocked || job.State == protocol.StateDisconnected) {
			t.Fatalf("ACP turn ended before approval: %+v", job)
		}
		time.Sleep(200 * time.Millisecond)
	}
	if job.State != protocol.StatePlanReady {
		t.Fatalf("native approval did not arrive: %+v", job)
	}

	if _, err = s.SetView(ctx, protocol.SetViewRequest{JobID: id, View: protocol.ViewHeaded}); err != nil {
		t.Fatal(err)
	}
	job = waitLive(t, id, s, func(j protocol.Job) bool {
		return j.ViewMode == protocol.ViewHeaded && j.InputOwner == protocol.OwnerTUI && j.TerminalPID > 0
	})
	sessionID := job.GrokSessionID
	workerPID := term.WorkerPID(sessionID)
	if workerPID <= 0 || !term.WorkerAlive(sessionID) {
		t.Fatalf("interactive worker did not start: pid=%d job=%+v", workerPID, job)
	}
	if extra := watch.unrequested(job.TerminalPID, terminal.SessionTitle(sessionID)); len(extra) > 0 {
		t.Fatalf("unrequested visible terminals: %v", extra)
	}

	if _, err = s.SetView(ctx, protocol.SetViewRequest{JobID: id, View: protocol.ViewHeadless}); err != nil {
		t.Fatal(err)
	}
	job = waitLive(t, id, s, func(j protocol.Job) bool { return j.ViewMode == protocol.ViewHeadless })
	if !term.WorkerAlive(sessionID) || term.WorkerPID(sessionID) != workerPID || job.InputOwner != protocol.OwnerTUI {
		t.Fatalf("closing viewer cancelled the worker: pid=%d job=%+v", term.WorkerPID(sessionID), job)
	}
	if _, err = s.SetView(ctx, protocol.SetViewRequest{JobID: id, View: protocol.ViewHeaded}); err != nil {
		t.Fatal(err)
	}
	job = waitLive(t, id, s, func(j protocol.Job) bool {
		return j.ViewMode == protocol.ViewHeaded && j.TerminalPID > 0
	})
	if term.WorkerPID(sessionID) != workerPID {
		t.Fatalf("reopen started another worker: %d -> %d", workerPID, term.WorkerPID(sessionID))
	}
	if extra := watch.unrequested(job.TerminalPID, terminal.SessionTitle(sessionID)); len(extra) > 0 {
		t.Fatalf("reopen created unrequested visible terminals: %v", extra)
	}

	renderDeadline := time.Now().Add(40 * time.Second)
	for time.Now().Before(renderDeadline) && len(term.SessionOutput(sessionID)) == 0 && term.WorkerAlive(sessionID) {
		time.Sleep(100 * time.Millisecond)
	}
	if err := sendViewerInput(job.TerminalPID, "Reply with exactly HANDOFF_TUI_REPLY. Do not run tools or modify files.\r"); err != nil {
		t.Fatal(err)
	}
	tokenDeadline := time.Now().Add(3 * time.Minute)
	for time.Now().Before(tokenDeadline) && term.WorkerAlive(sessionID) && !strings.Contains(string(term.SessionOutput(sessionID)), "HANDOFF_TUI_REPLY") {
		time.Sleep(200 * time.Millisecond)
	}
	output := string(term.SessionOutput(sessionID))
	if !strings.Contains(output, "HANDOFF_TUI_REPLY") {
		t.Fatalf("TUI did not return a complete model reply; alive=%v output=%q", term.WorkerAlive(sessionID), tail(output, 1200))
	}
	if _, err = s.Followup(ctx, protocol.FollowupRequest{JobID: id, Prompt: "steal"}); err != errTUIControl {
		t.Fatalf("followup during TUI control = %v", err)
	}
	if _, err = s.Continue(ctx, id); err != errTUIControl {
		t.Fatalf("continue during TUI control = %v", err)
	}
	if s.snapshot(id).QueueLength != 0 {
		t.Fatal("rejected Codex request entered the queue")
	}
	got, err := s.Status(ctx, id, protocol.ResultQuery{IncludeResult: true, RequestID: requestID})
	if err == nil && (got.State == protocol.StateCompleted || strings.Contains(got.Result.Text, "HANDOFF_TUI_REPLY")) {
		t.Fatalf("ACP status fabricated a TUI result: %+v", got)
	}
	if got.State == protocol.StateCompleted {
		t.Fatalf("yielded ACP request was marked completed: %+v", got)
	}
}

func waitLive(t *testing.T, id string, s *Service, ready func(protocol.Job) bool) protocol.Job {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	var job protocol.Job
	var err error
	for time.Now().Before(deadline) {
		job, err = s.Status(context.Background(), id)
		if err == nil && ready(job) {
			return job
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("live view did not become ready: %v %+v", err, job)
	return job
}

func tail(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[len(s)-n:]
}

func sendViewerInput(pid int, text string) error {
	cmd := exec.Command(os.Args[0])
	cmd.Env = append(os.Environ(), "GROK_HANDOFF_INJECT=1", "GROK_HANDOFF_PID="+itoa(pid), "GROK_HANDOFF_TEXT="+text)
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: 0x00000008}
	out, err := cmd.CombinedOutput()
	if err != nil {
		return &injectError{err: err, out: string(out)}
	}
	return nil
}

type injectError struct {
	err error
	out string
}

func (e *injectError) Error() string { return e.err.Error() + ": " + e.out }

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [16]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}

type keyEvent struct {
	KeyDown         int32
	RepeatCount     uint16
	VirtualKeyCode  uint16
	VirtualScanCode uint16
	UnicodeChar     uint16
	ControlKeyState uint32
}

type inputRecord struct {
	EventType uint16
	_         uint16
	Key       keyEvent
}

func injectViewerInput() int {
	pid := 0
	for _, c := range os.Getenv("GROK_HANDOFF_PID") {
		if c < '0' || c > '9' {
			return 2
		}
		pid = pid*10 + int(c-'0')
	}
	text := os.Getenv("GROK_HANDOFF_TEXT")
	if pid <= 0 || text == "" {
		return 2
	}
	kernel := windows.NewLazySystemDLL("kernel32.dll")
	attach := kernel.NewProc("AttachConsole")
	write := kernel.NewProc("WriteConsoleInputW")
	free := kernel.NewProc("FreeConsole")
	if r, _, err := attach.Call(uintptr(pid)); r == 0 {
		fmt.Fprintf(os.Stderr, "AttachConsole: %v", err)
		return 2
	}
	defer free.Call()
	name, err := windows.UTF16PtrFromString("CONIN$")
	if err != nil {
		fmt.Fprintf(os.Stderr, "CONIN$: %v", err)
		return 2
	}
	in, err := windows.CreateFile(name, windows.GENERIC_READ|windows.GENERIC_WRITE, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE, nil, windows.OPEN_EXISTING, 0, 0)
	if err != nil {
		fmt.Fprintf(os.Stderr, "CreateFile CONIN$: %v", err)
		return 2
	}
	defer windows.CloseHandle(in)
	records := make([]inputRecord, 0, len(text)*2)
	for _, r := range text {
		key := uint16(r)
		virtual := uint16(0)
		if r == '\r' {
			virtual = 0x0D
		}
		records = append(records,
			inputRecord{EventType: 0x0001, Key: keyEvent{KeyDown: 1, RepeatCount: 1, VirtualKeyCode: virtual, UnicodeChar: key}},
			inputRecord{EventType: 0x0001, Key: keyEvent{RepeatCount: 1, VirtualKeyCode: virtual, UnicodeChar: key}},
		)
	}
	var written uint32
	if r, _, err := write.Call(uintptr(in), uintptr(unsafe.Pointer(&records[0])), uintptr(len(records)), uintptr(unsafe.Pointer(&written))); r == 0 || int(written) != len(records) {
		fmt.Fprintf(os.Stderr, "WriteConsoleInput: %v written=%d", err, written)
		return 2
	}
	return 0
}

type handoffWindow struct {
	class, title string
	pid          uint32
}

type handoffWatch struct {
	mu     sync.Mutex
	seen   map[uintptr]handoffWindow
	thread uint32
	done   chan struct{}
}

func startHandoffWatch() (*handoffWatch, error) {
	w := &handoffWatch{seen: map[uintptr]handoffWindow{}, done: make(chan struct{})}
	ready := make(chan error, 1)
	go func() {
		goruntime.LockOSThread()
		defer goruntime.UnlockOSThread()
		defer close(w.done)
		user := windows.NewLazySystemDLL("user32.dll")
		enum := user.NewProc("EnumWindows")
		className := user.NewProc("GetClassNameW")
		text := user.NewProc("GetWindowTextW")
		visible := user.NewProc("IsWindowVisible")
		pidOf := user.NewProc("GetWindowThreadProcessId")
		hook := user.NewProc("SetWinEventHook")
		unhook := user.NewProc("UnhookWinEvent")
		get := user.NewProc("GetMessageW")
		var msg struct {
			Window         uintptr
			Message        uint32
			WParam, LParam uintptr
			Time           uint32
			X, Y           int32
			Private        uint32
		}
		w.thread = windows.GetCurrentThreadId()
		baseline := map[uintptr]bool{}
		cb := syscall.NewCallback(func(hwnd, unused uintptr) uintptr { baseline[hwnd] = true; return 1 })
		enum.Call(cb, 0)
		onShow := syscall.NewCallback(func(unused, event, hwnd, object, child, eventThread, eventTime uintptr) uintptr {
			if object != 0 || child != 0 || baseline[hwnd] {
				return 0
			}
			v, _, _ := visible.Call(hwnd)
			if v == 0 {
				return 0
			}
			var name [256]uint16
			className.Call(hwnd, uintptr(unsafe.Pointer(&name[0])), uintptr(len(name)))
			kind := windows.UTF16ToString(name[:])
			if kind != "ConsoleWindowClass" && kind != "CASCADIA_HOSTING_WINDOW_CLASS" {
				return 0
			}
			var title [512]uint16
			text.Call(hwnd, uintptr(unsafe.Pointer(&title[0])), uintptr(len(title)))
			var pid uint32
			pidOf.Call(hwnd, uintptr(unsafe.Pointer(&pid)))
			w.mu.Lock()
			w.seen[hwnd] = handoffWindow{class: kind, title: windows.UTF16ToString(title[:]), pid: pid}
			w.mu.Unlock()
			return 0
		})
		h, _, err := hook.Call(0x8002, 0x8002, 0, onShow, 0, 0, 0)
		if h == 0 {
			ready <- err
			return
		}
		defer unhook.Call(h)
		ready <- nil
		for {
			result, _, _ := get.Call(uintptr(unsafe.Pointer(&msg)), 0, 0, 0)
			if result == 0 || result == ^uintptr(0) {
				return
			}
		}
	}()
	return w, <-ready
}

func (w *handoffWatch) stop() {
	windows.NewLazySystemDLL("user32.dll").NewProc("PostThreadMessageW").Call(uintptr(w.thread), 0x12, 0, 0)
	<-w.done
}

func (w *handoffWatch) unrequested(viewerPID int, title string) []handoffWindow {
	w.mu.Lock()
	defer w.mu.Unlock()
	var extra []handoffWindow
	for _, win := range w.seen {
		if int(win.pid) == viewerPID || (title != "" && strings.Contains(win.title, title)) {
			continue
		}
		extra = append(extra, win)
	}
	return extra
}
