package handlers

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/leomaquiaveli/gme-open-server/internal/application"
)

type MediaCutsHandler struct {
	uc *application.CutsMediaUseCase
}

func NewMediaCutsHandler(uc *application.CutsMediaUseCase) *MediaCutsHandler {
	return &MediaCutsHandler{uc: uc}
}

func (h *MediaCutsHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	var req application.CutsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid json: " + err.Error()})
		return
	}

	if req.WebhookURL == "" {
		h.handleSync(w, req)
		return
	}
	h.handleAsync(w, req)
}

func (h *MediaCutsHandler) handleSync(w http.ResponseWriter, req application.CutsRequest) {
	result, err := h.uc.ExecuteSync(req)
	if err != nil {
		if errors.Is(err, application.ErrAtCapacity) {
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "server at capacity, retry later"})
			return
		}
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (h *MediaCutsHandler) handleAsync(w http.ResponseWriter, req application.CutsRequest) {
	jobID, err := h.uc.Execute(req)
	if err != nil {
		if errors.Is(err, application.ErrAtCapacity) {
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "server at capacity, retry later"})
			return
		}
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]string{
		"job_id": jobID,
		"status": "queued",
	})
}
