# GME Open Server — Guia de Instalação em VM (Compute Engine)

Instalação direta via Docker Compose. Sem Traefik, sem DNS — acesso por IP na porta 8080.

> Se já tiver Docker instalado, vá direto para o Passo 3.

---

## 1. Criar a VM no GCP

No Console GCP -> Compute Engine -> Create Instance:

- **Image**: Ubuntu 24.04 LTS
- **vCPUs/GPU**: Instâncias como `n2-standard-48` (apenas CPU) ou anexe uma NVIDIA T4/L4.
- **Boot disk**: SSD, mínimo 100GB
- **Service account**: padrão do Compute Engine (já tem acesso ao Artifact Registry do projeto)
- **Firewall**: marcar "Allow HTTP traffic" — ou abrir a porta 8080 manualmente (passo abaixo)

Abrir porta 8080 via gcloud (rodar uma vez, no seu terminal local):

```bash
gcloud compute firewall-rules create gme-server-8080 \
  --allow tcp:8080 \
  --target-tags gme-server \
  --description "GME Open Server"
```

---

## 2. Instalar Driver da NVIDIA (Opcional, se a VM tiver GPU)

Por padrão, a imagem "Ubuntu 24.04 LTS" do GCP vem **sem** os drivers da placa de vídeo. Se você escolheu uma VM com GPU (ex: L4 ou T4), execute o script oficial do Google para baixar e instalar o driver mais recente:

```bash
curl -sL https://raw.githubusercontent.com/GoogleCloudPlatform/compute-gpu-installation/main/linux/install_gpu_driver.py | sudo python3 - --install-type ubuntu
```

*Nota: Esse comando pode demorar alguns minutos. Após terminar, você pode rodar o comando `nvidia-smi` para confirmar que a placa de vídeo foi reconhecida.*

---

## 3. Instalar Docker (repositório oficial) e NVIDIA Container Toolkit

```bash
# 1. Instalar dependências base e o repositório oficial Docker
sudo apt-get update
sudo apt-get install -y ca-certificates curl
sudo install -m 0755 -d /etc/apt/keyrings
sudo curl -fsSL https://download.docker.com/linux/ubuntu/gpg -o /etc/apt/keyrings/docker.asc
sudo chmod a+r /etc/apt/keyrings/docker.asc

echo \
  "deb [arch=$(dpkg --print-architecture) signed-by=/etc/apt/keyrings/docker.asc] https://download.docker.com/linux/ubuntu \
  $(. /etc/os-release && echo "$VERSION_CODENAME") stable" | \
  sudo tee /etc/apt/sources.list.d/docker.list > /dev/null

sudo apt-get update
sudo apt-get install -y docker-ce docker-ce-cli containerd.io docker-buildx-plugin docker-compose-plugin

# 2. Permitir rodar docker sem sudo
sudo usermod -aG docker $USER
newgrp docker

# 3. NVIDIA Container Toolkit (Apenas se tiver GPU!)
# Isso permite que os containers "enxerguem" a placa de vídeo da VM
curl -fsSL https://nvidia.github.io/libnvidia-container/gpgkey | sudo gpg --dearmor -o /usr/share/keyrings/nvidia-container-toolkit-keyring.gpg \
  && curl -s -L https://nvidia.github.io/libnvidia-container/stable/deb/nvidia-container-toolkit.list | \
    sed 's#deb https://#deb [signed-by=/usr/share/keyrings/nvidia-container-toolkit-keyring.gpg] https://#g' | \
    sudo tee /etc/apt/sources.list.d/nvidia-container-toolkit.list

sudo apt-get update
sudo apt-get install -y nvidia-container-toolkit
sudo nvidia-ctk runtime configure --runtime=docker
sudo systemctl restart docker
```

---

## 4. Autenticar no Artifact Registry

A VM usa a service account padrão do Compute Engine — sem precisar de credenciais manuais:

```bash
gcloud auth configure-docker us-central1-docker.pkg.dev
```

---

## 4. Criar o arquivo `.env`

```bash
nano .env
```

Cole o conteúdo abaixo (ajuste os valores):

```env
API_KEY=sua-chave-secreta
STORAGE_TYPE=gcs
BUCKET_NAME=your-bucket-name
MAX_CONCURRENT_JOBS=48
CACHE_TTL_MINUTES=60
```

> `MAX_CONCURRENT_JOBS` = número de vCPUs da VM. Em 48 vCPUs use 48. Em 288 vCPUs use 288.

**Autenticação no GCS — duas opções:**

- **Opção recomendada (sem credenciais):** a VM usa a Service Account padrão do Compute Engine. Basta garantir que essa SA tem a role `Storage Object Admin` no bucket. Nenhuma variável de credencial é necessária — o servidor detecta automaticamente via metadata server do GCP.
- **Opção alternativa (chave JSON):** adicione `SERVICE_ACCOUNT_CREDENTIALS={"type":"service_account",...}` no `.env` em uma única linha. Útil quando a VM não está no GCP ou a SA padrão não tem acesso ao bucket.

---

## 7. Criar o arquivo `docker-compose.yml`

```bash
nano docker-compose.yml
```

Cole o conteúdo:

```yaml
services:
  gme-server:
    image: us-central1-docker.pkg.dev/YOUR_PROJECT_ID/gme-open-server/gme-open-server:latest
    container_name: gme-server
    restart: unless-stopped
    user: "0"
    ports:
      - "8080:8080"
    env_file:
      - .env
    volumes:
      - gme_cache:/tmp/gme
    # Adicione a chave abaixo se a VM possuir placa NVIDIA para o container enxergá-la
    deploy:
      resources:
        reservations:
          devices:
            - driver: nvidia
              count: all
              capabilities: [gpu]

volumes:
  gme_cache:
```

---

## 7. Subir o servidor

```bash
docker compose up -d
```

---

## 8. Verificar se subiu

```bash
# Health check
curl http://localhost:8080/health

# Resposta esperada:
# {"gpu":"libx264","status":"ok","version":"0.1.0"}
```

---

## 9. Comandos úteis

```bash
# Ver logs em tempo real
docker compose logs -f gme-server

# Parar
docker compose down

# Atualizar para nova imagem
docker compose pull && docker compose up -d

# Ver uso de CPU/memória
docker stats gme-server

docker compose up -d --force-recreate gme-server

```

---

## Referência de MAX_CONCURRENT_JOBS por VM

| vCPUs | MAX_CONCURRENT_JOBS | Uso esperado |
|-------|-------------|--------------|
| 8     | 8           | Teste inicial |
| 48    | 48          | Produção leve |
| 96    | 96          | Produção média |
| 288   | 288         | Escala máxima |

Para maximizar throughput (mais jobs simultâneos), use `-threads 1` por job e `MAX_CONCURRENT_JOBS` igual ao número de vCPUs.
Para minimizar latência por job individual, use `-threads 4` e `MAX_CONCURRENT_JOBS = vCPUs / 4`.
