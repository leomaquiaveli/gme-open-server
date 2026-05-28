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

type pendingVerticalJob struct {
	j   *job.Job
	req VerticalRequest
}

// Keyframe define o estado da câmera num momento específico do vídeo.
// Cada campo é opcional — só inclua o que quiser animar naquele instante.
// scroll_x e zoom podem aparecer juntos ou separados no mesmo keyframe.
type Keyframe struct {
	Time    float64  `json:"time"`               // segundos a partir do início do clipe
	ScrollX *float64 `json:"scroll_x,omitempty"` // -100 a +100
	Zoom    *float64 `json:"zoom,omitempty"`      // 0-500: percentual de zoom adicional
}

type VerticalRequest struct {
	ID         string      `json:"id"`
	InputURL   string      `json:"input_url"`
	FileName   string      `json:"file_name"`
	ScrollX    float64     `json:"scroll_x"`  // valor estático, ignorado se keyframes tiver scroll_x
	Zoom       float64     `json:"zoom"`      // 0-100: percentual de zoom adicional. 0=sem zoom, 100=2x zoom. Fator=1+zoom/100
	Keyframes  []Keyframe  `json:"keyframes"` // animação de scroll_x e/ou zoom ao longo do tempo
	Start      float64     `json:"start"`
	Duration   float64     `json:"duration"`
	End        float64     `json:"end"`
	CRF        int         `json:"crf"`
	Preset     string      `json:"preset"`
	WebhookURL string      `json:"webhook_url"`
}

type VerticalMediaUseCase struct {
	dl       *Downloader
	storage  ports.IStorage
	runner   ports.IMediaProcessor
	webhook  ports.IWebhookSender
	sem      chan struct{}
	jobQueue chan pendingVerticalJob
	workDir  string
	threads  int
}

func NewVerticalMediaUseCase(
	cache ports.IFileCache,
	storage ports.IStorage,
	runner ports.IMediaProcessor,
	webhook ports.IWebhookSender,
	maxWorkers int,
	workDir string,
	threads int,
) *VerticalMediaUseCase {
	queueSize := maxWorkers * 10
	if queueSize < 500 {
		queueSize = 500
	}
	uc := &VerticalMediaUseCase{
		dl:       NewDownloader(cache, storage, workDir),
		storage:  storage,
		runner:   runner,
		webhook:  webhook,
		sem:      make(chan struct{}, maxWorkers),
		jobQueue: make(chan pendingVerticalJob, queueSize),
		workDir:  workDir,
		threads:  threads,
	}
	go uc.dispatch()
	return uc
}

func (uc *VerticalMediaUseCase) dispatch() {
	for p := range uc.jobQueue {
		uc.sem <- struct{}{}
		go func(pending pendingVerticalJob) {
			var once sync.Once
			release := func() { once.Do(func() { <-uc.sem }) }
			defer release()
			res, err := uc.run(pending.j, pending.req, release)
			uc.sendWebhook(pending.j, pending.req.WebhookURL, res, err)
		}(p)
	}
}

func (uc *VerticalMediaUseCase) Execute(req VerticalRequest) (string, error) {
	if err := validateVertical(req); err != nil {
		return "", err
	}
	j := job.New(req.WebhookURL)

	select {
	case uc.jobQueue <- pendingVerticalJob{j, req}:
		return j.ID, nil
	default:
		return "", ErrAtCapacity
	}
}

