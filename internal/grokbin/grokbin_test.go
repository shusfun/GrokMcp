package grokbin

import (
	"context"
	"errors"
	"testing"
)

func TestDiagnoseMissing(t *testing.T) {
	f := Finder{
		LookPath: func(string) (string, error) { return "", errors.New("missing") },
		Run:      func(context.Context, string, ...string) (string, error) { return "", errors.New("cannot run") },
	}
	d := f.Diagnose(context.Background(), "/no/such/grok")
	if d.Error == "" {
		t.Fatal("expected error")
	}
	if d.Compatible {
		t.Fatal("not compatible")
	}
}

func TestVersionAtLeast(t *testing.T) {
	if !versionAtLeast("1.0.34", 1, 0, 34) {
		t.Fatal("equal")
	}
	if !versionAtLeast("1.1.0", 1, 0, 34) {
		t.Fatal("minor")
	}
	if versionAtLeast("1.0.33", 1, 0, 34) {
		t.Fatal("patch")
	}
	if versionAtLeast("1.0.0", 1, 0, 34) {
		t.Fatal("old")
	}
}
