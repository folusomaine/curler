package tui

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"curler/internal/config"
	"curler/internal/executor"
)

func TestSaveCurrentRenamesRequest(t *testing.T) {
	path := filepath.Join(t.TempDir(), config.FileName)
	file := config.Default()
	if err := config.SavePath(path, file); err != nil {
		t.Fatalf("save config: %v", err)
	}

	model := newModel(path, file, func(ctx context.Context, request *executor.ResolvedRequest) (*executor.Response, error) {
		return &executor.Response{}, nil
	})
	model.collectionInput.SetValue("admin")
	model.nameInput.SetValue("health")
	model.urlInput.SetValue("${BASE_URL}/health")
	model.methodInput.SetValue("GET")
	model.activeEnvInput.SetValue("local")

	if err := model.saveCurrent(); err != nil {
		t.Fatalf("save current: %v", err)
	}

	loaded, err := config.LoadPath(path)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if _, _, _, err := loaded.GetRequest("admin/health"); err != nil {
		t.Fatalf("expected renamed request: %v", err)
	}
}

func TestRunCurrentUsesEditedDraft(t *testing.T) {
	path := filepath.Join(t.TempDir(), config.FileName)
	file := config.Default()

	var captured *executor.ResolvedRequest
	model := newModel(path, file, func(ctx context.Context, request *executor.ResolvedRequest) (*executor.Response, error) {
		captured = request
		return &executor.Response{
			Status:      "200 OK",
			StatusCode:  200,
			Body:        []byte(`{"ok":true}`),
			ContentType: "application/json",
		}, nil
	})

	model.collectionInput.SetValue("default")
	model.nameInput.SetValue("users")
	model.methodInput.SetValue("POST")
	model.urlInput.SetValue("${BASE_URL}/users")
	model.bodyModeInput.SetValue(config.BodyModeJSON)
	model.bodyInput.SetValue(`{"hello":"world"}`)
	model.activeEnvInput.SetValue("local")

	cmd, err := model.runCurrent()
	if err != nil {
		t.Fatalf("run current: %v", err)
	}

	msg := cmd()
	result, ok := msg.(runResultMsg)
	if !ok {
		t.Fatalf("expected runResultMsg, got %T", msg)
	}
	if result.err != nil {
		t.Fatalf("unexpected run error: %v", result.err)
	}
	if captured == nil {
		t.Fatal("expected request to be executed")
	}
	if captured.Method != "POST" {
		t.Fatalf("expected POST, got %s", captured.Method)
	}
	if captured.Body.Content != `{"hello":"world"}` {
		t.Fatalf("unexpected body %s", captured.Body.Content)
	}
	if !strings.Contains(result.rendered, "\"ok\": true") {
		t.Fatalf("expected formatted response, got %s", result.rendered)
	}
}
