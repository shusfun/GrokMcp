package integration

import "testing"

func TestExeFromProtocolCommand(t *testing.T) {
	got := exeFromProtocolCommand(`"C:\Users\authw\AppData\Local\Programs\CCSWIT~1\CC-SWI~1.EXE" "%1"`)
	want := `C:\Users\authw\AppData\Local\Programs\CCSWIT~1\CC-SWI~1.EXE`
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
	if exeFromProtocolCommand(`cc-switch.exe "%1"`) != "cc-switch.exe" {
		t.Fatal("unquoted command")
	}
	if exeFromProtocolCommand("") != "" || exeFromProtocolCommand("%1") != "" {
		t.Fatal("empty command")
	}
}
