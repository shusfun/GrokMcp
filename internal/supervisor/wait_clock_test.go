package supervisor

import (
	"context"
	"grokmcp/internal/agent"
	"grokmcp/internal/protocol"
	"grokmcp/internal/terminal"
	"testing"
	"testing/synctest"
	"time"
)

func TestDefaultWaitReportsAfterFiveMinutes(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		fake := agent.NewFake()
		s := newTest(t, fake, terminal.NewFake())
		seedWaitJob(t, s, "clock", protocol.StateExecuting)
		start := time.Now()
		out, err := s.Wait(context.Background(), protocol.WaitRequest{JobIDs: []string{"clock"}})
		if err != nil || out.Reason != "report_due" || time.Since(start) != 5*time.Minute || fake.PromptCount() != 0 {
			t.Fatalf("default wait %+v err=%v elapsed=%s calls=%d", out, err, time.Since(start), fake.PromptCount())
		}
	})
}
