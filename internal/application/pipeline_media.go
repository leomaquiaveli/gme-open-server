package application

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"math"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/leomaquiaveli/gme-open-server/internal/domain/job"
	"github.com/leomaquiaveli/gme-open-server/internal/domain/ports"
)

var ErrAtCapacity = errors.New("worker pool at capacity")

type pendingPipelineJob struct {
	j   *job.Job
	req PipelineRequest
}

// --- Tipos de request ---

type InputFile struct {
	FileURL string         `json:"file_url"`
	Options []FFmpegOption `json:"options,omitempty"` // flags pré-input: -ss, -t, etc.
}

type FilterBlock struct {
	Filter string `json:"filter"`
}

// FFmpegOption usa json.RawMessage porque "argument" pode ser string ou número.
type FFmpegOption struct {
	Option   string          `json:"option"`
	Argument json.RawMessage `json:"argument"`
}

type OutputSpec struct {
	Options []FFmpegOption `json:"options"`
}

type PipelineRequest struct {
	ID         string        `json:"id"`
	FileName   string        `json:"file_name"`
	Inputs     []InputFile   `json:"inputs"`
	Filters    []FilterBlock `json:"filters"`
	Outputs    []OutputSpec  `json:"outputs"`
	WebhookURL string        `json:"webhook_url"`
}

// --- Resultado do job ---

type VideoInfo struct {
	DurationS float64 `json:"duration_s"`
	Width     int     `json:"width"`
	Height    int     `json:"height"`
	SizeMB    float64 `json:"size_mb"`
}

type JobResult struct {
	JobID     string       `json:"job_id"`
	FileName  string       `json:"file_name,omitempty"`
	Status    string       `json:"status"`
	Output    []OutputLink `json:"output,omitempty"`
	RunTime   float64      `json:"run_time"`
	Encoder   string       `json:"encoder,omitempty"`
	VideoInfo *VideoInfo   `json:"video_info,omitempty"`
	Error     string       `json:"error,omitempty"`
}

type OutputLink struct {
	Link string `json:"link"`
}

// runResult é o retorno interno do run() — mais rico que o publicURL simples.
type runResult struct {
	publicURL string
	fileName  string
	encoder   string
	videoInfo *VideoInfo
}

// --- Use Case ---

type PipelineMediaUseCase struct {
	dl       *Downloader
	storage  ports.IStorage
	runner   ports.IMediaProcessor
	webhook  ports.IWebhookSender
	sem      chan struct{}
	jobQueue chan pendingPipelineJob
	workDir  string
}

func NewPipelineMediaUseCase(
	cache ports.IFileCache,
	storage ports.IStorage,
	runner ports.IMediaProcessor,
	webhook ports.IWebhookSender,
	maxWorkers int,
	workDir string,
) *PipelineMediaUseCase {
	queueSize := maxWorkers * 10
	if queueSize < 500 {
		queueSize = 500
	}
	uc := &PipelineMediaUseCase{
		dl:       NewDownloader(cache, storage, workDir),
		storage:  storage,
		runner:   runner,
		webhook:  webhook,
		sem:      make(chan struct{}, maxWorkers),
		jobQueue: make(chan pendingPipelineJob, queueSize),
		workDir:  workDir,
	}
	go uc.dispatch()
	return uc
}

// dispatch lê da fila e despacha jobs quando um slot do semáforo fica disponível.
// Separa "aceitar o job" (202 imediato) de "executar o job" (quando há capacidade).
func (uc *PipelineMediaUseCase) dispatch() {
	for p := range uc.jobQueue {
		uc.sem <- struct{}{}
		go func(pending pendingPipelineJob) {
			var once sync.Once
			release := func() { once.Do(func() { <-uc.sem }) }
			defer release()
			res, err := uc.run(pending.j, pending.req, release)
			uc.sendWebhook(pending.j, pending.req.WebhookURL, res, err)
		}(p)
	}
}

// Execute aceita o job na fila e retorna o job_id imediatamente (202).
// O job é executado quando um slot do semáforo fica disponível.
// Retorna ErrAtCapacity apenas se a fila estiver cheia (raro — capacidade = maxWorkers*10).
func (uc *PipelineMediaUseCase) Execute(req PipelineRequest) (string, error) {
	if err := validate(req); err != nil {
		return "", err
	}
	j := job.New(req.WebhookURL)

	select {
	case uc.jobQueue <- pendingPipelineJob{j, req}:
		return j.ID, nil
	default:
		return "", ErrAtCapacity
	}
}

