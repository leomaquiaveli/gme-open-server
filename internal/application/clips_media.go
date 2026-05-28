package application

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/leomaquiaveli/gme-open-server/internal/domain/job"
	"github.com/leomaquiaveli/gme-open-server/internal/domain/ports"
)

// TimeValue aceita segundos (número) ou "HH:MM:SS" / "HH:MM:SS.mmm" no JSON.
type TimeValue float64

func (t *TimeValue) UnmarshalJSON(data []byte) error {
	var f float64
	if err := json.Unmarshal(data, &f); err == nil {
		*t = TimeValue(f)
		return nil
	}
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return fmt.Errorf("time must be a number (seconds) or string HH:MM:SS[.mmm]")
	}
	secs, err := parseTimeString(s)
	if err != nil {
		return err
	}
	*t = TimeValue(secs)
	return nil
}

func parseTimeString(s string) (float64, error) {
	parts := strings.SplitN(s, ":", 3)
	if len(parts) != 3 {
		return 0, fmt.Errorf("invalid time %q: expected HH:MM:SS or HH:MM:SS.mmm", s)
	}
	h, err := strconv.ParseFloat(strings.TrimSpace(parts[0]), 64)
	if err != nil {
		return 0, fmt.Errorf("invalid hours in %q", s)
	}
	m, err := strconv.ParseFloat(strings.TrimSpace(parts[1]), 64)
	if err != nil {
		return 0, fmt.Errorf("invalid minutes in %q", s)
	}
	sec, err := strconv.ParseFloat(strings.TrimSpace(parts[2]), 64)
	if err != nil {
		return 0, fmt.Errorf("invalid seconds in %q", s)
	}
	return h*3600 + m*60 + sec, nil
}

// ClipMode define o tipo de saída do clip.
// "vertical"   → crop 9:16 + scale 1080x1920 (com scroll_x/keyframes)
// "horizontal" → corte simples, mantém dimensão da fonte
// ""           → herda do request; se o request também for "", é corte simples
type ClipMode = string

const (
	ClipModeVertical   ClipMode = "vertical"
	ClipModeHorizontal ClipMode = "horizontal"
)

type ClipSpec struct {
	ID        string           `json:"id"`
	FileName  string           `json:"file_name"`
	Start     TimeValue        `json:"start"`
	End       TimeValue        `json:"end"`
	Duration  TimeValue        `json:"duration"`
	Mode      ClipMode         `json:"mode"`      // override do mode do request
	ScrollX   float64     `json:"scroll_x"`  // vertical apenas: valor estático
	Zoom      float64     `json:"zoom"`      // vertical apenas: 0-100, percentual de zoom adicional
	Keyframes []Keyframe  `json:"keyframes"` // vertical apenas: scroll_x e/ou zoom ao longo do tempo
	CRF       int              `json:"crf"`
	Preset    string           `json:"preset"`
}

type ClipsRequest struct {
	ID         string     `json:"id"`
	InputURL   string     `json:"input_url"`
	Clips      []ClipSpec `json:"clips"`
	Mode       ClipMode   `json:"mode"`   // default para todos os clips
	CRF        int        `json:"crf"`    // default para todos os clips
	Preset     string     `json:"preset"` // default para todos os clips
	WebhookURL string     `json:"webhook_url"`
}

type ClipOutput struct {
	ID         string  `json:"id"`
	InternalID string  `json:"internal_id"`
	FileName   string  `json:"file_name,omitempty"`
	Link       string  `json:"link,omitempty"`
	RunTime    float64 `json:"run_time,omitempty"`
	Error      string  `json:"error,omitempty"`
}

type ClipsResult struct {
	JobID   string       `json:"job_id"`
	Status  string       `json:"status"`
	Outputs []ClipOutput `json:"outputs"`
	RunTime float64      `json:"run_time"`
	Error   string       `json:"error,omitempty"`
}

type pendingClipsJob struct {
	j   *job.Job
	req ClipsRequest
}

