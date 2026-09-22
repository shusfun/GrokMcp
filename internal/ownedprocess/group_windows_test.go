//go:build windows

package ownedprocess

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"golang.org/x/sys/windows"
)

func TestOwnedHelper(t *testing.T) {
	role := os.Getenv("GS_PROCESS_ROLE")
	if role == "" {
		return
	}
	if role == "pipes" {
		in, _ := io.ReadAll(os.Stdin)
		hwnd, _, _ := syscall.NewLazyDLL("kernel32.dll").NewProc("GetConsoleWindow").Call()
		fmt.Fprintf(os.Stdout, "out:%s hwnd=%d", in, hwnd)
		fmt.Fprintf(os.Stderr, "err:%s", in)
		os.Exit(17)
	}
	dir := os.Getenv("GS_PROCESS_DIR")
	if role == "owner" {
		g, err := New()
		if err != nil {
			os.Exit(2)
		}
		_, err = g.Start(helperSpec("parent", dir))
		if err != nil {
			os.Exit(3)
		}
		_, _ = bufio.NewReader(os.Stdin).ReadString('\n')
		// 故意不 Close：验证宿主退出后内核回收 Job 内的整个树。
		os.Exit(0)
	}
	if role == "parent" || role == "child" {
		next := "child"
		if role == "child" {
			next = "grandchild"
		}
		cmd := exec.Command(os.Args[0], "-test.run=^TestOwnedHelper$")
		cmd.Env = helperSpec(next, dir).Env
		cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: windows.CREATE_NO_WINDOW}
		if err := cmd.Start(); err != nil {
			os.Exit(4)
		}
	}
	_ = os.WriteFile(filepath.Join(dir, role), []byte(strconv.Itoa(os.Getpid())), 0600)
	time.Sleep(time.Hour)
	os.Exit(0)
}

func helperSpec(role, dir string) Spec {
	env := []string{}
	for _, s := range os.Environ() {
		if !strings.HasPrefix(s, "GS_PROCESS_ROLE=") && !strings.HasPrefix(s, "GS_PROCESS_DIR=") {
			env = append(env, s)
		}
	}
	return Spec{Path: os.Args[0], Args: []string{"-test.run=^TestOwnedHelper$"}, Env: append(env, "GS_PROCESS_ROLE="+role, "GS_PROCESS_DIR="+dir)}
}

func waitFixture(t *testing.T, dir, role string) windows.Handle {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		b, err := os.ReadFile(filepath.Join(dir, role))
		if err == nil {
			pid, _ := strconv.Atoi(string(b))
			h, err := windows.OpenProcess(windows.SYNCHRONIZE|windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
			if err == nil {
				t.Cleanup(func() { windows.CloseHandle(h) })
				return h
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("fixture %s not ready", role)
	return 0
}

func assertExited(t *testing.T, h windows.Handle) {
	t.Helper()
	state, err := windows.WaitForSingleObject(h, 3000)
	if err != nil || state != windows.WAIT_OBJECT_0 {
		t.Fatalf("process remained: state=%d err=%v", state, err)
	}
}

func TestGroupOwnsDescendantsWithoutTouchingOtherGroup(t *testing.T) {
	g, err := New()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { g.Close() })
	other, err := New()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { other.Close() })
	dir := t.TempDir()
	if _, err = g.Start(helperSpec("parent", dir)); err != nil {
		t.Fatal(err)
	}
	ps := []windows.Handle{waitFixture(t, dir, "parent"), waitFixture(t, dir, "child"), waitFixture(t, dir, "grandchild")}
	if _, err = other.Start(helperSpec("unrelated", dir)); err != nil {
		t.Fatal(err)
	}
	unrelated := waitFixture(t, dir, "unrelated")
	if err = g.Close(); err != nil {
		t.Fatal(err)
	}
	for _, h := range ps {
		assertExited(t, h)
	}
	state, err := windows.WaitForSingleObject(unrelated, 0)
	if err != nil || state != uint32(windows.WAIT_TIMEOUT) {
		t.Fatalf("unrelated process stopped: %d %v", state, err)
	}
}

func TestOwnerExitClosesJobWithoutCleanup(t *testing.T) {
	dir := t.TempDir()
	spec := helperSpec("owner", dir)
	cmd := exec.Command(spec.Path, spec.Args...)
	cmd.Env = spec.Env
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: windows.CREATE_NO_WINDOW}
	in, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err = cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cmd.Process.Kill() })
	ps := []windows.Handle{waitFixture(t, dir, "parent"), waitFixture(t, dir, "child"), waitFixture(t, dir, "grandchild")}
	fmt.Fprintln(in, "exit")
	in.Close()
	if err = cmd.Wait(); err != nil {
		t.Fatal(err)
	}
	for _, h := range ps {
		assertExited(t, h)
	}
}

func TestNativeStartPreservesPipesHiddenWindowAndExit(t *testing.T) {
	g, err := New()
	if err != nil {
		t.Fatal(err)
	}
	defer g.Close()
	inR, inW, _ := os.Pipe()
	outR, outW, _ := os.Pipe()
	errR, errW, _ := os.Pipe()
	for _, f := range []*os.File{inR, inW, outR, outW, errR, errW} {
		defer f.Close()
	}
	spec := helperSpec("pipes", t.TempDir())
	spec.Stdin = inR
	spec.Stdout = outW
	spec.Stderr = errW
	p, err := g.Start(spec)
	if err != nil {
		t.Fatal(err)
	}
	inR.Close()
	outW.Close()
	errW.Close()
	fmt.Fprint(inW, "hello")
	inW.Close()
	out, _ := io.ReadAll(outR)
	errout, _ := io.ReadAll(errR)
	if err = p.Wait(); err == nil || !strings.Contains(err.Error(), "17") {
		t.Fatalf("exit: %v", err)
	}
	if string(out) != "out:hello hwnd=0" || string(errout) != "err:hello" {
		t.Fatalf("stdout=%q stderr=%q", out, errout)
	}
}
