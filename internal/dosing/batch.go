package dosing

import (
	"errors"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/wyw14/cry-97/internal/model"
)

type Change struct {
	ID          uuid.UUID    `json:"id"`
	LineID      model.LineID `json:"line_id"`
	BatchID     uuid.UUID    `json:"batch_id"`
	Chemical    string       `json:"chemical"`
	Rate        float64      `json:"rate"`
	Generation  uint64       `json:"generation"`
	RequestedAt time.Time    `json:"requested_at"`
}

func NewChange(lineID model.LineID, batchID uuid.UUID, chemical string, rate float64, generation uint64, now time.Time) (Change, error) {
	if lineID == "" || batchID == uuid.Nil || chemical == "" || rate <= 0 || generation == 0 {
		return Change{}, errors.New("dosing change is incomplete")
	}
	return Change{
		ID: uuid.New(), LineID: lineID, BatchID: batchID, Chemical: chemical,
		Rate: rate, Generation: generation, RequestedAt: now.UTC(),
	}, nil
}

type ledgerKey struct {
	lineID   model.LineID
	chemical string
}

type Ledger struct {
	mu      sync.RWMutex
	changes map[uuid.UUID][]Change
	current map[ledgerKey]Change
}

func NewLedger() *Ledger {
	return &Ledger{
		changes: make(map[uuid.UUID][]Change),
		current: make(map[ledgerKey]Change),
	}
}

func (l *Ledger) Record(change Change) {
	l.mu.Lock()
	l.changes[change.BatchID] = append(l.changes[change.BatchID], change)
	l.current[ledgerKey{change.LineID, change.Chemical}] = change
	l.mu.Unlock()
}

func (l *Ledger) Latest(batchID uuid.UUID) (Change, bool) {
	l.mu.RLock()
	defer l.mu.RUnlock()
	values := l.changes[batchID]
	if len(values) == 0 {
		return Change{}, false
	}
	return values[len(values)-1], true
}

// CurrentRate 返回某处理线、某药剂最近一次已下发的投加设定。无记录时返回 0。
func (l *Ledger) CurrentRate(lineID model.LineID, chemical string) (Change, bool) {
	l.mu.RLock()
	defer l.mu.RUnlock()
	change, ok := l.current[ledgerKey{lineID, chemical}]
	return change, ok
}

// Clear 清除某处理线、某药剂的当前设定（首次投加被撤销后无在执行设定）。
func (l *Ledger) Clear(lineID model.LineID, chemical string) {
	l.mu.Lock()
	delete(l.current, ledgerKey{lineID, chemical})
	l.mu.Unlock()
}
