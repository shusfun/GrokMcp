//go:build !windows

package terminal

import (
	"context"
	"errors"
)

func (e Exec) openViewer(context.Context, string, string, string) (Handle, error) {
	return nil, errors.New("Windows viewer unavailable")
}
func (e Exec) testViewerTemplate(context.Context, string) error {
	return errors.New("Windows viewer unavailable")
}
func RunViewer(context.Context, []string) error { return errors.New("Windows viewer unavailable") }
