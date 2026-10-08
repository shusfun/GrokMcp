package foreground

import "testing"

func TestForegroundYieldPolicy(t *testing.T) {
	if !shouldYieldOwnForeground(false) {
		t.Fatal("background startup must still yield its own foreground")
	}
	if shouldYieldOwnForeground(true) {
		t.Fatal("user show must stop yielding")
	}
	if !shouldHideOwnForeground(false, false) {
		t.Fatal("background with no other window may hide its own foreground")
	}
	if shouldHideOwnForeground(false, true) {
		t.Fatal("a found target must be used instead of hiding")
	}
	if shouldHideOwnForeground(true, false) || shouldHideOwnForeground(true, true) {
		t.Fatal("user show must not hide the window")
	}
}
