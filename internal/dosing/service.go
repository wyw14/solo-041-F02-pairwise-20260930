package dosing

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/wyw14/cry-97/internal/journal"
	"github.com/wyw14/cry-97/internal/model"
)

type CommandPublisher interface {
	Publish(context.Context, model.DeviceCommand) error
}

var (
	ErrRequestNotFound   = errors.New("dosing request is not found")
	ErrRequestNotPending = errors.New("dosing request is not awaiting review")
	ErrRequestStale      = errors.New("dosing request generation is stale")
	ErrRevokeNotAllowed  = errors.New("only an approved and dispatched dosing request can be revoked")
	ErrPendingConflict   = errors.New("chemical already has a pending change request")
)

type Service struct {
	journal   journal.Appender
	publisher CommandPublisher
	ledger    *Ledger
	requests  *RequestBook
	policy    RiskPolicy
}

func NewService(appender journal.Appender, publisher CommandPublisher, ledger *Ledger) (*Service, error) {
	if appender == nil || publisher == nil || ledger == nil {
		return nil, errors.New("dosing service dependencies are required")
	}
	return &Service{
		journal:   appender,
		publisher: publisher,
		ledger:    ledger,
		requests:  NewRequestBook(),
		policy:    DefaultRiskPolicy(),
	}, nil
}

// SubmitInput 是剂量变更申请的入参，generation 由工艺层按处理线当前代际提供。
type SubmitInput struct {
	LineID      model.LineID
	BatchID     uuid.UUID
	Chemical    string
	Rate        float64
	Generation  uint64
	RequestedBy string
	Note        string
}

// SubmitResult 描述一次提交的结果：普通变更 Command 非空并已进入设备总线；
// 高风险变更 Command 为空，Request 处于待审核状态，尚未触碰设备总线。
type SubmitResult struct {
	Request Request
	Command *model.DeviceCommand
}

// Submit 判定风险等级：普通范围内变更直接记事件并下发；高风险变更只登记申请。
func (s *Service) Submit(ctx context.Context, input SubmitInput, now time.Time) (SubmitResult, error) {
	if err := ctx.Err(); err != nil {
		return SubmitResult{}, err
	}
	if input.LineID == "" || input.BatchID == uuid.Nil || input.Chemical == "" || input.Rate <= 0 || input.Generation == 0 {
		return SubmitResult{}, errors.New("dosing change is incomplete")
	}
	oldRate := 0.0
	if previous, ok := s.ledger.CurrentRate(input.LineID, input.Chemical); ok {
		oldRate = previous.Rate
	}
	risk, reasons := s.policy.Classify(input.Chemical, oldRate, input.Rate)
	request, err := NewRequest(input.LineID, input.BatchID, input.Chemical, oldRate, input.Rate,
		input.Generation, input.RequestedBy, input.Note, risk, reasons, now)
	if err != nil {
		return SubmitResult{}, err
	}
	if risk == RiskNormal {
		// 普通范围同样登记申请事件，保持台账完整，但同步自动批准，不增加操作步骤。
		requestedEvent, err := model.NewEvent(request.LineID, request.Generation, model.EventDoseRequested, request, now)
		if err != nil {
			return SubmitResult{}, err
		}
		if _, err := s.journal.Append(ctx, requestedEvent); err != nil {
			return SubmitResult{}, fmt.Errorf("append dosing request failed: %w", err)
		}
		s.requests.Apply(request)
		command, err := s.applyRate(ctx, request, "system", "便捷范围内自动执行", now)
		if err != nil {
			return SubmitResult{}, err
		}
		request.Status = StatusApproved
		request.ReviewedBy = "system"
		request.ReviewNote = "变更处于普通范围，免审执行"
		stamp := now.UTC()
		request.ReviewedAt = &stamp
		request.CommandID = command.ID
		request.UpdatedAt = stamp
		if err := s.recordReview(ctx, request, now); err != nil {
			return SubmitResult{}, err
		}
		s.requests.Apply(request)
		return SubmitResult{Request: request, Command: &command}, nil
	}
	if pending := s.requests.Pending(); len(pending) > 0 {
		for _, existing := range pending {
			if existing.LineID == input.LineID && existing.Chemical == input.Chemical {
				return SubmitResult{}, fmt.Errorf("%w %s: %s", ErrPendingConflict, input.Chemical, existing.ID)
			}
		}
	}
	event, err := model.NewEvent(request.LineID, request.Generation, model.EventDoseRequested, request, now)
	if err != nil {
		return SubmitResult{}, err
	}
	if _, err := s.journal.Append(ctx, event); err != nil {
		return SubmitResult{}, fmt.Errorf("append dosing request failed: %w", err)
	}
	s.requests.Apply(request)
	return SubmitResult{Request: request}, nil
}

