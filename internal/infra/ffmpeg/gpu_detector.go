package ffmpeg

import (
	"bytes"
	"context"
	"fmt"
	"log"
	"os/exec"
	"runtime"
	"strings"
	"time"
)

// GPUMode representa o encoder de vídeo disponível na máquina.
type GPUMode string

const (
	ModeNVENC GPUMode = "h264_nvenc" // NVIDIA — 5-10x mais rápido que CPU
	ModeAMF   GPUMode = "h264_amf"   // AMD — 4-8x mais rápido que CPU
	ModeVAAPI GPUMode = "h264_vaapi" // Intel/AMD Linux — 2-4x mais rápido que CPU
	ModeMF    GPUMode = "h264_mf"    // Windows Media Foundation — qualquer GPU Windows via DirectX
	ModeCPU   GPUMode = "libx264"    // fallback universal — funciona em qualquer máquina
)

// DetectOrOverride retorna o encoder configurado em ENCODER (se válido) ou auto-detecta.
// ENCODER=auto (ou vazio) → auto-detect.
// ENCODER=h264_nvenc → força NVENC (NVIDIA).
// ENCODER=h264_mf    → força Windows Media Foundation.
// ENCODER=libx264    → força CPU (qualidade máxima, mais lento).
func DetectOrOverride(forceEncoder string) GPUMode {
	switch GPUMode(forceEncoder) {
	case ModeCPU, ModeNVENC, ModeVAAPI, ModeAMF, ModeMF:
		log.Printf("encoder: forçado via ENCODER=%s", forceEncoder)
		return GPUMode(forceEncoder)
	}
	return DetectGPU()
}

// DetectGPU detecta o melhor encoder disponível com probe real de encode.
//
// Ordem Windows: NVENC → AMF → MF → CPU (VAAPI é Linux-only)
// Ordem Linux:   NVENC → AMF → VAAPI → CPU (MF é Windows-only)
func DetectGPU() GPUMode {
	ffmpegPath, err := exec.LookPath("ffmpeg")
	if err != nil {
		log.Printf("encoder: ffmpeg não encontrado no PATH — usando CPU")
		return ModeCPU
	}
	log.Printf("encoder: ffmpeg em %s", ffmpegPath)

	out, err := exec.Command("ffmpeg", "-hide_banner", "-encoders").Output()
	if err != nil {
		log.Printf("encoder: falha ao listar encoders — usando CPU")
		return ModeCPU
	}
	encoders := string(out)

	if strings.Contains(encoders, "h264_nvenc") {
		if ok, reason := tryEncoder("h264_nvenc"); ok {
			log.Printf("encoder: h264_nvenc selecionado")
			return ModeNVENC
		} else {
			log.Printf("encoder: h264_nvenc descartado — %s", reason)
		}
	}

	if strings.Contains(encoders, "h264_amf") {
		if ok, reason := tryEncoder("h264_amf"); ok {
			log.Printf("encoder: h264_amf selecionado")
			return ModeAMF
		} else {
			log.Printf("encoder: h264_amf descartado — %s", reason)
		}
	}

	if runtime.GOOS != "windows" {
		if strings.Contains(encoders, "h264_vaapi") {
			if ok, reason := tryEncoder("h264_vaapi"); ok {
				log.Printf("encoder: h264_vaapi selecionado")
				return ModeVAAPI
			} else {
				log.Printf("encoder: h264_vaapi descartado — %s", reason)
			}
		}
	}

	if runtime.GOOS == "windows" {
		if strings.Contains(encoders, "h264_mf") {
			if ok, reason := tryEncoder("h264_mf"); ok {
				log.Printf("encoder: h264_mf selecionado")
				return ModeMF
			} else {
				log.Printf("encoder: h264_mf descartado — %s", reason)
			}
		}
	}

	log.Printf("encoder: nenhuma GPU disponível — usando libx264 (CPU)")
	return ModeCPU
}

// nvencSessionErrors são strings no stderr que indicam falha real de sessão NVENC.
// O FFmpeg pode retornar exit code não-zero E essas strings simultaneamente,
// ou em casos raros retornar exit 0 mas com sessão degradada.
var nvencSessionErrors = []string{
	"OpenEncodeSessionEx failed",
	"No capable devices found",
	"unsupported device",
}

func tryEncoder(encoder string) (bool, string) {
	args := buildProbeArgs(encoder)
	log.Printf("encoder probe: ffmpeg %s", strings.Join(args, " "))

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	var stderr bytes.Buffer
	cmd := exec.CommandContext(ctx, "ffmpeg", args...)
	cmd.Stderr = &stderr
	err := cmd.Run()
	stderrStr := stderr.String()

	// Verifica strings de falha de sessão NVENC no stderr independente do exit code.
	// O probe 128x128 passava (false positive) mas o encode real 1080p falhava —
	// agora usamos 1280x720 por 1s para forçar inicialização real da sessão.
	if encoder == "h264_nvenc" {
		for _, s := range nvencSessionErrors {
			if strings.Contains(stderrStr, s) {
				return false, fmt.Sprintf("sessão NVENC falhou: %q", s)
			}
		}
	}

	if err != nil {
		return false, fmt.Sprintf("probe falhou: %v", err)
	}
	return true, ""
}

// buildProbeArgs monta o comando de probe por encoder.
// NVENC usa testsrc2=1280x720 por 1 segundo — tamanho representativo que força
// inicialização real da sessão de encode (128x128 era falso positivo em Optimus).
func buildProbeArgs(encoder string) []string {
	switch encoder {
	case "h264_nvenc":
		return []string{
			"-hide_banner", "-y",
			"-f", "lavfi", "-i", "testsrc2=size=1280x720:rate=30",
			"-t", "1",
			"-c:v", "h264_nvenc",
			"-pix_fmt", "yuv420p",
			"-gpu", "0",
			"-f", "null", "-",
		}
	default:
		return []string{
			"-hide_banner", "-y",
			"-f", "lavfi", "-i", "color=black:s=128x128:r=25:d=0.1",
			"-frames:v", "1",
			"-c:v", encoder,
			"-f", "null", "-",
		}
	}
}
