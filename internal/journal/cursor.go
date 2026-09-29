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

type CursorStore struct {
	path string
	mu   sync.Mutex
	data map[model.LineID]uint64
}

func NewCursorStore(path string) (*CursorStore, error) {
	if path == "" {
		return nil, errors.New("cursor path is required")
	}
	store := &CursorStore{path: path, data: make(map[model.LineID]uint64)}
	if err := store.load(); err != nil {
		return nil, err
	}
	return store, nil
}

func (s *CursorStore) Current(lineID model.LineID) uint64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.data[lineID]
}

func (s *CursorStore) Commit(lineID model.LineID, sequence uint64) error {
	if lineID == "" || sequence == 0 {
		return errors.New("cursor identity is incomplete")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if sequence <= s.data[lineID] {
		return nil
	}
	next := make(map[model.LineID]uint64, len(s.data)+1)
	for key, value := range s.data {
		next[key] = value
	}
	next[lineID] = sequence
	if err := writeCursorFile(s.path, next); err != nil {
		return err
	}
	s.data = next
	return nil
}

func (s *CursorStore) load() error {
	data, err := os.ReadFile(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if err := json.Unmarshal(data, &s.data); err != nil {
		return fmt.Errorf("decode cursor file: %w", err)
	}
	return nil
}

func writeCursorFile(path string, value map[model.LineID]uint64) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	temporary := path + ".next"
	if err := os.WriteFile(temporary, append(data, '\n'), 0o640); err != nil {
		return err
	}
	return os.Rename(temporary, path)
}
