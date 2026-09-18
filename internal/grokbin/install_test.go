package grokbin

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestInstallOfficialNoSudo(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "sudo") {
			t.Fatal(r.URL.Path)
		}
		io.WriteString(w, "#!/bin/sh\necho installed\n")
	}))
	t.Cleanup(srv.Close)

	var ran []string
	in := Installer{
		GOOS:      "darwin",
		Client:    srv.Client(),
		ScriptURL: srv.URL + "/install.sh",
		LookPath:  func(name string) (string, error) { return name, nil },
		LoginPATH: func() string { return "/opt/homebrew/bin:/usr/bin" },
		Run: func(_ context.Context, _, name string, args []string, extraEnv []string) (string, error) {
			ran = append(ran, name+" "+strings.Join(args, " "))
			joined := strings.Join(append([]string{name}, args...), " ")
			if strings.Contains(joined, "sudo") {
				t.Fatal(joined)
			}
			hasPath := false
			for _, e := range extraEnv {
				if strings.HasPrefix(e, "PATH=") && strings.Contains(e, "/opt/homebrew/bin") {
					hasPath = true
				}
			}
			if !hasPath {
				t.Fatalf("PATH env: %v", extraEnv)
			}
			return "ok\n", nil
		},
	}
	log, err := in.Install(context.Background())
	if err != nil {
		t.Fatal(err, log)
	}
	if len(ran) != 1 {
		t.Fatalf("ran %v", ran)
	}
	if !strings.Contains(ran[0], "bash") {
		t.Fatal(ran[0])
	}
	if strings.Contains(log, "npm") {
		t.Fatal(log)
	}
}

func TestInstallFallsBackToNPM(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, "#!/bin/sh\nexit 1\n")
	}))
	t.Cleanup(srv.Close)

	var ran []string
	in := Installer{
		GOOS:      "darwin",
		Client:    srv.Client(),
		ScriptURL: srv.URL + "/install.sh",
		LookPath:  func(name string) (string, error) { return "/usr/local/bin/" + name, nil },
		Run: func(_ context.Context, _, name string, args []string, _ []string) (string, error) {
			line := name + " " + strings.Join(args, " ")
			ran = append(ran, line)
			if strings.Contains(line, "npm") {
				return "added 1 package\n", nil
			}
			return "fail\n", io.ErrUnexpectedEOF
		},
	}
	log, err := in.Install(context.Background())
	if err != nil {
		t.Fatal(err, log)
	}
	if len(ran) != 2 {
		t.Fatalf("ran %v", ran)
	}
	if !strings.Contains(ran[1], "npm") || !strings.Contains(ran[1], NPMPackage) {
		t.Fatal(ran[1])
	}
}

func TestInstallWindowsUsesPowerShellFile(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, "Write-Host installed")
	}))
	t.Cleanup(srv.Close)
	in := Installer{
		GOOS:      "windows",
		Client:    srv.Client(),
		ScriptURL: srv.URL + "/install.ps1",
		Run: func(_ context.Context, _, name string, args []string, _ []string) (string, error) {
			if name != "powershell" {
				t.Fatal(name)
			}
			joined := strings.Join(args, " ")
			if !strings.Contains(joined, "-File") {
				t.Fatal(joined)
			}
			if strings.Contains(joined, "iex") || strings.Contains(strings.ToLower(joined), "sudo") {
				t.Fatal(joined)
			}
			return "ok", nil
		},
	}
	if _, err := in.Install(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestMergePATHDedupes(t *testing.T) {
	got := mergePATH([]string{"/a:/b", "/b:/c"}, false)
	if got != "/a:/b:/c" {
		t.Fatal(got)
	}
}
