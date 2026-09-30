package dosing

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/wyw14/cry-97/internal/model"
)

type fakeAppender struct {
	events []model.Event
}

func (f *fakeAppender) Append(_ context.Context, event model.Event) (model.Event, error) {
	event.Sequence = uint64(len(f.events) + 1)
	f.events = append(f.events, event)
	return event, nil
}

type fakePublisher struct {
	commands []model.DeviceCommand
}

func (p *fakePublisher) Publish(_ context.Context, command model.DeviceCommand) error {
	p.commands = append(p.commands, command)
	return nil
}

func newTestService(t *testing.T) (*Service, *fakeAppender, *fakePublisher) {
	t.Helper()
	appender := &fakeAppender{}
	publisher := &fakePublisher{}
	service, err := NewService(appender, publisher, NewLedger())
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	return service, appender, publisher
}

func seedRate(t *testing.T, service *Service, lineID model.LineID, batchID uuid.UUID, chemical string, rate float64, gen uint64, now time.Time) {
	t.Helper()
	change, err := NewChange(lineID, batchID, chemical, rate, gen, now)
	if err != nil {
		t.Fatalf("seed NewChange: %v", err)
	}
	service.ledger.Record(change)
}

func TestSubmitNormalChangeDispatchesImmediately(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 9, 30, 10, 0, 0, 0, time.UTC)
	service, _, publisher := newTestService(t)
	lineID := model.LineID("line-1")
	batchID := uuid.New()
	seedRate(t, service, lineID, batchID, "pac", 60, 1, now)

	result, err := service.Submit(ctx, SubmitInput{
		LineID: lineID, BatchID: batchID, Chemical: "pac", Rate: 70,
		Generation: 1, RequestedBy: "op",
	}, now)
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	if result.Command == nil || result.Command.Setpoint != 70 {
		t.Fatalf("普通变更应直接下发，got %+v", result.Command)
	}
	if result.Request.Status != StatusApproved {
		t.Fatalf("普通变更申请状态应为 approved，got %s", result.Request.Status)
	}
	if len(publisher.commands) != 1 {
		t.Fatalf("设备总线应有 1 条命令，got %d", len(publisher.commands))
	}
}

func TestHighRiskRequestHeldUntilApproved(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 9, 30, 10, 0, 0, 0, time.UTC)
	service, appender, publisher := newTestService(t)
	lineID := model.LineID("line-1")
	batchID := uuid.New()
	seedRate(t, service, lineID, batchID, "pac", 60, 1, now)

	result, err := service.Submit(ctx, SubmitInput{
		LineID: lineID, BatchID: batchID, Chemical: "pac", Rate: 150,
		Generation: 1, RequestedBy: "op", Note: "冲击负荷",
	}, now)
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	if result.Command != nil {
		t.Fatal("未审核的高风险变更不得进入设备总线")
	}
	if result.Request.Status != StatusPending || result.Request.OldRate != 60 || result.Request.NewRate != 150 {
		t.Fatalf("申请单内容不正确: %+v", result.Request)
	}
	if len(publisher.commands) != 0 {
		t.Fatalf("待审核阶段总线必须为空，实际有 %d 条命令", len(publisher.commands))
	}
	if appender.events[len(appender.events)-1].Kind != model.EventDoseRequested {
		t.Fatalf("应记录 dosing.requested 事件，got %s", appender.events[len(appender.events)-1].Kind)
	}

	approved, command, err := service.Review(ctx, result.Request.ID, true, "lead", "同意", 1, now.Add(time.Minute))
	if err != nil {
		t.Fatalf("Review: %v", err)
	}
	if command == nil || command.Setpoint != 150 || command.Action != "set" {
		t.Fatalf("批准后应下发 set=150 命令，got %+v", command)
	}
	if approved.Status != StatusApproved || approved.CommandID != command.ID {
		t.Fatalf("批准状态/命令关联不正确: %+v", approved)
	}
	if len(publisher.commands) != 1 {
		t.Fatalf("设备总线应有 1 条命令，got %d", len(publisher.commands))
	}
}

func TestRejectNeverDispatches(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 9, 30, 10, 0, 0, 0, time.UTC)
	service, _, publisher := newTestService(t)
	lineID := model.LineID("line-1")
	batchID := uuid.New()
	seedRate(t, service, lineID, batchID, "pac", 60, 1, now)

	result, err := service.Submit(ctx, SubmitInput{
		LineID: lineID, BatchID: batchID, Chemical: "pac", Rate: 150,
		Generation: 1, RequestedBy: "op",
	}, now)
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	rejected, command, err := service.Review(ctx, result.Request.ID, false, "lead", "超出工艺允许", 1, now.Add(time.Minute))
	if err != nil {
		t.Fatalf("Review: %v", err)
	}
	if command != nil || rejected.Status != StatusRejected {
		t.Fatalf("驳回不应下发命令，got command=%+v status=%s", command, rejected.Status)
	}
	if len(publisher.commands) != 0 {
		t.Fatalf("驳回后总线必须为空，实际 %d 条", len(publisher.commands))
	}
	if _, _, err := service.Review(ctx, result.Request.ID, true, "lead", "重复审核", 1, now.Add(2*time.Minute)); !errors.Is(err, ErrRequestNotPending) {
		t.Fatalf("已终结申请重复审核应返回 ErrRequestNotPending，got %v", err)
	}
}

