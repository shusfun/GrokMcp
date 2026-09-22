//go:build !windows

package integration

import "context"

func openWindowsURL(context.Context, string) error { return errOpenUnsupported }

func openWindowsApp(context.Context) error { return errOpenUnsupported }
