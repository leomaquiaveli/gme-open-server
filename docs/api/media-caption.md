# Media Caption

## 1. Overview

Queima legendas diretamente no vídeo (hardcoded subtitles) usando o filtro `subtitles` do FFmpeg. O arquivo de legenda é baixado da URL informada, copiado para um caminho temporário local e referenciado pelo filtro.

**Formatos suportados:** `.srt`, `.ass`, `.ssa`, `.vtt` — qualquer formato que o FFmpeg aceite no filtro `subtitles`.

**Dois modos de operação:**
- **Sem `webhook_url`** → síncrono: bloqueia até terminar e retorna o resultado
- **Com `webhook_url`** → assíncrono: retorna `202` imediatamente, resultado entregue via POST no webhook

---

## 2. Endpoint

- **URL**: `/v1/media/caption`
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
| `video_url` | string | Sim | — | URL pública do vídeo fonte |
| `caption_url` | string | Sim | — | URL pública do arquivo de legenda (`.srt`, `.ass`, `.vtt`) |
| `file_name` | string | Não | `{job_id}.mp4` | Nome do arquivo de saída |
| `crf` | int | Não | `23` | Qualidade do encode: `1`=melhor, `51`=pior |
| `preset` | string | Não | `fast` | Preset do encoder: `ultrafast` → `veryslow` |
| `encoder` | string | Não | auto | Força um codec específico: `libx264`, `h264_nvenc`. Vazio = auto-detecção de GPU |
| `webhook_url` | string | Não | — | URL que recebe o resultado quando o job termina |

---

## 4. Exemplos

### Queimar legendas em um vídeo

```json
{
  "video_url": "https://storage.googleapis.com/bucket/video.mp4",
  "caption_url": "https://storage.googleapis.com/bucket/legenda.srt",
  "file_name": "video-legendado.mp4"
}
```

```bash
curl -X POST http://localhost:8080/v1/media/caption \
  -H "X-API-Key: sua_chave_aqui" \
  -H "Content-Type: application/json" \
  -d '{"video_url":"https://storage.googleapis.com/bucket/video.mp4","caption_url":"https://storage.googleapis.com/bucket/legenda.srt","file_name":"video-legendado.mp4"}'
```

---

### Com controle de qualidade

```json
{
  "video_url": "https://storage.googleapis.com/bucket/video.mp4",
  "caption_url": "https://storage.googleapis.com/bucket/legenda.ass",
  "file_name": "video-final.mp4",
  "crf": 18,
  "preset": "medium"
}
```

---

### Forçar encoder específico

```json
{
  "video_url": "https://storage.googleapis.com/bucket/video.mp4",
  "caption_url": "https://storage.googleapis.com/bucket/legenda.srt",
  "file_name": "video-nvenc.mp4",
  "encoder": "h264_nvenc"
}
```

---

### Modo assíncrono com webhook

```json
{
  "video_url": "https://storage.googleapis.com/bucket/video.mp4",
  "caption_url": "https://storage.googleapis.com/bucket/legenda.srt",
  "file_name": "video-legendado.mp4",
  "webhook_url": "https://seu-app.com/callback"
}
```

---

## 5. Response

### Síncrono — Sucesso (200 OK)

```json
{
  "job_id": "550e8400-e29b-41d4-a716-446655440000",
  "file_name": "video-legendado.mp4",
  "status": "done",
  "output": [
    { "link": "https://storage.googleapis.com/bucket/video-legendado.mp4" }
  ],
  "run_time": 22.1,
  "encoder": "libx264",
  "video_info": {
    "duration_s": 120.0,
    "width": 1920,
    "height": 1080,
    "size_mb": 95.3
  }
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

1. **Legendas hardcoded**: as legendas são gravadas permanentemente no vídeo (não são faixas separadas). Para legendas soft (selecionáveis no player), use `/v1/media/pipeline` com o filtro FFmpeg adequado.
2. **Estilo `.ass`**: arquivos `.ass` suportam estilos avançados (fonte, cor, posição, karaokê). O filtro `subtitles` do FFmpeg preserva esses estilos automaticamente.
3. **Ambos os arquivos são cacheados**: `video_url` e `caption_url` são cacheados separadamente pelo file cache. Se você processar o mesmo vídeo com diferentes legendas, o vídeo é baixado apenas uma vez por TTL.
