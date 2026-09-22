//go:build !windows

package ownedprocess

import "errors"

type consolePlatform struct{}

var errConPTY = errors.New("ConPTY is only available on Windows")

func NewConsole(int, int) (*Console, error)       { return nil, errConPTY }
func (consolePlatform) read([]byte) (int, error)  { return 0, errConPTY }
func (consolePlatform) write([]byte) (int, error) { return 0, errConPTY }
func (consolePlatform) resize(int, int) error     { return errConPTY }
func (consolePlatform) refresh() error            { return errConPTY }
func (consolePlatform) closeConsole()             {}
func (consolePlatform) closeOutput()              {}
