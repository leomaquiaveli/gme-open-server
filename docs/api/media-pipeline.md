# Media Pipeline

## 1. Overview

Executa um job de processamento de mídia com acesso direto aos parâmetros do FFmpeg. Aceita múltiplos arquivos de entrada (vídeo, imagem, áudio), filtros de composição e opções de saída.

**Dois modos de operação:**
- **Sem `webhook_url`** → síncrono: bloqueia até terminar e retorna o resultado direto na resposta HTTP
- **Com `webhook_url`** → assíncrono: retorna `202` imediatamente com `job_id`, resultado entregue via POST no webhook quando o job termina

O arquivo de entrada é cacheado por URL: 1000 clips do mesmo vídeo fazem apenas 1 download.

---

## 2. Endpoint

- **URL**: `/v1/media/pipeline`
- **Método**: `POST`

---

## 3. Request

### Headers

| Header | Obrigatório | Descrição |
|---|---|---|
| `X-API-Key` | Sim | Chave de autenticação |
| `Content-Type` | Sim | Deve ser `application/json` |

### Body Parameters

| Parâmetro | Tipo | Obrigatório | Descrição |
|---|---|---|---|
| `id` | string | Não | Label para identificação nos logs |
| `file_name` | string | Não | Nome do arquivo de saída (sem extensão adiciona `.mp4`) |
| `inputs` | array | Sim | Lista de arquivos de entrada |
| `inputs[].file_url` | string | Sim | URL pública ou assinada do arquivo |
| `inputs[].options` | array | Não | Flags FFmpeg **pré-input** (ex: `-ss` para fast seek — ver seção 7) |
| `filters` | array | Não | Blocos de filtro para `-filter_complex` |
| `filters[].filter` | string | Sim | String de filtro FFmpeg |
| `outputs` | array | Sim | Configurações de saída — **apenas o primeiro elemento é processado** |
| `outputs[0].options` | array | Sim | Flags FFmpeg de saída (codec, mapa de streams, etc.) |
| `outputs[0].options[].option` | string | Sim | Flag FFmpeg (ex: `-c:v`) |
| `outputs[0].options[].argument` | string \| number | Sim | Valor da flag (ex: `"libx264"` ou `18`) |
| `webhook_url` | string | Não | URL que recebe o resultado quando o job termina |

---

## 4. Como o servidor monta o comando FFmpeg

O servidor constrói os args do FFmpeg na seguinte ordem:

```
[pre-input options do input 0] -i <local_path_0>
[pre-input options do input 1] -i <local_path_1>
...
-filter_complex "<filter[0]>;<filter[1]>;..."   ← omitido se filters vazio
[output options]
<output_path>
```

**Importante — GPU auto-substitution:** antes de executar, o servidor percorre todos os args e:
1. Substitui `libx264` pelo encoder GPU detectado no startup (`h264_mf`, `h264_nvenc`, etc.)
2. Remove `-x264-params`, `-x264opts`, `-crf` e `-preset` do array (exclusivos do libx264, sem equivalente direto em hardware encoders)
3. Injeta flags de qualidade adequadas para o encoder GPU no lugar

Isso significa: **se você enviar `-crf 18 -preset slow` no payload e o servidor estiver usando GPU, esses flags serão silenciosamente ignorados** e o servidor usará seu próprio controle de qualidade. Use `ENCODER=libx264` no `.env` para forçar CPU se precisar controle fino de qualidade.

---

## 5. Exemplos de requisição

### Corte simples de vídeo (modo síncrono)

Corta 30 segundos do vídeo. Sem `webhook_url` → bloqueia e retorna o resultado direto.

> **Nota**: `-ss` e `-t` aqui estão nas `inputs[].options` (pré-input = fast seek).
> Fast seek é impreciso em alguns formatos — para frame-accuracy, mova `-ss`/`-t` para `outputs[0].options`.

```json
{
  "id": "corte-30s",
  "file_name": "output_cortado",
  "inputs": [
    {
      "file_url": "https://storage.googleapis.com/seu-bucket/video.mp4",
      "options": [
        { "option": "-ss", "argument": "00:00:10" },
        { "option": "-t",  "argument": "30" }
      ]
    }
  ],
  "filters": [],
  "outputs": [
    {
      "options": [
        { "option": "-c:v",    "argument": "libx264" },
        { "option": "-c:a",    "argument": "copy" },
        { "option": "-crf",    "argument": 18 },
        { "option": "-preset", "argument": "fast" }
      ]
    }
  ]
}
```

