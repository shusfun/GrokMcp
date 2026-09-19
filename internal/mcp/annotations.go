package mcp

import "github.com/modelcontextprotocol/go-sdk/mcp"

func boolPtr(v bool) *bool { return &v }

func annotations(readOnly, destructive, idempotent, openWorld bool) *mcp.ToolAnnotations {
	a := &mcp.ToolAnnotations{
		ReadOnlyHint:   readOnly,
		IdempotentHint: idempotent,
		OpenWorldHint:  boolPtr(openWorld),
	}
	if !readOnly {
		a.DestructiveHint = boolPtr(destructive)
	}
	return a
}

var (
	annExec        = annotations(false, true, false, true)
	annWait        = annotations(true, false, false, true)
	annCancelTurn  = annotations(false, true, true, true)
	annSetView     = annotations(false, true, true, true)
	annOpenTerm    = annotations(false, true, false, true)
	annStatus      = annotations(true, false, false, false)
	annDebugSet    = annotations(false, false, true, false)
	annDebugRead   = annotations(true, false, false, false)
	annDebugExport = annotations(false, true, true, false)
	annProjWrite   = annotations(false, true, true, false)
	annProjRead    = annotations(true, false, false, false)
	annSkillRead   = annotations(true, false, false, false)
	annSkillAdd    = annotations(false, false, true, false)
	annSkillUpdate = annotations(false, true, true, false)
	annSkillRemove = annotations(false, true, true, false)
	annPromptRead  = annotations(true, false, false, false)
	annPromptSave  = annotations(false, true, true, false)
)
