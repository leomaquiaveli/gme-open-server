# Media Clips

## 1. Overview

Gera múltiplos clips a partir de um único vídeo fonte em uma única requisição.

**Diferença em relação ao `/v1/media/vertical`:** enquanto o vertical processa um clip por requisição, o clips processa N clips com apenas 1 download do vídeo fonte, renderiza todos em paralelo, e sobe todos ao storage simultaneamente. Ideal para pipelines de produção em lote.

**Modos de saída por clip:**
- `"vertical"` → crop 9:16 + scale 1080×1920 (scroll_x, keyframes disponíveis)
- `"horizontal"` / sem mode → corte simples, mantém dimensão original da fonte

**Dois modos de operação:**
- **Sem `webhook_url`** → síncrono: bloqueia até terminar e retorna todos os resultados
- **Com `webhook_url`** → assíncrono: retorna `202` imediatamente, resultado entregue via POST no webhook

---

## 2. Endpoint

- **URL**: `/v1/media/clips`
- **Método**: `POST`

---

## 3. Request

### Headers

| Header | Obrigatório | Descrição |
|---|---|---|
| `X-API-Key` | Sim | Chave de autenticação |
| `Content-Type` | Sim | `application/json` |

### Body Parameters — Nível do request

| Parâmetro | Tipo | Obrigatório | Default | Descrição |
|---|---|---|---|---|
| `input_url` | string | Sim | — | URL pública do vídeo fonte (baixado apenas 1 vez) |
| `clips` | array | Sim | — | Array de clips a gerar |
| `mode` | string | Não | `""` (corte simples) | Default de mode para todos os clips: `"vertical"` ou `"horizontal"` |
| `crf` | int | Não | `23` | Default de qualidade para todos os clips |
| `preset` | string | Não | `"fast"` | Default de preset para todos os clips |
| `webhook_url` | string | Não | — | URL que recebe o resultado quando todos os clips terminam |

### Body Parameters — Nível de cada clip

| Parâmetro | Tipo | Obrigatório | Default | Descrição |
|---|---|---|---|---|
| `id` | string | Não | `""` | Identificador do clip para rastreamento externo |
| `file_name` | string | Não | `{job_id}-{idx}.mp4` | Nome do arquivo de saída |
| `start` | number \| string | Não | `0` | Início do clip: segundos (`90`) ou `"HH:MM:SS"` / `"HH:MM:SS.mmm"` |
| `end` | number \| string | Não | — | Fim do clip. Alternativa ao `duration` |
| `duration` | number \| string | Não | até o fim | Duração do clip. Alternativa ao `end` |
| `mode` | string | Não | herda do request | Override do mode deste clip: `"vertical"` ou `"horizontal"` |
| `scroll_x` | float | Não | `0` | Posição horizontal do crop, `-100` a `+100`. Apenas mode `"vertical"` |
| `keyframes` | array | Não | — | Scroll animado ao longo do tempo. Apenas mode `"vertical"` |
| `crf` | int | Não | herda do request | Override de qualidade deste clip |
| `preset` | string | Não | herda do request | Override de preset deste clip |

> `duration` e `end` são mutuamente exclusivos — use um ou outro, não os dois.

### Hierarquia do mode

```
clip.mode  (se definido)
    ↓ senão
req.mode   (se definido)
    ↓ senão
corte simples (mantém dimensão original)
```

---

## 4. Exemplos

### Lote vertical — vídeo de 1h cortado em clips de 1 minuto

```json
{
  "input_url": "https://storage.googleapis.com/bucket/video-1h.mp4",
  "mode": "vertical",
  "crf": 23,
  "preset": "fast",
  "clips": [
    { "id": "c1", "file_name": "ep01-intro.mp4",    "start": "00:00:00", "end": "00:01:00", "scroll_x": -20 },
    { "id": "c2", "file_name": "ep01-destaque.mp4", "start": "00:01:00", "end": "00:02:00", "scroll_x": 10  },
    { "id": "c3", "file_name": "ep01-encerramento.mp4", "start": "00:58:00", "end": "00:59:00" }
  ]
}
```

---

### Lote misto — clips verticais e horizontais no mesmo request

