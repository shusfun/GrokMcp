package protocol

import (
	"encoding/json"
	"strings"
)

const (
	TaskStateOpen  = "<GROK_TASK_STATE>"
	TaskStateClose = "</GROK_TASK_STATE>"
)

type TaskState struct {
	State   TaskMarker `json:"state"`
	Summary string     `json:"summary"`
	Next    string     `json:"next"`
}

func ParseTaskState(text string) (TaskState, bool) {
	start := strings.LastIndex(text, TaskStateOpen)
	if start < 0 {
		return TaskState{}, false
	}
	rest := text[start+len(TaskStateOpen):]
	end := strings.Index(rest, TaskStateClose)
	if end < 0 {
		return TaskState{}, false
	}
	raw := strings.TrimSpace(rest[:end])
	var ts TaskState
	if err := json.Unmarshal([]byte(raw), &ts); err != nil {
		return TaskState{}, false
	}
	switch ts.State {
	case MarkerWorking, MarkerNeedsInput, MarkerCompleted, MarkerBlocked:
		return ts, true
	default:
		return TaskState{}, false
	}
}

func RenderTaskState(ts TaskState) string {
	b, _ := json.Marshal(ts)
	return TaskStateOpen + "\n" + string(b) + "\n" + TaskStateClose
}
