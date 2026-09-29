package lab

import (
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/wyw14/cry-97/internal/model"
)

type wireResult struct {
	SampleID string  `json:"sample_id"`
	LineID   string  `json:"line_id"`
	BatchID  string  `json:"batch_id"`
	Revision uint64  `json:"revision"`
	COD      float64 `json:"cod"`
	Ammonia  float64 `json:"ammonia"`
}

func Decode(payload []byte, now time.Time) (model.LabResult, error) {
	var wire wireResult
	if err := json.Unmarshal(payload, &wire); err != nil {
		return model.LabResult{ReceivedAt: now.UTC(), Valid: false}, err
	}
	sampleID, err := uuid.Parse(wire.SampleID)
	if err != nil {
		return model.LabResult{ReceivedAt: now.UTC(), Valid: false}, errors.New("sample id is invalid")
	}
	batchID, err := uuid.Parse(wire.BatchID)
	if err != nil {
		return model.LabResult{SampleID: sampleID, ReceivedAt: now.UTC(), Valid: false}, errors.New("batch id is invalid")
	}
	lineID, err := model.ParseLineID(wire.LineID)
	if err != nil || wire.Revision == 0 {
		return model.LabResult{SampleID: sampleID, BatchID: batchID, ReceivedAt: now.UTC(), Valid: false}, errors.New("lab result identity is incomplete")
	}
	return model.LabResult{
		SampleID: sampleID, LineID: lineID, BatchID: batchID, Revision: wire.Revision,
		COD: wire.COD, Ammonia: wire.Ammonia, Valid: true, ReceivedAt: now.UTC(),
	}, nil
}
