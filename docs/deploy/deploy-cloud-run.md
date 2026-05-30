# Deploy no Cloud Run — GME Open Server

Guia completo de implantação: do zero ao servidor processando vídeo no ar.
Feito pra ser seguido **sem precisar do vídeo** — cole os comandos, entenda o porquê de cada um, e tenha o servidor rodando.

---

## Visão geral — como as peças se encaixam

Quatro serviços do Google Cloud trabalham juntos. Entender o fluxo deixa todo o resto óbvio:

```
   GitHub (código)
        │
        │  1. Cloud Build clona e builda a imagem Docker
        ▼
   Artifact Registry (guarda a imagem Docker)
        │
        │  2. Cloud Run puxa a imagem daqui
        ▼
   Cloud Run (roda o servidor HTTP)
        │
        │  3. processa vídeo e salva o resultado
        ▼
   Cloud Storage / GCS (guarda os vídeos prontos)
```

| Peça | Papel | Analogia |
|---|---|---|
| **Cloud Build** | Transforma o código em imagem Docker, na nuvem | A "fábrica" que monta o pacote |
| **Artifact Registry** | Armazena as imagens Docker | O "depósito" de pacotes prontos |
| **Cloud Run** | Roda o container e expõe via HTTP | O "garçom" que atende as requisições |
| **Cloud Storage (GCS)** | Guarda os vídeos processados | O "armário" de arquivos |

Tudo é feito pelo **Cloud Shell** — o terminal do console GCP (ícone `>_` no canto superior direito). Ele já vem com `gcloud`, `git` e `docker` instalados, então você não precisa instalar nada na sua máquina.

---

## Pré-requisitos

1. Uma conta Google Cloud com **billing ativado** (Cloud Run e Build exigem, mas o custo é baixíssimo — ver seção de custos).
2. Um projeto GCP criado e selecionado.
3. Acesso ao Cloud Shell (qualquer navegador).

---

## Variáveis

No Cloud Shell o projeto já está ativo. Cole o bloco abaixo — preencha só o `API_KEY` com uma chave secreta de sua escolha (é ela que protege o servidor):

```bash
PROJECT_ID=$(gcloud config get-value project)   # pega automático do contexto atual
REGION="us-central1"
REPO="gme-open-server"
IMAGE="gme-open-server"
TAG="latest"
SERVICE="gme-open-server"
API_KEY="troque-por-uma-chave-secreta"
BUCKET="${PROJECT_ID}-video-storage"            # nome único gerado com o project ID
SA_NAME="gme-server-sa"                         # nome da service account (você escolhe)
SA_EMAIL="${SA_NAME}@${PROJECT_ID}.iam.gserviceaccount.com"
```

> **Por que o bucket usa o project ID?** Nomes de bucket GCS são **globais** — únicos no mundo inteiro, não só no seu projeto. Prefixar com `${PROJECT_ID}` garante que ninguém já tenha usado o nome.

> **Cuidado com a sessão:** as variáveis só vivem enquanto a aba do Cloud Shell estiver aberta. Se fechar e abrir de novo, **cole o bloco novamente**.

### Conferir as variáveis

Antes de criar qualquer recurso, confirme que tudo está preenchido:

```bash
echo "PROJECT_ID: $PROJECT_ID"
echo "REGION:     $REGION"
echo "REPO:       $REPO"
echo "IMAGE:      $IMAGE"
echo "TAG:        $TAG"
echo "SERVICE:    $SERVICE"
echo "BUCKET:     $BUCKET"
echo "SA_EMAIL:   $SA_EMAIL"
```

`PROJECT_ID` e `BUCKET` devem aparecer já resolvidos (ex: `BUCKET: meu-projeto-video-storage`). Se algum vier vazio, você não colou o bloco de variáveis (ou abriu sessão nova).

---

## Passo 1 — Ativar as APIs (só na primeira vez)

Por padrão, as APIs vêm desligadas num projeto novo. Liga as que vamos usar:

```bash
gcloud services enable cloudbuild.googleapis.com artifactregistry.googleapis.com containerscanning.googleapis.com run.googleapis.com iam.googleapis.com
```

| API | O que faz |
|---|---|
| `cloudbuild` | Builda imagens Docker na nuvem sem precisar de Docker local |
| `artifactregistry` | Repositório privado de imagens Docker dentro do GCP |
| `containerscanning` | Escaneia vulnerabilidades automaticamente em cada imagem enviada |
| `run` | Executa containers como serviço HTTP gerenciado (Cloud Run) |
| `iam` | Gerencia permissões e service accounts |

