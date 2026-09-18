package grokbin

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"grokmcp/internal/paths"
	"grokmcp/internal/protocol"
)

type Finder struct {
	LookPath func(string) (string, error)
	Run      func(ctx context.Context, bin string, args ...string) (string, error)
}

func New() Finder {
	return Finder{
		LookPath: exec.LookPath,
		Run: func(ctx context.Context, bin string, args ...string) (string, error) {
			cmd := exec.CommandContext(ctx, bin, args...)
			var out bytes.Buffer
			cmd.Stdout = &out
			cmd.Stderr = &out
			err := cmd.Run()
			return out.String(), err
		},
	}
}

func (f Finder) Resolve(explicit string) (string, error) {
	if explicit != "" {
		return explicit, nil
	}
	if p := os.Getenv("GROK_BINARY"); p != "" {
		return p, nil
	}
	home, _ := os.UserHomeDir()
	candidates := []string{}
	if home != "" {
		candidates = append(candidates, filepath.Join(home, ".grok", "bin", "grok"))
	}
	if p, err := f.LookPath("grok"); err == nil {
		candidates = append([]string{p}, candidates...)
	}
	for _, c := range candidates {
		if _, err := os.Stat(c); err == nil {
			return c, nil
		}
	}
	if f.LookPath != nil {
		return f.LookPath("grok")
	}
	return "", os.ErrNotExist
}

var verRe = regexp.MustCompile(`(\d+\.\d+\.\d+)`)

func (f Finder) Diagnose(ctx context.Context, explicit string) protocol.DiagnoseResult {
	d := protocol.DiagnoseResult{LeaderSocket: paths.LeaderSocket()}
	bin, err := f.Resolve(explicit)
	if err != nil {
		d.Error = "Grok 二进制未找到。安装 Grok Build 并登录后再试。"
		return d
	}
	d.GrokPath = bin
	ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	out, err := f.Run(ctx, bin, "version")
	if err != nil && out == "" {
		d.Error = "无法执行 grok version: " + err.Error()
		return d
	}
	d.GrokVersion = strings.TrimSpace(out)
	if m := verRe.FindString(out); m != "" {
		d.GrokVersion = m
		d.Compatible = versionAtLeast(m, 1, 0, 34)
	}
	if !d.Compatible {
		d.Error = "Grok 版本过低或不兼容 ACP leader。"
	}
	if st, err := os.Stat(d.LeaderSocket); err == nil {
		d.LeaderRunning = st.Mode()&os.ModeSocket != 0
	}
	modelsOut, modelsErr := f.Run(ctx, bin, "models")
	d.LoggedIn = modelsErr == nil && strings.TrimSpace(modelsOut) != "" && !strings.Contains(strings.ToLower(modelsOut), "not logged")
	if !d.LoggedIn && d.Error == "" {
		d.Error = "Grok 未登录。运行 grok login。"
	}
	return d
}

func versionAtLeast(v string, major, minor, patch int) bool {
	var a, b, c int
	if _, err := fmt.Sscanf(v, "%d.%d.%d", &a, &b, &c); err != nil {
		return false
	}
	if a != major {
		return a > major
	}
	if b != minor {
		return b > minor
	}
	return c >= patch
}
