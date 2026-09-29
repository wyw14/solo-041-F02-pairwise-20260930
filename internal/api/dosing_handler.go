package api

import (
	"context"
	"errors"
	"net/http"

	"github.com/google/uuid"
	"github.com/wyw14/cry-97/internal/model"
)

type DoseChanger interface {
	ChangeDose(context.Context, model.LineID, uuid.UUID, string, float64) (model.DeviceCommand, error)
}

type DoseHandler struct {
	changer DoseChanger
}

func NewDoseHandler(changer DoseChanger) *DoseHandler {
	return &DoseHandler{changer: changer}
}

type doseRequest struct {
	LineID   string  `json:"line_id"`
	BatchID  string  `json:"batch_id"`
	Chemical string  `json:"chemical"`
	Rate     float64 `json:"rate"`
}

func (h *DoseHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if h.changer == nil {
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
	command, err := h.changer.ChangeDose(r.Context(), lineID, batchID, input.Chemical, input.Rate)
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, err)
		return
	}
	writeJSON(w, http.StatusAccepted, command)
}
