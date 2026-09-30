package api

import (
	"context"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/wyw14/cry-97/internal/dosing"
	"github.com/wyw14/cry-97/internal/model"
)

type DoseServices interface {
	SubmitDose(ctx context.Context, lineID model.LineID, batchID uuid.UUID, chemical string, rate float64, operator, note string) (dosing.SubmitResult, error)
	ReviewDose(ctx context.Context, requestID uuid.UUID, approved bool, reviewer, note string) (dosing.Request, *model.DeviceCommand, error)
	RevokeDose(ctx context.Context, requestID uuid.UUID, operator, note string) (dosing.Request, model.DeviceCommand, error)
	DoseRequest(requestID uuid.UUID) (dosing.Request, bool)
	DoseRequests() []dosing.Request
	PendingDoseRequests() []dosing.Request
	DoseRequestHistory() []dosing.Request
}

type DoseHandler struct {
	plant DoseServices
}

func NewDoseHandler(plant DoseServices) *DoseHandler {
	return &DoseHandler{plant: plant}
}

type doseRequest struct {
	LineID   string  `json:"line_id"`
	BatchID  string  `json:"batch_id"`
	Chemical string  `json:"chemical"`
	Rate     float64 `json:"rate"`
	Operator string  `json:"operator"`
	Note     string  `json:"note"`
}

type doseReviewRequest struct {
	Approved bool   `json:"approved"`
	Reviewer string `json:"reviewer"`
	Note     string `json:"note"`
}

// submit 提交剂量变更：普通范围直接执行，高风险返回待审核申请（不下发设备）。
func (h *DoseHandler) submit(w http.ResponseWriter, r *http.Request) {
	if h.plant == nil {
		writeError(w, http.StatusServiceUnavailable, errors.New("dosing controller is unavailable"))
		return
	}
	var input doseRequest
	if err := decodeJSON(r, &input); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	lineID, err := model.ParseLineID(input.LineID)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	batchID, err := uuid.Parse(input.BatchID)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	result, err := h.plant.SubmitDose(r.Context(), lineID, batchID, input.Chemical, input.Rate, input.Operator, input.Note)
	if err != nil {
		writeDoseError(w, err)
		return
	}
	status := http.StatusAccepted
	body := map[string]any{"request": result.Request}
	if result.Command != nil {
		// 普通范围变更已直接进入设备总线。
		status = http.StatusOK
		body["command"] = result.Command
		body["dispatched"] = true
	} else {
		status = http.StatusCreated
		body["dispatched"] = false
	}
	writeJSON(w, status, body)
}

func (h *DoseHandler) list(w http.ResponseWriter, r *http.Request) {
	body := map[string]any{
		"requests": h.plant.DoseRequests(),
		"pending":  h.plant.PendingDoseRequests(),
	}
	if r.URL.Query().Has("history") {
		body["history"] = h.plant.DoseRequestHistory()
	}
	writeJSON(w, http.StatusOK, body)
}

func (h *DoseHandler) review(w http.ResponseWriter, r *http.Request) {
	requestID, err := uuid.Parse(chi.URLParam(r, "requestID"))
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	var input doseReviewRequest
	if err := decodeJSON(r, &input); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if input.Reviewer == "" {
		writeError(w, http.StatusBadRequest, errors.New("reviewer is required"))
		return
	}
	request, command, err := h.plant.ReviewDose(r.Context(), requestID, input.Approved, input.Reviewer, input.Note)
	if err != nil {
		writeDoseError(w, err)
		return
	}
	body := map[string]any{"request": request}
	if command != nil {
		body["command"] = command
		body["dispatched"] = true
	} else {
		body["dispatched"] = false
	}
	writeJSON(w, http.StatusOK, body)
}

func (h *DoseHandler) revoke(w http.ResponseWriter, r *http.Request) {
	requestID, err := uuid.Parse(chi.URLParam(r, "requestID"))
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	var input doseReviewRequest
	if err := decodeJSON(r, &input); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if input.Reviewer == "" {
		writeError(w, http.StatusBadRequest, errors.New("revoking operator is required"))
		return
	}
	request, command, err := h.plant.RevokeDose(r.Context(), requestID, input.Reviewer, input.Note)
	if err != nil {
		writeDoseError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"request": request, "compensation_command": command})
}

func (h *DoseHandler) get(w http.ResponseWriter, r *http.Request) {
	requestID, err := uuid.Parse(chi.URLParam(r, "requestID"))
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	request, ok := h.plant.DoseRequest(requestID)
	if !ok {
		writeError(w, http.StatusNotFound, dosing.ErrRequestNotFound)
		return
	}
	writeJSON(w, http.StatusOK, request)
}

func writeDoseError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, dosing.ErrRequestNotFound):
		writeError(w, http.StatusNotFound, err)
	case errors.Is(err, dosing.ErrRequestStale), errors.Is(err, dosing.ErrRevokeNotAllowed),
		errors.Is(err, dosing.ErrRequestNotPending), errors.Is(err, dosing.ErrPendingConflict):
		writeError(w, http.StatusConflict, err)
	default:
		writeError(w, http.StatusServiceUnavailable, err)
	}
}
