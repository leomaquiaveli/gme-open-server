package application

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/leomaquiaveli/gme-open-server/internal/domain/job"
	"github.com/leomaquiaveli/gme-open-server/internal/domain/ports"
)

type CutSegment struct {
	Start    TimeValue `json:"start"`
	End      TimeValue `json:"end"`
	Duration TimeValue `json:"duration,omitempty"`
}

type CutsRequest struct {
	ID           string       `json:"id"`
	FileName     string       `json:"file_name,omitempty"`
	VideoURL     string       `json:"video_url"`
	Cuts         []CutSegment `json:"cuts"`
	VideoCodec   string       `json:"video_codec"`
	VideoPreset  string       `json:"video_preset,omitempty"`
	VideoCRF     int          `json:"video_crf,omitempty"`
	AudioCodec   string       `json:"audio_codec"`
	AudioBitrate string       `json:"audio_bitrate"`
	WebhookURL   string       `json:"webhook_url,omitempty"`
}

type CutsResult struct {
	JobID     string       `json:"job_id"`
	FileName  string       `json:"file_name,omitempty"`
	Status    string       `json:"status"`
	Output    []OutputLink `json:"output,omitempty"`
	RunTime   float64      `json:"run_time"`
	Encoder   string       `json:"encoder,omitempty"`
	VideoInfo *VideoInfo   `json:"video_info,omitempty"`
	Error     string       `json:"error,omitempty"`
}

type pendingCutsJob struct {
	j   *job.Job
	req CutsRequest
}

type CutsMediaUseCase struct {
	dl       *Downloader
	storage  ports.IStorage
	runner   ports.IMediaProcessor
	webhook  ports.IWebhookSender
	sem      chan struct{}
	jobQueue chan pendingCutsJob
	workDir  string
}

func NewCutsMediaUseCase(
	cache ports.IFileCache,
	storage ports.IStorage,
	runner ports.IMediaProcessor,
	webhook ports.IWebhookSender,
	maxWorkers int,
	workDir string,
) *CutsMediaUseCase {
	queueSize := maxWorkers * 10
	if queueSize < 500 {
		queueSize = 500
	}
	uc := &CutsMediaUseCase{
		dl:       NewDownloader(cache, storage, workDir),
		storage:  storage,
		runner:   runner,
		webhook:  webhook,
		sem:      make(chan struct{}, maxWorkers),
		jobQueue: make(chan pendingCutsJob, queueSize),
		workDir:  workDir,
	}
	go uc.dispatch()
	return uc
}

func (uc *CutsMediaUseCase) dispatch() {
	for p := range uc.jobQueue {
		uc.sem <- struct{}{}
		go func(pending pendingCutsJob) {
			var once sync.Once
			release := func() { once.Do(func() { <-uc.sem }) }
			defer release()
			res, err := uc.run(pending.j, pending.req, release)
			uc.sendWebhook(pending.j, pending.req.WebhookURL, res, err)
		}(p)
	}
}

func (uc *CutsMediaUseCase) Execute(req CutsRequest) (string, error) {
	if err := validateCuts(req); err != nil {
		return "", err
	}
	j := job.New(req.WebhookURL)
	select {
	case uc.jobQueue <- pendingCutsJob{j, req}:
		return j.ID, nil
	default:
		return "", ErrAtCapacity
	}
}

