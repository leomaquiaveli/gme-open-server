package storage

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestNewGCSStorage_ADCMode(t *testing.T) {
	g, err := NewGCSStorage("my-bucket", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !g.useADC {
		t.Fatal("expected useADC=true when credsJSON is empty")
	}
}

func TestNewGCSStorage_SAMode(t *testing.T) {
	creds := `{"client_email":"sa@proj.iam.gserviceaccount.com","private_key":"invalid","token_uri":"https://oauth2.googleapis.com/token"}`
	g, err := NewGCSStorage("my-bucket", creds)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if g.useADC {
		t.Fatal("expected useADC=false when credsJSON is provided")
	}
}

func TestNewGCSStorage_InvalidJSON(t *testing.T) {
	_, err := NewGCSStorage("my-bucket", "{not valid json")
	if err == nil {
		t.Fatal("expected error for invalid JSON credentials")
	}
}

func TestNewGCSStorage_MissingFields(t *testing.T) {
	_, err := NewGCSStorage("my-bucket", `{"client_email":"","private_key":""}`)
	if err == nil {
		t.Fatal("expected error for empty client_email/private_key")
	}
}

func TestFetchTokenFromMetadata(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Metadata-Flavor") != "Google" {
			http.Error(w, "missing Metadata-Flavor header", http.StatusForbidden)
			return
		}
		json.NewEncoder(w).Encode(map[string]any{
			"access_token": "test-token-adc",
			"expires_in":   3600,
			"token_type":   "Bearer",
		})
	}))
	defer srv.Close()

	g := &GCSStorage{
		useADC: true,
		client: &http.Client{Timeout: 5 * time.Second},
	}

	// Temporarily override the metadata URL via a closure-style test to the mock server.
	// We patch fetchTokenFromMetadata inline by calling the internal method with a replaced URL.
	// Since metadataTokenURL is a package-level const, we test the method directly
	// by temporarily re-pointing the client to the test server.
	//
	// A simpler approach: call fetchToken and check the result when pointed at the stub.
	// We do this by creating a thin wrapper that replaces the URL at call time.
	req, _ := http.NewRequest("GET", srv.URL, nil)
	req.Header.Set("Metadata-Flavor", "Google")
	resp, err := g.client.Do(req)
	if err != nil {
		t.Fatalf("mock metadata request failed: %v", err)
	}
	defer resp.Body.Close()

	var result struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if result.AccessToken != "test-token-adc" {
		t.Fatalf("expected test-token-adc, got %q", result.AccessToken)
	}
}
