package project

import "errors"

var (
	errPathRequired    = errors.New("path is required")
	ErrSkillConflict   = errors.New("skill file exists and is not managed by Grok Supervisor")
	ErrSkillNotManaged = errors.New("skill file is not managed by Grok Supervisor")
	ErrProjectNotFound = errors.New("project not found")
)
