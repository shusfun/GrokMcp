//go:build darwin

package terminal

import (
	"context"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"
)

var tabIDs sync.Map

func (e Exec) OpenDirectory(_ context.Context, cwd string) error {
	return exec.Command("open", cwd).Start()
}

func (e Exec) spawn(ctx context.Context, cwd, command, sessionID string) (Handle, error) {
	if strings.TrimSpace(e.Template) != "" {
		line := Render(e.Template, "grok", command, cwd, sessionID)
		cmd := exec.CommandContext(ctx, "sh", "-c", line)
		if err := cmd.Start(); err != nil {
			return nil, err
		}
		return procHandle{cmd: cmd, sessionID: sessionID}, nil
	}
	title := SessionTitle(sessionID)
	inner := CommandInDir(cwd, command)
	script := fmt.Sprintf(`tell application "Terminal"
  set t to do script %s
  try
    set custom title of t to %s
  end try
  activate
  set wid to id of front window as text
  set ty to ""
  try
    set ty to tty of t as text
  end try
  return wid & "," & ty
end tell`, appleQuote(inner), appleQuote(title))
	out, err := runOSA(ctx, script)
	if err != nil {
		return nil, err
	}
	id, tty := parseSpawnOut(out)
	rememberTab(title, id)
	h := &tabHandle{id: id, title: title, sessionID: sessionID, tty: tty}
	h.grokPID = FindResumePID(sessionID)
	return h, nil
}

func (e Exec) focus(ctx context.Context, title string) (bool, error) {
	if v, ok := tabIDs.Load(title); ok {
		if id, _ := v.(string); focusByID(ctx, id) {
			return true, nil
		}
	}
	return focusByTitle(ctx, title)
}

type tabHandle struct {
	id        string
	title     string
	sessionID string
	tty       string
	grokPID   int
	foundGrok bool
}

func (h *tabHandle) Wait() error {
	for {
		if pid := h.PID(); pid > 0 {
			h.grokPID = pid
			h.foundGrok = true
		}
		if h.foundGrok && !processAlive(h.grokPID) && FindResumePID(h.sessionID) == 0 {
			forgetTab(h.title)
			return nil
		}
		if !h.alive() {
			forgetTab(h.title)
			return nil
		}
		time.Sleep(200 * time.Millisecond)
	}
}

func (h *tabHandle) PID() int {
	if h.grokPID > 0 && processAlive(h.grokPID) {
		return h.grokPID
	}
	if pid := FindResumePID(h.sessionID); pid > 0 {
		h.grokPID = pid
		h.foundGrok = true
		return pid
	}
	return 0
}

func (h *tabHandle) WindowID() string { return h.id }

func (h *tabHandle) Close() error {
	if pid := h.PID(); pid > 0 {
		_ = terminatePID(pid, 2*time.Second)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_ = closeTerminalWindow(ctx, h.id, h.title)
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if FindResumePID(h.sessionID) == 0 {
			forgetTab(h.title)
			return nil
		}
		time.Sleep(50 * time.Millisecond)
	}
	forgetTab(h.title)
	return nil
}

func (h *tabHandle) Focus() (bool, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if focusByID(ctx, h.id) {
		return true, nil
	}
	return focusByTitle(ctx, h.title)
}

func (h *tabHandle) alive() bool {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if h.id != "" {
		return windowIDExists(ctx, h.id)
	}
	return titleExists(ctx, h.title)
}

func rememberTab(title, id string) {
	if title == "" || id == "" {
		return
	}
	tabIDs.Store(title, id)
}

func forgetTab(title string) {
	if title == "" {
		return
	}
	tabIDs.Delete(title)
}

func parseWindowID(s string) string {
	s = strings.TrimSpace(s)
	if _, err := strconv.Atoi(s); err != nil {
		return ""
	}
	return s
}

func parseSpawnOut(s string) (id, tty string) {
	s = strings.TrimSpace(s)
	id, tty, _ = strings.Cut(s, ",")
	id = parseWindowID(id)
	tty = strings.TrimSpace(tty)
	return id, tty
}

func runOSA(ctx context.Context, script string) (string, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if _, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, 3*time.Second)
		defer cancel()
	}
	out, err := exec.CommandContext(ctx, "osascript", "-e", script).Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

func windowIDExists(ctx context.Context, id string) bool {
	if parseWindowID(id) == "" {
		return false
	}
	script := fmt.Sprintf(`tell application "Terminal"
  try
    get id of (first window whose id is %s)
    return "1"
  on error
    return "0"
  end try
end tell`, id)
	out, err := runOSA(ctx, script)
	return err == nil && out == "1"
}

func titleExists(ctx context.Context, title string) bool {
	if strings.TrimSpace(title) == "" {
		return false
	}
	focused, err := focusProbe(ctx, title, false)
	return err == nil && focused
}

func focusByID(ctx context.Context, id string) bool {
	if parseWindowID(id) == "" {
		return false
	}
	script := fmt.Sprintf(`tell application "Terminal"
  try
    set w to first window whose id is %s
    set frontmost of w to true
    activate
    return "1"
  on error
    return "0"
  end try
end tell`, id)
	out, err := runOSA(ctx, script)
	return err == nil && out == "1"
}

func focusByTitle(ctx context.Context, title string) (bool, error) {
	return focusProbe(ctx, title, true)
}

func focusProbe(ctx context.Context, title string, activate bool) (bool, error) {
	tabHit := `return "1"`
	winHit := `return "1"`
	if activate {
		tabHit = `set selected of t to true
          set frontmost of w to true
          activate
          return "1"`
		winHit = `set frontmost of w to true
      activate
      return "1"`
	}
	script := fmt.Sprintf(`tell application "Terminal"
  repeat with w in windows
    repeat with t in tabs of w
      try
        if custom title of t is %s then
          %s
        end if
      end try
    end repeat
    if name of w contains %s then
      %s
    end if
  end repeat
  return "0"
end tell`, appleQuote(title), tabHit, appleQuote(title), winHit)
	out, err := runOSA(ctx, script)
	if err != nil {
		return false, err
	}
	return out == "1", nil
}

func closeTerminalWindow(ctx context.Context, id, title string) error {
	if parseWindowID(id) != "" {
		script := fmt.Sprintf(`tell application "Terminal"
  try
    close (first window whose id is %s) saving no
  end try
end tell`, id)
		_, err := runOSA(ctx, script)
		return err
	}
	if strings.TrimSpace(title) == "" {
		return nil
	}
	script := fmt.Sprintf(`tell application "Terminal"
  set toClose to {}
  repeat with w in windows
    try
      if name of w contains %s then
        set end of toClose to id of w
      else
        repeat with t in tabs of w
          try
            if custom title of t is %s then
              set end of toClose to id of w
              exit repeat
            end if
          end try
        end repeat
      end if
    end try
  end repeat
  repeat with i in toClose
    try
      close (first window whose id is i) saving no
    end try
  end repeat
end tell`, appleQuote(title), appleQuote(title))
	_, err := runOSA(ctx, script)
	return err
}

func appleQuote(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `"`, `\"`)
	return `"` + s + `"`
}
