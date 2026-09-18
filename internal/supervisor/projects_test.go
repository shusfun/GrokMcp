package supervisor

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"grokmcp/internal/agent"
	"grokmcp/internal/project"
	"grokmcp/internal/protocol"
	"grokmcp/internal/terminal"
)

func TestImportInstallsSkillByDefault(t *testing.T) {
	s := newTest(t, agent.NewFake(), terminal.NewFake())
	root := t.TempDir()
	p, err := s.ImportProject(context.Background(), root, true)
	if err != nil {
		t.Fatal(err)
	}
	if !p.Imported || p.SkillStatus != protocol.SkillInstalled {
		t.Fatalf("%+v", p)
	}
	if _, err := os.Stat(project.SkillPath(root)); err != nil {
		t.Fatal(err)
	}
}

func TestImportSkipSkill(t *testing.T) {
	s := newTest(t, agent.NewFake(), terminal.NewFake())
	root := t.TempDir()
	p, err := s.ImportProject(context.Background(), root, false)
	if err != nil {
		t.Fatal(err)
	}
	if !p.Imported || p.SkillStatus != protocol.SkillMissing {
		t.Fatalf("%+v", p)
	}
	if _, err := os.Stat(project.SkillPath(root)); !os.IsNotExist(err) {
		t.Fatalf("skill written: %v", err)
	}
}

