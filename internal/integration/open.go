package integration

import (
	"context"
	"fmt"
	"os/exec"
	"strings"

	"grokmcp/internal/silent"
)

func defaultOpenURL(ctx context.Context, goos, raw string) error {
	switch goos {
	case "darwin":
		return runOpen(ctx, "open", raw)
	case "windows":
		return openWindowsURL(ctx, raw)
	default:
		return fmt.Errorf("%w on %s", errOpenUnsupported, goos)
	}
}

func defaultOpenApp(ctx context.Context, goos string) error {
	switch goos {
	case "darwin":
		return runOpen(ctx, "open", "-a", "CC Switch")
	case "windows":
		return openWindowsApp(ctx)
	default:
		return fmt.Errorf("%w on %s", errOpenUnsupported, goos)
	}
}

func runOpen(ctx context.Context, name string, args ...string) error {
	cmd := exec.CommandContext(ctx, name, args...)
	silent.Hide(cmd)
	out, err := cmd.CombinedOutput()
	if err != nil {
		if len(out) > 0 {
			return fmt.Errorf("%s: %w: %s", name, err, string(out))
		}
		return fmt.Errorf("%s: %w", name, err)
	}
	return nil
}

func exeFromProtocolCommand(cmd string) string {
	cmd = strings.TrimSpace(cmd)
	if cmd == "" {
		return ""
	}
	if strings.HasPrefix(cmd, `"`) {
		rest := cmd[1:]
		if i := strings.Index(rest, `"`); i >= 0 {
			return rest[:i]
		}
	}
	fields := strings.Fields(cmd)
	if len(fields) == 0 {
		return ""
	}
	exe := strings.Trim(fields[0], `"`)
	if exe == "%1" {
		return ""
	}
	return exe
}
