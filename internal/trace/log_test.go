package trace

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestSnapshotCursorAndLimit(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 9, 18, 9, 10, 0, 0, time.UTC)
	l, err := Open(dir, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	l.SetDebug("j1", true, false)
	for i := 0; i < 5; i++ {
		l.Emit(Event{JobID: "j1", Level: LevelInfo, Source: SourceFIFO, Name: "queue.enqueued", Message: "item"})
	}
	snap := l.Snapshot("j1", 0, 2, nil, nil)
	if len(snap.Events) != 2 || snap.Events[0].Seq != 1 || snap.Cursor != 2 {
		t.Fatalf("%+v", snap)
	}
	snap = l.Snapshot("j1", snap.Cursor, 10, nil, nil)
	if len(snap.Events) != 3 || snap.Events[0].Seq != 3 || snap.Cursor != 5 {
		t.Fatalf("page %+v", snap)
	}
}

func TestDebugLevelDroppedWhenDisabled(t *testing.T) {
	l, err := Open(t.TempDir(), time.Now)
	if err != nil {
		t.Fatal(err)
	}
	l.Emit(Event{JobID: "j1", Level: LevelDebug, Source: SourceACP, Name: "acp.session_update"})
	l.Emit(Event{JobID: "j1", Level: LevelInfo, Source: SourceFIFO, Name: "pump.started"})
	snap := l.Snapshot("j1", 0, 20, nil, nil)
	if len(snap.Events) != 1 || snap.Events[0].Name != "pump.started" {
		t.Fatalf("%+v", snap)
	}
	l.SetDebug("j1", true, false)
	l.Emit(Event{JobID: "j1", Level: LevelDebug, Source: SourceACP, Name: "acp.session_update"})
	snap = l.Snapshot("j1", 0, 20, nil, nil)
	if len(snap.Events) != 2 {
		t.Fatalf("debug after enable %+v", snap)
	}
}

func TestPromptRedactedWithoutPayloads(t *testing.T) {
	l, err := Open(t.TempDir(), time.Now)
	if err != nil {
		t.Fatal(err)
	}
	secret := "Authorization: Bearer SECRETTOKEN cookie=abc prompt body"
	l.Emit(Event{
		JobID: "j1", Level: LevelInfo, Source: SourceACP, Name: "acp.prompt.started",
		Fields: PromptFields(secret, false),
	})
	snap := l.Snapshot("j1", 0, 10, nil, nil)
	raw, _ := json.Marshal(snap.Events[0])
	if strings.Contains(string(raw), "SECRETTOKEN") || strings.Contains(string(raw), "cookie=abc") {
		t.Fatalf("leaked: %s", raw)
	}
	if snap.Events[0].Fields["prompt_hash"] == nil || snap.Events[0].Fields["prompt"] != nil {
		t.Fatalf("fields %+v", snap.Events[0].Fields)
	}
	l.SetDebug("j1", true, true)
	l.Emit(Event{
		JobID: "j1", Level: LevelDebug, Source: SourceACP, Name: "acp.prompt.started",
		Fields: PromptFields(secret, true),
	})
	snap = l.Snapshot("j1", 1, 10, nil, nil)
	got, _ := snap.Events[0].Fields["prompt"].(string)
	if !strings.Contains(got, "<redacted>") || strings.Contains(got, "SECRETTOKEN") {
		t.Fatalf("payload %q", got)
	}
}

func TestRestartReadsTraceFile(t *testing.T) {
	dir := t.TempDir()
	l, err := Open(dir, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	l.Emit(Event{JobID: "j1", Level: LevelInfo, Source: SourceSupervisor, Name: "job.created"})
	l.Close()
	l2, err := Open(dir, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	snap := l2.Snapshot("j1", 0, 10, nil, nil)
	if len(snap.Events) != 1 || snap.Events[0].Name != "job.created" || snap.Cursor != 1 {
		t.Fatalf("%+v", snap)
	}
}

func TestWriteFailureDoesNotPanic(t *testing.T) {
	dir := t.TempDir()
	l, err := Open(dir, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	traces := filepath.Join(dir, "traces")
	if err := os.RemoveAll(traces); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(traces, []byte("notdir"), 0o600); err != nil {
		t.Fatal(err)
	}
	l.Emit(Event{JobID: "j1", Level: LevelInfo, Source: SourceFIFO, Name: "pump.started"})
	if l.WriteFails() == 0 {
		t.Fatal("expected write fail")
	}
}

func TestRotateDeletesOldFiles(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 9, 18, 0, 0, 0, 0, time.UTC)
	l, err := Open(dir, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	old := filepath.Join(dir, "traces", "old.jsonl")
	if err := os.WriteFile(old, []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	past := now.Add(-8 * 24 * time.Hour)
	if err := os.Chtimes(old, past, past); err != nil {
		t.Fatal(err)
	}
	l.rotate()
	if _, err := os.Stat(old); !os.IsNotExist(err) {
		t.Fatalf("old file kept: %v", err)
	}
}

func TestWaitUntilError(t *testing.T) {
	l, err := Open(t.TempDir(), time.Now)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	go func() {
		time.Sleep(20 * time.Millisecond)
		l.Emit(Event{JobID: "j1", Level: LevelInfo, Source: SourceFIFO, Name: "pump.started"})
		l.Emit(Event{JobID: "j1", Level: LevelError, Source: SourceACP, Name: "session.load.failed"})
	}()
	snap := l.Wait(ctx, "j1", 0, UntilError, time.Second)
	if len(snap.Events) == 0 || snap.Events[len(snap.Events)-1].Level != LevelError {
		t.Fatalf("%+v", snap)
	}
}
