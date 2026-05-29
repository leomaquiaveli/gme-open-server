package storage

import (
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// metadataTokenURL is the GCP metadata server endpoint for the instance's default service account.
const metadataTokenURL = "http://metadata.google.internal/computeMetadata/v1/instance/service-accounts/default/token"

type GCSStorage struct {
	bucket         string
	useADC         bool // true when running on GCP with Workload Identity / metadata server
	creds          serviceAccountCreds
	mu             sync.Mutex
	cachedToken    string
	tokenExpiry    time.Time
	client         *http.Client // upload + token (30min)
	downloadClient *http.Client // download — menor timeout para Cloud Run
}

type serviceAccountCreds struct {
	ClientEmail string `json:"client_email"`
	PrivateKey  string `json:"private_key"`
	TokenURI    string `json:"token_uri"`
}

// NewGCSStorage creates a GCS storage adapter.
// When credsJSON is empty, ADC mode is used: tokens are fetched from the GCP metadata server.
// This is the recommended approach for Cloud Run — no credentials need to be stored in env vars.
// When credsJSON is provided, the service account JSON key is used (useful for local dev and VMs).
func NewGCSStorage(bucket, credsJSON string) (*GCSStorage, error) {
	g := &GCSStorage{
		bucket:         bucket,
		client:         &http.Client{Timeout: 30 * time.Minute},
		downloadClient: &http.Client{Timeout: 10 * time.Minute},
	}
	if credsJSON == "" {
		g.useADC = true
		return g, nil
	}
	var creds serviceAccountCreds
	if err := json.Unmarshal([]byte(credsJSON), &creds); err != nil {
		return nil, fmt.Errorf("parse service account credentials: %w", err)
	}
	if creds.ClientEmail == "" || creds.PrivateKey == "" {
		return nil, fmt.Errorf("credentials missing client_email or private_key")
	}
	if creds.TokenURI == "" {
		creds.TokenURI = "https://oauth2.googleapis.com/token"
	}
	g.creds = creds
	return g, nil
}

// Download faz GET direto na URL pública usando downloadClient (10min timeout).
func (g *GCSStorage) Download(rawURL, destPath string) error {
	if err := os.MkdirAll(filepath.Dir(destPath), 0o755); err != nil {
		return fmt.Errorf("create dir: %w", err)
	}
	resp, err := g.downloadClient.Get(rawURL)
	if err != nil {
		return fmt.Errorf("download %s: %w", rawURL, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download returned status %d for %s", resp.StatusCode, rawURL)
	}
	f, err := os.Create(destPath)
	if err != nil {
		return fmt.Errorf("create file: %w", err)
	}
	defer f.Close()
	if _, err := io.Copy(f, resp.Body); err != nil {
		os.Remove(destPath) // remove arquivo parcial — não polui o disco
		return fmt.Errorf("write download: %w", err)
	}
	return nil
}

// Upload envia o arquivo para o bucket GCS e retorna a URL pública.
func (g *GCSStorage) Upload(localPath string, contentType string) (string, error) {
	token, err := g.accessToken()
	if err != nil {
		return "", fmt.Errorf("get access token: %w", err)
	}

	f, err := os.Open(localPath)
	if err != nil {
		return "", fmt.Errorf("open file: %w", err)
	}
	defer f.Close()

	stat, err := f.Stat()
	if err != nil {
		return "", fmt.Errorf("stat file: %w", err)
	}

	objectName := filepath.Base(localPath)
	uploadURL := fmt.Sprintf(
		"https://storage.googleapis.com/upload/storage/v1/b/%s/o?uploadType=media&name=%s",
		url.PathEscape(g.bucket),
		url.QueryEscape(objectName),
	)

	req, err := http.NewRequest("POST", uploadURL, f)
	if err != nil {
		return "", fmt.Errorf("build upload request: %w", err)
	}
	req.ContentLength = stat.Size()
	req.Header.Set("Authorization", "Bearer "+token)

	if contentType == "" {
		contentType = contentTypeForPath(localPath)
	}
	req.Header.Set("Content-Type", contentType)

	resp, err := g.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("upload: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		return "", fmt.Errorf("upload failed (%d): %s", resp.StatusCode, body)
	}

	return fmt.Sprintf("https://storage.googleapis.com/%s/%s",
		g.bucket, url.PathEscape(objectName)), nil
}

// contentTypeForPath infere o Content-Type pela extensão. Sem isso, o GCS recebe
// application/octet-stream e o navegador força download em vez de tocar inline.
func contentTypeForPath(path string) string {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".mp4", ".m4v":
		return "video/mp4"
	case ".mov":
		return "video/quicktime"
	case ".webm":
		return "video/webm"
	case ".mkv":
		return "video/x-matroska"
	case ".mp3":
		return "audio/mpeg"
	case ".m4a":
		return "audio/mp4"
	case ".aac":
		return "audio/aac"
	case ".wav":
		return "audio/wav"
	case ".ogg":
		return "audio/ogg"
	}
	if ct := mime.TypeByExtension(filepath.Ext(path)); ct != "" {
		return ct
	}
	return "application/octet-stream"
}

func (g *GCSStorage) accessToken() (string, error) {
	g.mu.Lock()
	defer g.mu.Unlock()

	if g.cachedToken != "" && time.Now().Before(g.tokenExpiry) {
		return g.cachedToken, nil
	}

	token, expiry, err := g.fetchToken()
	if err != nil {
		return "", err
	}
	g.cachedToken = token
	g.tokenExpiry = expiry
	return token, nil
}

func (g *GCSStorage) fetchToken() (string, time.Time, error) {
	if g.useADC {
		return g.fetchTokenFromMetadata()
	}
	return g.fetchTokenFromSA()
}

// fetchTokenFromMetadata fetches a short-lived token from the GCP instance metadata server.
// Requires the Cloud Run service account (or VM SA) to have Storage Object Admin on the bucket.
func (g *GCSStorage) fetchTokenFromMetadata() (string, time.Time, error) {
	req, err := http.NewRequest("GET", metadataTokenURL, nil)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("build metadata request: %w", err)
	}
	req.Header.Set("Metadata-Flavor", "Google")

	resp, err := g.client.Do(req)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("metadata token request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", time.Time{}, fmt.Errorf("metadata server returned %d: %s", resp.StatusCode, body)
	}

	var result struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", time.Time{}, fmt.Errorf("parse metadata token response: %w", err)
	}
	if result.AccessToken == "" {
		return "", time.Time{}, fmt.Errorf("empty access token from metadata server")
	}

	expiry := time.Now().Add(time.Duration(result.ExpiresIn-60) * time.Second)
	return result.AccessToken, expiry, nil
}

