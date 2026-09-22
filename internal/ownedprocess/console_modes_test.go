package ownedprocess

import (
	"strings"
	"testing"
)

func TestConsoleModesSurviveChunkBoundariesAndDiscardDrawing(t *testing.T) {
	c := &Console{modes: map[int]bool{}}
	for _, part := range []string{"old screen\x1b[?20", "04h\x1b[?1004;9001h", "\x1b[2J\x1b[15;30Hsecret text\x1b[?1004l"} {
		c.recordModes([]byte(part))
	}
	got := string(c.Modes())
	for _, want := range []string{"\x1b[?2004h", "\x1b[?9001h", "\x1b[?1004l"} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q in %q", want, got)
		}
	}
	if strings.Contains(got, "secret") || strings.Contains(got, "15;30") || strings.Contains(got, "2J") {
		t.Fatalf("replayed drawing: %q", got)
	}
	c.recordModes([]byte(strings.Repeat("x", consoleHistoryLimit+1)))
	if !strings.Contains(string(c.Modes()), "\x1b[?2004h") {
		t.Fatal("mode lost with old scrollback")
	}
}
