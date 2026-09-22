//go:build windows

package integration

import (
	"context"
	"os"
	"path/filepath"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

func openWindowsURL(_ context.Context, raw string) error {
	return shellOpen(raw)
}

func openWindowsApp(context.Context) error {
	if exe := registeredCCSwitchExe(); exe != "" {
		if err := shellOpen(exe); err == nil {
			return nil
		}
	}
	var last error
	for _, exe := range ccSwitchExeCandidates() {
		if _, err := os.Stat(exe); err != nil {
			continue
		}
		if err := shellOpen(exe); err == nil {
			return nil
		} else {
			last = err
		}
	}
	if last != nil {
		return last
	}
	return errOpenUnsupported
}

func shellOpen(target string) error {
	verb, err := windows.UTF16PtrFromString("open")
	if err != nil {
		return err
	}
	file, err := windows.UTF16PtrFromString(target)
	if err != nil {
		return err
	}
	return windows.ShellExecute(0, verb, file, nil, nil, windows.SW_SHOWNORMAL)
}

func registeredCCSwitchExe() string {
	for _, root := range []registry.Key{registry.CURRENT_USER, registry.LOCAL_MACHINE} {
		k, err := registry.OpenKey(root, `Software\Classes\ccswitch\shell\open\command`, registry.QUERY_VALUE)
		if err != nil {
			continue
		}
		val, _, qerr := k.GetStringValue("")
		_ = k.Close()
		if qerr != nil {
			continue
		}
		exe := exeFromProtocolCommand(val)
		if exe == "" {
			continue
		}
		return exe
	}
	return ""
}

func ccSwitchExeCandidates() []string {
	local := os.Getenv("LOCALAPPDATA")
	if local == "" {
		return nil
	}
	return []string{
		filepath.Join(local, "Programs", "CC Switch", "cc-switch.exe"),
		filepath.Join(local, "Programs", "CC-Switch", "cc-switch.exe"),
	}
}
