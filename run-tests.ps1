# Script de Validação Pre-Build e Quality Assurance
# Este script roda todos os testes e gera a cobertura em formato visual (HTML)
# para que os engenheiros validem o projeto antes de comitar.

$ErrorActionPreference = "Stop"

Write-Host "Iniciando Pipeline de QA e Testes..." -ForegroundColor Cyan

# 1. Executar os testes e salvar o profile de cobertura
Write-Host "`n[1/3] Executando go test..." -ForegroundColor Yellow
go test -v -coverprofile=coverage.out ./...

# 2. Verificar se o profile foi gerado
if (!(Test-Path "coverage.out")) {
    Write-Host "Erro: Arquivo coverage.out nao foi gerado. Pipeline abortado." -ForegroundColor Red
    exit 1
}

# 3. Gerar o relatorio em HTML
Write-Host "`n[2/3] Gerando Relatorio de Cobertura (HTML)..." -ForegroundColor Yellow
go tool cover -html=coverage.out -o coverage.html

Write-Host "`n[3/3] Sucesso! Pipeline concluido." -ForegroundColor Green
Write-Host "Pode abrir o arquivo 'coverage.html' no seu navegador para ver o que falta ser testado." -ForegroundColor Cyan
