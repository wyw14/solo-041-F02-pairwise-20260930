package sludge

import (
	"errors"
	"time"

	"github.com/wyw14/cry-97/internal/interlock"
	"github.com/wyw14/cry-97/internal/model"
)

type DrainOperation struct {
	LineID      model.LineID          `json:"line_id"`
	BasinID     string                `json:"basin_id"`
	Reservation interlock.Reservation `json:"reservation"`
	StartedAt   time.Time             `json:"started_at"`
}

type DrainService struct {
	arbiter *interlock.Arbiter
}

func NewDrainService(arbiter *interlock.Arbiter) (*DrainService, error) {
	if arbiter == nil {
		return nil, errors.New("drain service requires interlock arbiter")
	}
	return &DrainService{arbiter: arbiter}, nil
}

func (s *DrainService) Start(lineID model.LineID, basinID string, now time.Time) (DrainOperation, error) {
	request, err := interlock.NewRequest(
		lineID, interlock.OperationDrain, "drain-"+basinID,
		[]string{"line:" + string(lineID), "valve:drain:" + basinID}, now,
	)
	if err != nil {
		return DrainOperation{}, err
	}
	if !s.arbiter.IsAvailable(request.Resources) {
		return DrainOperation{}, errors.New("drain route is occupied")
	}
	reservation, err := s.arbiter.Reserve(request, now)
	if err != nil {
		return DrainOperation{}, err
	}
	return DrainOperation{LineID: lineID, BasinID: basinID, Reservation: reservation, StartedAt: now.UTC()}, nil
}
