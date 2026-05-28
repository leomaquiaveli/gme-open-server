package config

import (
	"log"
	"os"
	"runtime"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	APIKey      string
	Port        string
	StorageType string // "gcs" ou "local"
	// GCS
	BucketName                string
	ServiceAccountCredentials string
	// Local
	LocalPath string
	// Performance
	MaxConcurrentJobs int
	CacheTTL          time.Duration
	ForceEncoder      string        // "libx264" força CPU, "" auto-detecta
	FFmpegThreads     int           // 0 = FFmpeg decide, >0 limita threads por encode
	RenderTimeout     time.Duration // timeout por job de renderização (default: 5min)
	ShutdownTimeout    time.Duration // graceful shutdown timeout (default: 30s)
	HWVideoBitrateMbps int           // bitrate alvo para encoders de hardware em Mbps (default: 6)
}

func Load() *Config {
	loadDotEnv()

	maxJobs, _ := strconv.Atoi(getEnv("MAX_CONCURRENT_JOBS", strconv.Itoa(runtime.NumCPU())))
	cacheTTLMin, _ := strconv.Atoi(getEnv("CACHE_TTL_MINUTES", "60"))
	ffmpegThreads, _ := strconv.Atoi(getEnv("FFMPEG_THREADS", "0"))
	ffmpegTimeoutMin, _ := strconv.Atoi(getEnv("RENDER_TIMEOUT_MINUTES", "5"))
	shutdownTimeoutSec, _ := strconv.Atoi(getEnv("SHUTDOWN_TIMEOUT_SECONDS", "30"))
	hwBitrateMbps, _ := strconv.Atoi(getEnv("HW_VIDEO_BITRATE_MBPS", "6"))

	return &Config{
		APIKey:                    mustGetEnv("API_KEY"),
		Port:                      getEnv("PORT", "8080"),
		StorageType:               getEnv("STORAGE_TYPE", "local"),
		BucketName:                getEnv("BUCKET_NAME", ""),
		ServiceAccountCredentials: getEnv("SERVICE_ACCOUNT_CREDENTIALS", ""),
		LocalPath:                 getEnv("LOCAL_STORAGE_PATH", "/tmp/gme"),
		MaxConcurrentJobs:         maxJobs,
		CacheTTL:                  time.Duration(cacheTTLMin) * time.Minute,
		ForceEncoder:              getEnv("ENCODER", ""),
		FFmpegThreads:             ffmpegThreads,
		RenderTimeout:             time.Duration(ffmpegTimeoutMin) * time.Minute,
		ShutdownTimeout:           time.Duration(shutdownTimeoutSec) * time.Second,
		HWVideoBitrateMbps:        hwBitrateMbps,
	}
}

// loadDotEnv lê o arquivo .env na pasta atual e seta as variáveis de ambiente.
// Não sobrescreve variáveis já definidas no sistema (comportamento padrão dotenv).
// .env é opcional — se não existir, ignora silenciosamente.
func loadDotEnv() {
	data, err := os.ReadFile(".env")
	if err != nil {
		return
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		// ignora linhas vazias e comentários
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			continue
		}
		key := strings.TrimSpace(parts[0])
		// remove comentário inline: API_KEY=abc123  # comentário
		value := strings.TrimSpace(strings.SplitN(parts[1], "#", 2)[0])
		if key != "" && os.Getenv(key) == "" {
			os.Setenv(key, value)
		}
	}
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func mustGetEnv(key string) string {
	v := os.Getenv(key)
	if v == "" {
		log.Fatalf("FATAL: required environment variable not set: %s", key)
	}
	return v
}
