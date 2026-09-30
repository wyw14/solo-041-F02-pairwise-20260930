package process

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/wyw14/cry-97/internal/aeration"
	"github.com/wyw14/cry-97/internal/dosing"
	"github.com/wyw14/cry-97/internal/model"
	"github.com/wyw14/cry-97/internal/settling"
)

func (p *Plant) SubmitAeration(ctx context.Context, lineID model.LineID, basinID string, payload []byte) (aeration.Window, *model.DeviceCommand, error) {
	p.mu.RLock()
	state, ok := p.lines[lineID]
	p.mu.RUnlock()
	if !ok || state.Emergency {
		return aeration.Window{}, nil, errors.New("process line cannot accept aeration samples")
	}
	return p.aeration.SubmitWindow(ctx, lineID, basinID, payload, state.Generation, p.now())
}

func (p *Plant) SubmitDose(ctx context.Context, lineID model.LineID, batchID uuid.UUID, chemical string, rate float64, operator, note string) (dosing.SubmitResult, error) {
	p.mu.RLock()
	state, ok := p.lines[lineID]
	p.mu.RUnlock()
	if !ok || state.Emergency {
		return dosing.SubmitResult{}, errors.New("process line cannot change dosing")
	}
	return p.dosing.Submit(ctx, dosing.SubmitInput{
		LineID:      lineID,
		BatchID:     batchID,
		Chemical:    chemical,
		Rate:        rate,
		Generation:  state.Generation,
		RequestedBy: operator,
		Note:        note,
	}, p.now())
}

// ReviewDose 审核高风险剂量变更。批准的变更在此刻才进入设备总线。
func (p *Plant) ReviewDose(ctx context.Context, requestID uuid.UUID, approved bool, reviewer, note string) (dosing.Request, *model.DeviceCommand, error) {
	request, ok := p.dosing.Request(requestID)
	if !ok {
		return dosing.Request{}, nil, errors.New("dosing request is not found")
	}
	p.mu.RLock()
	state, exists := p.lines[request.LineID]
	p.mu.RUnlock()
	if !exists || state.Emergency {
		return dosing.Request{}, nil, errors.New("process line cannot accept dosing review")
	}
	return p.dosing.Review(ctx, requestID, approved, reviewer, note, state.Generation, p.now())
}

// RevokeDose 撤销已下发的高风险变更，并向总线发布恢复旧值的安全补偿命令。
func (p *Plant) RevokeDose(ctx context.Context, requestID uuid.UUID, operator, note string) (dosing.Request, model.DeviceCommand, error) {
	request, ok := p.dosing.Request(requestID)
	if !ok {
		return dosing.Request{}, model.DeviceCommand{}, errors.New("dosing request is not found")
	}
	p.mu.RLock()
	state, exists := p.lines[request.LineID]
	p.mu.RUnlock()
	if !exists {
		return dosing.Request{}, model.DeviceCommand{}, errors.New("process line is not found")
	}
	return p.dosing.Revoke(ctx, requestID, operator, note, state.Generation, p.now())
}

func (p *Plant) DoseRequest(requestID uuid.UUID) (dosing.Request, bool) {
	return p.dosing.Request(requestID)
}

func (p *Plant) DoseRequests() []dosing.Request {
	return p.dosing.AllRequests()
}

func (p *Plant) PendingDoseRequests() []dosing.Request {
	return p.dosing.PendingRequests()
}

func (p *Plant) DoseRequestHistory() []dosing.Request {
	return p.dosing.RequestHistory()
}

func (p *Plant) UpdateBlanket(lineID model.LineID, level float64) (settling.Snapshot, error) {
	state, ok := p.settling[lineID]
	if !ok {
		return settling.Snapshot{}, errors.New("settling line is not found")
	}
	return state.UpdateBlanket(level, 2.8, p.now()), nil
}

func (p *Plant) ReceiveSample(ctx context.Context, sample model.Sample) error {
	if err := p.receiver.Receive(ctx, sample, p.now()); err != nil {
		return err
	}
	return p.sampler.Publish(sample)
}

func (p *Plant) BeginSettling(lineID model.LineID) (settling.Cycle, error) {
	state, ok := p.settling[lineID]
	if !ok {
		return settling.Cycle{}, errors.New("settling line is not found")
	}
	state.BeginCycle(p.now())
	return p.cycles.Start(lineID, p.now())
}

func (p *Plant) AdvanceSettling(lineID model.LineID) (settling.Cycle, error) {
	return p.cycles.Advance(lineID, p.now())
}

func (p *Plant) Alarms() []model.Alarm {
	return p.alarms.All()
}

func (p *Plant) StatusBlowers() []aeration.Blower {
	return p.aeration.Blowers()
}

func (p *Plant) StatusAerationWindows() []aeration.Window {
	return p.aeration.Windows()
}

func (p *Plant) AcknowledgeAlarm(id uuid.UUID) (model.Alarm, error) {
	return p.alarms.Acknowledge(id)
}

func (p *Plant) RecoverAlarm(id uuid.UUID) (model.Alarm, error) {
	return p.alarms.Recover(id, p.now())
}

func (p *Plant) Clock() time.Time {
	return p.now().UTC()
}
