package terminal

import (
	"context"
	"testing"
	"time"
)

func TestWaitUnblocksOnClose(t *testing.T) {
	f := NewFake()
	h, err := f.OpenResume(context.Background(), "grok", "sess-1", `C:\Work\GrokMcp`)
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- h.Wait() }()
	if err := h.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("Wait hung after Close")
	}
}

func TestWaitUnblocksWhenResumeReplaced(t *testing.T) {
	f := NewFake()
	h1, err := f.OpenResume(context.Background(), "grok", "sess-1", `C:\Work\GrokMcp`)
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- h1.Wait() }()
	h2, err := f.OpenResume(context.Background(), "grok", "sess-1", `C:\Work\GrokMcp`)
	if err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("prior Wait hung after OpenResume replaced the handle")
	}
	if err := h2.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestCloseAllUnblocksWait(t *testing.T) {
	f := NewFake()
	h, err := f.OpenResume(context.Background(), "grok", "sess-1", `C:\Work\GrokMcp`)
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- h.Wait() }()
	f.CloseAll()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Wait hung after CloseAll")
	}
}
