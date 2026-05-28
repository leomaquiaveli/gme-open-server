package ffmpeg

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os/exec"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/leomaquiaveli/gme-open-server/internal/domain/ports"
)


// limitedWriter descarta bytes além de limit — evita OOM com muitos jobs simultâneos.
type limitedWriter struct {
	w       io.Writer
	written int64
	limit   int64
}

func (lw *limitedWriter) Write(p []byte) (int, error) {
	original := len(p)
	if lw.written >= lw.limit {
		return original, nil
	}
	remaining := lw.limit - lw.written
	if int64(original) > remaining {
		p = p[:remaining]
	}
	n, err := lw.w.Write(p)
	lw.written += int64(n)
	if err != nil {
		return n, err
	}
	// Retorna o tamanho original mesmo tendo truncado — evita io.ErrShortWrite
	// que Go lança quando n < len(p), derrubando o job com erro espúrio.
	return original, nil
}

// Runner implementa ports.IMediaProcessor.
// É o equivalente Go do subprocess.run() do Python — chama o FFmpeg como processo externo.
type Runner struct {
	encoder        atomic.Value // GPUMode — atualizável após detecção assíncrona
	timeout        time.Duration
	hwBitrateMbps  int // bitrate alvo para h264_mf e h264_amf
}

func NewRunner(gpu GPUMode, timeout time.Duration, hwBitrateMbps int) *Runner {
	if hwBitrateMbps <= 0 {
		hwBitrateMbps = 6
	}
	r := &Runner{timeout: timeout, hwBitrateMbps: hwBitrateMbps}
	r.encoder.Store(gpu)
	return r
}

func (r *Runner) GetEncoder() string {
	return string(r.encoder.Load().(GPUMode))
}

// SetEncoder atualiza o encoder após detecção assíncrona no startup.
func (r *Runner) SetEncoder(gpu GPUMode) {
	r.encoder.Store(gpu)
}

// substituteEncoder troca "libx264" pelo encoder detectado e remove flags
// exclusivas do libx264 que hardware encoders não suportam, depois injeta
// os parâmetros de qualidade adequados para o encoder selecionado.
func (r *Runner) substituteEncoder(args []string) []string {
	encoder := r.encoder.Load().(GPUMode)
	if encoder == ModeCPU {
		return args
	}
	result := make([]string, 0, len(args)+6)
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "libx264":
			result = append(result, string(encoder))
		case "-x264-params", "-x264opts", "-crf", "-preset":
			i++ // pula flag + valor — exclusivos do libx264
		default:
			result = append(result, args[i])
		}
	}
	return r.injectEncoderQuality(result, encoder)
}

// injectEncoderQuality insere flags de qualidade antes do arquivo de saída (último arg).
// Cada encoder de hardware tem sua própria API de qualidade — -crf não existe fora do libx264.
// Se o caller já especificou -b:v nos args, respeita e não injeta (pipeline com bitrate explícito).
func (r *Runner) injectEncoderQuality(args []string, encoder GPUMode) []string {
	// Se o usuário já controlou qualidade ou bitrate, não sobrescreve.
	for _, a := range args {
		if a == "-b:v" || a == "-cq" || a == "-qp" {
			return args
		}
	}
	bv := fmt.Sprintf("%dM", r.hwBitrateMbps)
	maxrate := fmt.Sprintf("%dM", r.hwBitrateMbps*2)
	var flags []string
	switch encoder {
	case ModeNVENC:
		// -gpu 0: força o device CUDA 0 — necessário em laptops Optimus onde o device
		// padrão do NVENC falha com "unsupported device" sem seleção explícita.
		// -cq 20 -b:v 0: modo VBR com alvo de qualidade (equivalente ao CRF do libx264).
		flags = []string{"-gpu", "0", "-pix_fmt", "yuv420p", "-cq", "20", "-b:v", "0"}
	case ModeAMF:
		flags = []string{"-b:v", bv, "-maxrate", maxrate}
	case ModeMF:
		flags = []string{"-b:v", bv}
	default:
		return args // VAAPI: pipeline hwaccel requer setup diferente — sem injeção
	}
	if len(args) == 0 {
		return args
	}
	// Insere antes do último elemento (caminho do arquivo de saída)
	out := make([]string, 0, len(args)+len(flags))
	out = append(out, args[:len(args)-1]...)
	out = append(out, flags...)
	out = append(out, args[len(args)-1])
	return out
}

func (r *Runner) RunFFmpeg(args []string) error {
	args = r.substituteEncoder(args)
	log.Printf("ffmpeg %s", strings.Join(args, " "))
	ctx, cancel := context.WithTimeout(context.Background(), r.timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "ffmpeg", args...)
	var stderr bytes.Buffer
	cmd.Stderr = &limitedWriter{w: &stderr, limit: 32 * 1024}
	if err := cmd.Run(); err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return fmt.Errorf("ffmpeg timeout after %s", r.timeout)
		}
		return fmt.Errorf("ffmpeg error: %w output: %s", err, strings.TrimSpace(stderr.String()))
	}
	return nil
}

func (r *Runner) ProbeMedia(path string) (ports.MediaInfo, error) {
	cmd := exec.Command("ffprobe",
		"-v", "quiet",
		"-print_format", "json",
		"-show_format",
		"-show_streams",
		path,
	)
	out, err := cmd.Output()
	if err != nil {
		return ports.MediaInfo{}, fmt.Errorf("ffprobe: %w", err)
	}

	var probe struct {
		Format struct {
			Duration string `json:"duration"`
			Size     string `json:"size"`
		} `json:"format"`
		Streams []struct {
			CodecType    string `json:"codec_type"`
			Width        int    `json:"width"`
			Height       int    `json:"height"`
			RFrameRate   string `json:"r_frame_rate"`
			AvgFrameRate string `json:"avg_frame_rate"`
		} `json:"streams"`
	}
	if err := json.Unmarshal(out, &probe); err != nil {
		return ports.MediaInfo{}, fmt.Errorf("parse ffprobe output: %w", err)
	}

	dur, _ := strconv.ParseFloat(probe.Format.Duration, 64)
	size, _ := strconv.ParseInt(probe.Format.Size, 10, 64)

	var width, height int
	var framerate float64
	for _, s := range probe.Streams {
		if s.CodecType == "video" {
			width = s.Width
			height = s.Height
			// avg_frame_rate é mais estável que r_frame_rate em VFR;
			// se vier "0/0" (stream sem fps reportado) cai pro r_frame_rate.
			framerate = parseFraction(s.AvgFrameRate)
			if framerate == 0 {
				framerate = parseFraction(s.RFrameRate)
			}
			break
		}
	}

	return ports.MediaInfo{
		Duration:  dur,
		Width:     width,
		Height:    height,
		SizeBytes: size,
		Framerate: framerate,
	}, nil
}

// parseFraction interpreta "30000/1001", "30/1" ou "30" como float64.
// Retorna 0 se a string for vazia, malformada ou avaliar para 0/0.
func parseFraction(s string) float64 {
	if s == "" {
		return 0
	}
	parts := strings.SplitN(s, "/", 2)
	num, err := strconv.ParseFloat(parts[0], 64)
	if err != nil {
		return 0
	}
	if len(parts) == 1 {
		return num
	}
	den, err := strconv.ParseFloat(parts[1], 64)
	if err != nil || den == 0 {
		return 0
	}
	return num / den
}
