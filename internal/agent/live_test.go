package agent

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"grokmcp/internal/grokbin"
	"grokmcp/internal/terminal"
)

func TestLiveNewSessionNoFork(t *testing.T) {
	if os.Getenv("GROK_LIVE") != "1" {
		t.Skip("set GROK_LIVE=1")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	finder := grokbin.New()
	d := finder.Diagnose(ctx, "")
	if !d.LoggedIn || !d.Compatible {
		t.Skip(d.Error)
	}
	g := NewGrok(d.GrokPath, finder)
	defer g.Close()
	cwd := t.TempDir()
	id, _, err := g.NewSession(ctx, cwd, false)
	if err != nil {
		t.Fatal(err)
	}
	if id == "" {
		t.Fatal("empty session id")
	}
	cmd := terminal.ResumeCommand(d.GrokPath, id)
	if strings.Contains(cmd, "fork-session") {
		t.Fatal(cmd)
	}
	home, _ := os.UserHomeDir()
	root := filepath.Join(home, ".grok", "sessions")
	found := false
	_ = filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if entry.IsDir() && entry.Name() == id {
			found = true
		}
		return nil
	})
	if !found {
		t.Logf("session id %s not yet on disk (leader may persist later)", id)
	}
}

func TestLiveDualLoad(t *testing.T) {
	if os.Getenv("GROK_LIVE") != "1" {
		t.Skip("set GROK_LIVE=1")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	finder := grokbin.New()
	d := finder.Diagnose(ctx, "")
	if !d.LoggedIn || !d.Compatible {
		t.Skip(d.Error)
	}
	g := NewGrok(d.GrokPath, finder)
	defer g.Close()
	if err := g.EnsureLeader(ctx); err != nil {
		t.Fatal(err)
	}
	mode := g.AttachMode()
	t.Logf("attach_mode=%s", mode)
	if mode != "live" && mode != "boundary" {
		t.Fatalf("unexpected mode %s", mode)
	}
}
