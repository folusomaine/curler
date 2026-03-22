package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFindPathWalksUpDirectories(t *testing.T) {
	root := t.TempDir()
	configPath := filepath.Join(root, FileName)
	if err := SavePath(configPath, Default()); err != nil {
		t.Fatalf("save config: %v", err)
	}

	nested := filepath.Join(root, "a", "b", "c")
	t.Setenv("PWD", nested)
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatalf("mkdir nested: %v", err)
	}

	found, err := FindPath(nested)
	if err != nil {
		t.Fatalf("find path: %v", err)
	}
	if found != configPath {
		t.Fatalf("expected %s, got %s", configPath, found)
	}
}

func TestFindPathFallsBackToLegacyConfigName(t *testing.T) {
	root := t.TempDir()
	legacyPath := filepath.Join(root, LegacyFileName)
	if err := SavePath(legacyPath, Default()); err != nil {
		t.Fatalf("save legacy config: %v", err)
	}

	found, err := FindPath(root)
	if err != nil {
		t.Fatalf("find path: %v", err)
	}
	if found != legacyPath {
		t.Fatalf("expected %s, got %s", legacyPath, found)
	}
}

func TestInterpolateStringReportsMissingVariables(t *testing.T) {
	_, err := InterpolateString("${BASE_URL}/users/${USER_ID}", map[string]string{"BASE_URL": "https://example.com"})
	if err == nil {
		t.Fatal("expected missing variable error")
	}
	if !strings.Contains(err.Error(), "USER_ID") {
		t.Fatalf("expected USER_ID in error, got %v", err)
	}
}

func TestLoadAndSaveRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), FileName)
	original := Default()
	original.UpsertRequest("admin", "health", &RequestDef{
		Method: "GET",
		URL:    "${BASE_URL}/health",
	})

	if err := SavePath(path, original); err != nil {
		t.Fatalf("save path: %v", err)
	}

	loaded, err := LoadPath(path)
	if err != nil {
		t.Fatalf("load path: %v", err)
	}

	if loaded.ActiveEnv != original.ActiveEnv {
		t.Fatalf("expected active env %q, got %q", original.ActiveEnv, loaded.ActiveEnv)
	}

	_, _, request, err := loaded.GetRequest("admin/health")
	if err != nil {
		t.Fatalf("get request: %v", err)
	}
	if request.URL != "${BASE_URL}/health" {
		t.Fatalf("unexpected request URL %q", request.URL)
	}
}
