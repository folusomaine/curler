package cli

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"postack/internal/config"
	"postack/internal/executor"
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

func TestRunShortCommandsWork(t *testing.T) {
	dir := t.TempDir()

	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	app := &App{
		stdout: stdout,
		stderr: stderr,
		cwd:    dir,
	}

	if code := app.Run([]string{"i"}); code != 0 {
		t.Fatalf("expected init shorthand success, got %d: %s", code, stderr.String())
	}

	stdout.Reset()
	stderr.Reset()

	app = &App{stdout: stdout, stderr: stderr, cwd: dir}
	if code := app.Run([]string{"l"}); code != 0 {
		t.Fatalf("expected list shorthand success, got %d: %s", code, stderr.String())
	}
	if got := strings.TrimSpace(stdout.String()); got != "default/users" {
		t.Fatalf("unexpected list output %q", got)
	}
}

func TestTopLevelHelpFlagsPrintSummary(t *testing.T) {
	for _, args := range [][]string{{"-h"}, {"--help"}, {"help"}} {
		stdout := &bytes.Buffer{}
		stderr := &bytes.Buffer{}
		app := &App{stdout: stdout, stderr: stderr, cwd: t.TempDir()}

		code := app.Run(args)
		if code != 0 {
			t.Fatalf("expected success for %v, got %d: %s", args, code, stderr.String())
		}

		output := stdout.String()
		if !strings.Contains(output, "Usage:") {
			t.Fatalf("expected usage header for %v, got %q", args, output)
		}
		if !strings.Contains(output, "postack <command> [arguments]") {
			t.Fatalf("expected top-level usage line for %v, got %q", args, output)
		}
		if !strings.Contains(output, "init, i") || !strings.Contains(output, "run, r") {
			t.Fatalf("expected command summary for %v, got %q", args, output)
		}
		if !strings.Contains(output, "-o, --output") || !strings.Contains(output, "-s, --status-only") {
			t.Fatalf("expected run flag summary for %v, got %q", args, output)
		}
		if stderr.Len() != 0 {
			t.Fatalf("expected no stderr output for %v, got %q", args, stderr.String())
		}
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
	if got := strings.TrimSpace(stdout.String()); got != bodyPath {
		t.Fatalf("expected stdout to contain only output path, got %q", got)
	}
	if strings.Contains(stdout.String(), "Body:") {
		t.Fatalf("expected response body to be hidden, got %q", stdout.String())
	}
}

func TestRunCommandStatusOnlyPrintsStatusCode(t *testing.T) {
	dir := t.TempDir()
	if err := config.SavePath(filepath.Join(dir, config.FileName), config.Default()); err != nil {
		t.Fatalf("save config: %v", err)
	}

	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	app := &App{
		stdout: stdout,
		stderr: stderr,
		cwd:    dir,
		execute: func(ctx context.Context, request *executor.ResolvedRequest) (*executor.Response, error) {
			return &executor.Response{
				Status:      "204 No Content",
				StatusCode:  204,
				Header:      map[string][]string{"Content-Type": {"application/json"}},
				Body:        []byte(`{"ok":true}`),
				ContentType: "application/json",
			}, nil
		},
	}

	code := app.Run([]string{"run", "default/users", "--status-only"})
	if code != 0 {
		t.Fatalf("expected success, got %d: %s", code, stderr.String())
	}

	if got := strings.TrimSpace(stdout.String()); got != "204" {
		t.Fatalf("expected status code only, got %q", got)
	}
	if strings.Contains(stdout.String(), "Body:") {
		t.Fatalf("expected body to be hidden, got %q", stdout.String())
	}
}

func TestRunCommandOutputSuppressesStatusOnlyTerminalOutput(t *testing.T) {
	dir := t.TempDir()
	if err := config.SavePath(filepath.Join(dir, config.FileName), config.Default()); err != nil {
		t.Fatalf("save config: %v", err)
	}

	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	bodyPath := filepath.Join(dir, "response.txt")
	app := &App{
		stdout: stdout,
		stderr: stderr,
		cwd:    dir,
		execute: func(ctx context.Context, request *executor.ResolvedRequest) (*executor.Response, error) {
			return &executor.Response{
				Status:      "204 No Content",
				StatusCode:  204,
				Header:      map[string][]string{"Content-Type": {"application/json"}},
				Body:        []byte(`{"ok":true}`),
				ContentType: "application/json",
			}, nil
		},
	}

	code := app.Run([]string{"run", "default/users", "--status-only", "--output", bodyPath})
	if code != 0 {
		t.Fatalf("expected success, got %d: %s", code, stderr.String())
	}

	if got := strings.TrimSpace(stdout.String()); got != bodyPath {
		t.Fatalf("expected stdout to contain only output path, got %q", got)
	}

	data, err := os.ReadFile(bodyPath)
	if err != nil {
		t.Fatalf("read output body: %v", err)
	}
	if string(data) != `{"ok":true}` {
		t.Fatalf("unexpected output file contents %q", string(data))
	}
}

func TestRunCommandShortFlagsWork(t *testing.T) {
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
		"r", "default/users",
		"-e", "staging",
		"-H", "X-Debug: 1",
		"-q", "page=2",
		"-b", `{"override":true}`,
		"-t", "5s",
		"-o", bodyPath,
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
	if got := strings.TrimSpace(stdout.String()); got != bodyPath {
		t.Fatalf("expected stdout to contain only output path, got %q", got)
	}
}

func TestRunCommandAllPrintsStatusesAndErrorsWithoutFailing(t *testing.T) {
	dir := t.TempDir()
	file := &config.File{
		Version:   config.ConfigVersionV1,
		ActiveEnv: "local",
		Environments: map[string]map[string]string{
			"local": {
				"BASE_URL": "https://example.com",
			},
		},
		Collections: map[string]*config.Collection{
			"default": {
				Requests: map[string]*config.RequestDef{
					"health": {
						Method: "GET",
						URL:    "${BASE_URL}/health",
					},
					"users": {
						Method: "GET",
						URL:    "${BASE_URL}/users",
					},
					"broken": {
						Method: "GET",
						URL:    "${MISSING_URL}",
					},
				},
			},
		},
	}
	if err := config.SavePath(filepath.Join(dir, config.FileName), file); err != nil {
		t.Fatalf("save config: %v", err)
	}

	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	callCount := 0
	app := &App{
		stdout: stdout,
		stderr: stderr,
		cwd:    dir,
		execute: func(ctx context.Context, request *executor.ResolvedRequest) (*executor.Response, error) {
			callCount++
			if strings.HasSuffix(request.URL, "/users") {
				return nil, errors.New("dial tcp timeout")
			}
			return &executor.Response{
				Status:     "200 OK",
				StatusCode: 200,
			}, nil
		},
	}

	code := app.Run([]string{"run", "all"})
	if code != 0 {
		t.Fatalf("expected success, got %d: %s", code, stderr.String())
	}
	if callCount != 2 {
		t.Fatalf("expected 2 execute calls, got %d", callCount)
	}

	output := stdout.String()
	if !strings.Contains(output, "ENDPOINT") || !strings.Contains(output, "RESULT") {
		t.Fatalf("expected table headers, got %q", output)
	}
	if !strings.Contains(output, "default/health") || !strings.Contains(output, "200") {
		t.Fatalf("expected successful endpoint row, got %q", output)
	}
	if !strings.Contains(output, "default/users") || !strings.Contains(output, "error: dial tcp timeout") {
		t.Fatalf("expected execution error row, got %q", output)
	}
	if !strings.Contains(output, "default/broken") || !strings.Contains(output, "error:") {
		t.Fatalf("expected preparation error row, got %q", output)
	}
	if stderr.Len() != 0 {
		t.Fatalf("expected no stderr output, got %q", stderr.String())
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
