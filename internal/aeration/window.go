package aeration

import (
	"errors"
	"math"
	"sort"
	"sync"

	"github.com/wyw14/cry-97/internal/sensor"
)

type Window struct {
	BasinID string    `json:"basin_id"`
	Values  []float64 `json:"values"`
	Average float64   `json:"average"`
	Stable  bool      `json:"stable"`
}

type WindowBook struct {
	mu      sync.RWMutex
	limit   int
	windows map[string][]float64
}

func NewWindowBook(limit int) (*WindowBook, error) {
	if limit < 3 {
		return nil, errors.New("aeration window must contain at least three values")
	}
	return &WindowBook{limit: limit, windows: make(map[string][]float64)}, nil
}

func (b *WindowBook) Store(basinID string, values []float64) (Window, error) {
	if basinID == "" || len(values) == 0 {
		return Window{}, errors.New("basin and samples are required")
	}
	start := 0
	if len(values) > b.limit {
		start = len(values) - b.limit
	}
	owned := sensor.CloneValues(values[start:])
	b.mu.Lock()
	b.windows[basinID] = owned
	b.mu.Unlock()
	return evaluate(basinID, owned), nil
}

func (b *WindowBook) All() []Window {
	b.mu.RLock()
	result := make([]Window, 0, len(b.windows))
	for basinID, values := range b.windows {
		result = append(result, evaluate(basinID, append([]float64(nil), values...)))
	}
	b.mu.RUnlock()
	sort.Slice(result, func(i, j int) bool { return result[i].BasinID < result[j].BasinID })
	return result
}

func evaluate(basinID string, values []float64) Window {
	var total float64
	minimum, maximum := math.Inf(1), math.Inf(-1)
	for _, value := range values {
		total += value
		minimum = math.Min(minimum, value)
		maximum = math.Max(maximum, value)
	}
	average := total / float64(len(values))
	return Window{BasinID: basinID, Values: append([]float64(nil), values...), Average: average, Stable: maximum-minimum <= 0.35}
}
