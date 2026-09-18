package notify

import (
	"testing"

	"grokmcp/internal/protocol"
)

func TestMessage(t *testing.T) {
	if _, ok := Message(protocol.StateNeedsInput, protocol.StateNeedsInput, "a"); ok {
		t.Fatal("duplicate")
	}
	got, ok := Message(protocol.StateExecuting, protocol.StateNeedsInput, "登录")
	if !ok || got != "登录 需要输入" {
		t.Fatalf("%q %v", got, ok)
	}
}
