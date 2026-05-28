package api

import (
	"encoding/json"
	"net/http"

	"github.com/leomaquiaveli/gme-open-server/internal/api/handlers"
	"github.com/leomaquiaveli/gme-open-server/internal/api/middleware"
	"github.com/leomaquiaveli/gme-open-server/internal/application"
)

func NewRouter(apiKey string, encoderFn func() string, version string, composeUC *application.PipelineMediaUseCase, verticalUC *application.VerticalMediaUseCase, clipsUC *application.ClipsMediaUseCase, cutsUC *application.CutsMediaUseCase, captionUC *application.CaptionMediaUseCase, toMp3UC *application.ToMP3MediaUseCase, uploadUC *application.UploadMediaUseCase) http.Handler {
	mux := http.NewServeMux()

	// /health é público — load balancer e Cloud Run não enviam API key
	mux.Handle("GET /health", handlers.NewHealthHandler(encoderFn, version))

	// Todas as rotas /v1/* requerem autenticação
	protected := http.NewServeMux()
	protected.Handle("POST /v1/media/pipeline", handlers.NewMediaPipelineHandler(composeUC))
	protected.Handle("POST /v1/media/vertical", handlers.NewMediaVerticalHandler(verticalUC))
	protected.Handle("POST /v1/media/clips", handlers.NewMediaClipsHandler(clipsUC))
	protected.Handle("POST /v1/media/cuts", handlers.NewMediaCutsHandler(cutsUC))
	protected.Handle("POST /v1/media/caption", handlers.NewMediaCaptionHandler(captionUC))
	protected.Handle("POST /v1/media/to-mp3", handlers.NewMediaToMP3Handler(toMp3UC))
	protected.Handle("POST /v1/media/upload", handlers.NewMediaUploadHandler(uploadUC))

	// Rota não encontrada — resposta JSON em vez do padrão texto do Go
	protected.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(map[string]string{
			"error":  "route not found",
			"method": r.Method,
			"path":   r.URL.Path,
		})
	})

	mux.Handle("/", middleware.Auth(apiKey, protected))

	return mux
}
