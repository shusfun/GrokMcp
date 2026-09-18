package protocol

import "testing"

func TestParseTaskState(t *testing.T) {
	text := "done\n<GROK_TASK_STATE>\n{\"state\":\"completed\",\"summary\":\"ok\",\"next\":\"\"}\n</GROK_TASK_STATE>\n"
	got, ok := ParseTaskState(text)
	if !ok {
		t.Fatal("expected marker")
	}
	if got.State != MarkerCompleted || got.Summary != "ok" {
		t.Fatalf("got %+v", got)
	}
}

func TestParseTaskStateMissing(t *testing.T) {
	if _, ok := ParseTaskState("no marker here"); ok {
		t.Fatal("expected miss")
	}
}

func TestParseTaskStateUsesLastBlock(t *testing.T) {
	text := RenderTaskState(TaskState{State: MarkerWorking, Summary: "a"}) +
		"\n" + RenderTaskState(TaskState{State: MarkerBlocked, Summary: "b"})
	got, ok := ParseTaskState(text)
	if !ok || got.State != MarkerBlocked || got.Summary != "b" {
		t.Fatalf("got %+v ok=%v", got, ok)
	}
}

func TestStageViewLabel(t *testing.T) {
	got := StageViewLabel(StateExecuting, ViewHeadless)
	if got != "实施中 · 无头" {
		t.Fatalf("got %q", got)
	}
}
