//go:build !windows

package foreground

import "unsafe"

func ShowExplicit(unsafe.Pointer) {}