```bash
curl -X POST http://localhost:8080/v1/media/pipeline \
  -H "X-API-Key: sua_chave_aqui" \
  -H "Content-Type: application/json" \
  -d '{
    "id": "corte-30s",
    "inputs": [{
      "file_url": "https://storage.googleapis.com/seu-bucket/video.mp4",
      "options": [
        { "option": "-ss", "argument": "00:00:10" },
        { "option": "-t",  "argument": "30" }
      ]
    }],
    "filters": [],
    "outputs": [{
      "options": [
        { "option": "-c:v",    "argument": "libx264" },
        { "option": "-c:a",    "argument": "copy" },
        { "option": "-crf",    "argument": 18 },
        { "option": "-preset", "argument": "fast" }
      ]
    }]
  }'
```

---

### Vídeo vertical com fundo borrado e headline (formato Moldura)

Transforma um vídeo horizontal em vertical 1080×1920 com fundo desfocado, vídeo original centralizado e headline no topo.

```json
{
  "id": "moldura-vertical",
  "file_name": "moldura_output",
  "inputs": [
    { "file_url": "https://storage.googleapis.com/seu-bucket/video.mp4" },
    { "file_url": "https://storage.googleapis.com/seu-bucket/headline.png" }
  ],
  "filters": [
    { "filter": "[0:v]crop=iw*0.315:ih:(iw-(iw*0.315))/2:0,scale=1080:1920,gblur=sigma=30[bg]" },
    { "filter": "[0:v]scale=1080:-1[fg]" },
    { "filter": "[bg][fg]overlay=(W-w)/2:(H-h)/2[base]" },
    { "filter": "[1:v]scale=iw*0.9:-1[resized]" },
    { "filter": "[base][resized]overlay=(main_w-overlay_w)/2:200[v]" }
  ],
  "outputs": [
    {
      "options": [
        { "option": "-map",    "argument": "[v]" },
        { "option": "-map",    "argument": "0:a" },
        { "option": "-c:a",    "argument": "copy" },
        { "option": "-c:v",    "argument": "libx264" },
        { "option": "-crf",    "argument": 18 },
        { "option": "-preset", "argument": "fast" }
      ]
    }
  ],
  "webhook_url": "https://seu-webhook.com/callback"
}
```

**Aviso sobre GPU**: neste exemplo, `-crf 18` e `-preset fast` serão removidos se o servidor estiver usando GPU (`h264_mf` ou `h264_nvenc`). O encoder de hardware vai usar o bitrate configurado em `HW_VIDEO_BITRATE_MBPS` (default 6 Mbps). Resultado visual equivalente na prática.

---

### Mesclar dois vídeos lado a lado

```json
{
  "id": "side-by-side",
  "inputs": [
    { "file_url": "https://storage.googleapis.com/bucket/video1.mp4" },
    { "file_url": "https://storage.googleapis.com/bucket/video2.mp4" }
  ],
  "filters": [
    { "filter": "[0:v]scale=960:1080[left]" },
    { "filter": "[1:v]scale=960:1080[right]" },
    { "filter": "[left][right]hstack=inputs=2[v]" }
  ],
  "outputs": [
    {
      "options": [
        { "option": "-map",    "argument": "[v]" },
        { "option": "-map",    "argument": "0:a" },
        { "option": "-c:v",    "argument": "libx264" },
        { "option": "-c:a",    "argument": "aac" },
        { "option": "-crf",    "argument": 23 },
        { "option": "-preset", "argument": "fast" }
      ]
    }
  ],
  "webhook_url": "https://n8n.seu-dominio.com/webhook/video-pronto"
}
```

---

## 6. Response

### Modo síncrono — Sucesso (200 OK)

```json
{
  "job_id": "550e8400-e29b-41d4-a716-446655440000",
  "file_name": "output_cortado",
  "status": "done",
  "output": [{ "link": "https://storage.googleapis.com/bucket/output_cortado.mp4" }],
  "run_time": 12.4,
  "encoder": "h264_mf",
  "video_info": {
    "duration_s": 30.0,
    "width": 1920,
    "height": 1080,
    "size_mb": 22.5
  }
}
```

