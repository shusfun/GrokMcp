package protocol

import (
	"fmt"
	"strings"
)

func TaskContract(cwd, title, userPrompt string) string {
	var b strings.Builder
	b.WriteString("You are Grok executing a supervised job for Codex.\n")
	b.WriteString("Stay in this persistent session. Do not create a replacement session.\n")
	if cwd != "" {
		fmt.Fprintf(&b, "Working directory: %s\n", cwd)
	}
	if title != "" {
		fmt.Fprintf(&b, "Job title: %s\n", title)
	}
	b.WriteString("\nProtocol:\n")
	b.WriteString("1. Start in plan mode. Investigate the repo, write plan.md in this session directory, then call exit_plan_mode.\n")
	b.WriteString("2. Wait for Codex approval before implementing. After approval, implement, test, and fix in this same session.\n")
	b.WriteString("3. Return the actual outcome and verification evidence. You may append this status block:\n")
	b.WriteString(TaskStateOpen + "\n")
	b.WriteString(`{"state":"working|needs_input|completed|blocked","summary":"...","next":"..."}` + "\n")
	b.WriteString(TaskStateClose + "\n")
	b.WriteString("4. state=working means continue automatically; needs_input pauses for Codex; completed is final for this job; blocked is a concrete blocker.\n")
	b.WriteString("5. plan_ready is a native ACP approval event, never a text state. Missing status blocks are reviewed from your final answer; do not emit format-only repair turns. Do not stream process logs to Codex. Keep summaries short.\n\n")
	b.WriteString("User task:\n")
	b.WriteString(strings.TrimSpace(userPrompt))
	b.WriteString("\n")
	return b.String()
}

func ContinuePrompt() string {
	return "Continue the same job in this session. Reconcile existing work before acting; do not repeat completed side effects. Return the outcome and optionally append " + TaskStateOpen + " … " + TaskStateClose + "."
}

func ApprovePrompt(notes string) string {
	msg := "The plan is approved. Exit plan mode if needed and implement it in this same session. End the turn with " + TaskStateOpen + " … " + TaskStateClose + "."
	if strings.TrimSpace(notes) != "" {
		msg += "\nAdditional notes:\n" + notes
	}
	return msg
}

func RevisePrompt(notes string) string {
	msg := "Revise the plan. Stay in plan mode, update plan.md, then call exit_plan_mode again."
	if strings.TrimSpace(notes) != "" {
		msg += "\nRevision notes:\n" + notes
	}
	return msg
}
