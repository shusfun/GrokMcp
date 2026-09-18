package textutil

import (
	"testing"
	"time"
)

func TestTruncateTitle(t *testing.T) {
	if TruncateTitle("  hello\nworld  ", 20) != "hello world" {
		t.Fatal(TruncateTitle("  hello\nworld  ", 20))
	}
	got := TruncateTitle("abcdefghij", 4)
	if got != "abcd…" {
		t.Fatalf("got %q", got)
	}
	if TruncateTitle("", 8) != "未命名任务" {
		t.Fatal("empty")
	}
}

func TestProjectName(t *testing.T) {
	if ProjectName("/tmp/suiyuan") != "suiyuan" {
		t.Fatal(ProjectName("/tmp/suiyuan"))
	}
}

func TestElapsedAndDigest(t *testing.T) {
	created := time.Date(2026, 9, 17, 12, 0, 0, 0, time.UTC)
	now := created.Add(18 * time.Minute)
	if ElapsedSeconds(created, now) != 18*60 {
		t.Fatal(ElapsedSeconds(created, now))
	}
	if FormatElapsed(18*60) != "18m" {
		t.Fatal(FormatElapsed(18 * 60))
	}
	if Digest("plan") == Digest("other") {
		t.Fatal("digest collision")
	}
}
