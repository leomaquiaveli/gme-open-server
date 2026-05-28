go build -o server.exe ./cmd/server
if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }
.\server.exe