func (uc *CutsMediaUseCase) ExecuteSync(req CutsRequest) (*CutsResult, error) {
	if err := validateCuts(req); err != nil {
		return nil, err
	}
	uc.sem <- struct{}{}
	var once sync.Once
	release := func() { once.Do(func() { <-uc.sem }) }
	defer release()

	j := job.New("")
	res, err := uc.run(j, req, release)
	result := &CutsResult{
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
	if err != nil {
		result.Error = err.Error()
	}
	return result, err
}

func (uc *CutsMediaUseCase) run(j *job.Job, req CutsRequest, releaseSlot func()) (runResult, error) {
	label := req.ID
	if label == "" {
		label = j.ID[:8]
	}
	encoder := uc.runner.GetEncoder()

	j.Start()
	log.Printf("[%s] cuts iniciado | %d corte(s) | job_id: %s | encoder: %s", label, len(req.Cuts), j.ID, encoder)

	t0 := time.Now()
	localPath, err := uc.dl.Acquire(req.VideoURL)
	if err != nil {
		j.Fail()
		return runResult{}, fmt.Errorf("download: %w", err)
	}
	defer uc.dl.Release(req.VideoURL)
	log.Printf("[%s] download concluído | %.1fs", label, time.Since(t0).Seconds())

	outName := outputFileName(req.FileName, j.ID)
	outPath := filepath.Join(uc.workDir, "outputs", outName)
	if err := os.MkdirAll(filepath.Dir(outPath), 0o755); err != nil {
		j.Fail()
		return runResult{}, fmt.Errorf("create output dir: %w", err)
	}

	args := buildCutsArgs(localPath, req, outPath)

	tFFmpeg := time.Now()
	ffmpegErr := uc.runner.RunFFmpeg(args)
	releaseSlot()
	if ffmpegErr != nil {
		j.Fail()
		log.Printf("[%s] ffmpeg falhou (%.1fs): %v", label, time.Since(tFFmpeg).Seconds(), ffmpegErr)
		return runResult{}, ffmpegErr
	}
	if info, statErr := os.Stat(outPath); statErr != nil || info.Size() < 1024 {
		j.Fail()
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
		return runResult{}, fmt.Errorf("upload: %w", err)
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

// buildCutsArgs monta o FFmpeg para REMOVER os trechos especificados e concatenar o restante.
// Lógica: calcula os segmentos a MANTER (inverso dos cuts) e usa trim+atrim+concat.
// Um único input — mais eficiente que múltiplos -i do mesmo arquivo.
func buildCutsArgs(localPath string, req CutsRequest, outPath string) []string {
	codec := req.VideoCodec
	if codec == "" {
		codec = "libx264"
	}
	crf := req.VideoCRF
	if crf == 0 {
		crf = 23
	}
	preset := req.VideoPreset
	if preset == "" {
		preset = "fast"
	}
	audioCodec := req.AudioCodec
	if audioCodec == "" {
		audioCodec = "aac"
	}
	audioBitrate := req.AudioBitrate
	if audioBitrate == "" {
		audioBitrate = "96k"
	}

	// Ordena cortes por start para calcular os segmentos mantidos corretamente.
	sorted := make([]CutSegment, len(req.Cuts))
	copy(sorted, req.Cuts)
	sort.Slice(sorted, func(i, j int) bool {
		return float64(sorted[i].Start) < float64(sorted[j].Start)
	})

	// Calcula segmentos a MANTER: tudo que não está dentro de nenhum corte.
	type kept struct {
		start float64
		end   float64 // -1 = até o fim do vídeo
	}
	var segments []kept
	prev := 0.0
	for _, cut := range sorted {
		start := float64(cut.Start)
		end := float64(cut.End)
		if end == 0 {
			end = start + float64(cut.Duration)
		}
		if start > prev {
			segments = append(segments, kept{prev, start})
		}
		prev = end
	}
	segments = append(segments, kept{prev, -1}) // do último corte até o fim

	// Monta filter_complex com trim/atrim por segmento mantido + concat.
	var sb strings.Builder
	n := len(segments)
	for i, seg := range segments {
		if seg.end < 0 {
			fmt.Fprintf(&sb, "[0:v]trim=start=%.3f,setpts=PTS-STARTPTS[v%d];", seg.start, i)
			fmt.Fprintf(&sb, "[0:a]atrim=start=%.3f,asetpts=PTS-STARTPTS[a%d];", seg.start, i)
		} else {
			fmt.Fprintf(&sb, "[0:v]trim=start=%.3f:end=%.3f,setpts=PTS-STARTPTS[v%d];", seg.start, seg.end, i)
			fmt.Fprintf(&sb, "[0:a]atrim=start=%.3f:end=%.3f,asetpts=PTS-STARTPTS[a%d];", seg.start, seg.end, i)
		}
	}
	for i := 0; i < n; i++ {
		fmt.Fprintf(&sb, "[v%d][a%d]", i, i)
	}
	fmt.Fprintf(&sb, "concat=n=%d:v=1:a=1[v][a]", n)

	args := []string{"-i", localPath}
	args = append(args, "-filter_complex", sb.String())
	args = append(args, "-map", "[v]", "-map", "[a]")
	args = append(args, "-c:v", codec, "-crf", fmt.Sprintf("%d", crf), "-preset", preset)
	args = append(args, "-c:a", audioCodec, "-b:a", audioBitrate)
	args = append(args, "-movflags", "+faststart")
	args = append(args, outPath)
	return args
}

func (uc *CutsMediaUseCase) sendWebhook(j *job.Job, webhookURL string, res runResult, jobErr error) {
	if webhookURL == "" {
		return
	}
	payload := CutsResult{
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

func validateCuts(req CutsRequest) error {
	if req.VideoURL == "" {
		return fmt.Errorf("video_url cannot be empty")
	}
	if len(req.Cuts) == 0 {
		return fmt.Errorf("cuts cannot be empty")
	}
	for i, cut := range req.Cuts {
		start := float64(cut.Start)
		end := float64(cut.End)
		dur := float64(cut.Duration)
		if start < 0 {
			return fmt.Errorf("cuts[%d]: start must be >= 0", i)
		}
		if end == 0 && dur == 0 {
			return fmt.Errorf("cuts[%d]: end or duration must be provided", i)
		}
		if end > 0 && end <= start {
			return fmt.Errorf("cuts[%d]: end must be greater than start", i)
		}
	}
	return nil
}
