# Media To MP3

## 1. Overview

Extrai o áudio de qualquer vídeo (ou arquivo de áudio) e converte para MP3. É um atalho sobre o `/v1/media/pipeline` — internamente monta os argumentos FFmpeg com `-vn` (sem vídeo) + `libmp3lame`.

**Dois modos de operação:**
- **Sem `webhook_url`** → síncrono: bloqueia até terminar e retorna o resultado
- **Com `webhook_url`** → assíncrono: retorna `202` imediatamente, resultado entregue via POST no webhook

---

## 2. Endpoint

- **URL**: `/v1/media/to-mp3`
- **Método**: `POST`

---

## 3. Request

### Headers

| Header | Obrigatório | Descrição |
|---|---|---|
| `X-API-Key` | Sim | Chave de autenticação |
| `Content-Type` | Sim | `application/json` |

### Body Parameters

| Parâmetro | Tipo | Obrigatório | Default | Descrição |
|---|---|---|---|---|
| `media_url` | string | Sim | — | URL pública do vídeo ou áudio fonte |
| `bitrate` | string | Não | `128k` | Bitrate do MP3 de saída: `96k`, `128k`, `192k`, `320k` |
| `webhook_url` | string | Não | — | URL que recebe o resultado quando o job termina |

---

## 4. Exemplos

### Extração básica de áudio

```json
{
  "media_url": "https://storage.googleapis.com/bucket/video.mp4"
}
```

```bash
curl -X POST http://localhost:8080/v1/media/to-mp3 \
  -H "X-API-Key: sua_chave_aqui" \
  -H "Content-Type: application/json" \
  -d '{"media_url":"https://storage.googleapis.com/bucket/video.mp4"}'
```

---

### Com bitrate alto (podcast, música)

```json
{
  "media_url": "https://storage.googleapis.com/bucket/podcast.mp4",
  "bitrate": "192k"
}
```

---

### Modo assíncrono com webhook

```json
{
  "media_url": "https://storage.googleapis.com/bucket/video.mp4",
  "bitrate": "128k",
  "webhook_url": "https://seu-app.com/callback"
}
```

---

## 5. Response

### Síncrono — Sucesso (200 OK)

```json
{
  "job_id": "550e8400-e29b-41d4-a716-446655440000",
  "file_name": "audio.mp3",
  "status": "done",
  "output": [
    { "link": "https://storage.googleapis.com/bucket/audio.mp3" }
  ],
  "run_time": 3.8,
  "encoder": "libmp3lame"
}
```

### Assíncrono — Job aceito (202 Accepted)

```json
{
  "job_id": "550e8400-e29b-41d4-a716-446655440000",
  "status": "queued"
}
```

---

## 6. Notas

1. **Nome do arquivo de saída**: sempre `audio.mp3`. Para controlar o nome, use `/v1/media/pipeline` diretamente com `file_name`.
2. **Compatível com qualquer mídia**: funciona com `.mp4`, `.mov`, `.avi`, `.mkv`, `.m4a`, `.aac`, `.wav` — qualquer formato com faixa de áudio que o FFmpeg suporte.
3. **Sem vídeo no output**: o flag `-vn` garante que nenhum frame de vídeo seja processado — conversão é rápida mesmo para vídeos longos.
