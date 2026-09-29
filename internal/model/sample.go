package model

import (
	"errors"
	"math"
	"strings"
	"time"

	"github.com/google/uuid"
)

func ParseSampleKind(raw string) (SampleKind, error) {
	kind := SampleKind(strings.TrimSpace(raw))
	switch kind {
	case SampleDissolvedOxygen, SampleWaterLevel, SampleSludgeBlanket, SampleCOD:
		return kind, nil
	default:
		return "", errors.New("sample kind is unsupported")
	}
}

type SampleKind string

const (
	SampleDissolvedOxygen SampleKind = "dissolved_oxygen"
	SampleWaterLevel      SampleKind = "water_level"
	SampleSludgeBlanket   SampleKind = "sludge_blanket"
	SampleCOD             SampleKind = "cod"
)

type Sample struct {
	ID         uuid.UUID  `json:"id"`
	LineID     LineID     `json:"line_id"`
	BasinID    string     `json:"basin_id"`
	Kind       SampleKind `json:"kind"`
	Sequence   uint64     `json:"sequence"`
	Value      float64    `json:"value"`
	Unit       string     `json:"unit"`
	ObservedAt time.Time  `json:"observed_at"`
}

func NewSample(lineID LineID, basinID string, kind SampleKind, sequence uint64, value float64, unit string, now time.Time) Sample {
	return Sample{
		ID: uuid.New(), LineID: lineID, BasinID: basinID, Kind: kind,
		Sequence: sequence, Value: value, Unit: unit, ObservedAt: now.UTC(),
	}
}

func (s Sample) Finite() bool {
	return !math.IsNaN(s.Value) && !math.IsInf(s.Value, 0)
}

type LabResult struct {
	SampleID   uuid.UUID `json:"sample_id"`
	LineID     LineID    `json:"line_id"`
	BatchID    uuid.UUID `json:"batch_id"`
	Revision   uint64    `json:"revision"`
	COD        float64   `json:"cod"`
	Ammonia    float64   `json:"ammonia"`
	Valid      bool      `json:"valid"`
	ReceivedAt time.Time `json:"received_at"`
}

func (r LabResult) Qualified() bool {
	return r.Valid && r.COD >= 0 && r.COD <= 50 && r.Ammonia >= 0 && r.Ammonia <= 5
}

func (r LabResult) NewerThan(other LabResult) bool {
	return r.SampleID == other.SampleID && r.Revision > other.Revision
}
