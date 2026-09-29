package settling

import (
	"errors"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/wyw14/cry-97/internal/model"
)

type CyclePhase string

const (
	PhaseFill   CyclePhase = "fill"
	PhaseSettle CyclePhase = "settle"
	PhaseDecant CyclePhase = "decant"
	PhaseDone   CyclePhase = "done"
)

type Cycle struct {
	ID        uuid.UUID    `json:"id"`
	LineID    model.LineID `json:"line_id"`
	Phase     CyclePhase   `json:"phase"`
	StartedAt time.Time    `json:"started_at"`
	UpdatedAt time.Time    `json:"updated_at"`
}

type CycleBook struct {
	mu     sync.RWMutex
	cycles map[model.LineID]Cycle
}

func NewCycleBook() *CycleBook {
	return &CycleBook{cycles: make(map[model.LineID]Cycle)}
}

func (b *CycleBook) Start(lineID model.LineID, now time.Time) (Cycle, error) {
	if lineID == "" {
		return Cycle{}, errors.New("settling line is required")
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if current, ok := b.cycles[lineID]; ok && current.Phase != PhaseDone {
		return Cycle{}, errors.New("settling cycle is already active")
	}
	cycle := Cycle{ID: uuid.New(), LineID: lineID, Phase: PhaseFill, StartedAt: now.UTC(), UpdatedAt: now.UTC()}
	b.cycles[lineID] = cycle
	return cycle, nil
}

func (b *CycleBook) Advance(lineID model.LineID, now time.Time) (Cycle, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	cycle, ok := b.cycles[lineID]
	if !ok {
		return Cycle{}, errors.New("settling cycle is not active")
	}
	switch cycle.Phase {
	case PhaseFill:
		cycle.Phase = PhaseSettle
	case PhaseSettle:
		cycle.Phase = PhaseDecant
	case PhaseDecant:
		cycle.Phase = PhaseDone
	default:
		return Cycle{}, errors.New("settling cycle is complete")
	}
	cycle.UpdatedAt = now.UTC()
	b.cycles[lineID] = cycle
	return cycle, nil
}

func (b *CycleBook) Current(lineID model.LineID) (Cycle, bool) {
	b.mu.RLock()
	defer b.mu.RUnlock()
	cycle, ok := b.cycles[lineID]
	return cycle, ok
}
