package application

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/leomaquiaveli/gme-open-server/internal/domain/job"
	"github.com/leomaquiaveli/gme-open-server/internal/domain/ports"
)

type pendingCaptionJob struct {
	j   *job.Job
	req CaptionRequest
}

type CaptionRequest struct {
	ID         string  `json:"id"`
	VideoURL   string  `json:"video_url"`
	CaptionURL string  `json:"caption_url"`
	FileName   string  `json:"file_name"`
	Encoder    string  `json:"encoder,omitempty"` // Força codec manual (ex: libx264, h264_nvenc) ou vazio pra Auto-GPU
	CRF        int     `json:"crf"`
	Preset     string  `json:"preset"`
	WebhookURL string  `json:"webhook_url"`
}

type CaptionMediaUseCase struct {
	dl       *Downloader
	storage  ports.IStorage
	runner   ports.IMediaProcessor
	webhook  ports.IWebhookSender
	sem      chan struct{}
	jobQueue chan pendingCaptionJob
	workDir  string
	threads  int
}

func NewCaptionMediaUseCase(
	cache ports.IFileCache,
	storage ports.IStorage,
	runner ports.IMediaProcessor,
	webhook ports.IWebhookSender,
	maxWorkers int,
	workDir string,
	threads int,
) *CaptionMediaUseCase {
	queueSize := maxWorkers * 10
	if queueSize < 500 {
		queueSize = 500
	}
	uc := &CaptionMediaUseCase{
		dl:       NewDownloader(cache, storage, workDir),
		storage:  storage,
		runner:   runner,
		webhook:  webhook,
		sem:      make(chan struct{}, maxWorkers),
		jobQueue: make(chan pendingCaptionJob, queueSize),
		workDir:  workDir,
		threads:  threads,
	}
	go uc.dispatch()
	return uc
}

func (uc *CaptionMediaUseCase) dispatch() {
	for p := range uc.jobQueue {
		uc.sem <- struct{}{}
		go func(pending pendingCaptionJob) {
			var once sync.Once
			release := func() { once.Do(func() { <-uc.sem }) }
			defer release()
			res, err := uc.run(pending.j, pending.req, release)
			uc.sendWebhook(pending.j, pending.req.WebhookURL, res, err)
		}(p)
	}
}

func (uc *CaptionMediaUseCase) Execute(req CaptionRequest) (string, error) {
	if err := validateCaption(req); err != nil {
		return "", err
	}
	j := job.New(req.WebhookURL)

	select {
	case uc.jobQueue <- pendingCaptionJob{j, req}:
		return j.ID, nil
	default:
		return "", ErrAtCapacity
	}
}

