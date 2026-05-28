package handlers

import (
	"encoding/json"
	"net/http"
)

type HealthHandler struct {
	encoderFn func() string
	version   string
}

func NewHealthHandler(encoderFn func() string, version string) *HealthHandler {
	return &HealthHandler{encoderFn: encoderFn, version: version}
}

func (h *HealthHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"status":  "ok",
		"gpu":     h.encoderFn(),
		"version": h.version,
	})
}
