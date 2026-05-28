# Media Vertical

## 1. Overview

Transforma qualquer vídeo em formato vertical 9:16 (1080×1920), com crop inteligente e posicionamento horizontal configurável.

**Dois modos de operação:**
- **Sem `webhook_url`** → síncrono: bloqueia até terminar e retorna o resultado direto
- **Com `webhook_url`** → assíncrono: retorna `202` imediatamente, resultado entregue via POST no webhook

O vídeo de entrada é cacheado por URL — múltiplos clips do mesmo vídeo fazem apenas 1 download.

---

## 2. Endpoint

- **URL**: `/v1/media/vertical`
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
| `input_url` | string | Sim | — | URL pública do vídeo fonte |
| `file_name` | string | Não | `{job_id}.mp4` | Nome do arquivo de saída |
| `scroll_x` | float | Não | `0` | Posição estática do crop: `-100`=esquerda, `0`=centro, `+100`=direita. Ignorado se `keyframes` tiver `scroll_x`. |
| `zoom` | float | Não | `0` | Zoom estático: `0`=sem zoom, `100`=2x, `200`=3x, `500`=6x (máx). Ignorado se `keyframes` tiver `zoom`. |
| `keyframes` | array | Não | — | Animação de câmera ao longo do tempo. Cada item: `{ "time": segundos, "scroll_x": -100..100, "zoom": 0..500 }`. `scroll_x` e `zoom` são opcionais por keyframe e podem aparecer juntos. |
| `start` | float | Não | `0` | Início do clipe em segundos |
| `duration` | float | Não | até o fim | Duração do clipe em segundos |
| `end` | float | Não | — | Fim do clipe em segundos (alternativa ao `duration`) |
| `crf` | int | Não | `23` | Qualidade do encode: `1`=melhor, `51`=pior. Valores comuns: 18 (alta), 23 (balanceado), 28 (rápido) |
| `preset` | string | Não | `fast` | Velocidade/qualidade do encode: `ultrafast`, `veryfast`, `faster`, `fast`, `medium`, `slow`, `veryslow` |
| `webhook_url` | string | Não | — | URL que recebe o resultado quando o job termina |

> `duration` e `end` são mutuamente exclusivos — use um ou outro, não os dois.

---

## 4. Como o scroll_x funciona

O crop sempre mantém a proporção 9:16. O `scroll_x` controla de qual parte horizontal do vídeo original o crop é extraído:

```
scroll_x = -100  →  borda esquerda
scroll_x =    0  →  centro (default)
scroll_x = +100  →  borda direita
```

**Exemplo visual** — vídeo 16:9 de 1920×1080:
- Largura do crop 9:16 = `1080 * 9/16 = 607px`
- Margem disponível para mover = `1920 - 607 = 1313px`
- `scroll_x = 0`   → x = 656 (centro)
- `scroll_x = -50` → x = 328 (mais à esquerda)
- `scroll_x = +50` → x = 984 (mais à direita)

---

## 5. Como o zoom funciona

O `zoom` é um **percentual de zoom adicional** sobre o campo de visão original e se aplica ao clip inteiro:

```
zoom =   0  →  sem zoom (campo de visão total, fator 1.0)
zoom =  50  →  50% a mais (fator 1.5)
zoom = 100  →  2x zoom (fator 2.0)
zoom = 200  →  3x zoom (fator 3.0)
zoom = 500  →  6x zoom (fator 6.0) — máximo
```

**Internamente:** `fator = 1 + zoom / 100`. O crop 9:16 é dividido pelo fator → área de crop menor → imagem parece mais próxima após `scale=1080:1920`.

**Zoom e scroll_x podem ser animados juntos ou separados.**

Internamente o servidor usa dois filtros distintos:
- **scroll_x** → animado via `crop` (parâmetro `x` reavaliado por frame) — rápido
- **zoom via keyframes** → animado via `zoompan` (suporta expressões com `t` em `z`, `x`, `y`) — ligeiramente mais lento que zoom estático

Quando só `zoom` estático (sem keyframes de zoom) é usado, o servidor usa apenas `crop` com fator fixo — caminho mais rápido.

---

## 6. Exemplos

### Clip vertical — centro, sem corte de tempo

```json
{
  "input_url": "https://storage.googleapis.com/bucket/video.mp4",
  "file_name": "clip-vertical.mp4"
}
```

```bash
curl -X POST http://localhost:8080/v1/media/vertical \
  -H "X-API-Key: sua_chave_aqui" \
  -H "Content-Type: application/json" \
  -d '{"input_url":"https://storage.googleapis.com/bucket/video.mp4","file_name":"clip-vertical.mp4"}'
```

---

### Clip vertical com posição e recorte de tempo

```json
{
  "input_url": "https://storage.googleapis.com/bucket/video.mp4",
  "file_name": "destaque-30s.mp4",
  "scroll_x": -30,
  "start": 10.0,
  "duration": 30.0
}
```

```bash
curl -X POST http://localhost:8080/v1/media/vertical \
  -H "X-API-Key: sua_chave_aqui" \
  -H "Content-Type: application/json" \
  -d '{"input_url":"https://storage.googleapis.com/bucket/video.mp4","file_name":"destaque-30s.mp4","scroll_x":-30,"start":10.0,"duration":30.0}'
```

---

### Usando `end` em vez de `duration`

```json
{
  "input_url": "https://storage.googleapis.com/bucket/video.mp4",
  "start": 15.0,
  "end": 45.0
}
```

```bash
curl -X POST http://localhost:8080/v1/media/vertical \
  -H "X-API-Key: sua_chave_aqui" \
  -H "Content-Type: application/json" \
  -d '{"input_url":"https://storage.googleapis.com/bucket/video.mp4","start":15.0,"end":45.0}'
```