---

## Passo 2 — Criar o repositório no Artifact Registry e buildar a imagem

O Artifact Registry é onde a imagem Docker fica guardada. O Cloud Build puxa o código do GitHub, builda e salva aqui. O Cloud Run só consome daqui no final.

```bash
# Cria o repositório no Artifact Registry
gcloud artifacts repositories create $REPO --repository-format=docker --location=$REGION --description="GME Open Server images"

# Permissão para o Cloud Build publicar no Artifact Registry
gcloud projects add-iam-policy-binding $PROJECT_ID --member="serviceAccount:$(gcloud projects describe $PROJECT_ID --format='value(projectNumber)')@cloudbuild.gserviceaccount.com" --role="roles/artifactregistry.writer"

# Clona o repositório e builda a imagem, salvando no Artifact Registry
git clone https://github.com/leomaquiaveli/gme-open-server.git
cd gme-open-server
gcloud builds submit --tag "$REGION-docker.pkg.dev/$PROJECT_ID/$REPO/$IMAGE:$TAG" .
```

> O `gcloud builds submit` envia o código pro Cloud Build, que builda a imagem **na nuvem** e a empurra pro Artifact Registry. O build leva 2-4 min na primeira vez.

Confirmar que a imagem chegou:
```bash
gcloud artifacts docker images list "$REGION-docker.pkg.dev/$PROJECT_ID/$REPO"
```

---

## Passo 3 — Criar o bucket de storage (só na primeira vez)

```bash
gcloud storage buckets create gs://$BUCKET --location=$REGION --uniform-bucket-level-access
```

> O `--uniform-bucket-level-access` deixa o controle de acesso só por IAM (padrão moderno do Google), evitando a camada antiga de ACL.

**Tornar o bucket público (leitura):**

Os vídeos processados são entregues por URL. Para que essa URL abra direto no navegador ou seja consumida por outro sistema (ex: um agente de IA), o bucket precisa permitir leitura pública:

```bash
gcloud storage buckets add-iam-policy-binding gs://$BUCKET --member=allUsers --role=roles/storage.objectViewer
```

> Qualquer pessoa **com a URL** consegue baixar o arquivo. Para cenários mais sensíveis, o caminho é Signed URLs (URL com assinatura e expiração) — exige ajuste no código. Para começar e testar o fluxo completo, público resolve.

---

## Passo 4 — Criar a Service Account (só na primeira vez)

A Service Account (SA) é a "identidade" que o servidor usa pra escrever no bucket — sem precisar de senha ou chave JSON exposta.

```bash
# Cria a service account
gcloud iam service-accounts create $SA_NAME --display-name="GME Open Server"

# Dá à SA permissão de leitura e escrita no bucket
gcloud storage buckets add-iam-policy-binding gs://$BUCKET --member=serviceAccount:${SA_EMAIL} --role=roles/storage.objectAdmin
```

> **Princípio do menor privilégio:** a SA só tem acesso ao bucket, nada além. Se algo for comprometido, o estrago fica contido.

---

## Passo 5 — Deploy no Cloud Run

