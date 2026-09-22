//go:build windows

package mcp

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"grokmcp/internal/grokbin"
	"grokmcp/internal/ownedprocess"
	"grokmcp/internal/protocol"
	appruntime "grokmcp/internal/runtime"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// 只有用户确认后由专用脚本显式开启，普通 go test 从不启动真实 Grok。
func TestLiveMCPCompleteChain(t *testing.T) {
	if os.Getenv("GROK_LIVE") != "1" || os.Getenv("GROK_ACCEPTANCE_APPROVED") != "1" {
		t.Skip("requires explicit controlled Grok acceptance approval")
	}
	if os.Getenv("GROK_ACCEPTANCE_ROOT") == "" || os.Getenv("GROK_ACCEPTANCE_REPO") == "" {
		t.Fatal("explicit isolated acceptance paths are required")
	}
	root, err := filepath.Abs(os.Getenv("GROK_ACCEPTANCE_ROOT"))
	if err != nil {
		t.Fatal(err)
	}
	repo, err := filepath.EvalSymlinks(os.Getenv("GROK_ACCEPTANCE_REPO"))
	if err != nil {
		t.Fatal(err)
	}
	rel, err := filepath.Rel(repo, root)
	if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		t.Fatal("acceptance root must be inside this repository")
	}
	if err := os.MkdirAll(root, 0700); err != nil {
		t.Fatal(err)
	}
	resolved, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}
	rel, err = filepath.Rel(repo, resolved)
	if err != nil || strings.HasPrefix(rel, "..") {
		t.Fatal("acceptance root resolves outside repository")
	}
	home, work := filepath.Join(root, "home"), filepath.Join(root, "work")
	for _, dir := range []string{home, work, filepath.Join(root, "decisions"), filepath.Join(root, "plans")} {
		if err := os.MkdirAll(dir, 0700); err != nil {
			t.Fatal(err)
		}
		canonical, err := filepath.EvalSymlinks(dir)
		if err != nil {
			t.Fatal(err)
		}
		relative, err := filepath.Rel(resolved, canonical)
		if err != nil || strings.HasPrefix(relative, "..") {
			t.Fatal("acceptance subdirectory escapes its isolated root")
		}
	}
	t.Setenv("GROK_SUPERVISOR_HOME", home)
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Minute)
	defer cancel()
	watcher, err := startConsoleWatch()
	if err != nil {
		t.Fatal(err)
	}
	report := map[string]any{"started_at": time.Now().UTC(), "real_acceptance": "running"}
	attemptReport := filepath.Join(root, "report-"+time.Now().UTC().Format("20060102T150405.000000000")+".json")
	defer func() {
		visible := watcher.stop()
		report["visible_terminals"] = visible
		if len(visible) > 0 {
			t.Errorf("new visible terminal windows: %v", visible)
		}
		report["finished_at"] = time.Now().UTC()
		if t.Failed() {
			report["real_acceptance"] = "failed"
		}
		writeLiveJSON(t, filepath.Join(root, "report.json"), report)
		writeLiveJSON(t, attemptReport, report)
	}()

	binary, err := grokbin.New().Resolve("")
	if err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(binary)
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(body)
	report["binary"] = binary
	report["binary_sha256"] = hex.EncodeToString(digest[:])

	cliCtx, cliCancel := context.WithTimeout(ctx, 5*time.Minute)
	output, err := runOwnedConsole(cliCtx, binary, []string{"--no-auto-update", "--cwd", work, "-p", "Only reply GROK_OK. Do not run tools or modify files."}, work)
	cliCancel()
	if err != nil || !strings.Contains(output, "GROK_OK") {
		t.Fatalf("direct CLI baseline failed: %v", err)
	}
	report["cli_baseline"] = "GROK_OK"
	host, cleanup, err := appruntime.Open(ctx, appruntime.Options{Home: home, GrokPath: binary})
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	ipcBackend, disconnect, err := appruntime.Connect(ctx, appruntime.ConnectOptions{Home: home, DisableStart: true})
	if err != nil {
		t.Fatal(err)
	}
	defer disconnect()
	mcpClient := connectMCP(t, ipcBackend)
	jobs, err := host.ListJobs(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var id string
	if len(jobs) == 0 {
		res := liveCall[protocol.DispatchResult](t, ctx, mcpClient, "grok_dispatch", protocol.DispatchRequest{Cwd: work, Tasks: []protocol.DispatchTask{{Title: "隔离协议验收", Prompt: "Only reply GROK_OK for this connectivity check. Do not modify files or run tools."}}})
		id = res.Jobs[0].JobID
	} else if len(jobs) == 1 {
		id = jobs[0].JobID
		if jobs[0].State != protocol.StateCompleted && jobs[0].PauseReason != "review_required" {
			if _, err := host.Continue(ctx, id); err != nil {
				t.Fatalf("resume the preserved session: %v", err)
			}
		}
	} else {
		t.Fatal("ambiguous acceptance jobs; preserve them for inspection")
	}
	report["job_id"] = id
	writeLiveJSON(t, filepath.Join(root, "report.json"), report)
	initial := liveBoundary(t, ctx, mcpClient, id, nil, root)
	report["session_id"] = initial.Jobs[0].GrokSessionID
	if len(jobs) == 0 {
		result := liveCall[protocol.Job](t, ctx, mcpClient, "grok_status", struct {
			JobID string `json:"job_id"`
			protocol.ResultQuery
		}{id, protocol.ResultQuery{IncludeResult: true, RequestID: initial.Jobs[0].RequestID}})
		if result.Result == nil || !strings.Contains(result.Result.Text, "GROK_OK") {
			t.Fatal("MCP connectivity result differs from the direct CLI baseline")
		}
	}
	if initial.Jobs[0].Busy || initial.Jobs[0].QueueLength != 0 {
		t.Fatal("initial turn not settled")
	}
	cursor := initial.Cursors
	prompts := []string{
		"In this isolated test directory only, create calc.py with add(a,b) returning a+b and test_calc.py using Python unittest for positive and negative values. Run python -m unittest -v. First submit a native plan for approval; after approval implement and verify. Return the concrete test results. Do not access other projects or any servers.",
		"In this same session, extend calc.py with multiply(a,b), add zero and negative unittest cases to test_calc.py, and run python -m unittest -v. First submit the revised native plan for approval. Change only these two files and return the actual verification results.",
	}
	for round, prompt := range prompts {
		accepted := liveCall[protocol.Job](t, ctx, mcpClient, "grok_followup", protocol.FollowupRequest{JobID: id, Prompt: prompt, Replan: true})
		settled := liveBoundary(t, ctx, mcpClient, id, cursor, root)
		j := settled.Jobs[0]
		if j.GrokSessionID != report["session_id"] || j.RequestID != accepted.AcceptedRequestID || j.Busy || j.QueueLength != 0 || j.ActiveTurnID != "" {
			t.Fatalf("session/request not settled: %+v", j)
		}
		answer := liveCall[protocol.Job](t, ctx, mcpClient, "grok_status", struct {
			JobID string `json:"job_id"`
			protocol.ResultQuery
		}{id, protocol.ResultQuery{IncludeResult: true, RequestID: j.RequestID}})
		if answer.Result == nil || answer.Result.Text == "" {
			t.Fatal("final answer missing")
		}
		python, err := exec.LookPath("python")
		if err != nil {
			t.Fatal(err)
		}
		verify, err := runOwnedConsole(ctx, python, []string{"-m", "unittest", "-v"}, work)
		if err != nil || !strings.Contains(verify, "OK") {
			t.Fatalf("independent unittest failed: %v", err)
		}
		if _, err := os.Stat(filepath.Join(work, "calc.py")); err != nil {
			t.Fatal(err)
		}
		if _, err := os.Stat(filepath.Join(work, "test_calc.py")); err != nil {
			t.Fatal(err)
		}
		assertion := "from calc import add; assert add(2,3)==5; assert add(-2,3)==1"
		if round == 1 {
			assertion += "; from calc import multiply; assert multiply(0,9)==0; assert multiply(-2,3)==-6"
		}
		if _, err := runOwnedConsole(ctx, python, []string{"-c", assertion}, work); err != nil {
			t.Fatalf("independent functional assertions failed: %v", err)
		}
		report[fmt.Sprintf("round_%d", round+1)] = map[string]any{"request_id": j.RequestID, "turn_id": j.ResultTurnID, "state": j.State, "unittest": "passed"}
		cursor = settled.Cursors
		writeLiveJSON(t, filepath.Join(root, "report.json"), report)
	}
	report["real_acceptance"] = "passed"
}
func liveCall[T any](t *testing.T, ctx context.Context, c *sdk.ClientSession, name string, args any) T {
	t.Helper()
	res, err := c.CallTool(ctx, &sdk.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		t.Fatal(name, err)
	}
	if res.IsError {
		t.Fatalf("%s returned a protocol error; inspect the isolated task status", name)
	}
	raw, err := json.Marshal(res.StructuredContent)
	if err != nil {
		t.Fatal(err)
	}
	var out T
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatal(err)
	}
	return out
}
func liveBoundary(t *testing.T, ctx context.Context, c *sdk.ClientSession, id string, cursors map[string]int64, root string) protocol.WaitResult {
	t.Helper()
	for {
		out := liveCall[protocol.WaitResult](t, ctx, c, "grok_wait", protocol.WaitRequest{JobIDs: []string{id}, Cursors: cursors})
		cursors = out.Cursors
		j := out.Jobs[0]
		if out.Reason == "report_due" {
			t.Logf("五分钟简报 request=%s state=%s queue=%d reason=%s last_activity=%s", j.RequestID, j.State, j.QueueLength, j.WaitReason, j.LastActivityAt)
			continue
		}
		if j.State == protocol.StatePlanReady {
			if j.ApprovalID == "" || j.PlanSummary == "" {
				t.Fatal("native approval or verified plan content missing")
			}
			writeLiveJSON(t, filepath.Join(root, "plans", j.ApprovalID+".json"), j)
			writeLiveJSON(t, filepath.Join(root, "pending-plan.json"), j)
			t.Logf("原生方案待 Codex 审核：%s；决定文件 decisions/%s.json", filepath.Join(root, "pending-plan.json"), j.ApprovalID)
			decisionPath := filepath.Join(root, "decisions", j.ApprovalID+".json")
			ticker := time.NewTicker(250 * time.Millisecond)
			var decision protocol.PlanDecideRequest
			for {
				b, err := os.ReadFile(decisionPath)
				if err == nil {
					if err := json.Unmarshal(b, &decision); err != nil {
						ticker.Stop()
						t.Fatal(err)
					}
					break
				}
				if !os.IsNotExist(err) {
					ticker.Stop()
					t.Fatal(err)
				}
				select {
				case <-ctx.Done():
					ticker.Stop()
					t.Fatal("approval review deadline reached; original session preserved")
				case <-ticker.C:
				}
			}
			ticker.Stop()
			if decision.JobID != j.JobID || decision.ApprovalID != j.ApprovalID || decision.RequestID != j.RequestID || decision.TurnID != j.PlanTurnID || decision.PlanVersion != j.PlanVersion {
				t.Fatal("review decision does not match the pending native approval")
			}
			liveCall[protocol.Job](t, ctx, c, "grok_plan_decide", decision)
			continue
		}
		if j.State == protocol.StateCompleted || (j.State == protocol.StateNeedsInput && j.PauseReason == "review_required") {
			return out
		}
		writeLiveJSON(t, filepath.Join(root, "blocked-"+j.RequestID+"-"+j.ActiveTurnID+".json"), j)
		t.Fatalf("acceptance blocked: state=%s reason=%s; original session preserved", j.State, j.WaitReason)
	}
}
func writeLiveJSON(t *testing.T, path string, v any) {
	t.Helper()
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		t.Error(err)
		return
	}
	if err := os.WriteFile(path, b, 0600); err != nil {
		t.Error(err)
	}
}
func runOwnedConsole(ctx context.Context, path string, args []string, cwd string) (string, error) {
	group, err := ownedprocess.New()
	if err != nil {
		return "", err
	}
	defer group.Close()
	console, err := ownedprocess.NewConsole(160, 40)
	if err != nil {
		return "", err
	}
	defer console.Close()
	initial, ch, off := console.Subscribe()
	defer off()
	done := make(chan string, 1)
	go func() {
		var out strings.Builder
		out.Write(initial)
		for b := range ch {
			if out.Len() < 2*1024*1024 {
				out.Write(b)
			}
		}
		done <- out.String()
	}()
	process, err := group.Start(ownedprocess.Spec{Path: path, Args: args, Dir: cwd, Console: console})
	if err != nil {
		console.Close()
		return <-done, err
	}
	select {
	case <-ctx.Done():
		group.Close()
		err = ctx.Err()
	case <-process.Done():
		err = process.Wait()
	}
	group.Close()
	console.Close()
	return <-done, err
}