// Review 审核高风险变更：驳回只落审核记录；批准才记剂量事件、生成并下发设备命令。
func (s *Service) Review(ctx context.Context, requestID uuid.UUID, approved bool, reviewer, note string, currentGeneration uint64, now time.Time) (Request, *model.DeviceCommand, error) {
	request, ok := s.requests.Get(requestID)
	if !ok {
		return Request{}, nil, fmt.Errorf("%w: %s", ErrRequestNotFound, requestID)
	}
	if request.Status != StatusPending {
		return Request{}, nil, ErrRequestNotPending
	}
	if currentGeneration != 0 && request.Generation != currentGeneration {
		expired := request.Expire(now)
		s.requests.Apply(expired)
		return Request{}, nil, ErrRequestStale
	}
	decided, err := request.Decide(approved, reviewer, note, now)
	if err != nil {
		return Request{}, nil, err
	}
	if !approved {
		if err := s.recordReview(ctx, decided, now); err != nil {
			return Request{}, nil, err
		}
		s.requests.Apply(decided)
		return decided, nil, nil
	}
	command, err := s.applyRate(ctx, decided, reviewer, note, now)
	if err != nil {
		return Request{}, nil, err
	}
	decided.CommandID = command.ID
	if err := s.recordReview(ctx, decided, now); err != nil {
		return Request{}, nil, err
	}
	s.requests.Apply(decided)
	return decided, &command, nil
}

// Revoke 撤销已批准并已下发的变更，立即生成恢复旧值的安全补偿命令并写入总线，
// 申请单与补偿命令均保留在事件历史中。
func (s *Service) Revoke(ctx context.Context, requestID uuid.UUID, operator, note string, currentGeneration uint64, now time.Time) (Request, model.DeviceCommand, error) {
	request, ok := s.requests.Get(requestID)
	if !ok {
		return Request{}, model.DeviceCommand{}, fmt.Errorf("%w: %s", ErrRequestNotFound, requestID)
	}
	if request.Status != StatusApproved || request.CommandID == uuid.Nil {
		return Request{}, model.DeviceCommand{}, ErrRevokeNotAllowed
	}
	if currentGeneration != 0 && request.Generation != currentGeneration {
		return Request{}, model.DeviceCommand{}, ErrRequestStale
	}
	revoked, err := request.Revoke(operator, note, now)
	if err != nil {
		return Request{}, model.DeviceCommand{}, err
	}
	compensation, err := s.buildCompensation(request, operator, note, now)
	if err != nil {
		return Request{}, model.DeviceCommand{}, err
	}
	commandEvent, err := model.NewEvent(compensation.LineID, compensation.Generation, model.EventDeviceCommanded, compensation, now)
	if err != nil {
		return Request{}, model.DeviceCommand{}, err
	}
	if _, err := s.journal.Append(ctx, commandEvent); err != nil {
		return Request{}, model.DeviceCommand{}, fmt.Errorf("append compensation command failed: %w", err)
	}
	if err := s.publisher.Publish(ctx, compensation); err != nil {
		return Request{}, model.DeviceCommand{}, fmt.Errorf("publish compensation command: %w", err)
	}
	if request.OldRate > 0 {
		restore, err := NewChange(request.LineID, request.BatchID, request.Chemical, request.OldRate, request.Generation, now)
		if err != nil {
			return Request{}, model.DeviceCommand{}, err
		}
		restoredEvent, err := model.NewEvent(restore.LineID, restore.Generation, model.EventDosingChanged, restore, now)
		if err != nil {
			return Request{}, model.DeviceCommand{}, err
		}
		if _, err := s.journal.Append(ctx, restoredEvent); err != nil {
			return Request{}, model.DeviceCommand{}, fmt.Errorf("append restored dosing change failed: %w", err)
		}
		s.ledger.Record(restore)
	} else {
		s.ledger.Clear(request.LineID, request.Chemical)
	}
	revokedEvent, err := model.NewEvent(revoked.LineID, revoked.Generation, model.EventDoseRevoked, revoked, now)
	if err != nil {
		return Request{}, model.DeviceCommand{}, err
	}
	if _, err := s.journal.Append(ctx, revokedEvent); err != nil {
		return Request{}, model.DeviceCommand{}, fmt.Errorf("append dosing revocation failed: %w", err)
	}
	s.requests.Apply(revoked)
	return revoked, compensation, nil
}

