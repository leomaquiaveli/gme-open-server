package storage

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

// LocalStorage implementa ports.IStorage para ambiente local e desenvolvimento.
// Download: HTTP GET com streaming direto para disco — sem buffer do vídeo em RAM.
// Upload: em modo local, o arquivo de saída já está no disco; retorna o caminho local.
type LocalStorage struct {
	basePath   string
	httpClient *http.Client
}

func NewLocalStorage(basePath string) (*LocalStorage, error) {
	for _, subdir := range []string{"cache", "outputs"} {
		if err := os.MkdirAll(filepath.Join(basePath, subdir), 0o755); err != nil {
			return nil, fmt.Errorf("create %s dir: %w", subdir, err)
		}
	}
	return &LocalStorage{
		basePath: basePath,
		// timeout generoso para vídeos grandes em redes lentas
		httpClient: &http.Client{Timeout: 30 * time.Minute},
	}, nil
}

func (s *LocalStorage) Download(url, destPath string) error {
	if err := os.MkdirAll(filepath.Dir(destPath), 0o755); err != nil {
		return fmt.Errorf("create dest dir: %w", err)
	}

	resp, err := s.httpClient.Get(url)
	if err != nil {
		return fmt.Errorf("http get: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download returned status %d for %s", resp.StatusCode, url)
	}

	f, err := os.Create(destPath)
	if err != nil {
		return fmt.Errorf("create file: %w", err)
	}
	defer f.Close()

	if _, err := io.Copy(f, resp.Body); err != nil {
		os.Remove(destPath) // remove arquivo parcial
		return fmt.Errorf("write download: %w", err)
	}
	return nil
}

// Upload em modo local: o arquivo de saída já está em disco — retorna o caminho como "URL".
func (s *LocalStorage) Upload(localPath string, contentType string) (string, error) {
	return localPath, nil
}
