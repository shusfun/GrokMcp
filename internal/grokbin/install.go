package grokbin

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"grokmcp/internal/silent"
)

const (
	ScriptURLUnix    = "https://x.ai/cli/install.sh"
	ScriptURLWindows = "https://x.ai/cli/install.ps1"
	NPMPackage       = "@xai-official/grok"
)

type Installer struct {
	GOOS      string
	Client    *http.Client
	ScriptURL string
	HomeDir   func() (string, error)
	LookPath  func(string) (string, error)
	LoginPATH func() string
	Run       func(ctx context.Context, dir, name string, args []string, extraEnv []string) (string, error)
}

func (in Installer) goos() string {
	if in.GOOS != "" {
		return in.GOOS
	}
	return runtime.GOOS
}

func (in Installer) client() *http.Client {
	if in.Client != nil {
		return in.Client
	}
	return &http.Client{Timeout: 2 * time.Minute}
}

func (in Installer) scriptURL() string {
	if in.ScriptURL != "" {
		return in.ScriptURL
	}
	if in.goos() == "windows" {
		return ScriptURLWindows
	}
	return ScriptURLUnix
}

func (in Installer) lookPath(name string) (string, error) {
	if in.LookPath != nil {
		return in.LookPath(name)
	}
	return exec.LookPath(name)
}

func (in Installer) run(ctx context.Context, dir, name string, args []string, extraEnv []string) (string, error) {
	if in.Run != nil {
		return in.Run(ctx, dir, name, args, extraEnv)
	}
	cmd := exec.CommandContext(ctx, name, args...)
	if dir != "" {
		cmd.Dir = dir
	}
	cmd.Env = mergeEnv(os.Environ(), extraEnv)
	silent.Hide(cmd)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func (in Installer) Install(ctx context.Context) (logText string, err error) {
	var b strings.Builder
	extra := in.pathEnv()
	url := in.scriptURL()
	fmt.Fprintf(&b, "download %s\n", url)
	script, err := in.download(ctx, url)
	if err != nil {
		return b.String(), err
	}
	defer os.Remove(script)
	out, runErr := in.runOfficial(ctx, script, extra)
	b.WriteString(out)
	if runErr != nil {
		fmt.Fprintf(&b, "\nofficial installer failed: %v\n", runErr)
		npmOut, npmErr := in.runNPM(ctx, extra)
		b.WriteString(npmOut)
		if npmErr != nil {
			fmt.Fprintf(&b, "\nnpm fallback failed: %v\n", npmErr)
			return b.String(), npmErr
		}
	}
	return b.String(), nil
}

func (in Installer) download(ctx context.Context, url string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	res, err := in.client().Do(req)
	if err != nil {
		return "", err
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return "", fmt.Errorf("download %s: HTTP %s", url, res.Status)
	}
	ext := ".sh"
	if in.goos() == "windows" {
		ext = ".ps1"
	}
	f, err := os.CreateTemp("", "grok-install-*"+ext)
	if err != nil {
		return "", err
	}
	_, err = io.Copy(f, res.Body)
	cerr := f.Close()
	if err != nil {
		os.Remove(f.Name())
		return "", err
	}
	if cerr != nil {
		os.Remove(f.Name())
		return "", cerr
	}
	if in.goos() != "windows" {
		_ = os.Chmod(f.Name(), 0o700)
	}
	return f.Name(), nil
}

func (in Installer) runOfficial(ctx context.Context, script string, extraEnv []string) (string, error) {
	if in.goos() == "windows" {
		return in.run(ctx, "", "powershell", []string{"-NoProfile", "-ExecutionPolicy", "Bypass", "-File", script}, extraEnv)
	}
	bash, err := in.lookPath("bash")
	if err != nil {
		bash = "bash"
	}
	return in.run(ctx, "", bash, []string{script}, extraEnv)
}

func (in Installer) runNPM(ctx context.Context, extraEnv []string) (string, error) {
	npm, err := in.lookPath("npm")
	if err != nil {
		return "", fmt.Errorf("npm not found: %w", err)
	}
	return in.run(ctx, "", npm, []string{"i", "-g", NPMPackage + "@latest"}, extraEnv)
}

func (in Installer) pathEnv() []string {
	var parts []string
	if in.LoginPATH != nil {
		if p := strings.TrimSpace(in.LoginPATH()); p != "" {
			parts = append(parts, p)
		}
	} else if p := loginShellPATH(); p != "" {
		parts = append(parts, p)
	}
	if home, err := homeDir(in.HomeDir); err == nil && home != "" {
		parts = append(parts, filepath.Join(home, ".grok", "bin"))
	}
	parts = append(parts, os.Getenv("PATH"))
	if in.goos() != "windows" {
		parts = append(parts, "/opt/homebrew/bin", "/usr/local/bin")
	}
	merged := mergePATH(parts, in.goos() == "windows")
	if merged == "" {
		return nil
	}
	return []string{"PATH=" + merged}
}

func homeDir(fn func() (string, error)) (string, error) {
	if fn != nil {
		return fn()
	}
	return os.UserHomeDir()
}

func loginShellPATH() string {
	if runtime.GOOS == "windows" {
		return ""
	}
	shell := os.Getenv("SHELL")
	if shell == "" {
		shell = "/bin/zsh"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, shell, "-lic", "/usr/bin/env")
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(out), "\n") {
		if p, ok := strings.CutPrefix(line, "PATH="); ok {
			return strings.TrimSpace(p)
		}
	}
	return ""
}

func mergePATH(parts []string, windows bool) string {
	sep := ":"
	if windows {
		sep = ";"
	}
	seen := map[string]bool{}
	var out []string
	for _, part := range parts {
		for _, seg := range strings.Split(part, sep) {
			seg = strings.TrimSpace(seg)
			if seg == "" || seen[seg] {
				continue
			}
			seen[seg] = true
			out = append(out, seg)
		}
	}
	return strings.Join(out, sep)
}

func mergeEnv(base, extra []string) []string {
	if len(extra) == 0 {
		return base
	}
	idx := map[string]int{}
	for i, kv := range base {
		if k, _, ok := strings.Cut(kv, "="); ok {
			idx[strings.ToUpper(k)] = i
		}
	}
	out := append([]string{}, base...)
	for _, kv := range extra {
		k, _, ok := strings.Cut(kv, "=")
		if !ok {
			continue
		}
		if i, exists := idx[strings.ToUpper(k)]; exists {
			out[i] = kv
			continue
		}
		out = append(out, kv)
	}
	return out
}
