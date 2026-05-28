# Health Check

## 1. Overview

Retorna o status atual do servidor, o encoder de vídeo detectado no startup e a versão do binário. Usado para verificar se a instância está operacional antes de enviar jobs.

---

## 2. Endpoint

- **URL**: `/health`
- **Método**: `GET`

---

## 3. Request

### Headers

| Header | Obrigatório | Descrição |
|---|---|---|
| `X-API-Key` | Sim | Chave de autenticação |

### Exemplo de requisição

```http
GET /health HTTP/1.1
Host: localhost:8080
X-API-Key: sua_chave_aqui
```

```bash
curl -X GET http://localhost:8080/health \
  -H "X-API-Key: sua_chave_aqui"
```

---

## 4. Response

### Sucesso (200 OK)

```json
{
  "status": "ok",
  "gpu": "libx264",
  "version": "0.1.0"
}
```

| Campo | Tipo | Descrição |
|---|---|---|
| `status` | string | Sempre `"ok"` quando o servidor está operacional |
| `gpu` | string | Encoder detectado no startup: `h264_nvenc`, `h264_vaapi` ou `libx264` |
| `version` | string | Versão do binário |

### Valores possíveis de `gpu`

| Valor | Hardware | Velocidade de encode |
|---|---|---|
| `h264_nvenc` | NVIDIA GPU | 5–10× mais rápido que CPU |
| `h264_vaapi` | Intel / AMD GPU | 2–4× mais rápido que CPU |
| `libx264` | CPU (fallback) | Compatível com qualquer máquina |

### Erro (401 Unauthorized)

```json
{"message": "unauthorized"}
```

---

## 5. Uso

Use este endpoint para:
- Verificar se o servidor subiu corretamente após o deploy
- Confirmar qual modo de GPU está ativo antes de enviar jobs
- Monitoramento de uptime (configure o header `X-API-Key` no seu monitor)
