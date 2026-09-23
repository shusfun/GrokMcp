package store

import (
	"encoding/json"
	"fmt"
	"grokmcp/internal/protocol"
)

// NotificationAfter 补读已经提交的最新边界；当前请求之外的历史只留在历史接口。
func (s *Store) NotificationAfter(id string, after int64, requestID string) (protocol.Job, bool, error) {
	rows, err := s.db.Query(`SELECT id,snapshot FROM job_notifications WHERE job_id=? AND id>? ORDER BY id DESC`, id, after)
	if err != nil {
		return protocol.Job{}, false, err
	}
	defer rows.Close()
	for rows.Next() {
		var cursor int64
		var raw string
		if err := rows.Scan(&cursor, &raw); err != nil {
			return protocol.Job{}, false, err
		}
		var j protocol.Job
		if err := json.Unmarshal([]byte(raw), &j); err != nil {
			return j, false, err
		}
		if j.RequestID == requestID {
			j.EventCursor = cursor
			return j, true, nil
		}
	}
	return protocol.Job{}, false, rows.Err()
}

func (s *Store) QueuedRequests(id string) ([]WorkRequest, error) {
	rows, err := s.db.Query(`SELECT request_id,prompt,planning,phase FROM work_requests WHERE job_id=? AND phase='queued' AND state<>'cancelled' ORDER BY rowid`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []WorkRequest
	for rows.Next() {
		var r WorkRequest
		if err := rows.Scan(&r.RequestID, &r.Prompt, &r.Planning, &r.Phase); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// WaitSnapshot 用一个读事务固定状态与事件上界，游标只描述本次快照。
func (s *Store) WaitSnapshot(id string, after int64) (Record, []protocol.Job, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return Record{}, nil, err
	}
	defer tx.Rollback()
	rec, err := scanJob(tx.QueryRow(`SELECT `+jobSelectCols+` FROM `+jobFrom+` WHERE jobs.job_id=?`, id))
	if err != nil {
		return rec, nil, err
	}
	if after > rec.Job.EventCursor {
		return rec, nil, fmt.Errorf("cursor is ahead of job %s", id)
	}
	rows, err := tx.Query(`SELECT id,snapshot FROM job_notifications WHERE job_id=? AND id>? AND id<=? ORDER BY id`, id, after, rec.Job.EventCursor)
	if err != nil {
		return rec, nil, err
	}
	var events []protocol.Job
	for rows.Next() {
		var cursor int64
		var raw string
		if err := rows.Scan(&cursor, &raw); err != nil {
			rows.Close()
			return rec, nil, err
		}
		var j protocol.Job
		if err := json.Unmarshal([]byte(raw), &j); err != nil {
			rows.Close()
			return rec, nil, err
		}
		j.EventCursor = cursor
		if j.RequestID == rec.Job.RequestID {
			events = append(events, j)
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return rec, nil, err
	}
	if err := tx.Commit(); err != nil {
		return rec, nil, err
	}
	return rec, events, nil
}

func (s *Store) Request(id, requestID string) (WorkRequest, error) {
	var r WorkRequest
	err := s.db.QueryRow(`SELECT request_id,prompt,planning,phase FROM work_requests WHERE job_id=? AND request_id=?`, id, requestID).Scan(&r.RequestID, &r.Prompt, &r.Planning, &r.Phase)
	return r, err
}

func (s *Store) CancelQueuedRequest(jobID, requestID string) (bool, error) {
	res, err := s.db.Exec(`UPDATE work_requests SET state='cancelled' WHERE job_id=? AND request_id=? AND phase='queued' AND state<>'cancelled' AND state<>'completed'`, jobID, requestID)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n > 0, err
}

func (s *Store) RequestState(jobID, requestID string) (string, error) {
	var state string
	err := s.db.QueryRow(`SELECT state FROM work_requests WHERE job_id=? AND request_id=?`, jobID, requestID).Scan(&state)
	return state, err
}
