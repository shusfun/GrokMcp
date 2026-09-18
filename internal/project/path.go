package project

import (
	"os/exec"
	"path"
	"path/filepath"
	"runtime"
	"strings"
)

type Resolved struct {
	Root          string
	CanonicalPath string
	GitRoot       string
	Name          string
}

func Resolve(path string) (Resolved, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return Resolved{}, errPathRequired
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return Resolved{}, err
	}
	abs = trimTrailingSep(filepath.Clean(abs))
	if eval, err := filepath.EvalSymlinks(abs); err == nil && eval != "" {
		abs = trimTrailingSep(eval)
	}
	gitRoot := detectGitRoot(abs)
	root := abs
	if gitRoot != "" {
		root = gitRoot
	}
	name := filepath.Base(root)
	if name == "." || name == string(filepath.Separator) || name == "" {
		name = root
	}
	return Resolved{
		Root:          root,
		CanonicalPath: CanonicalKey(root),
		GitRoot:       gitRoot,
		Name:          name,
	}, nil
}

func CanonicalKey(path string) string {
	return canonicalKey(path, runtime.GOOS == "windows")
}

func canonicalKey(p string, windows bool) string {
	if windows {
		p = strings.ReplaceAll(p, `\`, "/")
	}
	p = trimTrailingSep(path.Clean(p))
	if windows {
		p = strings.ToLower(p)
	}
	return p
}

func trimTrailingSep(p string) string {
	if p == "" {
		return p
	}
	vol := filepath.VolumeName(p)
	for len(p) > len(vol)+1 && (p[len(p)-1] == '/' || p[len(p)-1] == '\\') {
		p = p[:len(p)-1]
	}
	return p
}

func detectGitRoot(path string) string {
	cmd := exec.Command("git", "-C", path, "rev-parse", "--show-toplevel")
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	root := trimTrailingSep(strings.TrimSpace(string(out)))
	if root == "" {
		return ""
	}
	if eval, err := filepath.EvalSymlinks(root); err == nil && eval != "" {
		root = trimTrailingSep(eval)
	}
	return root
}
