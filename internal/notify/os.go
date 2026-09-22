package notify

import (
	"os/exec"
	"runtime"
	"strings"

	"grokmcp/internal/silent"
)

type OS struct{}

func (OS) Send(title, body string) {
	title = strings.TrimSpace(title)
	body = strings.TrimSpace(body)
	if title == "" {
		return
	}
	switch runtime.GOOS {
	case "darwin":
		script := `display notification ` + appleQuote(body) + ` with title ` + appleQuote(title)
		_ = exec.Command("osascript", "-e", script).Start()
	case "windows":
		ps := `Add-Type -AssemblyName System.Windows.Forms; $n = New-Object System.Windows.Forms.NotifyIcon; $n.Icon = [System.Drawing.SystemIcons]::Information; $n.Visible = $true; $n.ShowBalloonTip(4000, '` + psEscape(title) + `', '` + psEscape(body) + `', [System.Windows.Forms.ToolTipIcon]::Info)`
		cmd := exec.Command("powershell", "-NoProfile", "-Command", ps)
		silent.Hide(cmd)
		_ = cmd.Start()
	default:
		_ = exec.Command("notify-send", title, body).Start()
	}
}

func appleQuote(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `"`, `\"`)
	return `"` + s + `"`
}

func psEscape(s string) string {
	return strings.ReplaceAll(s, "'", "''")
}