---

### Posicionamento dinâmico com keyframes (podcast com dois apresentadores)

Quando a câmera foca em pessoas em posições diferentes, o `keyframes` move a janela de crop no segundo exato da transição — sem precisar cortar o vídeo.

```json
{
  "input_url": "https://storage.googleapis.com/bucket/podcast.mp4",
  "file_name": "podcast-vertical.mp4",
  "keyframes": [
    { "time": 0,  "scroll_x": -35 },
    { "time": 18, "scroll_x": 40  },
    { "time": 45, "scroll_x": -35 },
    { "time": 61, "scroll_x": 40  }
  ],
  "crf": 23,
  "preset": "fast"
}
```

```bash
curl -X POST http://localhost:8080/v1/media/vertical \
  -H "X-API-Key: sua_chave_aqui" \
  -H "Content-Type: application/json" \
  -d '{"input_url":"https://storage.googleapis.com/bucket/podcast.mp4","file_name":"podcast-vertical.mp4","keyframes":[{"time":0,"scroll_x":-35},{"time":18,"scroll_x":40},{"time":45,"scroll_x":-35},{"time":61,"scroll_x":40}],"crf":23,"preset":"fast"}'
```

> **Como funciona:** o servidor constrói a expressão FFmpeg `if(lt(t,18), x(-35), if(lt(t,45), x(40), ...))` avaliada frame a frame. A transição é instantânea no segundo exato — ideal para quando o Gemini mapeia o array de tempo e posição de cada câmera.

---

### Modo assíncrono com webhook

```json
{
  "input_url": "https://storage.googleapis.com/bucket/video.mp4",
  "file_name": "clip.mp4",
  "scroll_x": 50,
  "start": 0,
  "duration": 60,
  "webhook_url": "https://seu-webhook.com/callback"
}
```

```bash
curl -X POST http://localhost:8080/v1/media/vertical \
  -H "X-API-Key: sua_chave_aqui" \
  -H "Content-Type: application/json" \
  -d '{"input_url":"https://storage.googleapis.com/bucket/video.mp4","file_name":"clip.mp4","scroll_x":50,"start":0,"duration":60,"webhook_url":"https://seu-webhook.com/callback"}'
```

---

### Clip com zoom estático

```json
{
  "input_url": "https://storage.googleapis.com/bucket/video.mp4",
  "file_name": "zoom-25.mp4",
  "zoom": 25,
  "scroll_x": -20
}
```

Zoom de 25% adicional (fator 1.25), câmera levemente para a esquerda.

---

### Zoom animado por keyframes

```json
{
  "input_url": "https://storage.googleapis.com/bucket/video.mp4",
  "file_name": "zoom-animado.mp4",
  "keyframes": [
    { "time": 0,  "scroll_x": -30 },
    { "time": 5,  "scroll_x": -30, "zoom": 80 },
    { "time": 20, "scroll_x": 30,  "zoom": 0  }
  ]
}
```

De 0 a 5s: câmera na esquerda, sem zoom. Em t=5s: entra zoom de 80% (fator 1.8) mantendo câmera na esquerda. Em t=20s: câmera vai para a direita e zoom retorna a 0.

---

## 7. Response

### Síncrono — Sucesso (200 OK)

```json
{
  "job_id": "550e8400-e29b-41d4-a716-446655440000",
  "file_name": "clip-vertical.mp4",
  "status": "done",
  "output": [
    { "link": "https://storage.googleapis.com/bucket/clip-vertical.mp4" }
  ],
  "run_time": 8.432,
  "encoder": "h264_nvenc",
  "video_info": {
    "duration_s": 30.0,
    "width": 1080,
    "height": 1920,
    "size_mb": 45.231
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

### Payload do webhook — Sucesso

```json
{
  "job_id": "550e8400-e29b-41d4-a716-446655440000",
  "file_name": "clip.mp4",
  "status": "done",
  "output": [
    { "link": "https://storage.googleapis.com/bucket/clip.mp4" }
  ],
  "run_time": 8.432,
  "encoder": "h264_nvenc",
  "video_info": {
    "duration_s": 60.0,
    "width": 1080,
    "height": 1920,
    "size_mb": 90.123
  }
}
```

### Payload do webhook — Falha

```json
{
  "job_id": "550e8400-e29b-41d4-a716-446655440000",
  "status": "failed",
  "run_time": 1.2,
  "error": "download: status 403"
}
```

---

## 8. Error Responses

### input_url ausente (400)

```json
{"error": "input_url cannot be empty"}
```

### scroll_x fora do range (400)

```json
{"error": "scroll_x must be between -100 and 100"}
```

### zoom fora do range (400)

```json
{"error": "zoom must be between 0 and 500"}
```

### duration e end simultâneos (400)

```json
{"error": "use either duration or end, not both"}
```

### end menor ou igual ao start (400)

```json
{"error": "end must be greater than start"}
```

### Servidor sem capacidade (503)

```json
{"error": "server at capacity, retry later"}
```

---

## 9. Notas

1. **GPU automática**: se NVENC ou VAAPI estiver disponível, o encoder é substituído automaticamente. O campo `encoder` na resposta indica qual foi usado.

2. **Seek preciso**: o `-ss` é inserido antes do `-i` (fast seek), então a performance é alta mesmo para vídeos longos. O corte é frame-accurate.

3. **Áudio preservado**: o áudio original é mantido e re-encodado em AAC. Se o vídeo não tiver áudio, o output também não terá.

4. **Proporcão garantida**: o output é sempre `1080×1920`, independente da resolução do input.