### Modo assíncrono — Job aceito (202 Accepted)

```json
{
  "job_id": "550e8400-e29b-41d4-a716-446655440000",
  "status": "queued"
}
```

### Payload do webhook — Sucesso

Enviado via `POST` para o `webhook_url` quando o job termina.

```json
{
  "job_id": "550e8400-e29b-41d4-a716-446655440000",
  "file_name": "moldura_output",
  "status": "done",
  "output": [{ "link": "https://storage.googleapis.com/bucket/moldura_output.mp4" }],
  "run_time": 18.7,
  "encoder": "h264_mf",
  "video_info": {
    "duration_s": 60.0,
    "width": 1080,
    "height": 1920,
    "size_mb": 45.1
  }
}
```

### Payload do webhook — Falha

```json
{
  "job_id": "550e8400-e29b-41d4-a716-446655440000",
  "status": "failed",
  "run_time": 2.1,
  "error": "ffmpeg error: invalid option '-crf' for output stream..."
}
```

---

## 7. Notas críticas de uso

### Pre-input options vs output options (seeking)

O posicionamento de `-ss` e `-t` muda o comportamento:

| Posição | Onde colocar | Comportamento |
|---|---|---|
| Pré-input | `inputs[].options` | **Fast seek** — rápido, pode perder alguns frames |
| Pós-input | `outputs[0].options` | **Frame-accurate** — lento, preciso ao frame |

Para conteúdo social (corte aproximado): use pré-input. Para corte exato: use output.

### Apenas o primeiro output é processado

O campo `outputs` aceita um array mas **apenas `outputs[0]` é lido**. Enviar múltiplos outputs não gera múltiplos arquivos — é um limitador atual do servidor.

### GPU auto-substitution e qualidade

Quando o servidor usa um encoder de hardware, o pipeline automaticamente:
- Substitui `libx264` → encoder detectado (ex: `h264_mf`)
- Remove `-crf`, `-preset`, `-x264-params` (não existem em hardware encoders)
- Injeta qualidade via bitrate (`-b:v 6M` por padrão para h264_mf/amf, ou `-cq 20 -b:v 0` para NVENC)

Para **forçar CPU** e controle de qualidade manual (ex: `-crf 15 -preset veryslow`):
```env
ENCODER=libx264
```

Para aumentar a qualidade do GPU encoder:
```env
HW_VIDEO_BITRATE_MBPS=10
```

### Argumento numérico

O campo `argument` aceita string ou número. Ambos funcionam:
```json
{ "option": "-crf", "argument": 18 }
{ "option": "-crf", "argument": "18" }
```

### Sem áudio no vídeo fonte

Se o vídeo de entrada não tem trilha de áudio e você mapear `0:a`, o FFmpeg retorna erro. Omita `-map 0:a` e `-c:a` nesses casos.

### File cache

O mesmo `file_url` em múltiplos jobs simultâneos faz apenas 1 download. O cache expira conforme `CACHE_TTL_MINUTES` (default: 60 min).

---

## 8. Error Responses

| Código | Causa |
|---|---|
| `401` | `X-API-Key` ausente ou incorreta |
| `400` | JSON inválido, `inputs` vazio, `outputs` vazio |
| `500` | Erro no FFmpeg (filtro mal formado, codec inválido, URL inacessível) |
| `503` | Fila cheia — todos os slots `MAX_CONCURRENT_JOBS` ocupados |

```json
{"error": "inputs cannot be empty"}
{"error": "outputs cannot be empty"}
{"error": "server at capacity, retry later"}
```

---

## 9. Problemas comuns

**URL inacessível**: o `file_url` deve ser publicamente acessível ou usar URL assinada válida. O servidor não passa credenciais adicionais no download.

**Filtro mal formado**: erros de sintaxe aparecem no campo `error` do webhook. Teste localmente antes:
```bash
ffmpeg -i video.mp4 -filter_complex "seu_filtro" -f null -
```

**Qualidade ruim com GPU**: adicione `HW_VIDEO_BITRATE_MBPS=10` no `.env` para aumentar o bitrate do encoder de hardware.

**503 frequente**: aumente `MAX_CONCURRENT_JOBS` no `.env`.
