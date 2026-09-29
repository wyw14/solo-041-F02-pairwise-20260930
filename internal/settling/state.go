package settling

import (
	"errors"
	"sync"
	"time"

	"github.com/wyw14/cry-97/internal/model"
)

type Snapshot struct {
	LineID       model.LineID `json:"line_id"`
	BasinID      string       `json:"basin_id"`
	BlanketLevel float64      `json:"blanket_level"`
	High         bool         `json:"high"`
	Cycle        uint64       `json:"cycle"`
	UpdatedAt    time.Time    `json:"updated_at"`
}

type BlanketNotice struct {
	Before Snapshot
	After  Snapshot
}

type Listener interface {
	BlanketChanged(BlanketNotice)
}

type State struct {
	mu       sync.RWMutex
	value    Snapshot
	listener Listener
}

func NewState(lineID model.LineID, basinID string, listener Listener) (*State, error) {
	if lineID == "" || basinID == "" {
		return nil, errors.New("settling state identity is required")
	}
	return &State{value: Snapshot{LineID: lineID, BasinID: basinID}, listener: listener}, nil
}

func (s *State) UpdateBlanket(level, highThreshold float64, now time.Time) Snapshot {
	s.mu.Lock()
	before := s.value
	s.value.BlanketLevel = level
	s.value.High = level >= highThreshold
	s.value.UpdatedAt = now.UTC()
	after := s.value
	listener := s.listener
	s.mu.Unlock()
	if listener != nil && before.High != after.High {
		listener.BlanketChanged(BlanketNotice{Before: before, After: after})
	}
	return after
}

func (s *State) BeginCycle(now time.Time) Snapshot {
	s.mu.Lock()
	s.value.Cycle++
	s.value.UpdatedAt = now.UTC()
	value := s.value
	s.mu.Unlock()
	return value
}

func (s *State) Snapshot() Snapshot {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.value
}