```json
{
  "input_url": "https://storage.googleapis.com/bucket/podcast.mp4",
  "mode": "vertical",
  "crf": 23,
  "clips": [
    { "id": "shorts-1", "file_name": "shorts-1.mp4", "start": "00:05:30", "end": "00:06:30", "scroll_x": -30 },
    { "id": "shorts-2", "file_name": "shorts-2.mp4", "start": "00:12:00", "end": "00:13:00", "keyframes": [
        { "time": 0,  "scroll_x": -35 },
        { "time": 20, "scroll_x": 40  }
    ]},
    { "id": "full-clip", "file_name": "full-horizontal.mp4", "start": "00:05:00", "end": "00:10:00", "mode": "horizontal" }
  ]
}
```

`shorts-1` e `shorts-2` herdam `"vertical"` do request. `full-clip` overrida para `"horizontal"`.

---

### Cortes simples sem transformação

```json
{
  "input_url": "https://storage.googleapis.com/bucket/entrevista.mp4",
  "crf": 18,
  "clips": [
    { "id": "parte-1", "file_name": "parte-1.mp4", "start": 0,    "end": 300  },
    { "id": "parte-2", "file_name": "parte-2.mp4", "start": 300,  "end": 600  },
    { "id": "parte-3", "file_name": "parte-3.mp4", "start": 600,  "end": 900  }
  ]
}
```

Sem `mode` → corte simples, dimensão original preservada.

---

### Modo assíncrono com webhook

```json
{
  "input_url": "https://storage.googleapis.com/bucket/video.mp4",
  "mode": "vertical",
  "crf": 23,
  "webhook_url": "https://seu-webhook.com/callback",
  "clips": [
    { "id": "c1", "start": 0,  "duration": 60 },
    { "id": "c2", "start": 60, "duration": 60 }
  ]
}
```

---

## 5. Response

### Síncrono — Sucesso (200 OK)

```json
{
  "job_id": "550e8400-e29b-41d4-a716-446655440000",
  "status": "done",
  "run_time": 45.231,
  "outputs": [
    {
      "id": "c1",
      "internal_id": "550e8400-00",
      "file_name": "ep01-intro.mp4",
      "link": "https://storage.googleapis.com/bucket/ep01-intro-550e8400-00.mp4",
      "run_time": 38.512
    },
    {
      "id": "c2",
      "internal_id": "550e8400-01",
      "file_name": "ep01-destaque.mp4",
      "link": "https://storage.googleapis.com/bucket/ep01-destaque-550e8400-01.mp4",
      "run_time": 40.124
    }
  ]
}
```

### Assíncrono — Job aceito (202 Accepted)

```json
{
  "job_id": "550e8400-e29b-41d4-a716-446655440000",
  "status": "queued"
}
```

### Falha parcial — alguns clips falham, outros completam

```json
{
  "job_id": "550e8400-e29b-41d4-a716-446655440000",
  "status": "done",
  "run_time": 42.1,
  "outputs": [
    { "id": "c1", "internal_id": "550e8400-00", "link": "https://...", "run_time": 38.5 },
    { "id": "c2", "internal_id": "550e8400-01", "error": "ffmpeg error: invalid start time" }
  ]
}
```

### Erro de validação (400)

```json
{ "error": "clip[2]: end must be greater than start" }
```

---

## 6. Notas

1. **1 download por request**: o `input_url` é baixado uma única vez e compartilhado entre todos os clips do lote. Múltiplos requests com a mesma URL reutilizam o cache.

2. **Renders em paralelo**: cada clip adquire um slot do pool de CPU (`MAX_CONCURRENT_JOBS`) individualmente. O slot é liberado assim que o FFmpeg termina — o upload não segura capacidade de render.

3. **Uploads em paralelo**: todos os clips renderizados sobem ao storage simultaneamente após o final da fase de render.

4. **`internal_id`**: identificador gerado pelo servidor no formato `{job_id[:8]}-{índice}`. Aparece nos logs e na resposta para debug.

5. **Formato de tempo**: `start`, `end`, `duration` aceitam segundos (`90`, `90.5`) ou string `"HH:MM:SS"` / `"HH:MM:SS.mmm"`.

6. **GPU automática**: o encoder detectado (NVENC/VAAPI/libx264) é usado em todos os clips do lote.