// ExecuteSync processa de forma síncrona e retorna o resultado diretamente.
// Bloqueia até ter slot disponível — nunca retorna 503 por capacidade,
// pois o cliente (N8N, etc.) já está esperando a resposta HTTP.
func (uc *PipelineMediaUseCase) ExecuteSync(req PipelineRequest) (*JobResult, error) {
	if err := validate(req); err != nil {
		return nil, err
	}

	uc.sem <- struct{}{}
	var once sync.Once
	release := func() { once.Do(func() { <-uc.sem }) }
	defer release()

	j := job.New("")
	res, processErr := uc.run(j, req, release)

	result := &JobResult{
		JobID:     j.ID,
		FileName:  res.fileName,
		Status:    string(j.Status),
		RunTime:   round3(j.RunTime),
		Encoder:   res.encoder,
		VideoInfo: res.videoInfo,
	}
	if res.publicURL != "" {
		result.Output = []OutputLink{{Link: res.publicURL}}
	}
	if processErr != nil {
		result.Error = processErr.Error()
	}
	return result, processErr
}

// run é o núcleo do processamento — compartilhado pelo modo síncrono e assíncrono.
func (uc *PipelineMediaUseCase) run(j *job.Job, req PipelineRequest, releaseSlot func()) (runResult, error) {
	label := req.ID
	if label == "" {
		label = j.ID[:8]
	}
	encoder := uc.runner.GetEncoder()

	j.Start()
	log.Printf("[%s] job iniciado | inputs: %d | job_id: %s | encoder: %s",
		label, len(req.Inputs), j.ID, encoder)

	t0 := time.Now()
	localPaths, err := uc.downloadInputs(req.Inputs)
	if err != nil {
		j.Fail()
		log.Printf("[%s] download falhou (%.1fs): %v", label, time.Since(t0).Seconds(), err)
		return runResult{}, err
	}
	log.Printf("[%s] download concluído | %d arquivo(s) | %.1fs", label, len(localPaths), time.Since(t0).Seconds())

	defer func() {
		for _, inp := range req.Inputs {
			uc.dl.Release(inp.FileURL)
		}
	}()

	outName := outputFileName(req.FileName, j.ID)
	outPath := filepath.Join(uc.workDir, "outputs", outName)
	if err := os.MkdirAll(filepath.Dir(outPath), 0o755); err != nil {
		j.Fail()
		return runResult{}, fmt.Errorf("create output dir: %w", err)
	}

	args, err := buildFFmpegArgs(req.Inputs, localPaths, req.Filters, req.Outputs, outPath)
	if err != nil {
		j.Fail()
		return runResult{}, err
	}

	tFFmpeg := time.Now()
	ffmpegErr := uc.runner.RunFFmpeg(args)
	releaseSlot() // libera slot de CPU antes do upload — upload é IO, não CPU
	if ffmpegErr != nil {
		j.Fail()
		log.Printf("[%s] ffmpeg falhou (%.1fs): %v", label, time.Since(tFFmpeg).Seconds(), ffmpegErr)
		return runResult{}, ffmpegErr
	}
	// Detecta saída vazia — ocorre quando o encoder inicia mas não grava nenhum frame
	// (ex: NVENC em Optimus com flags conflitantes ou sessão mal inicializada).
	if info, statErr := os.Stat(outPath); statErr != nil || info.Size() < 1024 {
		j.Fail()
		log.Printf("[%s] ffmpeg retornou sucesso mas arquivo de saída está vazio ou ausente (%.1fs)", label, time.Since(tFFmpeg).Seconds())
		return runResult{}, fmt.Errorf("ffmpeg produced empty output — encoder may have silently failed")
	}
	log.Printf("[%s] ffmpeg concluído | %.1fs | encoder: %s", label, time.Since(tFFmpeg).Seconds(), encoder)

	var vi *VideoInfo
	if info, err := uc.runner.ProbeMedia(outPath); err != nil {
		log.Printf("[%s] probe falhou: %v", label, err)
	} else {
		vi = &VideoInfo{
			DurationS: round3(info.Duration),
			Width:     info.Width,
			Height:    info.Height,
			SizeMB:    round3(float64(info.SizeBytes) / 1024 / 1024),
		}
		log.Printf("[%s] output: %s | %dx%d | %.3f MB",
			label, formatDuration(info.Duration), info.Width, info.Height, vi.SizeMB)
	}

	tUpload := time.Now()
	log.Printf("[%s] upload iniciado → %s", label, outName)
	publicURL, err := uc.storage.Upload(outPath, "")
	if err != nil {
		j.Fail()
		log.Printf("[%s] upload falhou (%.1fs): %v", label, time.Since(tUpload).Seconds(), err)
		return runResult{}, fmt.Errorf("upload output: %w", err)
	}
	log.Printf("[%s] upload concluído | %.1fs → %s", label, time.Since(tUpload).Seconds(), publicURL)

	if strings.HasPrefix(publicURL, "https://") {
		if err := os.Remove(outPath); err != nil {
			log.Printf("[%s] cleanup: %v", label, err)
		}
		uc.dl.Invalidate(publicURL)
	}

	j.Complete()
	log.Printf("[%s] job concluído | total: %.3fs", label, j.RunTime)
	return runResult{publicURL: publicURL, fileName: req.FileName, encoder: encoder, videoInfo: vi}, nil
}

