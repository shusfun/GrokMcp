package integration

import (
	"context"
	"fmt"
	"os/exec"
)

func defaultOpenURL(ctx context.Context, goos, raw string) error {
	switch goos {
	case "darwin":
		return runOpen(ctx, "open", raw)
	case "windows":
		return runOpen(ctx, "rundll32", "url.dll,FileProtocolHandler", raw)
	default:
		return fmt.Errorf("%w on %s", errOpenUnsupported, goos)
	}
}

func defaultOpenApp(ctx context.Context, goos string) error {
	switch goos {
	case "darwin":
		return runOpen(ctx, "open", "-a", "CC Switch")
	case "windows":
		return runOpen(ctx, "cmd", "/c", "start", "", "CC Switch")
	default:
		return fmt.Errorf("%w on %s", errOpenUnsupported, goos)
	}
}

func runOpen(ctx context.Context, name string, args ...string) error {
	cmd := exec.CommandContext(ctx, name, args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		if len(out) > 0 {
			return fmt.Errorf("%s: %w: %s", name, err, string(out))
		}
		return fmt.Errorf("%s: %w", name, err)
	}
	return nil
}
