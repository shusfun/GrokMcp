package store

import (
	"fmt"
	"grokmcp/internal/protocol"
)

func (s *Store) Result(id string, q protocol.ResultQuery) (protocol.ResultPage, error) {
	out := protocol.ResultPage{}
	if q.RequestID == "" || q.Offset < 0 || q.Limit < 0 || q.Limit > 65536 {
		return out, fmt.Errorf("result requires request_id and a valid character offset/limit (max 65536)")
	}
	limit := q.Limit
	if limit == 0 {
		limit = 16384
	}
	var body string
	err := s.db.QueryRow(`SELECT request_id,turn_id,body FROM turn_results WHERE job_id=? AND request_id=? AND (?='' OR turn_id=?) ORDER BY rowid DESC LIMIT 1`, id, q.RequestID, q.TurnID, q.TurnID).Scan(&out.RequestID, &out.TurnID, &body)
	if err != nil {
		return out, fmt.Errorf("result not available for request %s: %w", q.RequestID, err)
	}
	runes := []rune(body)
	if q.Offset > len(runes) {
		return out, fmt.Errorf("result offset exceeds length")
	}
	end := min(q.Offset+limit, len(runes))
	out.Text = string(runes[q.Offset:end])
	out.Offset = q.Offset
	out.NextOffset = end
	out.Total = len(runes)
	out.HasMore = end < len(runes)
	return out, nil
}
