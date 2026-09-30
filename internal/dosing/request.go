package dosing

import (
	"errors"
	"sort"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/wyw14/cry-97/internal/model"
)

type RequestStatus string

const (
	StatusPending  RequestStatus = "pending"
	StatusApproved RequestStatus = "approved"
	StatusRejected RequestStatus = "rejected"
	StatusRevoked  RequestStatus = "revoked"
	StatusExpired  RequestStatus = "expired"
)

// Request 是一次高风险剂量变更的申请单，完整记录旧值、新值、药剂与关联批次。
type Request struct {
	ID          uuid.UUID     `json:"id"`
	LineID      model.LineID  `json:"line_id"`
	BatchID     uuid.UUID     `json:"batch_id"`
	Chemical    string        `json:"chemical"`
	OldRate     float64       `json:"old_rate"`
	NewRate     float64       `json:"new_rate"`
	Risk        RiskLevel     `json:"risk"`
	Reasons     []string      `json:"risk_reasons,omitempty"`
	Generation  uint64        `json:"generation"`
	RequestedBy string        `json:"requested_by"`
	RequestNote string        `json:"request_note,omitempty"`
	Status      RequestStatus `json:"status"`
	ReviewedBy  string        `json:"reviewed_by,omitempty"`
	ReviewNote  string        `json:"review_note,omitempty"`
	ReviewedAt  *time.Time    `json:"reviewed_at,omitempty"`
	CommandID   uuid.UUID     `json:"command_id,omitempty"`
	CreatedAt   time.Time     `json:"created_at"`
	UpdatedAt   time.Time     `json:"updated_at"`
}

func NewRequest(lineID model.LineID, batchID uuid.UUID, chemical string, oldRate, newRate float64,
	generation uint64, requestedBy, note string, risk RiskLevel, reasons []string, now time.Time) (Request, error) {
	if lineID == "" || batchID == uuid.Nil || chemical == "" || newRate <= 0 || generation == 0 {
		return Request{}, errors.New("dosing request is incomplete")
	}
	if requestedBy == "" {
		requestedBy = "operator"
	}
	stamp := now.UTC()
	return Request{
		ID: uuid.New(), LineID: lineID, BatchID: batchID, Chemical: chemical,
		OldRate: oldRate, NewRate: newRate, Risk: risk, Reasons: reasons,
		Generation: generation, RequestedBy: requestedBy, RequestNote: note,
		Status: StatusPending, CreatedAt: stamp, UpdatedAt: stamp,
	}, nil
}

// Decide 记录审核结论。approved 为 false 时驳回申请。
func (r Request) Decide(approved bool, reviewer, note string, now time.Time) (Request, error) {
	if r.Status != StatusPending {
		return r, errors.New("dosing request is not awaiting review")
	}
	if reviewer == "" {
		return r, errors.New("reviewer is required")
	}
	r.Status = StatusRejected
	if approved {
		r.Status = StatusApproved
	}
	r.ReviewedBy = reviewer
	r.ReviewNote = note
	stamp := now.UTC()
	r.ReviewedAt = &stamp
	r.UpdatedAt = stamp
	return r, nil
}

// Revoke 撤销一份已批准且已下发的申请，恢复目标由调用方以安全补偿命令执行。
func (r Request) Revoke(reviewer, note string, now time.Time) (Request, error) {
	if r.Status != StatusApproved {
		return r, errors.New("only approved dosing requests can be revoked")
	}
	if reviewer == "" {
		return r, errors.New("revoking operator is required")
	}
	r.Status = StatusRevoked
	r.ReviewNote = note
	r.UpdatedAt = now.UTC()
	return r, nil
}

// Expire 标记因处理线代际变化（如急停隔离）而失效的未决申请。
func (r Request) Expire(now time.Time) Request {
	if r.Status != StatusPending {
		return r
	}
	r.Status = StatusExpired
	r.UpdatedAt = now.UTC()
	return r
}

func (r Request) clone() Request {
	r.Reasons = append([]string(nil), r.Reasons...)
	return r
}

// RequestBook 保存全部申请单的最新状态，并保留每次状态迁移的历史快照。
type RequestBook struct {
	mu      sync.RWMutex
	current map[uuid.UUID]Request
	history []Request
}

func NewRequestBook() *RequestBook {
	return &RequestBook{current: make(map[uuid.UUID]Request)}
}

// Apply 写入申请单快照。创建、审核、撤销、失效均通过此入口，保证历史完整。
func (b *RequestBook) Apply(request Request) {
	b.mu.Lock()
	b.current[request.ID] = request.clone()
	b.history = append(b.history, request.clone())
	b.mu.Unlock()
}

func (b *RequestBook) Get(id uuid.UUID) (Request, bool) {
	b.mu.RLock()
	defer b.mu.RUnlock()
	request, ok := b.current[id]
	return request, ok
}

func (b *RequestBook) Pending() []Request {
	b.mu.RLock()
	defer b.mu.RUnlock()
	result := make([]Request, 0)
	for _, request := range b.current {
		if request.Status == StatusPending {
			result = append(result, request)
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i].CreatedAt.Before(result[j].CreatedAt) })
	return result
}

// All 返回申请单最新状态，按创建时间升序。
func (b *RequestBook) All() []Request {
	b.mu.RLock()
	defer b.mu.RUnlock()
	result := make([]Request, 0, len(b.current))
	for _, request := range b.current {
		result = append(result, request)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].CreatedAt.Before(result[j].CreatedAt) })
	return result
}

// History 返回全部状态迁移快照（含创建、审核、撤销、失效），按时间升序。
func (b *RequestBook) History() []Request {
	b.mu.RLock()
	defer b.mu.RUnlock()
	result := make([]Request, len(b.history))
	copy(result, b.history)
	sort.SliceStable(result, func(i, j int) bool {
		if result[i].UpdatedAt.Equal(result[j].UpdatedAt) {
			return result[i].ID.String() < result[j].ID.String()
		}
		return result[i].UpdatedAt.Before(result[j].UpdatedAt)
	})
	return result
}

// ExpireGeneration 将属于指定处理线、代际早于 currentGeneration 的未决申请置为失效。
func (b *RequestBook) ExpireGeneration(lineID model.LineID, currentGeneration uint64, now time.Time) []Request {
	b.mu.Lock()
	defer b.mu.Unlock()
	expired := make([]Request, 0)
	for id, request := range b.current {
		if request.LineID == lineID && request.Status == StatusPending && request.Generation < currentGeneration {
			request = request.Expire(now)
			b.current[id] = request
			b.history = append(b.history, request)
			expired = append(expired, request)
		}
	}
	return expired
}
