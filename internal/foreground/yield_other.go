//go:build !windows

package foreground

import "time"

func YieldIfCurrent() {}

func StartYield(time.Duration) {}

func LockDuring(fn func() error) error { return fn() }
