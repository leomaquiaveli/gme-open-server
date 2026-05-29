# Fase 1: compilação
FROM golang:1.26-alpine AS builder
WORKDIR /app
COPY go.mod ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o gme-open-server ./cmd/server

# Fase 2: runtime — imagem mínima com FFmpeg
FROM ubuntu:24.04
RUN apt-get update && apt-get install -y --no-install-recommends \
    ffmpeg \
    ca-certificates \
    && rm -rf /var/lib/apt/lists/*

RUN useradd -r -u 1001 -g root gme
WORKDIR /app
COPY --from=builder /app/gme-open-server .

USER gme
EXPOSE 8080
CMD ["/app/gme-open-server"]