func (uc *PipelineMediaUseCase) downloadInputs(inputs []InputFile) ([]string, error) {
	localPaths := make([]string, len(inputs))
	errs := make([]error, len(inputs))

	var wg sync.WaitGroup
	for i, inp := range inputs {
		wg.Add(1)
		go func(idx int, fileURL string) {
			defer wg.Done()
			path, err := uc.dl.Acquire(fileURL)
			if err != nil {
				errs[idx] = fmt.Errorf("input %d (%s): %w", idx, fileURL, err)
				return
			}
			localPaths[idx] = path
		}(i, inp.FileURL)
	}
	wg.Wait()

	for _, err := range errs {
		if err != nil {
			return nil, err
		}
	}
	return localPaths, nil
}


func (uc *PipelineMediaUseCase) sendWebhook(j *job.Job, webhookURL string, res runResult, jobErr error) {
	if webhookURL == "" {
		return
	}
	payload := JobResult{
		JobID:     j.ID,
		FileName:  res.fileName,
		Status:    string(j.Status),
		RunTime:   round3(j.RunTime),
		Encoder:   res.encoder,
		VideoInfo: res.videoInfo,
	}
	if res.publicURL != "" {
		payload.Output = []OutputLink{{Link: res.publicURL}}
	}
	if jobErr != nil {
		payload.Error = jobErr.Error()
	}
	if err := uc.webhook.Send(webhookURL, payload); err != nil {
		log.Printf("webhook failed for job %s: %v", j.ID, err)
	}
}

func validate(req PipelineRequest) error {
	if len(req.Inputs) == 0 {
		return fmt.Errorf("inputs cannot be empty")
	}
	if len(req.Outputs) == 0 {
		return fmt.Errorf("outputs cannot be empty")
	}
	return nil
}

func outputFileName(fileName, jobID string) string {
	if fileName == "" {
		return jobID + ".mp4"
	}
	name := sanitizeFileName(filepath.Base(fileName))
	
	ext := strings.ToLower(filepath.Ext(name))
	validExts := map[string]bool{
		".mp4": true, ".mp3": true, ".wav": true, ".m4a": true, ".aac": true, ".mkv": true,
	}
	
	if !validExts[ext] {
		name += ".mp4"
	}
	return name
}

// sanitizeFileName substitui caracteres inválidos em nomes de arquivo (Windows e Linux).
// Cobre o caso de file_name gerado por templates com datas no formato pt-BR (/, :, ,).
func sanitizeFileName(name string) string {
	r := strings.NewReplacer(
		"/", "-", "\\", "-", ":", "-",
		"*", "-", "?", "-", `"`, "-",
		"<", "-", ">", "-", "|", "-",
		",", "", " ", "_",
	)
	return r.Replace(name)
}

func buildFFmpegArgs(inputFiles []InputFile, localPaths []string, filters []FilterBlock, outputs []OutputSpec, outPath string) ([]string, error) {
	var args []string

	for i, p := range localPaths {
		if i < len(inputFiles) {
			for _, opt := range inputFiles[i].Options {
				argStr, err := rawToString(opt.Argument)
				if err != nil {
					return nil, fmt.Errorf("input option %q: %w", opt.Option, err)
				}
				args = append(args, opt.Option)
				if argStr != "" {
					args = append(args, argStr)
				}
			}
		}
		args = append(args, "-i", p)
	}

	if len(filters) > 0 {
		parts := make([]string, len(filters))
		for i, f := range filters {
			parts[i] = f.Filter
		}
		args = append(args, "-filter_complex", strings.Join(parts, ";"))
	}

	if len(outputs) > 0 {
		for _, opt := range outputs[0].Options {
			argStr, err := rawToString(opt.Argument)
			if err != nil {
				return nil, fmt.Errorf("option %q: %w", opt.Option, err)
			}
			args = append(args, opt.Option)
			if argStr != "" {
				args = append(args, argStr)
			}
		}
	}

	args = append(args, outPath)
	return args, nil
}

func rawToString(raw json.RawMessage) (string, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return "", nil
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return s, nil
	}
	var n json.Number
	if err := json.Unmarshal(raw, &n); err == nil {
		return n.String(), nil
	}
	var b bool
	if err := json.Unmarshal(raw, &b); err == nil {
		if b {
			return "", nil // Se for "true", a intenção é só passar a flag sem argumento
		}
		return "", nil
	}
	return "", fmt.Errorf("argument must be string or number, got: %s", raw)
}

func cacheKey(url string) string {
	h := sha256.Sum256([]byte(url))
	return fmt.Sprintf("%x", h)
}

func round3(v float64) float64 {
	return math.Round(v*1000) / 1000
}

func formatDuration(secs float64) string {
	if secs < 60 {
		return fmt.Sprintf("%.1fs", secs)
	}
	m := int(secs) / 60
	s := secs - float64(m*60)
	return fmt.Sprintf("%dm%.1fs", m, s)
}
