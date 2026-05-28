package handlers

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/leomaquiaveli/gme-open-server/internal/application"
)

type MediaToMP3Handler struct {
	uc *application.ToMP3MediaUseCase
}

func NewMediaToMP3Handler(uc *application.ToMP3MediaUseCase) *MediaToMP3Handler {
	return &MediaToMP3Handler{uc: uc}
}

func (h *MediaToMP3Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	var req application.ToMP3Request
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

func (h *MediaToMP3Handler) handleSync(w http.ResponseWriter, req application.ToMP3Request) {
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

func (h *MediaToMP3Handler) handleAsync(w http.ResponseWriter, req application.ToMP3Request) {
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
