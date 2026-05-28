# Contributing to GME Open Server

Welcome. This document explains how the project is structured, how to add new features, and the conventions the codebase follows.

---

## Project structure

```
cmd/server/main.go          ← Composition Root — only place dependencies are instantiated
internal/
  domain/
    job/                    ← Job and Status types
    ports/                  ← Go interfaces (never import infra)
  application/              ← Use cases (orchestrate domain + ports)
  infra/
    ffmpeg/                 ← FFmpeg runner, GPU detector, file cache
    storage/                ← GCS and local storage adapters
    webhook/                ← HTTP webhook sender
  api/
    handlers/               ← HTTP handlers
    middleware/             ← Auth middleware
    router.go               ← Route registration
pkg/config/                 ← .env loading
```

**Dependency rule:** `api` → `application` → `domain/ports` ← `infra`

The application layer never imports infra. If you're in `internal/application/` and need a file cache, use the `IFileCache` interface — not the concrete type. `cmd/server/main.go` wires everything together.

---

## Coding conventions

### Zero external dependencies

`go.mod` has no `require` block. Every capability uses Go stdlib. Before adding any library, check if `net/http`, `crypto/*`, `encoding/*`, `os/exec`, etc. can do the job. They almost always can.

### Naming

- Use cases: `<Domain>MediaUseCase` (e.g. `VerticalMediaUseCase`)
- Constructors: `New<Type>` (e.g. `NewFileCache`)
- Sentinel errors: `Err<Reason>` (e.g. `ErrAtCapacity`)
- Interfaces: `I` prefix (`IFileCache`, `IStorage`) — convention of this project, keep it
- Private helpers: imperative snake_case (`buildScrollXExpr`, `cacheKey`)

### Routes

- Routes are always `/v1/media/<resource>` — never expose implementation details like `/v1/ffmpeg/*`
- Handler names: `Media<Name>Handler` — not `FFmpeg<Name>Handler`
- Struct names: `<Name>Request` — not `FFmpeg<Name>Request`

### Comments

Only comment the **why**, never the what. The code explains what it does. Add a comment only when the reason is non-obvious: a hidden constraint, a workaround for a specific FFmpeg behavior, a performance invariant. One line maximum.

### Error handling

- Validate at the HTTP boundary (handler). Trust types inside.
- Handle errors from subprocesses (FFmpeg) and network calls (webhooks, downloads).
- Don't add error handling for things that cannot happen.

---

## Adding a new route

Follow this checklist in order:

**1. Use case** — `internal/application/<name>_media.go`

```go
type <Name>Request struct { ... }

type <Name>MediaUseCase struct {
    cache   ports.IFileCache
    storage ports.IStorage
    runner  ports.IMediaProcessor
    webhook ports.IWebhookSender
    sem     chan struct{}
    // ...
}

func New<Name>MediaUseCase(...) *<Name>MediaUseCase { ... }
func (uc *<Name>MediaUseCase) Execute(req <Name>Request) (*job.Job, error) { ... }
func (uc *<Name>MediaUseCase) ExecuteSync(req <Name>Request) (*JobResult, error) { ... }
```

**2. Handler** — `internal/api/handlers/<name>.go`

```go
func Media<Name>Handler(uc *application.<Name>MediaUseCase) http.HandlerFunc {
    return func(w http.ResponseWriter, r *http.Request) {
        var req application.<Name>Request
        if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
            http.Error(w, `{"error":"invalid JSON"}`, http.StatusBadRequest)
            return
        }
        // validate, call uc.Execute or uc.ExecuteSync, write response
    }
}
```

**3. Register the route** — `internal/api/router.go`

```go
mux.Handle("POST /v1/media/<name>", middleware.Auth(apiKey,
    handlers.Media<Name>Handler(uc<Name>)))
```

**4. Wire the dependency** — `cmd/server/main.go`

```go
uc<Name> := application.New<Name>MediaUseCase(cache, storage, runner, webhook, cfg)
```

**5. Document it** — `docs/api/<name>.md` and update the API table in `README.md`

---

## Reporting bugs

Open an [issue](https://github.com/leomaquiaveli/gme-open-server/issues/new?template=bug_report.md) and include:

- Go version (`go version`)
- FFmpeg version (`ffmpeg -version`)
- OS and whether you have a GPU
- The JSON payload you sent
- The error response or webhook payload you received

---

## Submitting a pull request

1. Fork and create a branch: `git checkout -b feat/my-feature`
2. Make your changes, keeping commits focused
3. Run `go build ./...` and `go test ./...` to verify nothing is broken
4. Open a PR with a clear description of what and why

There is no CLA. By opening a PR you agree your contribution will be licensed under MIT.
