package terminal

import (
	"strings"
	"testing"
)

func TestSessionTitle(t *testing.T) {
	if SessionTitle("abcdefghijklmnop") != "grok-sess-abcdefghijkl" {
		t.Fatal(SessionTitle("abcdefghijklmnop"))
	}
}

func TestRender(t *testing.T) {
	got := Render("cd {cwd} && {command}", "/bin/grok", "grok --resume abc", "/tmp/p", "abc")
	if got != "cd /tmp/p && grok --resume abc" {
		t.Fatal(got)
	}
	if Render("", "", "echo hi", "", "") != "echo hi" {
		t.Fatal("empty template")
	}
	cmd := ResumeCommand("/bin/grok", "abc-session")
	if strings.Contains(cmd, "fork-session") {
		t.Fatal(cmd)
	}
	if !strings.Contains(cmd, "--resume abc-session") {
		t.Fatal(cmd)
	}
}

func TestCommandInDirSkipsEmptyCwd(t *testing.T) {
	cmd := DashboardCommand("/bin/grok")
	if got := CommandInDir("", cmd); got != cmd {
		t.Fatal(got)
	}
	if got := CommandInDirWindows("", cmd); got != cmd {
		t.Fatal(got)
	}
	if got := CommandInDir("/tmp/p", cmd); got != "cd '/tmp/p' && "+cmd {
		t.Fatal(got)
	}
	if got := CommandInDirWindows(`C:\work`, cmd); got != `cd /d C:\work && `+cmd {
		t.Fatal(got)
	}
}