func TestRevokeDispatchesCompensationToOldRate(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 9, 30, 10, 0, 0, 0, time.UTC)
	service, appender, publisher := newTestService(t)
	lineID := model.LineID("line-1")
	batchID := uuid.New()
	seedRate(t, service, lineID, batchID, "pac", 60, 1, now)

	result, err := service.Submit(ctx, SubmitInput{
		LineID: lineID, BatchID: batchID, Chemical: "pac", Rate: 150,
		Generation: 1, RequestedBy: "op",
	}, now)
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	if _, _, err := service.Review(ctx, result.Request.ID, true, "lead", "同意", 1, now.Add(time.Minute)); err != nil {
		t.Fatalf("Review: %v", err)
	}
	revoked, compensation, err := service.Revoke(ctx, result.Request.ID, "lead", "投加异常，立即撤回", 1, now.Add(2*time.Minute))
	if err != nil {
		t.Fatalf("Revoke: %v", err)
	}
	if revoked.Status != StatusRevoked {
		t.Fatalf("撤销后状态应为 revoked，got %s", revoked.Status)
	}
	if compensation.Action != "set" || compensation.Setpoint != 60 {
		t.Fatalf("安全补偿应恢复旧值 set=60，got action=%s setpoint=%v", compensation.Action, compensation.Setpoint)
	}
	if compensation.Metadata["compensation"] != "dose_revoke" || compensation.Metadata["request_id"] != result.Request.ID.String() {
		t.Fatalf("补偿命令缺少溯源元数据: %+v", compensation.Metadata)
	}
	if len(publisher.commands) != 2 {
		t.Fatalf("总线应有原始命令+补偿命令 2 条，got %d", len(publisher.commands))
	}
	kinds := map[model.EventKind]bool{}
	for _, event := range appender.events {
		kinds[event.Kind] = true
	}
	for _, want := range []model.EventKind{model.EventDoseRequested, model.EventDoseReviewed, model.EventDeviceCommanded, model.EventDoseRevoked} {
		if !kinds[want] {
			t.Fatalf("撤销后事件历史缺少 %s", want)
		}
	}
}

func TestRevokeFirstDoseStopsDoser(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 9, 30, 10, 0, 0, 0, time.UTC)
	service, _, publisher := newTestService(t)
	lineID := model.LineID("line-2")
	batchID := uuid.New()

	// 无历史设定，首次投加属于高风险。
	result, err := service.Submit(ctx, SubmitInput{
		LineID: lineID, BatchID: batchID, Chemical: "pam", Rate: 5,
		Generation: 1, RequestedBy: "op",
	}, now)
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	if _, _, err := service.Review(ctx, result.Request.ID, true, "lead", "", 1, now.Add(time.Minute)); err != nil {
		t.Fatalf("Review: %v", err)
	}
	_, compensation, err := service.Revoke(ctx, result.Request.ID, "lead", "", 1, now.Add(2*time.Minute))
	if err != nil {
		t.Fatalf("Revoke: %v", err)
	}
	if compensation.Action != "stop" || compensation.Setpoint != 0 {
		t.Fatalf("首次投加撤销应安全停泵，got action=%s setpoint=%v", compensation.Action, compensation.Setpoint)
	}
	if len(publisher.commands) != 2 {
		t.Fatalf("应有下发+停泵 2 条命令，got %d", len(publisher.commands))
	}
}

func TestStaleGenerationExpiresRequest(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 9, 30, 10, 0, 0, 0, time.UTC)
	service, _, publisher := newTestService(t)
	lineID := model.LineID("line-1")
	batchID := uuid.New()
	seedRate(t, service, lineID, batchID, "pac", 60, 1, now)

	result, err := service.Submit(ctx, SubmitInput{
		LineID: lineID, BatchID: batchID, Chemical: "pac", Rate: 150,
		Generation: 1, RequestedBy: "op",
	}, now)
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	// 处理线急停后代际推进到 2，旧申请必须失效且不得下发。
	if _, _, err := service.Review(ctx, result.Request.ID, true, "lead", "", 2, now.Add(time.Minute)); !errors.Is(err, ErrRequestStale) {
		t.Fatalf("代际陈旧应返回 ErrRequestStale，got %v", err)
	}
	stored, ok := service.Request(result.Request.ID)
	if !ok || stored.Status != StatusExpired {
		t.Fatalf("陈旧申请应标记 expired，got %+v ok=%v", stored, ok)
	}
	if len(publisher.commands) != 0 {
		t.Fatalf("失效申请不得下发命令，实际 %d 条", len(publisher.commands))
	}
}

func TestPendingConflictRejected(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 9, 30, 10, 0, 0, 0, time.UTC)
	service, _, _ := newTestService(t)
	lineID := model.LineID("line-1")
	batchID := uuid.New()
	seedRate(t, service, lineID, batchID, "pac", 60, 1, now)

	input := SubmitInput{LineID: lineID, BatchID: batchID, Chemical: "pac", Rate: 150, Generation: 1, RequestedBy: "op"}
	if _, err := service.Submit(ctx, input, now); err != nil {
		t.Fatalf("Submit 第一次: %v", err)
	}
	if _, err := service.Submit(ctx, input, now.Add(time.Second)); !errors.Is(err, ErrPendingConflict) {
		t.Fatalf("同处理线同药剂存在未决申请时应拒绝，got %v", err)
	}
}