// fetchTokenFromSA signs a JWT with the service account private key and exchanges it for an OAuth2 token.
func (g *GCSStorage) fetchTokenFromSA() (string, time.Time, error) {
	// iat 30s no passado para absorver drift do relógio vs servidores Google.
	iat := time.Now().Add(-30 * time.Second)
	exp := iat.Add(time.Hour)

	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"RS256","typ":"JWT"}`))

	claimsJSON, _ := json.Marshal(map[string]any{
		"iss":   g.creds.ClientEmail,
		"scope": "https://www.googleapis.com/auth/devstorage.read_write",
		"aud":   g.creds.TokenURI,
		"exp":   exp.Unix(),
		"iat":   iat.Unix(),
	})
	claims := base64.RawURLEncoding.EncodeToString(claimsJSON)

	signingInput := header + "." + claims
	sig, err := signRSASHA256([]byte(signingInput), g.creds.PrivateKey)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("sign JWT: %w", err)
	}

	jwt := signingInput + "." + base64.RawURLEncoding.EncodeToString(sig)

	formBody := url.Values{
		"grant_type": {"urn:ietf:params:oauth:grant-type:jwt-bearer"},
		"assertion":  {jwt},
	}

	resp, err := g.client.PostForm(g.creds.TokenURI, formBody)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("token request: %w", err)
	}
	defer resp.Body.Close()

	var result struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"`
		Error       string `json:"error"`
		ErrorDesc   string `json:"error_description"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", time.Time{}, fmt.Errorf("parse token response: %w", err)
	}
	if result.Error != "" {
		return "", time.Time{}, fmt.Errorf("token error: %s: %s", result.Error, result.ErrorDesc)
	}
	if result.AccessToken == "" {
		return "", time.Time{}, fmt.Errorf("empty access token in response")
	}

	tokenExpiry := iat.Add(time.Duration(result.ExpiresIn-60) * time.Second)
	return result.AccessToken, tokenExpiry, nil
}

func signRSASHA256(message []byte, pemKey string) ([]byte, error) {
	block, _ := pem.Decode([]byte(pemKey))
	if block == nil {
		return nil, fmt.Errorf("failed to decode PEM block from private key")
	}
	key, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("parse private key: %w", err)
	}
	rsaKey, ok := key.(*rsa.PrivateKey)
	if !ok {
		return nil, fmt.Errorf("private key is not RSA")
	}
	hash := sha256.Sum256(message)
	return rsa.SignPKCS1v15(rand.Reader, rsaKey, crypto.SHA256, hash[:])
}