func (uc *VerticalMediaUseCase) ExecuteSync(req VerticalRequest) (*JobResult, error) {
	if err := validateVertical(req); err != nil {
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

func (uc *VerticalMediaUseCase) run(j *job.Job, req VerticalRequest, releaseSlot func()) (runResult, error) {
	label := req.ID
	if label == "" {
		label = j.ID[:8]
	}
	encoder := uc.runner.GetEncoder()

	j.Start()
	log.Printf("[%s] vertical iniciado | job_id: %s | encoder: %s | scroll_x: %g",
		label, j.ID, encoder, req.ScrollX)

	t0 := time.Now()
	localPath, err := uc.dl.Acquire(req.InputURL)
	if err != nil {
		j.Fail()
		log.Printf("[%s] download falhou (%.1fs): %v", label, time.Since(t0).Seconds(), err)
		return runResult{}, err
	}
	log.Printf("[%s] download concluído | %.1fs", label, time.Since(t0).Seconds())

	defer uc.dl.Release(req.InputURL)

	outName := outputFileName(req.FileName, j.ID)
	outPath := filepath.Join(uc.workDir, "outputs", outName)
	if err := os.MkdirAll(filepath.Dir(outPath), 0o755); err != nil {
		j.Fail()
		return runResult{}, fmt.Errorf("create output dir: %w", err)
	}

	// Probar fps do source apenas quando há zoom dinâmico — paga o ffprobe extra
	// só nos jobs que realmente precisam dele (zoompan exige fps explícito para
	// não cair no default 25 e dessincronizar A/V).
	var sourceFPS float64
	if hasZoomKeyframes(req.Keyframes) {
		if info, probeErr := uc.runner.ProbeMedia(localPath); probeErr != nil {
			log.Printf("[%s] probe source falhou (%v) — usando fps default 30", label, probeErr)
		} else {
			sourceFPS = info.Framerate
		}
	}

	args := buildVerticalArgs(localPath, req, uc.threads, sourceFPS, outPath)

	tFFmpeg := time.Now()
	ffmpegErr := uc.runner.RunFFmpeg(args)
	releaseSlot() // libera slot de CPU antes do upload — upload é IO, não CPU
	if ffmpegErr != nil {
		j.Fail()
		log.Printf("[%s] ffmpeg falhou (%.1fs): %v", label, time.Since(tFFmpeg).Seconds(), ffmpegErr)
		return runResult{}, ffmpegErr
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

func (uc *VerticalMediaUseCase) sendWebhook(j *job.Job, webhookURL string, res runResult, jobErr error) {
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

func buildVerticalArgs(inputPath string, req VerticalRequest, threads int, sourceFPS float64, outPath string) []string {
	var args []string

	if req.Start > 0 {
		args = append(args, "-ss", fmt.Sprintf("%.3f", req.Start))
	}
	args = append(args, "-i", inputPath)

	dur := req.Duration
	if dur == 0 && req.End > req.Start {
		dur = req.End - req.Start
	}
	if dur > 0 {
		args = append(args, "-t", fmt.Sprintf("%.3f", dur))
	}

	var filter string
	if hasZoomKeyframes(req.Keyframes) {
		// zoom animado: crop full 9:16 com scroll_x (que crop suporta via t por frame),
		// scale para 1080×1920, zoompan aplica zoom por frame usando "it" (input time)
		// e a variável "zoom" (último z computado). fps explícito do source evita o
		// default 25 do zoompan que dessincronizaria A/V em vídeos com outro framerate.
		margin := "(iw-ih*9/16)/2"
		xExpr := buildScrollXExpr(req.ScrollX, req.Keyframes, margin)
		zExpr := buildZoomExpr(req.Zoom, req.Keyframes)
		fps := sourceFPS
		if fps <= 0 {
			fps = 30
		}
		filter = fmt.Sprintf(
			"crop=ih*9/16:ih:%s:0,scale=1080:1920,zoompan=z='%s':x='(iw-iw/zoom)/2':y='(ih-ih/zoom)/2':d=1:fps=%.3f:s=1080x1920",
			xExpr, zExpr, fps,
		)
	} else {
		// zoom estático: crop com fator fixo — mais rápido (sem eval=frame).
		f := fmt.Sprintf("%g", zoomFactor(req.Zoom))
		margin := fmt.Sprintf("(iw-ih*9/16/%s)/2", f)
		xExpr := buildScrollXExpr(req.ScrollX, req.Keyframes, margin)
		filter = fmt.Sprintf("crop=ih*9/16/%s:ih/%s:%s:(ih-ih/%s)/2,scale=1080:1920", f, f, xExpr, f)
	}

	args = append(args, "-vf", filter)
	args = append(args, "-map", "0:v:0", "-map", "0:a:0?")

	crf := req.CRF
	if crf == 0 {
		crf = 23
	}
	preset := req.Preset
	if preset == "" {
		preset = "fast"
	}

	// ref=1 obrigatório: 1080x1920 com ref=3 aloca ~11.7MB causando malloc no libx264.
	// Ignorado pelo runner quando encoder for NVENC/VAAPI.
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

// zoomFactor converte zoom do usuário (0-100) para fator FFmpeg (1.0-2.0).
// zoom=0 → 1.0 (sem zoom), zoom=100 → 2.0 (dobro do zoom). Fórmula: 1 + zoom/100.
func zoomFactor(z float64) float64 {
	if z <= 0 {
		return 1.0
	}
	return 1.0 + z/100.0
}

// buildZoomExpr constrói a expressão de fator de zoom em função do input time.
// Usa "it" (input time) — a variável "t" não existe dentro do zoompan e era a
// causa do erro "Undefined constant" antes do fix. Vírgulas vão literais porque
// a expressão é embebida em zoompan=z='...' (aspas simples protegem do parser).
func buildZoomExpr(staticZoom float64, keyframes []Keyframe) string {
	var kfs []Keyframe
	for _, kf := range keyframes {
		if kf.Zoom != nil {
			kfs = append(kfs, kf)
		}
	}
	if len(kfs) == 0 {
		return fmt.Sprintf("%g", zoomFactor(staticZoom))
	}
	sort.Slice(kfs, func(i, j int) bool { return kfs[i].Time < kfs[j].Time })
	expr := fmt.Sprintf("%g", zoomFactor(*kfs[len(kfs)-1].Zoom))
	for i := len(kfs) - 2; i >= 0; i-- {
		expr = fmt.Sprintf("if(lt(it,%g),%g,%s)", kfs[i+1].Time, zoomFactor(*kfs[i].Zoom), expr)
	}
	if kfs[0].Time > 0 {
		expr = fmt.Sprintf("if(lt(it,%g),%g,%s)", kfs[0].Time, zoomFactor(staticZoom), expr)
	}
	return expr
}

func hasZoomKeyframes(keyframes []Keyframe) bool {
	for _, kf := range keyframes {
		if kf.Zoom != nil {
			return true
		}
	}
	return false
}

// buildScrollXExpr extrai os keyframes com scroll_x definido e constrói a expressão FFmpeg.
// Vírgulas dentro das expressões if() são escapadas com \, (nível FFmpeg, não shell).
func buildScrollXExpr(staticScrollX float64, keyframes []Keyframe, margin string) string {
	xOf := func(sx float64) string {
		return fmt.Sprintf("%s*(1+(%g/100))", margin, sx)
	}
	var kfs []Keyframe
	for _, kf := range keyframes {
		if kf.ScrollX != nil {
			kfs = append(kfs, kf)
		}
	}
	if len(kfs) == 0 {
		return xOf(staticScrollX)
	}
	sort.Slice(kfs, func(i, j int) bool { return kfs[i].Time < kfs[j].Time })
	expr := xOf(*kfs[len(kfs)-1].ScrollX)
	for i := len(kfs) - 2; i >= 0; i-- {
		expr = fmt.Sprintf("if(lt(t\\,%g)\\,%s\\,%s)", kfs[i+1].Time, xOf(*kfs[i].ScrollX), expr)
	}
	// estado antes do primeiro keyframe: usa scroll_x estático
	if kfs[0].Time > 0 {
		expr = fmt.Sprintf("if(lt(t\\,%g)\\,%s\\,%s)", kfs[0].Time, xOf(staticScrollX), expr)
	}
	return expr
}

var validPresets = map[string]bool{
	"ultrafast": true, "superfast": true, "veryfast": true, "faster": true,
	"fast": true, "medium": true, "slow": true, "slower": true, "veryslow": true,
}

func validateVertical(req VerticalRequest) error {
	if req.InputURL == "" {
		return fmt.Errorf("input_url cannot be empty")
	}
	if req.ScrollX < -100 || req.ScrollX > 100 {
		return fmt.Errorf("scroll_x must be between -100 and 100")
	}
	if req.Zoom < 0 || req.Zoom > 500 {
		return fmt.Errorf("zoom must be between 0 and 500")
	}
	for i, kf := range req.Keyframes {
		if kf.Time < 0 {
			return fmt.Errorf("keyframes[%d]: time must be non-negative", i)
		}
		if kf.ScrollX != nil && (*kf.ScrollX < -100 || *kf.ScrollX > 100) {
			return fmt.Errorf("keyframes[%d]: scroll_x must be between -100 and 100", i)
		}
		if kf.Zoom != nil && (*kf.Zoom < 0 || *kf.Zoom > 500) {
			return fmt.Errorf("keyframes[%d]: zoom must be between 0 and 500", i)
		}
	}
	if req.Duration > 0 && req.End > 0 {
		return fmt.Errorf("use either duration or end, not both")
	}
	if req.End > 0 && req.End <= req.Start {
		return fmt.Errorf("end must be greater than start")
	}
	if req.CRF != 0 && (req.CRF < 1 || req.CRF > 51) {
		return fmt.Errorf("crf must be between 1 and 51")
	}
	if req.Preset != "" && !validPresets[req.Preset] {
		return fmt.Errorf("preset must be one of: ultrafast, superfast, veryfast, faster, fast, medium, slow, slower, veryslow")
	}
	return nil
}
