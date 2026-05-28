package handlers

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/leomaquiaveli/gme-open-server/internal/application"
)

type MediaPipelineHandler struct {
	uc *application.PipelineMediaUseCase
}

func NewMediaPipelineHandler(uc *application.PipelineMediaUseCase) *MediaPipelineHandler {
	return &MediaPipelineHandler{uc: uc}
}

func (h *MediaPipelineHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	var req application.PipelineRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid json: " + err.Error()})
		return
	}

	// Sem webhook_url → modo síncrono: bloqueia até o job terminar e retorna o resultado direto.
	// Com webhook_url → modo assíncrono: retorna 202 imediatamente, resultado vai para o webhook.
	if req.WebhookURL == "" {
		h.handleSync(w, req)
		return
	}
	h.handleAsync(w, req)
}

func (h *MediaPipelineHandler) handleSync(w http.ResponseWriter, req application.PipelineRequest) {
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

func (h *MediaPipelineHandler) handleAsync(w http.ResponseWriter, req application.PipelineRequest) {
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

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(v)
}
