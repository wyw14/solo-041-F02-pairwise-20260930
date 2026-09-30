package process

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/wyw14/cry-97/internal/dosing"
	"github.com/wyw14/cry-97/internal/model"
)

func newTestPlant(t *testing.T) (*Plant, model.LineID, uuid.UUID, time.Time) {
	t.Helper()
	now := time.Date(2026, 9, 30, 10, 0, 0, 0, time.UTC)
	clock := func() time.Time { return now }
	plant, err := NewPlant(t.TempDir(), []model.LineID{"line-1"}, clock)
	if err != nil {
		t.Fatalf("NewPlant: %v", err)
	}
	return plant, "line-1", uuid.New(), now
}

func TestPlantHighRiskFlowHeldThenApproved(t *testing.T) {
	ctx := context.Background()
	plant, lineID, batchID, _ := newTestPlant(t)

	// 首次投加无旧值，属于高风险。
	result, err := plant.SubmitDose(ctx, lineID, batchID, "pac", 60, "op-a", "")
	if err != nil {
		t.Fatalf("SubmitDose: %v", err)
	}
	if result.Command != nil {
		t.Fatal("未审核的高风险变更不得进入设备总线")
	}
	if result.Request.Risk != dosing.RiskHigh || result.Request.Status != dosing.StatusPending {
		t.Fatalf("申请应为高风险待审核，got risk=%s status=%s", result.Request.Risk, result.Request.Status)
	}
	if before := plant.devices.All(); len(before) != 0 {
		t.Fatalf("待审核阶段设备总线应为空，got %d", len(before))
	}

	approved, command, err := plant.ReviewDose(ctx, result.Request.ID, true, "lead-a", "同意")
	if err != nil {
		t.Fatalf("ReviewDose: %v", err)
	}
	if command == nil || command.Setpoint != 60 {
		t.Fatalf("批准后应下发 set=60，got %+v", command)
	}
	if approved.OldRate != 0 || approved.NewRate != 60 || approved.Chemical != "pac" || approved.BatchID != batchID {
		t.Fatalf("申请单旧值/新值/药剂/批次记录不完整: %+v", approved)
	}

	// 已下发撤销：首次投加无旧值，补偿命令必须是安全停泵。
	revoked, compensation, err := plant.RevokeDose(ctx, result.Request.ID, "lead-a", "现场撤回")
	if err != nil {
		t.Fatalf("RevokeDose: %v", err)
	}
	if revoked.Status != dosing.StatusRevoked || compensation.Action != "stop" {
		t.Fatalf("撤销应生成停泵补偿，got status=%s action=%s", revoked.Status, compensation.Action)
	}
	if compensation.Metadata["compensation"] != "dose_revoke" || compensation.Metadata["revert_command_id"] != command.ID.String() {
		t.Fatalf("补偿命令缺少溯源信息: %+v", compensation.Metadata)
	}
}

func TestPlantNormalRangeStaysConvenient(t *testing.T) {
	ctx := context.Background()
	plant, lineID, batchID, _ := newTestPlant(t)

	// 首次投加高风险，审核后建立 60 的当前设定。
	first, err := plant.SubmitDose(ctx, lineID, batchID, "pac", 60, "op-a", "")
	if err != nil {
		t.Fatalf("SubmitDose first: %v", err)
	}
	if _, _, err := plant.ReviewDose(ctx, first.Request.ID, true, "lead-a", ""); err != nil {
		t.Fatalf("ReviewDose first: %v", err)
	}

	// 60→70 在 pac 便捷范围（5~120，跳变≤35%）内，应直接下发，无需审核。
	second, err := plant.SubmitDose(ctx, lineID, batchID, "pac", 70, "op-a", "微调")
	if err != nil {
		t.Fatalf("SubmitDose second: %v", err)
	}
	if second.Command == nil || second.Request.Status != dosing.StatusApproved {
		t.Fatalf("普通范围变更应免审直接下发，got %+v", second)
	}
}

func TestPlantEmergencyExpiresPendingRequest(t *testing.T) {
	ctx := context.Background()
	plant, lineID, batchID, _ := newTestPlant(t)

	result, err := plant.SubmitDose(ctx, lineID, batchID, "pac", 60, "op-a", "")
	if err != nil {
		t.Fatalf("SubmitDose: %v", err)
	}
	if _, err := plant.EmergencyStop(ctx, lineID, "operator safety check"); err != nil {
		t.Fatalf("EmergencyStop: %v", err)
	}
	// 急停推进代际，未决申请应立即失效；审核入口在隔离态也被拒绝。
	stored, ok := plant.DoseRequest(result.Request.ID)
	if !ok || stored.Status != dosing.StatusExpired {
		t.Fatalf("急停后未决申请应失效，got %+v ok=%v", stored, ok)
	}
	if _, _, err := plant.ReviewDose(ctx, result.Request.ID, true, "lead-a", ""); err == nil {
		t.Fatal("隔离处理线不得继续审核加药申请")
	}
	if commands := plant.devices.All(); len(commands) != 0 {
		t.Fatalf("失效申请不得进入设备总线，got %d 条", len(commands))
	}
}

func TestPlantDoseRequestHistoryRetained(t *testing.T) {
	ctx := context.Background()
	plant, lineID, batchID, _ := newTestPlant(t)

	result, err := plant.SubmitDose(ctx, lineID, batchID, "pac", 60, "op-a", "")
	if err != nil {
		t.Fatalf("SubmitDose: %v", err)
	}
	if _, _, err := plant.ReviewDose(ctx, result.Request.ID, true, "lead-a", "同意"); err != nil {
		t.Fatalf("ReviewDose: %v", err)
	}
	if _, _, err := plant.RevokeDose(ctx, result.Request.ID, "lead-a", "撤回"); err != nil {
		t.Fatalf("RevokeDose: %v", err)
	}
	history := plant.DoseRequestHistory()
	if len(history) < 3 {
		t.Fatalf("历史应保留创建/批准/撤销快照至少 3 条，got %d", len(history))
	}
	last := history[len(history)-1]
	if last.Status != dosing.StatusRevoked {
		t.Fatalf("最后一条历史应为撤销，got %s", last.Status)
	}
}
