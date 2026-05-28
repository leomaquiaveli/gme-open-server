package application

import (
	"encoding/json"
	"fmt"
)

type ToMP3Request struct {
	MediaURL   string `json:"media_url"`
	WebhookURL string `json:"webhook_url"`
	Bitrate    string `json:"bitrate"` // e.g., "128k"
}

type ToMP3MediaUseCase struct {
	pipelineUC *PipelineMediaUseCase
}

func NewToMP3MediaUseCase(pipelineUC *PipelineMediaUseCase) *ToMP3MediaUseCase {
	return &ToMP3MediaUseCase{pipelineUC: pipelineUC}
}

func (uc *ToMP3MediaUseCase) buildPipelineReq(req ToMP3Request) PipelineRequest {
	bitrate := req.Bitrate
	if bitrate == "" {
		bitrate = "128k"
	}

	return PipelineRequest{
		Inputs: []InputFile{
			{FileURL: req.MediaURL},
		},
		Outputs: []OutputSpec{
			{
				Options: []FFmpegOption{
					{Option: "-vn"}, // Desabilita o processamento de vídeo (extrai apenas áudio)
					{Option: "-c:a", Argument: json.RawMessage(`"libmp3lame"`)}, // Encoder MP3
					{Option: "-b:a", Argument: json.RawMessage(`"` + bitrate + `"`)}, // Bitrate
				},
			},
		},
		FileName:   "audio.mp3",
		WebhookURL: req.WebhookURL,
	}
}

func (uc *ToMP3MediaUseCase) Execute(req ToMP3Request) (string, error) {
	if req.MediaURL == "" {
		return "", fmt.Errorf("media_url cannot be empty")
	}
	return uc.pipelineUC.Execute(uc.buildPipelineReq(req))
}

func (uc *ToMP3MediaUseCase) ExecuteSync(req ToMP3Request) (*JobResult, error) {
	if req.MediaURL == "" {
		return nil, fmt.Errorf("media_url cannot be empty")
	}
	return uc.pipelineUC.ExecuteSync(uc.buildPipelineReq(req))
}
