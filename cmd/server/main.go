package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/leomaquiaveli/gme-open-server/internal/api"
	"github.com/leomaquiaveli/gme-open-server/internal/application"
	"github.com/leomaquiaveli/gme-open-server/internal/domain/ports"
	"github.com/leomaquiaveli/gme-open-server/internal/infra/ffmpeg"
	"github.com/leomaquiaveli/gme-open-server/internal/infra/storage"
	"github.com/leomaquiaveli/gme-open-server/internal/infra/webhook"
	"github.com/leomaquiaveli/gme-open-server/pkg/config"
)

const version = "0.1.0"

func main() {
	cfg := config.Load()
	log.Printf("GME Open Server v%s — startup", version)
	log.Printf("storage: %s | port: %s | max_concurrent_jobs: %d | render_timeout: %s", cfg.StorageType, cfg.Port, cfg.MaxConcurrentJobs, cfg.RenderTimeout)

	fileCache := ffmpeg.NewFileCache(cfg.CacheTTL)

	var store ports.IStorage
	var storeErr error
	switch cfg.StorageType {
	case "gcs":
		if cfg.BucketName == "" {
			log.Fatal("FATAL: STORAGE_TYPE=gcs requires BUCKET_NAME")
		}
		if cfg.ServiceAccountCredentials == "" {
			log.Printf("GCS auth: Application Default Credentials (metadata server)")
		} else {
			log.Printf("GCS auth: service account key")
		}
		log.Printf("connecting to GCS bucket: %s", cfg.BucketName)
		store, storeErr = storage.NewGCSStorage(cfg.BucketName, cfg.ServiceAccountCredentials)
	default:
		store, storeErr = storage.NewLocalStorage(cfg.LocalPath)
	}
	if storeErr != nil {
		log.Fatalf("FATAL: storage init error: %v", storeErr)
	}
	log.Printf("storage ready")

	// Inicia com CPU imediatamente — detecta GPU em background sem bloquear o startup.
	runner := ffmpeg.NewRunner(ffmpeg.ModeCPU, cfg.RenderTimeout, cfg.HWVideoBitrateMbps)
	go func() {
		gpu := ffmpeg.DetectOrOverride(cfg.ForceEncoder)
		runner.SetEncoder(gpu)
		log.Printf("encoder: %s", gpu)
	}()

	webhookSender := webhook.NewHTTPSender()

	pipelineUC := application.NewPipelineMediaUseCase(
		fileCache,
		store,
		runner,
		webhookSender,
		cfg.MaxConcurrentJobs,
		cfg.LocalPath,
	)

	verticalUC := application.NewVerticalMediaUseCase(
		fileCache,
		store,
		runner,
		webhookSender,
		cfg.MaxConcurrentJobs,
		cfg.LocalPath,
		cfg.FFmpegThreads,
	)

	clipsUC := application.NewClipsMediaUseCase(
		fileCache,
		store,
		runner,
		webhookSender,
		cfg.MaxConcurrentJobs,
		cfg.LocalPath,
		cfg.FFmpegThreads,
	)

	cutsUC := application.NewCutsMediaUseCase(
		fileCache,
		store,
		runner,
		webhookSender,
		cfg.MaxConcurrentJobs,
		cfg.LocalPath,
	)

	captionUC := application.NewCaptionMediaUseCase(
		fileCache,
		store,
		runner,
		webhookSender,
		cfg.MaxConcurrentJobs,
		cfg.LocalPath,
		cfg.FFmpegThreads,
	)

	toMp3UC := application.NewToMP3MediaUseCase(pipelineUC)
	uploadUC := application.NewUploadMediaUseCase(store, cfg.LocalPath)

	router := api.NewRouter(cfg.APIKey, runner.GetEncoder, version, pipelineUC, verticalUC, clipsUC, cutsUC, captionUC, toMp3UC, uploadUC)
	addr := fmt.Sprintf(":%s", cfg.Port)

	srv := &http.Server{
		Addr:         addr,
		Handler:      router,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 15 * time.Minute, // sync jobs podem esperar fila — N jobs × render_timeout
		IdleTimeout:  120 * time.Second,
	}

	go func() {
		log.Printf("listening on %s", addr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("server error: %v", err)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGTERM, syscall.SIGINT)
	<-quit

	log.Printf("shutting down (%s timeout)...", cfg.ShutdownTimeout)
	ctx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		log.Printf("shutdown error: %v", err)
	}
	log.Println("server stopped")
}
