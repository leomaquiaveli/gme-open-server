package application

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"mime/multipart"
	"os"
	"path/filepath"
	"strings"

	"github.com/leomaquiaveli/gme-open-server/internal/domain/ports"
)

type UploadRequest struct {
	FileURL     string         // Option A: URL para download
	FileName    string         // Opcional
	File        multipart.File // Option B: Arquivo via multipart form
	ContentType string         // MIME Type opcional vindo do formulário
}

type UploadResult struct {
	FileURL  string `json:"file_url"`
	FileName string `json:"filename"`
	Public   bool   `json:"public"`
	Bucket   string `json:"bucket,omitempty"` // Pode ser retornado se necessário, mas GCS abstrai a URL
}

type UploadMediaUseCase struct {
	storage ports.IStorage
	workDir string
}

func NewUploadMediaUseCase(storage ports.IStorage, workDir string) *UploadMediaUseCase {
	return &UploadMediaUseCase{
		storage: storage,
		workDir: workDir,
	}
}

func (uc *UploadMediaUseCase) Execute(req UploadRequest) (*UploadResult, error) {
	if req.FileURL == "" && req.File == nil {
		return nil, fmt.Errorf("must provide either file_url or a multipart file")
	}

	fileName := req.FileName
	if fileName == "" {
		if req.FileURL != "" {
			fileName = sanitizeFileName(filepath.Base(req.FileURL))
			// Se a URL não tiver um nome de arquivo válido no final
			if fileName == "" || fileName == "/" || fileName == "." {
				fileName = generateID() + ".bin"
			} else {
				// Remove query parameters se houver (ex: video.mp4?token=123)
				if idx := strings.Index(fileName, "?"); idx != -1 {
					fileName = fileName[:idx]
				}
			}
		} else {
			fileName = generateID() + ".bin"
		}
	} else {
		fileName = sanitizeFileName(fileName)
	}

	tempDir := filepath.Join(uc.workDir, "tmp")
	os.MkdirAll(tempDir, 0o755)

	// Cria pasta única para evitar colisões com uploads simultâneos do mesmo filename
	uniqueDir := filepath.Join(tempDir, generateID())
	os.MkdirAll(uniqueDir, 0o755)
	defer os.RemoveAll(uniqueDir) // Limpa ao finalizar

	// O GCS Storage Adapter usa o filepath.Base() do localPath para o nome no bucket
	finalTempPath := filepath.Join(uniqueDir, fileName)

	if req.FileURL != "" {
		if err := uc.storage.Download(req.FileURL, finalTempPath); err != nil {
			return nil, fmt.Errorf("failed to download from URL: %w", err)
		}
	} else if req.File != nil {
		out, err := os.Create(finalTempPath)
		if err != nil {
			return nil, fmt.Errorf("failed to create temp file: %w", err)
		}
		_, err = io.Copy(out, req.File)
		out.Close()
		if err != nil {
			return nil, fmt.Errorf("failed to save uploaded file: %w", err)
		}
	}

	publicURL, err := uc.storage.Upload(finalTempPath, req.ContentType)
	if err != nil {
		return nil, fmt.Errorf("failed to upload to storage: %w", err)
	}

	return &UploadResult{
		FileURL:  publicURL,
		FileName: fileName,
		Public:   true,
	}, nil
}

func generateID() string {
	b := make([]byte, 16)
	rand.Read(b)
	return hex.EncodeToString(b)
}
