//go:build windows

package silent

import (
	"bytes"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
)

func TestHidePipesStdinStdoutStderrAndNonZeroExit(t *testing.T) {
	if os.Getenv("GROKMCP_HIDE_CHILD") == "1" {
		hwnd, _, _ := syscall.NewLazyDLL("kernel32.dll").NewProc("GetConsoleWindow").Call()
		in, _ := io.ReadAll(os.Stdin)
		_, _ = os.Stdout.WriteString("stdout:" + string(in) + " hwnd=" + strconv.FormatUint(uint64(hwnd), 10) + "\n")
		_, _ = os.Stderr.WriteString("stderr:" + string(in) + "\n")
		os.Exit(17)
	}
	cmd := exec.Command(os.Args[0], "-test.run=^TestHidePipesStdinStdoutStderrAndNonZeroExit$")
	cmd.Env = append(os.Environ(), "GROKMCP_HIDE_CHILD=1")
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: 0x00000200}
	Hide(cmd)
	if cmd.SysProcAttr.CreationFlags&0x00000200 == 0 || cmd.SysProcAttr.CreationFlags&createNoWindow == 0 || !cmd.SysProcAttr.HideWindow {
		t.Fatalf("flags not preserved: %#v", cmd.SysProcAttr)
	}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	if _, err := io.WriteString(stdin, "payload-stdin"); err != nil {
		t.Fatal(err)
	}
	if err := stdin.Close(); err != nil {
		t.Fatal(err)
	}
	var outBuf, errBuf bytes.Buffer
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		_, _ = io.Copy(&outBuf, stdout)
	}()
	go func() {
		defer wg.Done()
		_, _ = io.Copy(&errBuf, stderr)
	}()
	wg.Wait()
	err = cmd.Wait()
	exit, ok := err.(*exec.ExitError)
	if !ok || exit.ExitCode() != 17 {
		t.Fatalf("exit err=%v", err)
	}
	out := outBuf.String()
	errOut := errBuf.String()
	if !strings.Contains(out, "stdout:payload-stdin") || !strings.Contains(out, "hwnd=0") || strings.Contains(out, "stderr:") {
		t.Fatalf("stdout %q", out)
	}
	if !strings.Contains(errOut, "stderr:payload-stdin") || strings.Contains(errOut, "stdout:") || strings.Contains(errOut, "hwnd=") {
		t.Fatalf("stderr %q", errOut)
	}
}
