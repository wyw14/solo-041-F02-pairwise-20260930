package sensor

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/wyw14/cry-97/internal/journal"
	"github.com/wyw14/cry-97/internal/model"
)

type Cursor interface {
	Current(model.LineID) uint64
	Commit(model.LineID, uint64) error
}

type Receiver struct {
	journal journal.Appender
	cursor  Cursor
}

func NewReceiver(appender journal.Appender, cursor Cursor) (*Receiver, error) {
	if appender == nil || cursor == nil {
		return nil, errors.New("sample receiver requires journal and cursor")
	}
	return &Receiver{journal: appender, cursor: cursor}, nil
}

func (r *Receiver) Receive(ctx context.Context, sample model.Sample, now time.Time) error {
	if !sample.Finite() || sample.Sequence == 0 || sample.LineID == "" {
		return errors.New("sample is invalid")
	}
	current := r.cursor.Current(sample.LineID)
	if sample.Sequence <= current {
		return nil
	}
	if sample.Sequence != current+1 {
		return fmt.Errorf("sample sequence gap: current=%d received=%d", current, sample.Sequence)
	}
	event, err := model.NewEvent(sample.LineID, 1, model.EventSampleStored, sample, now)
	if err != nil {
		return err
	}
	if _, err := r.journal.Append(ctx, event); err != nil {
		return fmt.Errorf("store sample %d failed: %w", sample.Sequence, err)
	}
	if err := r.cursor.Commit(sample.LineID, sample.Sequence); err != nil {
		return fmt.Errorf("commit sample cursor: %w", err)
	}
	return nil
}

func (r *Receiver) Current(lineID model.LineID) uint64 {
	return r.cursor.Current(lineID)
}
