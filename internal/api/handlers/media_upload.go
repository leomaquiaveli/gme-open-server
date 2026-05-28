package handlers

import (
	"encoding/json"
	"net/http"

	"github.com/leomaquiaveli/gme-open-server/internal/application"
)

type MediaUploadHandler struct {
	uc *application.UploadMediaUseCase
}

func NewMediaUploadHandler(uc *application.UploadMediaUseCase) *MediaUploadHandler {
	return &MediaUploadHandler{uc: uc}
}

func (h *MediaUploadHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Parse req: json com file_url OU multipart form com file
	contentType := r.Header.Get("Content-Type")

	var req application.UploadRequest

	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	// Verifica se é multipart
	if r.MultipartForm != nil || r.Header.Get("Content-Type") != "" && len(contentType) >= 19 && contentType[:19] == "multipart/form-data" {
		err := r.ParseMultipartForm(1024 << 20) // 1GB max memory
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "failed to parse multipart form: " + err.Error()})
			return
		}

		req.FileName = r.FormValue("filename")
		req.ContentType = r.FormValue("mime_type")
		
		// Pode ter passado file_url num campo de texto do form
		req.FileURL = r.FormValue("file_url")

		file, handler, err := r.FormFile("file")
		if err == nil {
			defer file.Close()
			req.File = file
			if req.FileName == "" {
				req.FileName = handler.Filename
			}
		} else if req.FileURL == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "must provide 'file' or 'file_url' in form"})
			return
		}
	} else {
		// É JSON
		var jsonReq struct {
			FileURL  string `json:"file_url"`
			FileName string `json:"filename"`
		}
		if err := json.NewDecoder(r.Body).Decode(&jsonReq); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid json: " + err.Error()})
			return
		}
		req.FileURL = jsonReq.FileURL
		req.FileName = jsonReq.FileName

		if req.FileURL == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "file_url is required when using JSON"})
			return
		}
	}

	// Executa Use Case
	result, err := h.uc.Execute(req)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	writeJSON(w, http.StatusOK, result)
}
