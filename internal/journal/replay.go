package journal

import (
	"context"
	"errors"
	"sort"

	"github.com/wyw14/cry-97/internal/model"
)

type EventApplier interface {
	ApplyRecovered(context.Context, model.Event) error
}

type Replayer struct {
	reader  Reader
	applier EventApplier
}

func NewReplayer(reader Reader, applier EventApplier) (*Replayer, error) {
	if reader == nil || applier == nil {
		return nil, errors.New("replayer requires reader and applier")
	}
	return &Replayer{reader: reader, applier: applier}, nil
}

func (r *Replayer) ReplayLine(ctx context.Context, lineID model.LineID) (uint64, error) {
	events, err := r.reader.Events(ctx, lineID)
	if err != nil {
		return 0, err
	}
	ordered := OrderByCommit(events)
	var last uint64
	for _, event := range ordered {
		if err := ctx.Err(); err != nil {
			return last, err
		}
		if event.Sequence <= last {
			return last, errors.New("journal sequence is not strictly increasing")
		}
		if err := r.applier.ApplyRecovered(ctx, event); err != nil {
			return last, err
		}
		last = event.Sequence
	}
	return last, nil
}

func OrderByCommit(events []model.Event) []model.Event {
	ordered := append([]model.Event(nil), events...)
	sort.SliceStable(ordered, func(i, j int) bool {
		if ordered[i].LineID == ordered[j].LineID {
			return ordered[i].Sequence < ordered[j].Sequence
		}
		return ordered[i].LineID < ordered[j].LineID
	})
	return ordered
}