Esse é o comando que coloca o servidor no ar. Está em **linha única** de propósito — comando multilinha com `\` costuma quebrar ao colar no Cloud Shell:

```bash
gcloud run deploy $SERVICE --image "$REGION-docker.pkg.dev/$PROJECT_ID/$REPO/$IMAGE:$TAG" --region $REGION --set-env-vars "API_KEY=$API_KEY,STORAGE_TYPE=gcs,BUCKET_NAME=$BUCKET" --memory 8Gi --cpu 8 --concurrency 8 --min-instances 0 --max-instances 5 --timeout 300 --execution-environment=gen2 --service-account $SA_EMAIL --allow-unauthenticated
```

### Anatomia do comando — o que cada flag faz

| Flag | Valor | O que significa |
|---|---|---|
| `--image` | caminho no Artifact Registry | Qual imagem o Cloud Run vai rodar |
| `--set-env-vars` | API_KEY, STORAGE_TYPE, BUCKET_NAME | Variáveis de ambiente que o servidor lê no startup |
| `--memory` | `8Gi` | RAM por instância. Processar vídeo consome memória — 8GB é folgado |
| `--cpu` | `8` | vCPUs por instância. Mais CPU = encode mais rápido (sem GPU, a CPU é tudo) |
| `--concurrency` | `8` | Quantas requisições uma instância atende ao mesmo tempo |
| `--min-instances` | `0` | Quando ninguém usa, **zera** as instâncias = custo zero parado |
| `--max-instances` | `5` | Teto de instâncias. Trava o custo num pico — sem isso o default é 100 (perigoso) |
| `--timeout` | `300` | Tempo máx (segundos) que uma requisição pode durar. 300 = 5 min |
| `--execution-environment` | `gen2` | Ambiente de 2ª geração — CPU/rede mais rápidas, ideal pra vídeo |
| `--service-account` | a SA criada | Identidade usada pra acessar o GCS (sem expor credenciais) |
| `--allow-unauthenticated` | — | Permite chamadas sem login IAM. A segurança fica por conta do `X-API-Key` |

> **Por que `--max-instances 5` importa:** vídeo é caro de processar. Cada instância é 8 vCPU / 8GB. Sem teto, um pico de requisições poderia disparar 100 instâncias e gerar uma conta altíssima. Com 5, você tem um limite previsível.

### Sync vs Async — entenda antes de escolher como chamar

O servidor tem dois modos, e isso afeta como o Cloud Run se comporta:

- **Sync** (requisição **sem** `webhook_url`): a conexão HTTP **fica aberta** até o vídeo ficar pronto, e a resposta volta com o resultado. A CPU fica garantida o tempo todo (a requisição não terminou). **É o modo mais confiável no Cloud Run.** Ideal pra clipes curtos que cabem no `--timeout`.

- **Async** (requisição **com** `webhook_url`): o servidor responde `202` na hora e processa **em background**, mandando o resultado pro webhook quando termina. **Pegadinha:** por padrão o Cloud Run estrangula a CPU depois que a requisição HTTP retorna — então a tarefa em background pode travar. Se for usar async, adicione no deploy:
  ```
  --no-cpu-throttling
  ```
  Isso mantém a CPU ligada pra tarefa terminar (com `--min-instances 0`, a instância ainda zera quando tudo acaba).

> **Regra prática:** clipes curtos / poucas requisições → use **sync**, mais simples e confiável. Processamento longo ou em lote pesado → considere async + `--no-cpu-throttling`, ou uma arquitetura com fila + Cloud Run Jobs.

---

## Passo 6 — Verificar que subiu

```bash
URL=$(gcloud run services describe $SERVICE --region $REGION --format "value(status.url)")
curl "$URL/health"
```

Resposta esperada:
```json
{"status":"ok","gpu":"libx264","version":"0.1.0"}
```

> `"gpu":"libx264"` é o esperado no Cloud Run — **ele não tem GPU**, então o servidor usa o encoder de CPU (libx264). NVENC (GPU NVIDIA) só funciona em VM com placa dedicada.

---

## Passo 7 — Testar processando um vídeo de verdade

Pega a URL do serviço (se ainda não pegou no passo anterior):
```bash
URL=$(gcloud run services describe $SERVICE --region $REGION --format "value(status.url)")
```

Faz uma requisição **sync** (sem webhook) pra rota de pipeline — ela espera e devolve o link do resultado:

```bash
curl -X POST "$URL/v1/media/pipeline" \
  -H "X-API-Key: $API_KEY" \
  -H "Content-Type: application/json" \
  -d '{
    "inputs": [{ "file_url": "https://SEU-VIDEO-DE-TESTE.mp4" }],
    "filters": [],
    "outputs": [{
      "options": [
        { "option": "-map", "argument": "0:v" },
        { "option": "-map", "argument": "0:a" },
        { "option": "-c:v", "argument": "libx264" },
        { "option": "-preset", "argument": "veryfast" },
        { "option": "-c:a", "argument": "aac" },
        { "option": "-f", "argument": "mp4" }
      ]
    }]
  }'
