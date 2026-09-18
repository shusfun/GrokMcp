package terminal

import "strings"

func SessionTitle(sessionID string) string {
	id := strings.TrimSpace(sessionID)
	if len(id) > 12 {
		id = id[:12]
	}
	return "grok-sess-" + id
}
