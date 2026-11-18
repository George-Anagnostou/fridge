package main

import (
	"encoding/json"
	"net/http"
)

func (s *server) healthzHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if s.db == nil {
		w.WriteHeader(http.StatusServiceUnavailable)
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "error"})
		return
	}
	if err := s.db.PingContext(r.Context()); err != nil {
		w.WriteHeader(http.StatusServiceUnavailable)
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "error"})
		return
	}
	if _, err := s.db.ExecContext(r.Context(), "SELECT 1"); err != nil {
		w.WriteHeader(http.StatusServiceUnavailable)
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "error"})
		return
	}
	if err := json.NewEncoder(w).Encode(map[string]string{
		"status": "ok",
		"commit": Commit,
	}); err != nil {
		http.Error(w, "Failed to encode health check", http.StatusInternalServerError)
	}
}
