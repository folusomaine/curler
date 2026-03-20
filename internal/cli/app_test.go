package cli

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"curler/internal/config"
	"curler/internal/executor"
)

func TestRunInitCreatesConfig(t *testing.T) {
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	app := &App{
		stdout: stdout,
		stderr: stderr,
		cwd:    t.TempDir(),
	}

	code := app.Run([]string{"init"})
	if code != 0 {
		t.Fatalf("expected success, got %d: %s", code, stderr.String())
	}

	if _, err := os.Stat(filepath.Join(app.cwd, config.FileName)); err != nil {
		t.Fatalf("expected config file: %v", err)
	}
	if !strings.Contains(stdout.String(), "created") {
		t.Fatalf("unexpected stdout: %s", stdout.String())
	}
}

func TestRunListPrintsSavedRequests(t *testing.T) {
	dir := t.TempDir()
	if err := config.SavePath(filepath.Join(dir, config.FileName), config.Default()); err != nil {
		t.Fatalf("save config: %v", err)
	}

	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	app := &App{stdout: stdout, stderr: stderr, cwd: dir}

	code := app.Run([]string{"list"})
	if code != 0 {
		t.Fatalf("expected success, got %d: %s", code, stderr.String())
	}
	if got := strings.TrimSpace(stdout.String()); got != "default/users" {
		t.Fatalf("unexpected list output %q", got)
	}
}

func TestRunCommandAppliesOverridesAndWritesOutput(t *testing.T) {
	dir := t.TempDir()
	if err := config.SavePath(filepath.Join(dir, config.FileName), config.Default()); err != nil {
		t.Fatalf("save config: %v", err)
	}

	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	bodyPath := filepath.Join(dir, "response.txt")

	var captured *executor.ResolvedRequest
	app := &App{
		stdout: stdout,
		stderr: stderr,
		cwd:    dir,
		execute: func(ctx context.Context, request *executor.ResolvedRequest) (*executor.Response, error) {
			captured = request
			return &executor.Response{
				Status:      "200 OK",
				StatusCode:  200,
				Header:      map[string][]string{"Content-Type": {"application/json"}},
				Body:        []byte(`{"ok":true}`),
				ContentType: "application/json",
			}, nil
		},
	}

	code := app.Run([]string{
		"run", "default/users",
		"--env", "staging",
		"--header", "X-Debug: 1",
		"--query", "page=2",
		"--body", `{"override":true}`,
		"--timeout", "5s",
		"--output", bodyPath,
	})
	if code != 0 {
		t.Fatalf("expected success, got %d: %s", code, stderr.String())
	}
	if captured == nil {
		t.Fatal("expected execute to be called")
	}
	if !strings.Contains(captured.URL, "page=2") {
		t.Fatalf("expected query override in URL, got %s", captured.URL)
	}
	if captured.Headers["X-Debug"] != "1" {
		t.Fatalf("expected header override, got %#v", captured.Headers)
	}
	if captured.Body.Content != `{"override":true}` {
		t.Fatalf("expected body override, got %s", captured.Body.Content)
	}

	data, err := os.ReadFile(bodyPath)
	if err != nil {
		t.Fatalf("read output body: %v", err)
	}
	if string(data) != `{"ok":true}` {
		t.Fatalf("unexpected output file contents %q", string(data))
	}
}

func TestRunWithoutArgsLaunchesTUI(t *testing.T) {
	dir := t.TempDir()
	if err := config.SavePath(filepath.Join(dir, config.FileName), config.Default()); err != nil {
		t.Fatalf("save config: %v", err)
	}

	app := &App{
		stdout: &bytes.Buffer{},
		stderr: &bytes.Buffer{},
		cwd:    dir,
		runTUI: func(path string, file *config.File) error {
			if path == "" || file == nil {
				t.Fatal("expected config to be passed to TUI")
			}
			return nil
		},
	}

	if code := app.Run(nil); code != 0 {
		t.Fatalf("expected success, got %d", code)
	}
}
