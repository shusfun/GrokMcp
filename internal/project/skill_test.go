package project

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSkillInstallUpdateRemoveIdempotent(t *testing.T) {
	root := t.TempDir()
	if st := Probe(root); st.SkillStatus != "missing" {
		t.Fatalf("%+v", st)
	}
	if err := Install(root); err != nil {
		t.Fatal(err)
	}
	if err := Install(root); err != nil {
		t.Fatal(err)
	}
	st := Probe(root)
	if st.SkillStatus != "installed" || st.SkillVersion != SkillVersion {
		t.Fatalf("%+v", st)
	}
	body, err := os.ReadFile(SkillPath(root))
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := ParseManaged(string(body)); !ok {
		t.Fatal("missing marker")
	}
	if err := Update(root); err != nil {
		t.Fatal(err)
	}
	if err := Remove(root); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(SkillPath(root)); !os.IsNotExist(err) {
		t.Fatalf("skill still present: %v", err)
	}
	if err := Remove(root); err != nil {
		t.Fatal(err)
	}
}

func TestSkillConflictDoesNotOverwrite(t *testing.T) {
	root := t.TempDir()
	p := SkillPath(root)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	user := []byte("# my skill\n")
	if err := os.WriteFile(p, user, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := Install(root); err != ErrSkillConflict {
		t.Fatalf("err %v", err)
	}
	got, _ := os.ReadFile(p)
	if string(got) != string(user) {
		t.Fatalf("overwrote user file: %q", got)
	}
	st := Probe(root)
	if st.SkillStatus != "conflict" || st.SkillMessage == "" {
		t.Fatalf("%+v", st)
	}
	if err := Remove(root); err != ErrSkillNotManaged {
		t.Fatalf("remove %v", err)
	}
	got, _ = os.ReadFile(p)
	if string(got) != string(user) {
		t.Fatal("remove deleted user file")
	}
}

func TestSkillDoesNotTouchAgentsMD(t *testing.T) {
	root := t.TempDir()
	agents := filepath.Join(root, "AGENTS.md")
	orig := []byte("# keep me\nuser rules\n")
	if err := os.WriteFile(agents, orig, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := Install(root); err != nil {
		t.Fatal(err)
	}
	if err := Update(root); err != nil {
		t.Fatal(err)
	}
	if err := Remove(root); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(agents)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(orig) {
		t.Fatalf("AGENTS.md changed: %q", got)
	}
}

func TestSkillContentHasNoModelOrForcedDelegate(t *testing.T) {
	body := ManagedBody()
	for _, bad := range []string{"model config", "强制委派", "必须把工作交给"} {
		if containsFold(body, bad) {
			t.Fatalf("skill contains %q", bad)
		}
	}
	for _, need := range []string{"grok_dispatch", "grok_wait", "plan", "session"} {
		if !containsFold(body, need) {
			t.Fatalf("skill missing %q", need)
		}
	}
}

func containsFold(s, sub string) bool {
	return strings.Contains(strings.ToLower(s), strings.ToLower(sub))
}
