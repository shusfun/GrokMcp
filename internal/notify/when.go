package notify

import "grokmcp/internal/protocol"

func Message(prev, next protocol.JobState, title string) (string, bool) {
	if prev == next {
		return "", false
	}
	switch next {
	case protocol.StateNeedsInput:
		return title + " 需要输入", true
	case protocol.StateFailed:
		return title + " 失败", true
	case protocol.StateDisconnected:
		return title + " 已断连", true
	default:
		return "", false
	}
}
