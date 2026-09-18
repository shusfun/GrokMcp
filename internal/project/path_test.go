package project

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestCanonicalKeyUnixAndWindows(t *testing.T) {
	unix := canonicalKey("/tmp/foo/", false)
	if unix != "/tmp/foo" {
		t.Fatalf("unix %q", unix)
	}
	win := canonicalKey(`C:\Work\Foo\`, true)
	if win != "c:/work/foo" {
		t.Fatalf("windows %q", win)
	}
	if canonicalKey(`C:/Work/Foo`, true) != canonicalKey(`c:\work\foo\`, true) {
		t.Fatal("windows slash/case should collide")
	}
}

func TestResolveAbsAndTrailing(t *testing.T) {
	dir := t.TempDir()
	r, err := Resolve(dir + string(filepath.Separator))
	if err != nil {
		t.Fatal(err)
	}
	if r.CanonicalPath == "" || r.Name == "" {
		t.Fatalf("%+v", r)
	}
	if r.GitRoot != "" {
		t.Fatalf("non-git git_root=%q", r.GitRoot)
	}
	abs, _ := filepath.Abs(dir)
	if eval, err := filepath.EvalSymlinks(abs); err == nil {
		abs = eval
	}
	if CanonicalKey(r.Root) != CanonicalKey(abs) {
		t.Fatalf("root %q abs %q", r.Root, abs)
	}
}

func TestResolveDedupesRelative(t *testing.T) {
	dir := t.TempDir()
	sub := filepath.Join(dir, "proj")
	if err := os.Mkdir(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	a, err := Resolve(sub)
	if err != nil {
		t.Fatal(err)
	}
	t.Chdir(dir)
	b, err := Resolve("proj")
	if err != nil {
		t.Fatal(err)
	}
	if a.CanonicalPath != b.CanonicalPath {
		t.Fatalf("%q vs %q", a.CanonicalPath, b.CanonicalPath)
	}
}

func TestResolveGitRoot(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	root := t.TempDir()
	cmd := exec.Command("git", "init")
	cmd.Dir = root
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git init: %s: %v", out, err)
	}
	sub := filepath.Join(root, "pkg")
	if err := os.Mkdir(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	r, err := Resolve(sub)
	if err != nil {
		t.Fatal(err)
	}
	want := root
	if eval, err := filepath.EvalSymlinks(root); err == nil {
		want = eval
	}
	if CanonicalKey(r.Root) != CanonicalKey(want) {
		t.Fatalf("want git root %q got %+v", want, r)
	}
	if r.GitRoot == "" {
		t.Fatal("expected git_root")
	}
}

func TestResolveMissingPathStillCanonical(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "gone")
	r, err := Resolve(missing)
	if err != nil {
		t.Fatal(err)
	}
	if r.CanonicalPath == "" || r.GitRoot != "" {
		t.Fatalf("%+v", r)
	}
}

func TestResolveEmpty(t *testing.T) {
	if _, err := Resolve("  "); err == nil {
		t.Fatal("expected error")
	}
}

func TestWindowsFoldDoesNotApplyOnUnix(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip()
	}
	if canonicalKey("/tmp/Foo", false) == canonicalKey("/tmp/foo", false) && strings.Contains("/tmp/Foo", "F") {
		if CanonicalKey("/tmp/Foo") == CanonicalKey("/tmp/foo") {
			t.Fatal("unix paths are case-sensitive")
		}
	}
}
