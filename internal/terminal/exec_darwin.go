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

func (e Exec) spawn(_ context.Context, cwd, command, sessionID string) (Handle, error) {
	if strings.TrimSpace(e.Template) != "" {
		line := Render(e.Template, "grok", command, cwd, sessionID)
		cmd := exec.Command("sh", "-c", line)
		if err := cmd.Start(); err != nil {
			return nil, err
		}
		return procHandle{cmd: cmd}, nil
	}
	title := SessionTitle(sessionID)
	inner := fmt.Sprintf(`cd %s && %s`, shSingle(cwd), command)
	script := fmt.Sprintf(`tell application "Terminal"
  set t to do script %s
  try
    set custom title of t to %s
  end try
  activate
  return id of front window as text
end tell`, appleQuote(inner), appleQuote(title))
	out, err := runOSA(script)
	if err != nil {
		return nil, err
	}
	id := parseWindowID(out)
	rememberTab(title, id)
	return tabHandle{id: id, title: title}, nil
}

func (e Exec) focus(_ context.Context, title string) (bool, error) {
	if v, ok := tabIDs.Load(title); ok {
		if id, _ := v.(string); focusByID(id) {
			return true, nil
		}
	}
	return focusByTitle(title)
}

type tabHandle struct {
	id    string
	title string
}

func (h tabHandle) Wait() error {
	for {
		if !h.alive() {
			forgetTab(h.title)
			return nil
		}
		time.Sleep(200 * time.Millisecond)
	}
}

func (h tabHandle) PID() int { return 0 }

func (h tabHandle) Close() error {
	_ = closeTerminalWindow(h.id, h.title)
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if !h.alive() {
			forgetTab(h.title)
			return nil
		}
		time.Sleep(50 * time.Millisecond)
	}
	forgetTab(h.title)
	return nil
}

func (h tabHandle) Focus() (bool, error) {
	if focusByID(h.id) {
		return true, nil
	}
	return focusByTitle(h.title)
}

func (h tabHandle) alive() bool {
	if h.id != "" {
		return windowIDExists(h.id)
	}
	return titleExists(h.title)
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

func runOSA(script string) (string, error) {
	out, err := exec.Command("osascript", "-e", script).Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

func windowIDExists(id string) bool {
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
	out, err := runOSA(script)
	return err == nil && out == "1"
}

func titleExists(title string) bool {
	if strings.TrimSpace(title) == "" {
		return false
	}
	focused, err := focusProbe(title, false)
	return err == nil && focused
}

func focusByID(id string) bool {
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
	out, err := runOSA(script)
	return err == nil && out == "1"
}

func focusByTitle(title string) (bool, error) {
	return focusProbe(title, true)
}

func focusProbe(title string, activate bool) (bool, error) {
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
	out, err := runOSA(script)
	if err != nil {
		return false, err
	}
	return out == "1", nil
}

func closeTerminalWindow(id, title string) error {
	if parseWindowID(id) != "" {
		script := fmt.Sprintf(`tell application "Terminal"
  try
    close (first window whose id is %s) saving no
  end try
end tell`, id)
		_, err := runOSA(script)
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
	_, err := runOSA(script)
	return err
}

func appleQuote(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `"`, `\"`)
	return `"` + s + `"`
}

func shSingle(s string) string {
	return `'` + strings.ReplaceAll(s, `'`, `'"'"'`) + `'`
}
