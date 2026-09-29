package api

import (
	"io/fs"
	"net/http"
	"path/filepath"
)

func (s *Server) page(name string) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		data, err := fs.ReadFile(s.web, filepath.ToSlash(name))
		if err != nil {
			writeError(w, http.StatusNotFound, err)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(data)
	}
}