type ClipsMediaUseCase struct {
	dl       *Downloader
	storage  ports.IStorage
	runner   ports.IMediaProcessor
	webhook  ports.IWebhookSender
	sem      chan struct{}
	jobQueue chan pendingClipsJob
	workDir  string
	threads  int
}

func NewClipsMediaUseCase(
	cache ports.IFileCache,
	storage ports.IStorage,
	runner ports.IMediaProcessor,
	webhook ports.IWebhookSender,
	maxWorkers int,
	workDir string,
	threads int,
) *ClipsMediaUseCase {
	queueSize := maxWorkers * 10
	if queueSize < 500 {
		queueSize = 500
	}
	uc := &ClipsMediaUseCase{
		dl:       NewDownloader(cache, storage, workDir),
		storage:  storage,
		runner:   runner,
		webhook:  webhook,
		sem:      make(chan struct{}, maxWorkers),
		jobQueue: make(chan pendingClipsJob, queueSize),
		workDir:  workDir,
		threads:  threads,
	}
	go uc.dispatch()
	return uc
}

// dispatch não pré-ocupa slot — cada clip goroutine gerencia o próprio slot de CPU.
func (uc *ClipsMediaUseCase) dispatch() {
	for p := range uc.jobQueue {
		go func(pending pendingClipsJob) {
			result, err := uc.run(pending.j, pending.req)
			uc.sendWebhook(pending.j, pending.req.WebhookURL, result, err)
		}(p)
	}
}

func (uc *ClipsMediaUseCase) Execute(req ClipsRequest) (string, error) {
	if err := validateClips(req); err != nil {
		return "", err
	}
	j := job.New(req.WebhookURL)
	select {
	case uc.jobQueue <- pendingClipsJob{j, req}:
		return j.ID, nil
	default:
		return "", ErrAtCapacity
	}
}

func (uc *ClipsMediaUseCase) ExecuteSync(req ClipsRequest) (*ClipsResult, error) {
	if err := validateClips(req); err != nil {
		return nil, err
	}
	j := job.New("")
	result, err := uc.run(j, req)
	return &result, err
}

