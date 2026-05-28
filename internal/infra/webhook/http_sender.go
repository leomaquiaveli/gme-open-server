package webhook

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"time"
)

// HTTPSender implementa ports.IWebhookSender.
// Compatível com o padrão de callback do render engine TypeScript.
type HTTPSender struct {
	client *http.Client
}

func NewHTTPSender() *HTTPSender {
	return &HTTPSender{
		client: &http.Client{Timeout: 10 * time.Second},
	}
}

// Send envia o payload para o webhook com até 3 tentativas (backoff: 1s, 2s).
func (s *HTTPSender) Send(webhookURL string, payload any) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("webhook marshal error: %w", err)
	}

	delays := []time.Duration{0, time.Second, 2 * time.Second}
	var lastErr error
	for attempt, delay := range delays {
		if delay > 0 {
			time.Sleep(delay)
		}
		if err := s.post(webhookURL, body); err != nil {
			lastErr = err
			log.Printf("webhook attempt %d/3 failed: %v", attempt+1, err)
			continue
		}
		return nil
	}
	return lastErr
}

func (s *HTTPSender) post(webhookURL string, body []byte) error {
	resp, err := s.client.Post(webhookURL, "application/json", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("webhook send error: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return fmt.Errorf("webhook returned status %d", resp.StatusCode)
	}
	return nil
}