func TestImportSkillConflictStillRegisters(t *testing.T) {
	s := newTest(t, agent.NewFake(), terminal.NewFake())
	root := t.TempDir()
	p := project.SkillPath(root)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	user := []byte("user skill\n")
	if err := os.WriteFile(p, user, 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := s.ImportProject(context.Background(), root, true)
	if err != nil {
		t.Fatal(err)
	}
	if !got.Imported || got.SkillStatus != protocol.SkillConflict || got.SkillMessage == "" {
		t.Fatalf("%+v", got)
	}
	body, _ := os.ReadFile(p)
	if string(body) != string(user) {
		t.Fatal("overwrote user skill")
	}
}

func TestRemoveDemotesKeepsIDJobsAndSkill(t *testing.T) {
	s := newTest(t, agent.NewFake(), terminal.NewFake())
	root := t.TempDir()
	agents := filepath.Join(root, "AGENTS.md")
	orig := []byte("do not touch\n")
	if err := os.WriteFile(agents, orig, 0o644); err != nil {
		t.Fatal(err)
	}
	p, err := s.ImportProject(context.Background(), root, true)
	if err != nil {
		t.Fatal(err)
	}
	res, err := s.Dispatch(context.Background(), protocol.DispatchRequest{
		ProjectID: p.ProjectID,
		Tasks:     []protocol.DispatchTask{{Prompt: "x"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Jobs[0].ProjectID != p.ProjectID {
		t.Fatalf("%+v", res.Jobs[0])
	}
	got, err := s.RemoveProject(context.Background(), p.ProjectID)
	if err != nil || got.Imported || got.ProjectID != p.ProjectID {
		t.Fatalf("%+v %v", got, err)
	}
	job, err := s.Status(context.Background(), res.Jobs[0].JobID)
	if err != nil || job.ProjectID != p.ProjectID {
		t.Fatalf("%+v %v", job, err)
	}
	if _, err := os.Stat(project.SkillPath(root)); err != nil {
		t.Fatal("skill removed with project")
	}
	if _, err := os.Stat(root); err != nil {
		t.Fatal("source deleted")
	}
	body, _ := os.ReadFile(agents)
	if string(body) != string(orig) {
		t.Fatal("AGENTS.md changed")
	}
	again, err := s.RemoveProject(context.Background(), p.ProjectID)
	if err != nil || again.ProjectID != p.ProjectID || again.Imported {
		t.Fatalf("idempotent %+v %v", again, err)
	}
}

func TestRemoveKeepsEmptyDiscovered(t *testing.T) {
	s := newTest(t, agent.NewFake(), terminal.NewFake())
	root := t.TempDir()
	p, err := s.ImportProject(context.Background(), root, false)
	if err != nil {
		t.Fatal(err)
	}
	got, err := s.RemoveProject(context.Background(), p.ProjectID)
	if err != nil || got.Imported {
		t.Fatalf("%+v %v", got, err)
	}
	listed, err := s.ListProjects(context.Background())
	if err != nil || len(listed) != 1 || listed[0].ProjectID != p.ProjectID {
		t.Fatalf("%+v %v", listed, err)
	}
}

func TestDispatchDiscoversWithoutSkill(t *testing.T) {
	s := newTest(t, agent.NewFake(), terminal.NewFake())
	root := t.TempDir()
	res, err := s.Dispatch(context.Background(), protocol.DispatchRequest{
		Cwd:   root,
		Tasks: []protocol.DispatchTask{{Prompt: "x"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Jobs[0].ProjectID == "" {
		t.Fatal("missing project_id")
	}
	p, err := s.GetProject(context.Background(), res.Jobs[0].ProjectID)
	if err != nil || p.Imported || p.SkillStatus != protocol.SkillMissing {
		t.Fatalf("%+v %v", p, err)
	}
	if _, err := os.Stat(project.SkillPath(root)); !os.IsNotExist(err) {
		t.Fatal("dispatch installed skill")
	}
}

func TestImportSkillWriteErrorStillImportedSameID(t *testing.T) {
	s := newTest(t, agent.NewFake(), terminal.NewFake())
	root := t.TempDir()
	block := project.SkillDir(root)
	if err := os.MkdirAll(filepath.Dir(block), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(block, []byte("not-a-dir"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := s.ImportProject(context.Background(), root, true)
	if err != nil {
		t.Fatalf("import should succeed: %v", err)
	}
	if !got.Imported || got.SkillStatus != protocol.SkillError || got.SkillMessage == "" {
		t.Fatalf("%+v", got)
	}
	if _, err := os.Stat(project.SkillPath(root)); err == nil {
		t.Fatal("wrote skill over blocked path")
	}
	again, err := s.ImportProject(context.Background(), root, true)
	if err != nil {
		t.Fatal(err)
	}
	if again.ProjectID != got.ProjectID {
		t.Fatalf("project_id %q vs %q", again.ProjectID, got.ProjectID)
	}
	if !again.Imported || again.SkillStatus != protocol.SkillError {
		t.Fatalf("retry %+v", again)
	}
}

func TestPromptGenerateDoesNotDispatch(t *testing.T) {
	fake := agent.NewFake()
	s := newTest(t, fake, terminal.NewFake())
	root := t.TempDir()
	p, _ := s.ImportProject(context.Background(), root, false)
	out, err := s.GeneratePrompt(context.Background(), protocol.GeneratePromptRequest{
		ProjectID: p.ProjectID, Goal: "做完", Constraints: "无", Acceptance: "过测试",
	})
	if err != nil {
		t.Fatal(err)
	}
	if out.Text == "" || out.Builtin == "" {
		t.Fatal(out)
	}
	if len(fake.NewCalls) != 0 {
		t.Fatalf("dispatched %v", fake.NewCalls)
	}
	saved, err := s.SavePrompt(context.Background(), protocol.SavePromptRequest{ProjectID: p.ProjectID, Template: "saved {goal}"})
	if err != nil || saved.PromptTemplate != "saved {goal}" {
		t.Fatalf("%+v %v", saved, err)
	}
	again, err := s.GeneratePrompt(context.Background(), protocol.GeneratePromptRequest{
		ProjectID: p.ProjectID, Goal: "二次", Constraints: "仍约束", Acceptance: "再验收",
	})
	if err != nil {
		t.Fatal(err)
	}
	if again.Template != "saved {goal}" {
		t.Fatalf("raw template lost: %q", again.Template)
	}
	if !strings.Contains(again.Text, "二次") || strings.Contains(again.Text, "{goal}") {
		t.Fatalf("draft %q", again.Text)
	}
}

func TestSkillInstallConflictError(t *testing.T) {
	s := newTest(t, agent.NewFake(), terminal.NewFake())
	root := t.TempDir()
	p, _ := s.ImportProject(context.Background(), root, false)
	path := project.SkillPath(root)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("user"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := s.SkillInstall(context.Background(), p.ProjectID)
	if !errors.Is(err, project.ErrSkillConflict) {
		t.Fatalf("%v", err)
	}
	if got.SkillStatus != protocol.SkillConflict {
		t.Fatalf("%+v", got)
	}
}
