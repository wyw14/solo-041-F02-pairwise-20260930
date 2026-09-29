package journal

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/wyw14/cry-97/internal/model"
)

type SnapshotStore struct {
	dir string
	mu  sync.Mutex
}

func NewSnapshotStore(dir string) (*SnapshotStore, error) {
	if dir == "" {
		return nil, errors.New("snapshot directory is required")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	return &SnapshotStore{dir: dir}, nil
}

func (s *SnapshotStore) Save(state model.LineState) error {
	if state.ID == "" {
		return errors.New("snapshot line is required")
	}
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	path := filepath.Join(s.dir, string(state.ID)+".json")
	temporary := path + ".next"
	if err := os.WriteFile(temporary, append(data, '\n'), 0o640); err != nil {
		return err
	}
	return os.Rename(temporary, path)
}

func (s *SnapshotStore) Load(lineID model.LineID) (model.LineState, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	path := filepath.Join(s.dir, string(lineID)+".json")
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return model.LineState{}, false, nil
	}
	if err != nil {
		return model.LineState{}, false, err
	}
	var state model.LineState
	if err := json.Unmarshal(data, &state); err != nil {
		return model.LineState{}, false, fmt.Errorf("decode line snapshot: %w", err)
	}
	return state, true, nil
}
