package process

import (
	"context"
	"errors"

	"github.com/wyw14/cry-97/internal/dosing"
	"github.com/wyw14/cry-97/internal/journal"
	"github.com/wyw14/cry-97/internal/model"
)

func (p *Plant) RecoverLine(ctx context.Context, lineID model.LineID) (uint64, error) {
	if snapshot, found, err := p.snapshots.Load(lineID); err != nil {
		return 0, err
	} else if found {
		p.mu.Lock()
		p.lines[lineID] = snapshot
		p.mu.Unlock()
	}
	replayer, err := journal.NewReplayer(p.store, p)
	if err != nil {
		return 0, err
	}
	last, err := replayer.ReplayLine(ctx, lineID)
	if err != nil {
		return last, err
	}
	// 急停隔离会推进代际；重放后将遗留在旧代际的未决申请置为失效，
	// 与运行时急停的内存状态保持一致。
	p.mu.RLock()
	state, ok := p.lines[lineID]
	p.mu.RUnlock()
	if ok && state.Emergency {
		p.dosing.ExpireLine(lineID, state.Generation, state.UpdatedAt)
	}
	return last, nil
}

func (p *Plant) ApplyRecovered(ctx context.Context, event model.Event) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	state, ok := p.lines[event.LineID]
	if !ok {
		return errors.New("recovery line is not configured")
	}
	if event.Sequence <= state.LastEventSeq {
		return nil
	}
	if state.Emergency && event.Generation < state.Generation {
		state.LastEventSeq = event.Sequence
		p.lines[event.LineID] = state
		return nil
	}
	if event.IsTerminal() {
		var record EmergencyRecord
		if err := event.Decode(&record); err != nil {
			return err
		}
		if record.LineID != event.LineID || record.Generation != event.Generation {
			return errors.New("emergency recovery payload does not match event identity")
		}
		state = state.Isolate(event.Sequence, event.OccurredAt)
		p.lines[event.LineID] = state
		p.devices.StopLine(event.LineID, state.Generation)
		p.dosing.ExpireLine(event.LineID, state.Generation, event.OccurredAt)
		return nil
	}
	switch event.Kind {
	case model.EventDoseRequested, model.EventDoseReviewed, model.EventDoseRevoked:
		var request dosing.Request
		if err := event.Decode(&request); err != nil {
			return err
		}
		if request.LineID != event.LineID || request.Generation != event.Generation {
			return errors.New("dosing request recovery payload does not match event identity")
		}
		p.dosing.Restore(request)
	case model.EventDosingChanged:
		var change dosing.Change
		if err := event.Decode(&change); err != nil {
			return err
		}
		if change.LineID != event.LineID || change.Generation != event.Generation {
			return errors.New("dosing change recovery payload does not match event identity")
		}
		p.dosing.RestoreChange(change)
	case model.EventDeviceCommanded:
		var command model.DeviceCommand
		if err := event.Decode(&command); err != nil {
			return err
		}
		if err := p.devices.Publish(ctx, command); err != nil {
			return err
		}
	}
	state.LastEventSeq = event.Sequence
	state.UpdatedAt = event.OccurredAt
	p.lines[event.LineID] = state
	return nil
}