// applyRate 是唯一允许把剂量设定推向设备总线的路径：先落剂量与命令事件，再发布。
func (s *Service) applyRate(ctx context.Context, request Request, operator, note string, now time.Time) (model.DeviceCommand, error) {
	change, err := NewChange(request.LineID, request.BatchID, request.Chemical, request.NewRate, request.Generation, now)
	if err != nil {
		return model.DeviceCommand{}, err
	}
	changedEvent, err := model.NewEvent(change.LineID, change.Generation, model.EventDosingChanged, change, now)
	if err != nil {
		return model.DeviceCommand{}, err
	}
	if _, err := s.journal.Append(ctx, changedEvent); err != nil {
		return model.DeviceCommand{}, fmt.Errorf("append dosing change failed: %w", err)
	}
	command, err := model.NewDeviceCommand(change.LineID, change.BatchID, "doser-"+change.Chemical, model.DeviceDoser, change.Generation, "set", now)
	if err != nil {
		return model.DeviceCommand{}, err
	}
	command.Setpoint = change.Rate
	command.Metadata["change_id"] = change.ID.String()
	command.Metadata["request_id"] = request.ID.String()
	command.Metadata["operator"] = operator
	if note != "" {
		command.Metadata["note"] = note
	}
	commandEvent, err := model.NewEvent(command.LineID, command.Generation, model.EventDeviceCommanded, command, now)
	if err != nil {
		return model.DeviceCommand{}, err
	}
	if _, err := s.journal.Append(ctx, commandEvent); err != nil {
		return model.DeviceCommand{}, fmt.Errorf("append dosing command failed: %w", err)
	}
	if err := s.publisher.Publish(ctx, command); err != nil {
		return model.DeviceCommand{}, fmt.Errorf("publish dosing command: %w", err)
	}
	s.ledger.Record(change)
	return command, nil
}

// buildCompensation 依据申请单旧值构造安全补偿命令：有旧值则恢复旧值，首次投加则安全停泵。
func (s *Service) buildCompensation(request Request, operator, note string, now time.Time) (model.DeviceCommand, error) {
	action := "stop"
	setpoint := 0.0
	if request.OldRate > 0 {
		action = "set"
		setpoint = request.OldRate
	}
	command, err := model.NewDeviceCommand(request.LineID, request.BatchID, "doser-"+request.Chemical,
		model.DeviceDoser, request.Generation, action, now)
	if err != nil {
		return model.DeviceCommand{}, err
	}
	command.Setpoint = setpoint
	command.Metadata["compensation"] = "dose_revoke"
	command.Metadata["request_id"] = request.ID.String()
	command.Metadata["revert_command_id"] = request.CommandID.String()
	command.Metadata["revert_to"] = fmt.Sprintf("%v", request.OldRate)
	command.Metadata["operator"] = operator
	if note != "" {
		command.Metadata["note"] = note
	}
	return command, nil
}

func (s *Service) recordReview(ctx context.Context, request Request, now time.Time) error {
	event, err := model.NewEvent(request.LineID, request.Generation, model.EventDoseReviewed, request, now)
	if err != nil {
		return err
	}
	if _, err := s.journal.Append(ctx, event); err != nil {
		return fmt.Errorf("append dosing review failed: %w", err)
	}
	return nil
}

func (s *Service) Request(id uuid.UUID) (Request, bool) {
	return s.requests.Get(id)
}

func (s *Service) PendingRequests() []Request {
	return s.requests.Pending()
}

func (s *Service) AllRequests() []Request {
	return s.requests.All()
}

func (s *Service) RequestHistory() []Request {
	return s.requests.History()
}

// Restore 从事件历史重建审批台账，供启动重放使用。
func (s *Service) Restore(request Request) {
	s.requests.Apply(request)
}

// ExpireLine 在处理线代际推进（如急停隔离）时，将该线所有旧代际的未决申请置为失效。
func (s *Service) ExpireLine(lineID model.LineID, currentGeneration uint64, now time.Time) []Request {
	return s.requests.ExpireGeneration(lineID, currentGeneration, now)
}

// RestoreChange 从 dosing.changed 事件重建剂量台账，供启动重放使用。
func (s *Service) RestoreChange(change Change) {
	s.ledger.Record(change)
}

func (s *Service) Latest(batchID uuid.UUID) (Change, bool) {
	return s.ledger.Latest(batchID)
}
