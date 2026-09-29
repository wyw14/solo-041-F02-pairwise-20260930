package interlock

import (
	"errors"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/wyw14/cry-97/internal/model"
)

type Operation string

const (
	OperationBackwash Operation = "backwash"
	OperationDrain    Operation = "drain"
)

type Request struct {
	ID        uuid.UUID    `json:"id"`
	LineID    model.LineID `json:"line_id"`
	Operation Operation    `json:"operation"`
	RouteID   string       `json:"route_id"`
	Resources []string     `json:"resources"`
	CreatedAt time.Time    `json:"created_at"`
}

func NewRequest(lineID model.LineID, operation Operation, routeID string, resources []string, now time.Time) (Request, error) {
	if lineID == "" || routeID == "" || len(resources) == 0 {
		return Request{}, errors.New("interlock request is incomplete")
	}
	if operation != OperationBackwash && operation != OperationDrain {
		return Request{}, errors.New("interlock operation is unsupported")
	}
	normalized := make([]string, 0, len(resources))
	seen := make(map[string]struct{})
	for _, resource := range resources {
		resource = strings.TrimSpace(resource)
		if resource == "" {
			return Request{}, errors.New("interlock resource is empty")
		}
		if _, exists := seen[resource]; exists {
			continue
		}
		seen[resource] = struct{}{}
		normalized = append(normalized, resource)
	}
	sort.Strings(normalized)
	return Request{
		ID: uuid.New(), LineID: lineID, Operation: operation, RouteID: routeID,
		Resources: normalized, CreatedAt: now.UTC(),
	}, nil
}

type Reservation struct {
	Request    Request   `json:"request"`
	ReservedAt time.Time `json:"reserved_at"`
}
