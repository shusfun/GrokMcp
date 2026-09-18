package ids

import (
	"fmt"
	"sync/atomic"

	"github.com/google/uuid"
)

type Generator interface {
	JobID() string
}

type UUID struct{}

func (UUID) JobID() string { return uuid.NewString() }

type Seq struct {
	n atomic.Int64
}

func (s *Seq) JobID() string {
	return fmt.Sprintf("job-%d", s.n.Add(1))
}
