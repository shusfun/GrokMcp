package runtime

import (
	"context"
	"grokmcp/internal/agent"
	"grokmcp/internal/protocol"
	"grokmcp/internal/terminal"
	"sync/atomic"
	"testing"
	"time"
)

type passiveAgent struct {
	*agent.Fake
	calls atomic.Int32
}

func (a *passiveAgent) EnsureLeader(ctx context.Context) error {
	a.calls.Add(1)
	<-ctx.Done()
	return ctx.Err()
}
func (a *passiveAgent) Diagnose(context.Context) protocol.DiagnoseResult {
	a.calls.Add(1)
	return protocol.DiagnoseResult{}
}

func TestIPCColdStartAndProbeDoNotLaunchAgent(t *testing.T) {
	a := &passiveAgent{Fake: agent.NewFake()}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	_, closeHost, err := Open(ctx, Options{Home: testHome(t), Agent: a, Term: terminal.NewFake()})
	if err != nil {
		t.Fatal(err)
	}
	defer closeHost()
	for i := 0; i < 10; i++ {
		if !Probe(ctx) {
			t.Fatal("passive IPC not ready")
		}
	}
	if a.calls.Load() != 0 {
		t.Fatalf("cold start/probe invoked agent %d times", a.calls.Load())
	}
}

