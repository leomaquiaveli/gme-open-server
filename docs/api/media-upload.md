# Media Upload

## 1. Overview

Faz upload de um arquivo para o storage configurado (GCS ou local). Aceita dois métodos:

- **Via URL**: o servidor faz o download da URL informada e re-envia para o storage
- **Via multipart form**: você envia o arquivo diretamente no body da requisição

Retorna a URL pública do arquivo após o upload.

---

## 2. Endpoint

- **URL**: `/v1/media/upload`
- **Método**: `POST`

---

## 3. Request

### Headers

| Header | Obrigatório | Descrição |
|---|---|---|
| `X-API-Key` | Sim | Chave de autenticação |
| `Content-Type` | Depende | `application/json` para upload via URL; `multipart/form-data` para upload de arquivo |

---

## 4. Opção A — Upload via URL (JSON)

O servidor baixa o arquivo da URL e envia para o storage.

### Body Parameters

| Parâmetro | Tipo | Obrigatório | Descrição |
|---|---|---|---|
| `file_url` | string | Sim | URL pública do arquivo a ser copiado |
| `file_name` | string | Não | Nome do arquivo no storage. Se omitido, é extraído da URL |

### Exemplo

```json
{
  "file_url": "https://exemplo.com/video.mp4",
  "file_name": "meu-video.mp4"
}
```

```bash
curl -X POST http://localhost:8080/v1/media/upload \
  -H "X-API-Key: sua_chave_aqui" \
  -H "Content-Type: application/json" \
  -d '{"file_url":"https://exemplo.com/video.mp4","file_name":"meu-video.mp4"}'
```

---

## 5. Opção B — Upload de arquivo (multipart)

Envio direto de arquivo pelo corpo da requisição.

```bash
curl -X POST http://localhost:8080/v1/media/upload \
  -H "X-API-Key: sua_chave_aqui" \
  -F "file=@/caminho/local/video.mp4"
```

> O nome do campo do formulário deve ser `file`.

---

## 6. Response

### Sucesso (200 OK)

```json
{
  "file_url": "https://storage.googleapis.com/bucket/meu-video.mp4",
  "filename": "meu-video.mp4",
  "public": true
}
```

---

## 7. Notas

1. **Content-Type automático**: se não informado, o servidor usa `application/octet-stream`. Para garantir streaming correto no GCS, prefira enviar o Content-Type correto via multipart.
2. **Colisões de nome**: arquivos com o mesmo nome no bucket sobrescrevem a versão anterior (comportamento padrão do GCS com `uploadType=media`).
3. **Uso típico**: ideal para pré-carregar arquivos no bucket antes de processar com outros endpoints, ou para integrar com pipelines de N8N que precisam mover arquivos entre storages.
