package terminal

import (
	"fmt"
	"strings"
)

func Render(template, grokPath, command, cwd, sessionID string) string {
	replacer := strings.NewReplacer(
		"{grok}", grokPath,
		"{command}", command,
		"{cwd}", cwd,
		"{session_id}", sessionID,
	)
	if strings.TrimSpace(template) == "" {
		return command
	}
	return replacer.Replace(template)
}

func ResumeCommand(grokPath, sessionID string) string {
	return fmt.Sprintf("%s --resume %s", shellQuote(grokPath), sessionID)
}

func DashboardCommand(grokPath string) string {
	return fmt.Sprintf("%s dashboard", shellQuote(grokPath))
}

func CommandInDir(cwd, command string) string {
	cwd = strings.TrimSpace(cwd)
	if cwd == "" {
		return command
	}
	return "cd " + shellSingleQuote(cwd) + " && " + command
}

func CommandInDirWindows(cwd, command string) string {
	cwd = strings.TrimSpace(cwd)
	if cwd == "" {
		return command
	}
	return "cd /d " + cwd + " && " + command
}

func shellSingleQuote(s string) string {
	return "'" + strings.ReplaceAll(s, `'`, `'"'"'`) + "'"
}

func shellQuote(s string) string {
	if s == "" {
		return "grok"
	}
	if !strings.ContainsAny(s, " \t\"'") {
		return s
	}
	return `"` + strings.ReplaceAll(s, `"`, `\"`) + `"`
}
