package ports

// MediaInfo contém os metadados do arquivo de vídeo de saída.
type MediaInfo struct {
	Duration  float64 // segundos
	Width     int
	Height    int
	SizeBytes int64
	// Framerate em fps (avg_frame_rate do ffprobe). 0 se desconhecido.
	// Usado pelo caminho de zoom dinâmico para passar fps explícito ao filtro
	// zoompan, evitando o default 25 que dessincroniza A/V quando source != 25.
	Framerate float64
}

// IMediaProcessor é a porta de saída (Driven Port) na Clean Architecture que abstrai o motor
// de processamento de mídia.
// O motivo de sua existência é isolar as regras de negócio da ferramenta física de processamento 
// (como o binário do FFmpeg). Isso garante que o Domínio orquestre as edições de vídeo 
// sem acoplamento direto a linhas de comando, processos do SO ou bibliotecas específicas.
// GetEncoder retorna o encoder detectado no startup: h264_nvenc, h264_vaapi, ou libx264.
type IMediaProcessor interface {
	RunFFmpeg(args []string) error
	GetEncoder() string
	ProbeMedia(path string) (MediaInfo, error)
}