func (uc *CaptionMediaUseCase) ExecuteSync(req CaptionRequest) (*JobResult, error) {
	if err := validateCaption(req); err != nil {
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

func escapeSubtitlePath(path string) string {
	// FFmpeg's subtitle filter is notoriously strict about Windows file paths.
	// We need to escape backslashes and colons (e.g. C:\path -> C\:\\path).
	// However, forward slashes (/) are safe in FFmpeg on Windows!
	// So we convert all \ to / first, and then escape the colon.
	unixPath := filepath.ToSlash(path)
	escapedPath := strings.ReplaceAll(unixPath, ":", "\\\\:")
	return escapedPath
}

func (uc *CaptionMediaUseCase) run(j *job.Job, req CaptionRequest, releaseSlot func()) (runResult, error) {
	label := req.ID
	if label == "" {
		label = j.ID[:8]
	}

	j.Start()
	log.Printf("[%s] caption iniciado | job_id: %s", label, j.ID)

	t0 := time.Now()
	
	// Phase 1: Download Media and Subtitles
	videoPath, err := uc.dl.Acquire(req.VideoURL)
	if err != nil {
		j.Fail()
		log.Printf("[%s] download do video falhou (%.1fs): %v", label, time.Since(t0).Seconds(), err)
		return runResult{}, err
	}
	defer uc.dl.Release(req.VideoURL)

	captionPath, err := uc.dl.Acquire(req.CaptionURL)
	if err != nil {
		j.Fail()
		log.Printf("[%s] download da legenda falhou (%.1fs): %v", label, time.Since(t0).Seconds(), err)
		return runResult{}, err
	}
	defer uc.dl.Release(req.CaptionURL)
	
	log.Printf("[%s] downloads concluídos | %.1fs", label, time.Since(t0).Seconds())

	outName := outputFileName(req.FileName, j.ID)
	outPath := filepath.Join(uc.workDir, "outputs", outName)
	if err := os.MkdirAll(filepath.Dir(outPath), 0o755); err != nil {
		j.Fail()
		return runResult{}, fmt.Errorf("create output dir: %w", err)
	}

	// Phase 2: FFmpeg Arguments Building
	// O Segredo: Escapar as barras invertidas e o ":" da letra do drive do Windows para o filtro não quebrar
	safeCaptionPath := escapeSubtitlePath(captionPath)
	filter := fmt.Sprintf("subtitles=f='%s'", safeCaptionPath)
	
	args := []string{
		"-i", videoPath,
		"-vf", filter,
		"-c:a", "copy", // Mantém o áudio do corte intocado
	}
	
	crf := req.CRF
	if crf == 0 {
		crf = 23
	}
	preset := req.Preset
	if preset == "" {
		preset = "fast"
	}

	// Encoder Management: GPU Override or Fallback
	encoder := req.Encoder
	if encoder == "" {
		encoder = "libx264" // O Runner vai interceptar o "libx264" e trocar pela GPU ativa automaticamente!
	}
	
	x264params := "ref=1"
	if uc.threads > 0 {
		x264params += fmt.Sprintf(":threads=%d", uc.threads)
	}
	
	args = append(args, "-c:v", encoder)
	
	if encoder == "libx264" || req.Encoder == "" {
		// Só envia CRF e PRESET para libx264. Se o runner interceptar e trocar pra NVENC,
		// o próprio runner vai injetar o cq e remover isso depois, mantendo a arquitetura original saudável.
		args = append(args, "-x264-params", x264params, "-crf", fmt.Sprintf("%d", crf), "-preset", preset)
	}

	args = append(args, outPath)

	tFFmpeg := time.Now()
	ffmpegErr := uc.runner.RunFFmpeg(args)
	
	// Registramos o encoder real que foi ativado (CPU ou NVENC)
	realEncoder := uc.runner.GetEncoder()
	if req.Encoder != "" {
		realEncoder = req.Encoder // Se o user forçou "h264_nvenc", mantemos a resposta exata.
	}

	releaseSlot() 

	if ffmpegErr != nil {
		j.Fail()
		log.Printf("[%s] ffmpeg falhou (%.1fs): %v", label, time.Since(tFFmpeg).Seconds(), ffmpegErr)
		return runResult{}, ffmpegErr
	}
	
	log.Printf("[%s] ffmpeg concluído | %.1fs | encoder: %s", label, time.Since(tFFmpeg).Seconds(), realEncoder)

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
	return runResult{publicURL: publicURL, fileName: req.FileName, encoder: realEncoder, videoInfo: vi}, nil
}

func (uc *CaptionMediaUseCase) sendWebhook(j *job.Job, webhookURL string, res runResult, jobErr error) {
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

func validateCaption(req CaptionRequest) error {
	if req.VideoURL == "" {
		return fmt.Errorf("video_url cannot be empty")
	}
	if req.CaptionURL == "" {
		return fmt.Errorf("caption_url cannot be empty")
	}
	if req.CRF != 0 && (req.CRF < 1 || req.CRF > 51) {
		return fmt.Errorf("crf must be between 1 and 51")
	}
	validPresets := map[string]bool{
		"ultrafast": true, "superfast": true, "veryfast": true, "faster": true,
		"fast": true, "medium": true, "slow": true, "slower": true, "veryslow": true,
	}
	if req.Preset != "" && !validPresets[req.Preset] {
		return fmt.Errorf("preset must be one of the valid fast/slow keywords")
	}
	return nil
}
