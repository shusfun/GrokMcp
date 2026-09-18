//go:build darwin

package terminal

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func writeResumeStub(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "grokstub")
	if err := os.WriteFile(path, []byte("#!/bin/sh\nexec sleep 86400\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestOpenResumeWaitUntilTabClosed(t *testing.T) {
	if testing.Short() || os.Getenv("GROK_LIVE") != "1" {
		t.Skip("set GROK_LIVE=1 to open Terminal.app")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()
	title := SessionTitle("testdetach99")
	e := Exec{}
	h, err := e.OpenResume(ctx, writeResumeStub(t), "testdetach99", "/tmp")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = h.Close() })

	done := make(chan error, 1)
	go func() { done <- h.Wait() }()
	time.Sleep(500 * time.Millisecond)
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
		t.Fatal("Wait returned before the tab closed")
	default:
	}
	deadline := time.Now().Add(3 * time.Second)
	var names []string
	for time.Now().Before(deadline) {
		names = terminalWindowNames("testdetach99", title)
		if len(names) > 0 {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if len(names) == 0 {
		t.Fatal("expected an open Terminal window")
	}
	for _, name := range names {
		if strings.Contains(name, "sleep 86400") {
			t.Fatalf("keep-alive sleep still running: %s", name)
		}
	}
	if err := h.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Wait did not return after Close")
	}
	if names := terminalWindowNames("testdetach99", title); len(names) != 0 {
		t.Fatalf("window still open: %v", names)
	}
}

func terminalWindowNames(needles ...string) []string {
	script := `tell application "Terminal"
  set out to ""
  repeat with w in windows
    try
      set out to out & (name of w as text) & linefeed
    end try
  end repeat
  return out
end tell`
	out, err := exec.Command("osascript", "-e", script).Output()
	if err != nil {
		return nil
	}
	var names []string
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		for _, needle := range needles {
			if needle != "" && strings.Contains(line, needle) {
				names = append(names, line)
				break
			}
		}
	}
	return names
}