func (uc *ClipsMediaUseCase) run(j *job.Job, req ClipsRequest) (ClipsResult, error) {
	label := req.ID
	if label == "" {
		label = j.ID[:8]
	}

	j.Start()
	t0 := time.Now()
	log.Printf("[%s] clips iniciado | %d clips | mode: %q | job_id: %s", label, len(req.Clips), req.Mode, j.ID)

	// Fase 1: download único
	localPath, err := uc.dl.Acquire(req.InputURL)
	if err != nil {
		j.Fail()
		return ClipsResult{}, fmt.Errorf("download: %w", err)
	}
	defer uc.dl.Release(req.InputURL)
	log.Printf("[%s] download concluído | %.1fs", label, time.Since(t0).Seconds())

	// Fase 2+3: render e upload em pipeline — cada clip sobe assim que o FFmpeg termina.
	// Não acumula todos os arquivos no disco antes de começar os uploads.
	type renderOut struct {
		idx        int
		outPath    string
		internalID string
		runTime    float64
		err        error
	}

	rendered := make(chan renderOut, len(req.Clips))
	var renderWg sync.WaitGroup

	for i, clip := range req.Clips {
		internalID := fmt.Sprintf("%s-%02d", j.ID[:8], i)
		renderWg.Add(1)
		go func(i int, clip ClipSpec, internalID string) {
			defer renderWg.Done()
			uc.sem <- struct{}{}
			defer func() { <-uc.sem }() // slot liberado com segurança após FFmpeg (previne vazamento em panic)
			tRender := time.Now()
			outPath, err := uc.renderClip(localPath, clip, req, j.ID, i)
			rt := time.Since(tRender).Seconds()
			if err != nil {
				log.Printf("[%s] clip[%s] falhou: %v", label, internalID, err)
			} else {
				log.Printf("[%s] clip[%s] renderizado | %.1fs", label, internalID, rt)
			}
			rendered <- renderOut{idx: i, outPath: outPath, internalID: internalID, runTime: rt, err: err}
		}(i, clip, internalID)
	}

	// Fecha o canal quando todos os renders terminarem
	go func() {
		renderWg.Wait()
		close(rendered)
	}()

	// Lê do canal e sobe cada clip imediatamente — sem esperar os demais renderizarem
	outputs := make([]ClipOutput, len(req.Clips))
	var uploadWg sync.WaitGroup

	for r := range rendered {
		clip := req.Clips[r.idx]
		if r.err != nil {
			outputs[r.idx] = ClipOutput{ID: clip.ID, InternalID: r.internalID, FileName: clip.FileName, Error: r.err.Error()}
			continue
		}
		uploadWg.Add(1)
		go func(r renderOut, clip ClipSpec) {
			defer uploadWg.Done()
			url, err := uc.storage.Upload(r.outPath, "")
			if err != nil {
				outputs[r.idx] = ClipOutput{ID: clip.ID, InternalID: r.internalID, FileName: clip.FileName, Error: fmt.Sprintf("upload: %v", err)}
				return
			}
			outputs[r.idx] = ClipOutput{
				ID:         clip.ID,
				InternalID: r.internalID,
				FileName:   clip.FileName,
				Link:       url,
				RunTime:    round3(r.runTime),
			}
			os.Remove(r.outPath)
		}(r, clip)
	}
	uploadWg.Wait()
	log.Printf("[%s] pipeline concluído | total: %.3fs", label, time.Since(t0).Seconds())

	j.Complete()
	return ClipsResult{
		JobID:   j.ID,
		Status:  string(j.Status),
		Outputs: outputs,
		RunTime: round3(j.RunTime),
	}, nil
}

func (uc *ClipsMediaUseCase) renderClip(inputPath string, clip ClipSpec, req ClipsRequest, jobID string, idx int) (string, error) {
	var outName string
	if clip.FileName != "" {
		base := filepath.Base(clip.FileName)
		ext := filepath.Ext(base)
		stem := base[:len(base)-len(ext)]
		outName = fmt.Sprintf("%s-%s-%02d.mp4", stem, jobID[:8], idx)
	} else {
		outName = fmt.Sprintf("%s-%02d.mp4", jobID[:8], idx)
	}
	outPath := filepath.Join(uc.workDir, "outputs", outName)

	if err := os.MkdirAll(filepath.Dir(outPath), 0o755); err != nil {
		return "", fmt.Errorf("create output dir: %w", err)
	}

	// Hierarquia: clip.Mode > req.Mode > corte simples
	effectiveMode := clip.Mode
	if effectiveMode == "" {
		effectiveMode = req.Mode
	}

	crf := clip.CRF
	if crf == 0 {
		crf = req.CRF
	}
	preset := clip.Preset
	if preset == "" {
		preset = req.Preset
	}

	var args []string
	switch effectiveMode {
	case ClipModeVertical:
		vReq := VerticalRequest{
			Start:     float64(clip.Start),
			End:       float64(clip.End),
			Duration:  float64(clip.Duration),
			ScrollX:   clip.ScrollX,
			Keyframes: clip.Keyframes,
			Zoom:      clip.Zoom,
			CRF:       crf,
			Preset:    preset,
		}
		// Source fps só importa quando há keyframes de zoom (zoompan exige fps explícito).
		// Demais casos passam 0 e buildVerticalArgs ignora.
		var sourceFPS float64
		if hasZoomKeyframes(clip.Keyframes) {
			if info, probeErr := uc.runner.ProbeMedia(inputPath); probeErr == nil {
				sourceFPS = info.Framerate
			}
		}
		args = buildVerticalArgs(inputPath, vReq, uc.threads, sourceFPS, outPath)
	default:
		// ClipModeHorizontal ou "" → corte simples, mantém dimensão da fonte
		args = buildSourceArgs(inputPath, float64(clip.Start), float64(clip.End), float64(clip.Duration), crf, preset, uc.threads, outPath)
	}

	if err := uc.runner.RunFFmpeg(args); err != nil {
		return "", err
	}
	return outPath, nil
}

