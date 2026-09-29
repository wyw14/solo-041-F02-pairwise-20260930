package sensor

import (
	"encoding/json"
	"errors"
	"sync"
)

type BatchDecoder struct {
	mu      sync.Mutex
	scratch []float64
}

func NewBatchDecoder(capacity int) (*BatchDecoder, error) {
	if capacity < 1 {
		return nil, errors.New("decoder capacity must be positive")
	}
	return &BatchDecoder{scratch: make([]float64, 0, capacity)}, nil
}

func (d *BatchDecoder) Decode(payload []byte) ([]float64, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.scratch = d.scratch[:0]
	if err := json.Unmarshal(payload, &d.scratch); err != nil {
		return nil, err
	}
	if len(d.scratch) == 0 {
		return nil, errors.New("sample batch is empty")
	}
	owned := make([]float64, len(d.scratch))
	copy(owned, d.scratch)
	return owned, nil
}

func CloneValues(values []float64) []float64 {
	if values == nil {
		return nil
	}
	copyOfValues := make([]float64, len(values))
	copy(copyOfValues, values)
	return copyOfValues
}