```

A resposta traz o `output[].link` — a URL do vídeo no bucket, que abre direto no navegador.

> **Importante:** no Cloud Run sempre use `"-c:v": "libx264"` (CPU). Forçar `h264_nvenc` quebra com `Cannot load libcuda.so.1`, porque não há GPU. O `-preset veryfast` deixa o encode bem rápido.

---

## Atualizar versão (próximos deploys)

Sempre que houver commit novo no GitHub, esse comando único puxa o código, rebuilda a imagem e redeploya — tudo de uma vez. O `&&` faz cada etapa rodar só se a anterior deu certo (build que falha não deploya imagem velha):

```bash
cd ~/gme-open-server && git pull origin main && gcloud builds submit --tag "$REGION-docker.pkg.dev/$PROJECT_ID/$REPO/$IMAGE:$TAG" . && gcloud run deploy $SERVICE --image "$REGION-docker.pkg.dev/$PROJECT_ID/$REPO/$IMAGE:$TAG" --region $REGION
```

> As variáveis precisam estar setadas na sessão. Se abriu sessão nova, cole o bloco de variáveis antes.

### Opcional: atalho `redeploy`

Defina uma vez na sessão e depois só digite `redeploy`:

```bash
redeploy() {
  cd ~/gme-open-server && git pull origin main \
    && gcloud builds submit --tag "$REGION-docker.pkg.dev/$PROJECT_ID/$REPO/$IMAGE:$TAG" . \
    && gcloud run deploy $SERVICE --image "$REGION-docker.pkg.dev/$PROJECT_ID/$REPO/$IMAGE:$TAG" --region $REGION
}
```

(Pra deixar permanente, adicione no `~/.bashrc` do Cloud Shell.)

---

## Custos — o que você realmente paga

Tudo com `--min-instances 0`, então **parado = custo zero**. As ordens de grandeza:

| Serviço | Como cobra | Estimativa |
|---|---|---|
| **Cloud Run** | Por vCPU-segundo + GiB-segundo **enquanto processa** | Só paga quando há requisição. Idle = $0 |
| **Cloud Build** | Por minuto de build | 120 min/dia **grátis** — sobra pra esse projeto |
| **Artifact Registry** | Armazenamento (~$0.10/GB/mês) | ~5 imagens ≈ 1GB ≈ **centavos/mês** |
| **Cloud Storage** | Armazenamento (~$0.02/GB/mês) + saída de rede | Depende do volume de vídeo |

> **Higiene de imagens:** o Artifact Registry acumula uma imagem por build. Pra não juntar lixo, apague as antigas (mantenha a `latest`) ou configure uma **política de limpeza** automática mantendo só as N mais recentes.

---

## Troubleshooting — erros reais e como resolver

| Sintoma | Causa | Solução |
|---|---|---|
| `Cannot load libcuda.so.1` no FFmpeg | Payload pediu `h264_nvenc`, mas Cloud Run não tem GPU | Use `"-c:v": "libx264"` no payload |
| Escalonamento ficou `0 a 100` | O comando de deploy quebrou no paste (multilinha) e perdeu `--max-instances` | Use a versão em **linha única**, ou `gcloud run services update $SERVICE --region $REGION --max-instances 5` |
| Variáveis vazias no `echo` | Sessão nova do Cloud Shell, ou colou com `bash` na frente | Cole o bloco de variáveis de novo, sem `bash` |
| Vídeo baixa em vez de tocar no navegador | Arquivo subiu como `application/octet-stream` | Já corrigido no servidor (infere `video/mp4` pela extensão). Rebuilde com a versão mais recente |
| `git clone` falha: "already exists" | A pasta já existe de um clone anterior | `rm -rf gme-open-server` e clone de novo, ou `cd gme-open-server && git pull` |
| Tarefa async não termina | CPU estrangulada após o `202` | Adicione `--no-cpu-throttling` no deploy, ou use modo sync |

### Comandos de diagnóstico úteis

```bash
# Logs em tempo real
gcloud run services logs tail $SERVICE --region $REGION

# Últimos 50 logs
gcloud run services logs read $SERVICE --region $REGION --limit 50

# Ver a config ativa (imagem, escalonamento, etc.)
gcloud run services describe $SERVICE --region $REGION
```

---

## Notas finais

- **ADC (Application Default Credentials):** não passamos `SERVICE_ACCOUNT_CREDENTIALS`. O servidor detecta que está no GCP e pega o token pelo metadata server automaticamente, usando a Service Account associada. Zero segredos no ambiente.
- **Segurança do endpoint:** `--allow-unauthenticated` deixa o endpoint público, mas **toda rota (exceto `/health`) exige o header `X-API-Key`**. Trate essa chave como secreta.
- **GPU:** Cloud Run não tem. Pra velocidade máxima com NVENC, o caminho é uma VM com placa NVIDIA. No Cloud Run, o acelerador é o `-preset` mais rápido (`veryfast`, `superfast`).