// buildSourceArgs corta o segmento sem filtro de crop — mantém dimensão original da fonte.
func buildSourceArgs(inputPath string, start, end, duration float64, crf int, preset string, threads int, outPath string) []string {
	var args []string
	if start > 0 {
		args = append(args, "-ss", fmt.Sprintf("%.3f", start))
	}
	args = append(args, "-i", inputPath)

	dur := duration
	if dur == 0 && end > start {
		dur = end - start
	}
	if dur > 0 {
		args = append(args, "-t", fmt.Sprintf("%.3f", dur))
	}

	args = append(args, "-map", "0:v:0", "-map", "0:a:0?")

	if crf == 0 {
		crf = 23
	}
	if preset == "" {
		preset = "fast"
	}

	x264params := "ref=1"
	if threads > 0 {
		x264params += fmt.Sprintf(":threads=%d", threads)
	}
	args = append(args, "-x264-params", x264params)
	args = append(args, "-c:v", "libx264", "-crf", fmt.Sprintf("%d", crf), "-preset", preset)
	args = append(args, "-c:a", "aac")
	args = append(args, outPath)
	return args
}

func (uc *ClipsMediaUseCase) sendWebhook(j *job.Job, webhookURL string, result ClipsResult, jobErr error) {
	if webhookURL == "" {
		return
	}
	result.JobID = j.ID
	result.Status = string(j.Status)
	if jobErr != nil {
		result.Error = jobErr.Error()
	}
	if err := uc.webhook.Send(webhookURL, result); err != nil {
		log.Printf("webhook failed for job %s: %v", j.ID, err)
	}
}

func validateClips(req ClipsRequest) error {
	if req.InputURL == "" {
		return fmt.Errorf("input_url cannot be empty")
	}
	if len(req.Clips) == 0 {
		return fmt.Errorf("clips cannot be empty")
	}
	if req.Mode != "" && req.Mode != ClipModeVertical && req.Mode != ClipModeHorizontal {
		return fmt.Errorf("mode must be %q or %q", ClipModeVertical, ClipModeHorizontal)
	}
	if req.CRF != 0 && (req.CRF < 1 || req.CRF > 51) {
		return fmt.Errorf("crf must be between 1 and 51")
	}
	if req.Preset != "" && !validPresets[req.Preset] {
		return fmt.Errorf("invalid preset %q", req.Preset)
	}
	for i, clip := range req.Clips {
		if clip.Mode != "" && clip.Mode != ClipModeVertical && clip.Mode != ClipModeHorizontal {
			return fmt.Errorf("clip[%d]: mode must be %q or %q", i, ClipModeVertical, ClipModeHorizontal)
		}
		if float64(clip.End) > 0 && float64(clip.End) <= float64(clip.Start) {
			return fmt.Errorf("clip[%d]: end must be greater than start", i)
		}
		if float64(clip.Duration) > 0 && float64(clip.End) > 0 {
			return fmt.Errorf("clip[%d]: use either duration or end, not both", i)
		}
		if len(clip.Keyframes) == 0 && (clip.ScrollX < -100 || clip.ScrollX > 100) {
			return fmt.Errorf("clip[%d]: scroll_x must be between -100 and 100", i)
		}
		if clip.CRF != 0 && (clip.CRF < 1 || clip.CRF > 51) {
			return fmt.Errorf("clip[%d]: crf must be between 1 and 51", i)
		}
		if clip.Preset != "" && !validPresets[clip.Preset] {
			return fmt.Errorf("clip[%d]: invalid preset %q", i, clip.Preset)
		}
	}
	return nil
}
