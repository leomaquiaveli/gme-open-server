package handlers

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/leomaquiaveli/gme-open-server/internal/application"
)

type MediaVerticalHandler struct {
	uc *application.VerticalMediaUseCase
}

func NewMediaVerticalHandler(uc *application.VerticalMediaUseCase) *MediaVerticalHandler {
	return &MediaVerticalHandler{uc: uc}
}

func (h *MediaVerticalHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	var req application.VerticalRequest
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

func (h *MediaVerticalHandler) handleSync(w http.ResponseWriter, req application.VerticalRequest) {
	result, err := h.uc.ExecuteSync(req)
	if err != nil {
		if errors.Is(err, application.ErrAtCapacity) {
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "server at capacity, retry later"})
			return
		}
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if result.Error != "" {
		writeJSON(w, http.StatusInternalServerError, result)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (h *MediaVerticalHandler) handleAsync(w http.ResponseWriter, req application.VerticalRequest) {
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
