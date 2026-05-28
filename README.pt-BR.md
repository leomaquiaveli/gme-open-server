# GME Open Server

**Servidor de processamento de vídeo de alta performance que você hospeda e chama de qualquer linguagem.**  
Feito em Go, movido a FFmpeg, projetado para pipelines de vídeo em produção.

Por [Leonardo Maquiaveli](https://github.com/leomaquiaveli) · [Growth Media Engine](https://github.com/leomaquiaveli)

[![Go 1.24](https://img.shields.io/badge/Go-1.24-00ADD8?logo=go)](https://go.dev/)
[![License MIT](https://img.shields.io/badge/License-MIT-green.svg)](LICENSE)
[![Zero deps](https://img.shields.io/badge/dependencies-zero-brightgreen)](go.mod)

[🇺🇸 English version](README.md)

---

## O que é isso?

GME Open Server é um servidor HTTP que recebe jobs de processamento de vídeo via uma API JSON simples e entrega os resultados via webhook ou resposta síncrona.

Você manda uma requisição descrevendo o que quer — cortar um vídeo, converter para vertical 9:16, gerar vários clipes de uma única fonte — e o servidor cuida do resto: baixa os arquivos, processa com FFmpeg (com aceleração GPU automática quando disponível), sobe para o storage e notifica sua aplicação quando o job terminar.

Foi construído para rodar a infra do [Growth Media Engine](https://github.com/leomaquiaveli), uma plataforma de produção de conteúdo em escala que processa milhares de clipes por dia. O projeto é open source para que qualquer um possa rodar a mesma infraestrutura que alimenta um sistema real em produção.

**Para quem é:**
- Desenvolvedores construindo pipelines de automação de vídeo (N8N, Make, APIs próprias)
- Times que precisam de processamento de vídeo escalável sem pagar por minuto de SaaS
- Quem está construindo agentes de IA que precisam renderizar vídeo em escala

---

## Funcionalidades

- **Modo assíncrono + síncrono** — mande uma URL de webhook para fire-and-forget, ou omita para receber o resultado direto na resposta HTTP
- **Detecção automática de GPU** — detecta NVIDIA (NVENC), Intel/AMD (VAAPI) e cai para CPU automaticamente. Zero configuração.
- **Cache inteligente de arquivos** — se 100 jobs usam o mesmo vídeo fonte, ele baixa uma vez. Cache com contagem de referências e TTL.
- **Clipes em lote** — gere N clipes de um único vídeo em uma requisição: 1 download, N renders em paralelo, N uploads em paralelo
- **Zero dependências externas** — só stdlib do Go. Sem frameworks, sem risco de supply chain, imagem Docker de ~145MB
- **Cloud-native** — roda no Cloud Run, qualquer VM, ou na sua máquina local

---

## Como funciona

```
Sua aplicação
      │
      │  POST /v1/media/pipeline
      │  { "inputs": [...], "filters": [...], "outputs": [...] }
      │
      ▼
┌─────────────────────────────────┐
│         GME Open Server         │
│                                 │
│  ┌─────────────────────────┐    │
│  │  Handler valida JSON    │    │
│  │  e enfileira o job      │    │─── 202 Accepted (assíncrono)
│  └───────────┬─────────────┘    │    ou
│              │ goroutine        │    200 + resultado (síncrono)
│  ┌───────────▼─────────────┐    │
│  │  Baixa inputs           │    │
│  │  (file cache: 1 download│    │
│  │   por URL única)        │    │
│  └───────────┬─────────────┘    │
│              │                  │
│  ┌───────────▼─────────────┐    │
│  │  Roda FFmpeg            │    │
│  │  GPU: NVENC/VAAPI       │    │
│  │  CPU: libx264 fallback  │    │
│  └───────────┬─────────────┘    │
│              │                  │
│  ┌───────────▼─────────────┐    │
│  │  Sobe output            │    │
│  │  (GCS ou disco local)   │    │
│  └───────────┬─────────────┘    │
│              │                  │
│  ┌───────────▼─────────────┐    │
│  │  POST webhook           │    │──▶ Sua aplicação recebe o resultado
│  │  { status, output_url } │    │
│  └─────────────────────────┘    │
│                                 │
└─────────────────────────────────┘
```

Múltiplos jobs rodam em paralelo (controlado por `MAX_CONCURRENT_JOBS`). O servidor nunca derruba uma requisição — ele enfileira e processa na ordem, retornando `503` apenas quando a própria fila está cheia.

---

## Arquitetura

O código segue Ports & Adapters (Arquitetura Hexagonal) com camadas de Clean Architecture:

```
cmd/server/main.go          ← Composition Root — único lugar onde as dependências são instanciadas
internal/
  domain/
    job/                    ← Tipos Job e Status
    ports/                  ← Interfaces: IFileCache, IStorage, IMediaProcessor, IWebhookSender
  application/              ← Use cases — toda a lógica de negócio fica aqui
                               Não sabe nada sobre HTTP, FFmpeg ou GCS. Só interfaces.
  infra/
    ffmpeg/                 ← Runner FFmpeg, detector de GPU, implementação do file cache
    storage/                ← Adapter GCS (RS256 JWT do zero) e storage local
    webhook/                ← Sender HTTP de webhook com retry
  api/
    handlers/               ← HTTP handlers: decodifica JSON → chama use case → encoda resposta
    middleware/             ← Autenticação por API key
pkg/config/                 ← Leitura do .env e variáveis de ambiente
```

**A regra principal:** `application/` depende apenas das interfaces em `domain/ports/`. Nunca importa `infra/`. É isso que torna a camada de storage intercambiável (local ↔ GCS) e os use cases testáveis isoladamente.

---

## Começando

### Pré-requisitos

- [Go 1.24+](https://go.dev/dl/)
- [FFmpeg](https://ffmpeg.org/download.html) instalado e no PATH
  - **Windows**: use os [builds do gyan.dev](https://www.gyan.dev/ffmpeg/builds/) — baixe `ffmpeg-release-full.7z`, extraia e adicione a pasta `bin/` ao PATH
  - **Linux**: `sudo apt install ffmpeg`
  - **macOS**: `brew install ffmpeg`

### 1. Clone e configure

```bash
git clone https://github.com/leomaquiaveli/gme-open-server.git
cd gme-open-server

cp .env.example .env
# Abra o .env e defina no mínimo: API_KEY=qualquer-segredo-que-você-escolher
```

### 2. Rode

```bash
# Windows
.\dev.ps1

# Linux / macOS
API_KEY=minha-chave go run ./cmd/server
```

### 3. Verifique

```bash
# Health check — sem autenticação
curl http://localhost:8080/health

# Resposta esperada:
# {"status":"ok","gpu":"libx264","version":"0.1.0"}
# "gpu" vai mostrar h264_nvenc se NVIDIA for detectada
```

### Docker

```bash
docker build -t gme-open-server .

docker run \
  -e API_KEY=minha-chave \
  -e STORAGE_TYPE=local \
  -p 8080:8080 \
  gme-open-server
```

---

## Variáveis de Ambiente

| Variável | Obrigatório | Padrão | Descrição |
|---|---|---|---|
| `API_KEY` | **Sim** | — | Segredo enviado no header `X-API-Key` |
| `PORT` | Não | `8080` | Porta HTTP |
| `STORAGE_TYPE` | Não | `local` | `local` ou `gcs` |
| `LOCAL_STORAGE_PATH` | Não | `/tmp/gme` | Diretório de saída no modo local |
| `BUCKET_NAME` | Se GCS | — | Nome do bucket no Google Cloud Storage |
| `SERVICE_ACCOUNT_CREDENTIALS` | Opcional | — | JSON da service account (inline ou caminho do arquivo). Não necessário no Cloud Run com ADC. |
| `MAX_CONCURRENT_JOBS` | Não | auto | Máximo de processos FFmpeg em paralelo (padrão: número de vCPUs) |
| `CACHE_TTL_MINUTES` | Não | `60` | Quanto tempo os arquivos fonte baixados ficam em cache |
| `ENCODER` | Não | auto | Forçar: `libx264`, `h264_nvenc`, `h264_vaapi`, `h264_mf` |
| `HW_VIDEO_BITRATE_MBPS` | Não | `6` | Alvo de qualidade para encoders de hardware |
| `RENDER_TIMEOUT_MINUTES` | Não | `5` | Timeout por job — mantenha baixo no Cloud Run |
| `FFMPEG_THREADS` | Não | `0` (auto) | Threads por job. Use `4` no Windows para evitar problemas de malloc. |

Copie o `.env.example` para um template completo com comentários.

---

## Referência da API

### Autenticação

Todas as rotas exceto `/health` exigem:
```
X-API-Key: sua_api_key
```

---

### GET /health

Retorna o status do servidor e o encoder em uso. Sem autenticação.

```bash
curl http://localhost:8080/health
```
```json
{"status": "ok", "gpu": "h264_nvenc", "version": "0.1.0"}
```

Valores de `gpu`: `h264_nvenc` (NVIDIA) · `h264_vaapi` (Intel/AMD Linux) · `h264_mf` (GPU Windows) · `libx264` (CPU)

---

### POST /v1/media/pipeline

Execute qualquer job FFmpeg com inputs, filtros e opções de output arbitrários.

```json
{
  "id": "meu-job",
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
  "webhook_url": "https://sua-app.com/callback"
}
```

**Modo síncrono** (sem `webhook_url`): bloqueia e retorna o resultado na resposta HTTP (`200`).  
**Modo assíncrono** (com `webhook_url`): retorna `202` imediatamente e faz POST do resultado no seu webhook quando terminar.

Documentação completa: [docs/api/media-pipeline.md](docs/api/media-pipeline.md)

---

### POST /v1/media/vertical

Converte qualquer vídeo para vertical 9:16 (1080×1920) com crop inteligente e câmera animada opcional.

```json
{
  "input_url": "https://storage.googleapis.com/bucket/video-horizontal.mp4",
  "start": 10,
  "duration": 30,
  "keyframes": [
    { "time": 0,  "scroll_x": -40 },
    { "time": 15, "scroll_x":  40 },
    { "time": 30, "scroll_x":   0 }
  ],
  "webhook_url": "https://sua-app.com/callback"
}
```

`scroll_x` vai de `-100` (borda esquerda) a `+100` (borda direita), `0` é o centro. Use `keyframes` para uma câmera que se move suavemente pelo vídeo. Documentação completa: [docs/api/media-vertical.md](docs/api/media-vertical.md)

---

### POST /v1/media/clips

Gere múltiplos clipes de um único vídeo fonte em uma requisição.  
**1 download → N renders em paralelo → N uploads em paralelo.**

```json
{
  "input_url": "https://storage.googleapis.com/bucket/episodio-completo.mp4",
  "mode": "vertical",
  "webhook_url": "https://sua-app.com/callback",
  "clips": [
    { "id": "clip-1", "start": "00:01:00", "end": "00:01:30", "scroll_x": 0 },
    { "id": "clip-2", "start": 120, "duration": 45, "scroll_x": -20, "mode": "horizontal" },
    { "id": "clip-3", "start": "00:05:00", "duration": 20 }
  ]
}
```

Documentação completa: [docs/api/media-clips.md](docs/api/media-clips.md)

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

Documentação completa: [docs/api/media-cuts.md](docs/api/media-cuts.md)

---

### POST /v1/media/caption

Queima legendas diretamente no vídeo (hardcoded subtitles). Aceita `.srt`, `.ass`, `.vtt` — qualquer formato que o FFmpeg suporte no filtro `subtitles`.

```json
{
  "video_url": "https://storage.googleapis.com/bucket/video.mp4",
  "caption_url": "https://storage.googleapis.com/bucket/legenda.srt",
  "file_name": "video-legendado.mp4"
}
```

Documentação completa: [docs/api/media-caption.md](docs/api/media-caption.md)

---

### POST /v1/media/to-mp3

Extrai o áudio de qualquer vídeo e converte para MP3.

```json
{
  "media_url": "https://storage.googleapis.com/bucket/video.mp4",
  "bitrate": "192k"
}
```

Documentação completa: [docs/api/media-to-mp3.md](docs/api/media-to-mp3.md)

---

### POST /v1/media/upload

Envia um arquivo para o storage configurado. Aceita URL (download + re-upload) ou arquivo direto via multipart.

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

Documentação completa: [docs/api/media-upload.md](docs/api/media-upload.md)

---

### Payload do webhook

Todos os jobs assíncronos entregam o resultado como `POST` na sua `webhook_url`:

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

Em caso de falha: `"status": "failed"` com um campo `"error"` descrevendo o erro do FFmpeg.

---

### Códigos de erro

| Código | Significado |
|---|---|
| `401` | `X-API-Key` ausente ou inválida |
| `400` | JSON inválido ou campos obrigatórios faltando |
| `500` | Erro do FFmpeg (filtro inválido, URL inacessível, codec inválido) |
| `503` | Todos os slots ocupados — tente novamente em instantes |

---

## Deploy

### Google Cloud Run

```bash
# Build e push
gcloud builds submit \
  --tag us-central1-docker.pkg.dev/SEU_PROJECT_ID/gme-open-server/gme-open-server:latest

# Deploy
gcloud run deploy gme-open-server \
  --image us-central1-docker.pkg.dev/SEU_PROJECT_ID/gme-open-server/gme-open-server:latest \
  --region us-central1 \
  --set-env-vars "API_KEY=$API_KEY,STORAGE_TYPE=gcs,BUCKET_NAME=$BUCKET_NAME" \
  --memory 8Gi \
  --cpu 8 \
  --concurrency 8 \
  --min-instances 0 \
  --max-instances 5 \
  --timeout 300 \
  --service-account SUA_SERVICE_ACCOUNT@SEU_PROJETO.iam.gserviceaccount.com
```

**Autenticação no GCS sem expor credenciais (recomendado):** o `SERVICE_ACCOUNT_CREDENTIALS` **não é necessário** no Cloud Run. Basta associar uma Service Account com a role `Storage Object Admin` no bucket ao serviço via `--service-account`. O servidor detecta automaticamente e usa o metadata server do GCP — zero segredos expostos.

> Mantenha `RENDER_TIMEOUT_MINUTES` ≤ 5 e `MAX_CONCURRENT_JOBS` = `--concurrency` no Cloud Run.

### VM com Docker Compose (suporte a GPU)

Veja o guia completo passo a passo: [VM_INSTALL_GUIDE.md](VM_INSTALL_GUIDE.md)

---

## Contribuindo

Veja o [CONTRIBUTING.md](CONTRIBUTING.md) para detalhes de arquitetura, convenções de código e como adicionar uma nova rota.

Encontrou um bug? Abra uma [issue](https://github.com/leomaquiaveli/gme-open-server/issues).  
Vulnerabilidade de segurança? Leia o [SECURITY.md](SECURITY.md) primeiro.

---

## Licença

[MIT](LICENSE) — Copyright (c) 2026 Leonardo Maquiaveli / Growth Media Engine

---

*Infraestrutura powered by Google Cloud · Growth Media Engine é membro do [Google Cloud for Startups](https://cloud.google.com/startup)*
