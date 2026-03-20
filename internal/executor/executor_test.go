package executor

import (
	"context"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"curler/internal/config"
)

func TestPrepareRequestInterpolatesAndOverrides(t *testing.T) {
	request := &config.RequestDef{
		Method: "GET",
		URL:    "${BASE_URL}/users",
		Headers: map[string]string{
			"Accept": "application/json",
		},
		Query: map[string]string{
			"page": "${PAGE}",
		},
		Auth: config.AuthConfig{
			Type:  config.AuthTypeBearer,
			Token: "${TOKEN}",
		},
		Timeout: "10s",
	}

	resolved, err := PrepareRequest(request, map[string]string{
		"BASE_URL": "https://example.com",
		"PAGE":     "1",
		"TOKEN":    "abc123",
	}, OverrideOptions{
		Headers: map[string]string{"X-Debug": "true"},
		Query:   map[string]string{"page": "2"},
		Timeout: 5 * time.Second,
	})
	if err != nil {
		t.Fatalf("prepare request: %v", err)
	}

	if resolved.URL != "https://example.com/users?page=2" {
		t.Fatalf("unexpected URL %q", resolved.URL)
	}
	if resolved.Headers["X-Debug"] != "true" {
		t.Fatalf("expected header override")
	}
	if resolved.Auth.Token != "abc123" {
		t.Fatalf("expected interpolated token, got %q", resolved.Auth.Token)
	}
	if resolved.Timeout != 5*time.Second {
		t.Fatalf("expected timeout override, got %s", resolved.Timeout)
	}
}

func TestExecuteBearerJSONRequest(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("expected POST, got %s", r.Method)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer secret-token" {
			t.Fatalf("unexpected auth header %q", got)
		}
		if got := r.URL.Query().Get("page"); got != "5" {
			t.Fatalf("unexpected query value %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer server.Close()

	response, err := Execute(context.Background(), &ResolvedRequest{
		Method:  http.MethodPost,
		URL:     server.URL + "/users?page=5",
		Headers: map[string]string{"Accept": "application/json"},
		Auth: config.AuthConfig{
			Type:  config.AuthTypeBearer,
			Token: "secret-token",
		},
		Body: config.BodyConfig{
			Mode:    config.BodyModeJSON,
			Content: `{"name":"curler"}`,
		},
		Timeout: time.Second,
	})
	if err != nil {
		t.Fatalf("execute request: %v", err)
	}

	if response.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", response.StatusCode)
	}
	if response.IsBinary {
		t.Fatal("expected JSON response to be treated as text")
	}
}

func TestExecuteBasicAuth(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		expected := "Basic " + base64.StdEncoding.EncodeToString([]byte("demo:secret"))
		if got := r.Header.Get("Authorization"); got != expected {
			t.Fatalf("unexpected auth header %q", got)
		}
		_, _ = w.Write([]byte("ok"))
	}))
	defer server.Close()

	_, err := Execute(context.Background(), &ResolvedRequest{
		Method:  http.MethodGet,
		URL:     server.URL,
		Headers: map[string]string{},
		Auth: config.AuthConfig{
			Type:     config.AuthTypeBasic,
			Username: "demo",
			Password: "secret",
		},
		Body:    config.BodyConfig{Mode: config.BodyModeNone},
		Timeout: time.Second,
	})
	if err != nil {
		t.Fatalf("execute request: %v", err)
	}
}

func TestExecuteTimeoutAndTransportError(t *testing.T) {
	slowServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(100 * time.Millisecond)
		_, _ = w.Write([]byte("too slow"))
	}))
	defer slowServer.Close()

	_, err := Execute(context.Background(), &ResolvedRequest{
		Method:  http.MethodGet,
		URL:     slowServer.URL,
		Headers: map[string]string{},
		Auth:    config.AuthConfig{Type: config.AuthTypeNone},
		Body:    config.BodyConfig{Mode: config.BodyModeNone},
		Timeout: 10 * time.Millisecond,
	})
	if err == nil {
		t.Fatal("expected timeout error")
	}

	_, err = Execute(context.Background(), &ResolvedRequest{
		Method:  http.MethodGet,
		URL:     "http://not-a-real-domain.invalid",
		Headers: map[string]string{},
		Auth:    config.AuthConfig{Type: config.AuthTypeNone},
		Body:    config.BodyConfig{Mode: config.BodyModeNone},
		Timeout: 100 * time.Millisecond,
	})
	if err == nil {
		t.Fatal("expected transport error")
	}
}

func TestExecuteMarksBinaryResponses(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/octet-stream")
		_, _ = w.Write([]byte{0xff, 0xd8, 0xff, 0x00})
	}))
	defer server.Close()

	response, err := Execute(context.Background(), &ResolvedRequest{
		Method:  http.MethodGet,
		URL:     server.URL,
		Headers: map[string]string{},
		Auth:    config.AuthConfig{Type: config.AuthTypeNone},
		Body:    config.BodyConfig{Mode: config.BodyModeNone},
		Timeout: time.Second,
	})
	if err != nil {
		t.Fatalf("execute request: %v", err)
	}
	if !response.IsBinary {
		t.Fatal("expected binary response")
	}
	if !strings.Contains(response.ContentType, "application/octet-stream") {
		t.Fatalf("unexpected content type %q", response.ContentType)
	}
}
