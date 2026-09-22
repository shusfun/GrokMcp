//go:build windows

package terminal

import (
	"bufio"
	"context"
	"encoding/json"
	"net"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/Microsoft/go-winio"
	"grokmcp/internal/ownedprocess"
)

func fixtureWorker(t *testing.T) *terminalWorker {
	t.Helper()
	node, err := exec.LookPath("node")
	if err != nil {
		t.Fatal("Node fixture runtime required:", err)
	}
	c, err := ownedprocess.NewConsole(100, 30)
	if err != nil {
		t.Fatal(err)
	}
	g, err := ownedprocess.New()
	if err != nil {
		c.Close()
		t.Fatal(err)
	}
	script := `let n=0;process.stdin.setRawMode(true);process.stdin.on('data',d=>process.stdout.write('INPUT:'+d.toString()+'\r\n'));setInterval(()=>process.stdout.write('TICK:'+(++n)+'\r\n'),50)`
	p, err := g.Start(ownedprocess.Spec{Path: node, Args: []string{"-e", script}, Console: c})
	if err != nil {
		g.Close()
		c.Close()
		t.Fatal(err)
	}
	w := &terminalWorker{process: p, group: g, console: c, jobID: "fixture-job", sessionID: "original-session", ready: make(chan struct{})}
	close(w.ready)
	t.Cleanup(w.close)
	return w
}
func connectFixtureView(t *testing.T, v *terminalView, gen string) net.Conn {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	c, err := winio.DialPipeContext(ctx, v.pipe)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Close() })
	if err = json.NewEncoder(c).Encode(viewFrame{Job: v.worker.jobID, Session: v.worker.sessionID, Generation: gen, PID: 1234}); err != nil {
		t.Fatal(err)
	}
	return c
}
func readUntil(t *testing.T, c net.Conn, needle string) string {
	t.Helper()
	_ = c.SetReadDeadline(time.Now().Add(3 * time.Second))
	scan := bufio.NewScanner(c)
	scan.Buffer(make([]byte, 4096), 2*1024*1024)
	var text string
	for scan.Scan() {
		var f viewFrame
		if json.Unmarshal(scan.Bytes(), &f) != nil {
			t.Fatal("invalid output frame")
		}
		text += string(f.Output)
		if strings.Contains(text, needle) {
			return text
		}
	}
	t.Fatalf("missing %q, error=%v output=%q", needle, scan.Err(), text)
	return ""
}

func TestCloseAndReconnectViewerPreservesInteractiveWorker(t *testing.T) {
	w := fixtureWorker(t)
	pid := w.process.PID
	v, err := newTerminalView(w)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { v.Close() })
	go v.serve()
	c := connectFixtureView(t, v, v.generation)
	readUntil(t, c, "TICK:")
	if err = json.NewEncoder(c).Encode(viewFrame{Input: []byte("ping")}); err != nil {
		t.Fatal(err)
	}
	readUntil(t, c, "INPUT:ping")
	before := string(w.console.Output())
	c.Close()
	select {
	case <-v.done:
	case <-time.After(3 * time.Second):
		t.Fatal("viewer did not detach")
	}
	time.Sleep(150 * time.Millisecond)
	if !w.alive() || w.process.PID != pid || len(w.console.Output()) <= len(before) {
		t.Fatal("closing view stopped task progress")
	}
	v2, err := newTerminalView(w)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { v2.Close() })
	go v2.serve()
	c2 := connectFixtureView(t, v2, v2.generation)
	readUntil(t, c2, "TICK:")
	if err = json.NewEncoder(c2).Encode(viewFrame{Cols: 90, Rows: 24, Input: []byte("again")}); err != nil {
		t.Fatal(err)
	}
	readUntil(t, c2, "INPUT:again")
	if w.process.PID != pid || w.sessionID != "original-session" {
		t.Fatal("reopen replaced worker or session")
	}
}

func TestStaleViewerCannotInjectInput(t *testing.T) {
	w := fixtureWorker(t)
	v, err := newTerminalView(w)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { v.Close() })
	go v.serve()
	bad := connectFixtureView(t, v, "old-generation")
	_ = json.NewEncoder(bad).Encode(viewFrame{Input: []byte("not-allowed")})
	_ = bad.SetReadDeadline(time.Now().Add(time.Second))
	var one [1]byte
	if _, err := bad.Read(one[:]); err == nil {
		t.Fatal("stale generation accepted")
	}
	good := connectFixtureView(t, v, v.generation)
	readUntil(t, good, "TICK:")
	if strings.Contains(string(w.console.Output()), "not-allowed") {
		t.Fatal("stale input reached worker")
	}
}

func TestTemplatesLaunchViewerNotGrok(t *testing.T) {
	args := []string{"terminal-view", "--pipe", `\\.\pipe\fixture`, "--generation", "123"}
	for _, template := range []string{"", `cmd /c {command}`, `wt new-tab {command}`, `wt new-tab {grok} --resume {session_id}`} {
		spec, err := viewerSpec(`C:\Programs\Grok Supervisor\GrokMcp.exe`, args, template, `C:\work space`, "same-session")
		if err != nil {
			t.Fatal(err)
		}
		text := strings.Join(spec.Args, " ")
		if !strings.Contains(text, "terminal-view") || strings.Contains(text, "--resume") {
			t.Fatalf("template bypassed viewer: %s", text)
		}
	}
	if _, err := viewerSpec("app.exe", args, `{grok} agent leader`, "", "id"); err == nil {
		t.Fatal("legacy worker template accepted")
	}
}

func TestViewerIsAllowedToExitNormallyBeforeGroupCleanup(t *testing.T) {
	w := fixtureWorker(t)
	v, err := newTerminalView(w)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { v.Close() })
	g, err := ownedprocess.New()
	if err != nil {
		t.Fatal(err)
	}
	v.launcher = g
	args, _ := json.Marshal(viewFrame{Job: w.jobID, Session: w.sessionID, Generation: v.generation, PID: 1})
	script := `const net=require('net');const c=net.connect(process.argv[1]);c.on('connect',()=>{let h=JSON.parse(process.argv[2]);h.pid=process.pid;c.write(JSON.stringify(h)+'\n')});c.on('data',()=>{});c.on('end',()=>process.exit(0));c.on('error',()=>process.exit(2))`
	node, err := exec.LookPath("node")
	if err != nil {
		t.Fatal(err)
	}
	p, err := g.Start(ownedprocess.Spec{Path: node, Args: []string{"-e", script, v.pipe, string(args)}})
	if err != nil {
		t.Fatal(err)
	}
	v.process = p
	go v.serve()
	deadline := time.Now().Add(2 * time.Second)
	for v.pid.Load() == 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if v.pid.Load() == 0 {
		t.Fatal("fixture viewer did not connect")
	}
	_ = v.Close()
	if err := p.Wait(); err != nil {
		t.Fatalf("viewer was forcibly terminated: %v", err)
	}
	if !w.alive() {
		t.Fatal("viewer close killed worker")
	}
}
