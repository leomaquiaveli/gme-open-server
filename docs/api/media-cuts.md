# Media Cuts

## 1. Overview

Remove trechos específicos de um vídeo e concatena o que sobrou em um único arquivo de saída. Útil para eliminar pausas, erros, anúncios ou qualquer trecho indesejado sem precisar cortar manualmente.

**Lógica:** você informa os trechos a **remover** (não os a manter). O servidor calcula os segmentos restantes, aplica `trim`/`atrim` em cada um e concatena com `concat`.

**Dois modos de operação:**
- **Sem `webhook_url`** → síncrono: bloqueia até terminar e retorna o resultado
- **Com `webhook_url`** → assíncrono: retorna `202` imediatamente, resultado entregue via POST no webhook

---

## 2. Endpoint

- **URL**: `/v1/media/cuts`
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
| `cuts` | array | Sim | — | Lista de trechos a **remover**. Cada item: `{ "start": ..., "end": ... }` |
| `file_name` | string | Não | `{job_id}.mp4` | Nome do arquivo de saída |
| `video_codec` | string | Não | `libx264` | Codec de vídeo: `libx264`, `h264_nvenc`, `copy` |
| `video_preset` | string | Não | `fast` | Preset do encoder: `ultrafast` → `veryslow` |
| `video_crf` | int | Não | `23` | Qualidade: `1`=melhor, `51`=pior |
| `audio_codec` | string | Não | `aac` | Codec de áudio: `aac`, `copy` |
| `audio_bitrate` | string | Não | `128k` | Bitrate de áudio: `128k`, `192k`, `320k` |
| `webhook_url` | string | Não | — | URL que recebe o resultado quando o job termina |

#### Campos de `cuts[i]`

| Campo | Tipo | Descrição |
|---|---|---|
| `start` | float ou `"HH:MM:SS"` | Início do trecho a remover |
| `end` | float ou `"HH:MM:SS"` | Fim do trecho a remover |

> `start` e `end` aceitam segundos (número) ou string no formato `"HH:MM:SS"` / `"HH:MM:SS.mmm"`.

---

## 4. Exemplos

### Remover um trecho no meio do vídeo

```json
{
  "video_url": "https://storage.googleapis.com/bucket/video.mp4",
  "file_name": "video-editado.mp4",
  "cuts": [
    { "start": 30, "end": 45 }
  ],
  "video_codec": "libx264",
  "audio_codec": "aac",
  "audio_bitrate": "128k"
}
```

```bash
curl -X POST http://localhost:8080/v1/media/cuts \
  -H "X-API-Key: sua_chave_aqui" \
  -H "Content-Type: application/json" \
  -d '{"video_url":"https://storage.googleapis.com/bucket/video.mp4","file_name":"video-editado.mp4","cuts":[{"start":30,"end":45}],"video_codec":"libx264","audio_codec":"aac","audio_bitrate":"128k"}'
```

---

### Remover múltiplos trechos (ex: pausas de um podcast)

```json
{
  "video_url": "https://storage.googleapis.com/bucket/podcast.mp4",
  "file_name": "podcast-limpo.mp4",
  "cuts": [
    { "start": "00:02:10", "end": "00:02:45" },
    { "start": "00:15:30", "end": "00:15:55" },
    { "start": "00:42:00", "end": "00:42:20" }
  ],
  "video_codec": "libx264",
  "audio_codec": "aac",
  "audio_bitrate": "192k"
}
```

---

### Modo assíncrono com webhook

```json
{
  "video_url": "https://storage.googleapis.com/bucket/video.mp4",
  "file_name": "video-final.mp4",
  "cuts": [
    { "start": 0, "end": 5 },
    { "start": 120, "end": 130 }
  ],
  "video_codec": "libx264",
  "audio_codec": "aac",
  "audio_bitrate": "128k",
  "webhook_url": "https://seu-app.com/callback"
}
```

---

## 5. Response

### Síncrono — Sucesso (200 OK)

```json
{
  "job_id": "550e8400-e29b-41d4-a716-446655440000",
  "file_name": "video-editado.mp4",
  "status": "done",
  "output": [
    { "link": "https://storage.googleapis.com/bucket/video-editado.mp4" }
  ],
  "run_time": 14.231,
  "encoder": "libx264",
  "video_info": {
    "duration_s": 285.0,
    "width": 1920,
    "height": 1080,
    "size_mb": 210.4
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

1. **Ordem dos cortes**: a ordem do array `cuts` não importa — o servidor ordena por tempo automaticamente.
2. **Codec `copy`**: usar `"video_codec": "copy"` é muito mais rápido (sem re-encode), mas pode causar imprecisão nos pontos de corte por conta dos GOP boundaries. Use `libx264` quando precisar de corte preciso.
3. **GPU automática**: se `video_codec` for `libx264` e NVENC estiver disponível, o runner substitui automaticamente. O campo `encoder` na resposta indica o que foi usado.
