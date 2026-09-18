package protocol

import (
	"cmp"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

const (
	DefaultJobLimit = 40
	MaxJobLimit     = 100
)

func DashboardActive(s JobState) bool {
	switch s {
	case StateStarting, StatePlanning, StateExecuting, StateRecovering, StatePlanReady, StateNeedsInput, StateDisconnected:
		return true
	default:
		return false
	}
}

func DashboardHistory(s JobState) bool {
	switch s {
	case StateCompleted, StateCancelled, StateFailed, StateBlocked:
		return true
	default:
		return false
	}
}

func ActiveGroup(s JobState) int {
	if DashboardActive(s) {
		return 1
	}
	return 0
}

func CompareJobs(a, b Job) int {
	if c := cmp.Compare(ActiveGroup(b.State), ActiveGroup(a.State)); c != 0 {
		return c
	}
	if c := cmp.Compare(b.UpdatedAt.Unix(), a.UpdatedAt.Unix()); c != 0 {
		return c
	}
	if c := cmp.Compare(b.CreatedAt.Unix(), a.CreatedAt.Unix()); c != 0 {
		return c
	}
	return cmp.Compare(b.JobID, a.JobID)
}

type JobCursor struct {
	G int    `json:"g"`
	U int64  `json:"u"`
	C int64  `json:"c"`
	I string `json:"i"`
}

func EncodeJobCursor(j Job) string {
	raw, _ := json.Marshal(JobCursor{
		G: ActiveGroup(j.State),
		U: j.UpdatedAt.Unix(),
		C: j.CreatedAt.Unix(),
		I: j.JobID,
	})
	return base64.RawURLEncoding.EncodeToString(raw)
}

func DecodeJobCursor(s string) (JobCursor, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return JobCursor{}, nil
	}
	raw, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil {
		return JobCursor{}, fmt.Errorf("invalid cursor")
	}
	var cur JobCursor
	if err := json.Unmarshal(raw, &cur); err != nil {
		return JobCursor{}, fmt.Errorf("invalid cursor")
	}
	return cur, nil
}

func ClampJobLimit(n int) int {
	if n <= 0 {
		return DefaultJobLimit
	}
	if n > MaxJobLimit {
		return MaxJobLimit
	}
	return n
}

func UnixTime(ts int64) time.Time {
	if ts == 0 {
		return time.Time{}
	}
	return time.Unix(ts, 0).UTC()
}
