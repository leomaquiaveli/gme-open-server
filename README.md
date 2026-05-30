# GME Open Server

**A high-performance media processing server you can self-host and call from any language.**  
Built in Go, powered by FFmpeg, designed for production video pipelines.

By [Leonardo Maquiaveli](https://github.com/leomaquiaveli) · [Growth Media Engine](https://github.com/leomaquiaveli)

[![Go 1.24](https://img.shields.io/badge/Go-1.24-00ADD8?logo=go)](https://go.dev/)
[![License MIT](https://img.shields.io/badge/License-MIT-green.svg)](LICENSE)
[![Zero deps](https://img.shields.io/badge/dependencies-zero-brightgreen)](go.mod)

[🇧🇷 Versão em Português](README.pt-BR.md)

---

## What is this?

GME Open Server is an HTTP server that accepts video processing jobs via a simple JSON API and delivers results via webhook or synchronous response.

You send a request describing what you want — cut a video, convert to vertical 9:16, generate multiple clips from a single source — and the server handles the rest: downloading files, processing them with FFmpeg (with automatic GPU acceleration when available), uploading to storage, and notifying your application when the job is done.

It powers [Growth Media Engine](https://github.com/leomaquiaveli) — a platform that turns commands into finished videos: the capabilities of professional video editing software delivered through an API, with rendering delegated to the cloud. Your own "creative cloud" — pay-per-use instead of software licenses, specialists, and workstations that sit idle and depreciate. Open source, so anyone can run their own media engine.

**Who is this for:**
- Developers building video automation pipelines (N8N, Make, custom APIs)
- Teams that need scalable video processing without paying for per-minute SaaS pricing
- Anyone building AI-powered video agents that need to render output at scale

---

## Features

- **Async + sync modes** — send a webhook URL for fire-and-forget, or omit it to wait for the result inline
- **GPU auto-detection** — detects NVIDIA (NVENC), Intel/AMD (VAAPI), and falls back to CPU. Zero configuration.
- **Smart file cache** — if 100 jobs share the same source video, it downloads once. Cache is reference-counted and TTL-based.
- **Batch clips** — generate N clips from a single video in one request: 1 download, N parallel renders, N parallel uploads
- **Zero external dependencies** — Go stdlib only. No frameworks, no supply chain risk, `~145MB` Docker image
- **Cloud-native** — runs on Cloud Run, any VM, or your local machine

---

## How it works

```
Your application
      │
      │  POST /v1/media/pipeline
      │  { "inputs": [...], "filters": [...], "outputs": [...] }
      │
      ▼
┌─────────────────────────────────┐
│         GME Open Server         │
│                                 │
│  ┌─────────────────────────┐    │
│  │  HTTP handler validates │    │
│  │  JSON and queues job    │    │─── 202 Accepted (async)
│  └───────────┬─────────────┘    │    or
│              │ goroutine        │    200 + result (sync)
│  ┌───────────▼─────────────┐    │
│  │  Download inputs        │    │
│  │  (file cache: 1 download│    │
│  │   per unique URL)       │    │
│  └───────────┬─────────────┘    │
│              │                  │
│  ┌───────────▼─────────────┐    │
│  │  Run FFmpeg             │    │
│  │  GPU: NVENC/VAAPI       │    │
│  │  CPU: libx264 fallback  │    │
│  └───────────┬─────────────┘    │
│              │                  │
│  ┌───────────▼─────────────┐    │
│  │  Upload output          │    │
│  │  (GCS or local disk)    │    │
│  └───────────┬─────────────┘    │
│              │                  │
│  ┌───────────▼─────────────┐    │
│  │  POST webhook           │    │──▶ Your application receives result
│  │  { status, output_url } │    │
│  └─────────────────────────┘    │
│                                 │
└─────────────────────────────────┘
```

Multiple jobs run concurrently (controlled by `MAX_CONCURRENT_JOBS`). The server never drops a request — it queues and processes in order, returning `503` only when the queue itself is full.

---

## Architecture

The codebase follows Ports & Adapters (Hexagonal Architecture) with Clean Architecture layering:

```
cmd/server/main.go          ← Composition Root — all dependencies wired here, nowhere else
internal/
  domain/
    job/                    ← Job and Status types
    ports/                  ← Interfaces: IFileCache, IStorage, IMediaProcessor, IWebhookSender
  application/              ← Use cases — the business logic lives here
                               Knows nothing about HTTP, FFmpeg, or GCS. Only interfaces.
  infra/
    ffmpeg/                 ← FFmpeg runner, GPU detector, file cache implementation
    storage/                ← GCS adapter (JWT RS256 from scratch) and local storage
    webhook/                ← HTTP webhook sender with retry
  api/
    handlers/               ← HTTP handlers: decode JSON → call use case → encode response
    middleware/             ← API key authentication
pkg/config/                 ← .env loading and environment variable parsing
```

**The key rule:** `application/` depends only on `domain/ports/` interfaces. It never imports `infra/`. This is what makes the storage layer swappable (local ↔ GCS) and the use cases testable in isolation.

---

## Quick Start

### Prerequisites

- [Go 1.24+](https://go.dev/dl/)
- [FFmpeg](https://ffmpeg.org/download.html) installed and in your PATH
  - **Windows**: use [gyan.dev builds](https://www.gyan.dev/ffmpeg/builds/) — baixe `ffmpeg-release-full.7z`, extraia e adicione a pasta `bin/` ao PATH
  - **Linux/macOS**: `sudo apt install ffmpeg` / `brew install ffmpeg`

### 1. Clone and configure

```bash
git clone https://github.com/leomaquiaveli/gme-open-server.git
cd gme-open-server

cp .env.example .env
# Open .env and set at minimum: API_KEY=any-secret-you-choose
```

### 2. Run

```bash
# Windows
.\dev.ps1

# Linux / macOS
API_KEY=my-dev-key go run ./cmd/server
```

### 3. Verify

```bash
# Health check — no auth required
curl http://localhost:8080/health

# Expected response:
# {"status":"ok","gpu":"libx264","version":"0.1.0"}
# "gpu" will show h264_nvenc if NVIDIA is detected
```

### Docker

```bash
docker build -t gme-open-server .

docker run \
  -e API_KEY=my-dev-key \
  -e STORAGE_TYPE=local \
  -p 8080:8080 \
  gme-open-server
```

---

## Environment Variables

| Variable | Required | Default | Description |
|---|---|---|---|
| `API_KEY` | **Yes** | — | Secret passed in `X-API-Key` header |
| `PORT` | No | `8080` | HTTP port |
| `STORAGE_TYPE` | No | `local` | `local` or `gcs` |
| `LOCAL_STORAGE_PATH` | No | `/tmp/gme` | Output directory for local mode |
| `BUCKET_NAME` | If GCS | — | Google Cloud Storage bucket name |
| `SERVICE_ACCOUNT_CREDENTIALS` | If GCS | — | Service account JSON (inline or file path) |
| `MAX_CONCURRENT_JOBS` | No | auto | Max parallel FFmpeg processes (default: vCPU count) |
| `CACHE_TTL_MINUTES` | No | `60` | How long downloaded source files are kept |
| `ENCODER` | No | auto | Force: `libx264`, `h264_nvenc`, `h264_vaapi`, `h264_mf` |
| `HW_VIDEO_BITRATE_MBPS` | No | `6` | Quality target for hardware encoders |
| `RENDER_TIMEOUT_MINUTES` | No | `5` | Per-job timeout — keep low on Cloud Run |
| `FFMPEG_THREADS` | No | `0` (auto) | Threads per job. Use `4` on Windows to avoid malloc issues. |

Copy `.env.example` for a complete template with comments.

---

## API Reference

### Authentication

All routes except `/health` require:
```
X-API-Key: your_api_key
```

---

### GET /health

Returns server status and the encoder being used. No auth required.

```bash
curl http://localhost:8080/health
```
```json
{"status": "ok", "gpu": "h264_nvenc", "version": "0.1.0"}
```

`gpu` values: `h264_nvenc` (NVIDIA) · `h264_vaapi` (Intel/AMD Linux) · `h264_mf` (Windows GPU) · `libx264` (CPU)

---

### POST /v1/media/pipeline

Run any FFmpeg job with arbitrary inputs, filters, and output options.

```json
{
  "id": "my-job",
  "inputs": [
    {
      "file_url": "https://storage.googleapis.com/bucket/video.mp4",
      "options": [
        { "option": "-ss", "argument": "10" },
        { "option": "-t",  "argument": "30" }
      ]
    }
  ],
  "filters": [
    { "filter": "[0:v]scale=1280:720[v]" }
  ],
  "outputs": [{
    "options": [
      { "option": "-map",    "argument": "[v]" },
      { "option": "-c:v",    "argument": "libx264" },
      { "option": "-c:a",    "argument": "copy" },
      { "option": "-preset", "argument": "fast" }
    ]
  }],
  "webhook_url": "https://your-app.com/callback"
}
```

**Sync mode** (no `webhook_url`): blocks and returns the result in the HTTP response (`200`).  
**Async mode** (with `webhook_url`): returns `202` immediately and POSTs the result to your webhook when done.

Full documentation: [docs/api/media-pipeline.md](docs/api/media-pipeline.md)

---

### POST /v1/media/vertical

Convert any video to vertical 9:16 (1080×1920) with smart crop and optional animated camera.

```json
{
  "input_url": "https://storage.googleapis.com/bucket/landscape.mp4",
  "start": 10,
  "duration": 30,
  "keyframes": [
    { "time": 0,  "scroll_x": -40 },
    { "time": 15, "scroll_x":  40 },
    { "time": 30, "scroll_x":   0 }
  ],
  "webhook_url": "https://your-app.com/callback"
}
```

`scroll_x` ranges from `-100` (left edge) to `+100` (right edge), `0` is center. Use `keyframes` for a smooth animated pan across the video. Full documentation: [docs/api/media-vertical.md](docs/api/media-vertical.md)

---

### POST /v1/media/clips

Generate multiple clips from a single source video in one request.  
**1 download → N parallel renders → N parallel uploads.**

```json
{
  "input_url": "https://storage.googleapis.com/bucket/full-episode.mp4",
  "mode": "vertical",
  "webhook_url": "https://your-app.com/callback",
  "clips": [
    { "id": "clip-1", "start": "00:01:00", "end": "00:01:30", "scroll_x": 0 },
    { "id": "clip-2", "start": 120, "duration": 45, "scroll_x": -20, "mode": "horizontal" },
    { "id": "clip-3", "start": "00:05:00", "duration": 20 }
  ]
}
```

Full documentation: [docs/api/media-clips.md](docs/api/media-clips.md)

---

### POST /v1/media/cuts

Remove trechos de um vídeo e concatena o restante. Informe os segmentos a **remover** — o servidor calcula o inverso e entrega um arquivo limpo.

```json
{
  "video_url": "https://storage.googleapis.com/bucket/podcast.mp4",
  "file_name": "podcast-editado.mp4",
  "cuts": [
    { "start": "00:02:10", "end": "00:02:45" },
    { "start": "00:15:30", "end": "00:15:55" }
  ],
  "video_codec": "libx264",
  "audio_codec": "aac",
  "audio_bitrate": "128k"
}
```

Full documentation: [docs/api/media-cuts.md](docs/api/media-cuts.md)

---

### POST /v1/media/caption

Queima legendas diretamente no vídeo (hardcoded). Aceita `.srt`, `.ass`, `.vtt` — qualquer formato que o FFmpeg suporte no filtro `subtitles`.

```json
{
  "video_url": "https://storage.googleapis.com/bucket/video.mp4",
  "caption_url": "https://storage.googleapis.com/bucket/legenda.srt",
  "file_name": "video-legendado.mp4"
}
```

Full documentation: [docs/api/media-caption.md](docs/api/media-caption.md)

---

### POST /v1/media/to-mp3

Extrai o áudio de qualquer vídeo e converte para MP3.

```json
{
  "media_url": "https://storage.googleapis.com/bucket/video.mp4",
  "bitrate": "192k"
}
```

Full documentation: [docs/api/media-to-mp3.md](docs/api/media-to-mp3.md)

---

### POST /v1/media/upload

Envia um arquivo para o storage. Aceita URL (download + re-upload) ou arquivo direto via multipart.

```bash
# Via URL
curl -X POST http://localhost:8080/v1/media/upload \
  -H "X-API-Key: sua_chave" \
  -H "Content-Type: application/json" \
  -d '{"file_url":"https://exemplo.com/video.mp4","file_name":"meu-video.mp4"}'

# Via arquivo local
curl -X POST http://localhost:8080/v1/media/upload \
  -H "X-API-Key: sua_chave" \
  -F "file=@/caminho/para/video.mp4"
```

Full documentation: [docs/api/media-upload.md](docs/api/media-upload.md)

---

### Webhook payload

All async jobs deliver the result as a `POST` to your `webhook_url`:

```json
{
  "job_id": "550e8400-e29b-41d4-a716-446655440000",
  "status": "done",
  "output": [{ "link": "https://storage.googleapis.com/bucket/output.mp4" }],
  "run_time": 12.4,
  "encoder": "h264_nvenc",
  "video_info": {
    "duration_s": 30.0,
    "width": 1080,
    "height": 1920,
    "size_mb": 45.1
  }
}
```

On failure: `"status": "failed"` with an `"error"` field describing the FFmpeg error.

---

### Error codes

| Code | Meaning |
|---|---|
| `401` | Missing or invalid `X-API-Key` |
| `400` | Invalid JSON or missing required fields |
| `500` | FFmpeg error (invalid filter, inaccessible URL, bad codec) |
| `503` | All slots busy — retry after a moment |

---

## Deploy

### Google Cloud Run

> **Step-by-step guide:** [docs/deploy/deploy-cloud-run.md](docs/deploy/deploy-cloud-run.md) — from zero to a running server, with every command and the reasoning behind it.

```bash
# Build and push
gcloud builds submit \
  --tag us-central1-docker.pkg.dev/YOUR_PROJECT_ID/gme-open-server/gme-open-server:latest

# Deploy
gcloud run deploy gme-open-server \
  --image us-central1-docker.pkg.dev/YOUR_PROJECT_ID/gme-open-server/gme-open-server:latest \
  --region us-central1 \
  --set-env-vars "API_KEY=$API_KEY,STORAGE_TYPE=gcs,BUCKET_NAME=$BUCKET_NAME" \
  --memory 8Gi \
  --cpu 8 \
  --concurrency 8 \
  --min-instances 0 \
  --max-instances 5 \
  --timeout 300 \
  --service-account YOUR_SERVICE_ACCOUNT@YOUR_PROJECT.iam.gserviceaccount.com
```

**Autenticação no GCS sem credenciais expostas (recomendado):** o `SERVICE_ACCOUNT_CREDENTIALS` **não é necessário** no Cloud Run. Basta associar uma Service Account com a role `Storage Object Admin` no bucket ao serviço via `--service-account`. O servidor detecta automaticamente e usa o metadata server do GCP.

> Mantenha `RENDER_TIMEOUT_MINUTES` ≤ 5 e `MAX_CONCURRENT_JOBS` = `--concurrency` no Cloud Run.

### VM with Docker Compose (GPU support)

See the full step-by-step guide: [VM_INSTALL_GUIDE.md](VM_INSTALL_GUIDE.md)

---

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md) for architecture details, coding conventions, and how to add a new route.

Found a bug? Open an [issue](https://github.com/leomaquiaveli/gme-open-server/issues).  
Security vulnerability? Read [SECURITY.md](SECURITY.md) first.

---

## License

[MIT](LICENSE) — Copyright (c) 2026 Leonardo Maquiaveli / Growth Media Engine

---

*Infrastructure powered by Google Cloud · Growth Media Engine is a [Google Cloud for Startups](https://cloud.google.com/startup) member*
