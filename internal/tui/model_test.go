package tui

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"postack/internal/config"
	"postack/internal/executor"
)

func TestSummaryIsReadOnlyAndUsesYAMLSource(t *testing.T) {
	path := filepath.Join(t.TempDir(), config.FileName)
	m := newModel(path, config.Default(), func(ctx context.Context, request *executor.ResolvedRequest) (*executor.Response, error) {
		return &executor.Response{}, nil
	})

	summary := m.renderSummary()
	if !strings.Contains(summary, "Summary (read-only)") {
		t.Fatalf("expected read-only summary, got %q", summary)
	}
	if !strings.Contains(summary, "Edit in YAML: "+path) {
		t.Fatalf("expected yaml path in summary, got %q", summary)
	}
	if !strings.Contains(summary, "Selected: default/users") {
		t.Fatalf("expected selected request in summary, got %q", summary)
	}
}

func TestRunSelectedUsesSavedRequestAndRendersResponse(t *testing.T) {
	path := filepath.Join(t.TempDir(), config.FileName)
	file := config.Default()

	var captured *executor.ResolvedRequest
	m := newModel(path, file, func(ctx context.Context, request *executor.ResolvedRequest) (*executor.Response, error) {
		captured = request
		return &executor.Response{
			Status:      "200 OK",
			StatusCode:  200,
			Body:        []byte(`{"ok":true}`),
			ContentType: "application/json",
		}, nil
	})

	cmd, err := m.runSelected()
	if err != nil {
		t.Fatalf("run selected: %v", err)
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
		t.Fatal("expected selected request to be executed")
	}
	if captured.Method != "GET" {
		t.Fatalf("expected GET, got %s", captured.Method)
	}
	if !strings.Contains(captured.URL, "/users") {
		t.Fatalf("expected request URL to include /users, got %s", captured.URL)
	}
	if !strings.Contains(result.rendered, "\"ok\": true") {
		t.Fatalf("expected formatted response, got %s", result.rendered)
	}
}

func TestResponsePaneScrollsWhenFocused(t *testing.T) {
	m := newModel(filepath.Join(t.TempDir(), config.FileName), config.Default(), func(ctx context.Context, request *executor.ResolvedRequest) (*executor.Response, error) {
		return &executor.Response{}, nil
	})

	m.width = 120
	m.height = 40
	m.resize()
	m.focus = paneResponse

	lines := make([]string, 0, 80)
	for i := 0; i < 80; i++ {
		lines = append(lines, "line "+strings.Repeat("x", 10))
	}
	m.resetResponse(strings.Join(lines, "\n"))

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyDown})
	scrolled := updated.(*model)
	if scrolled.responseView.YOffset == 0 {
		t.Fatal("expected response viewport to scroll down")
	}
}

func TestSelectionChangeResetsStaleResponse(t *testing.T) {
	file := config.Default()
	file.UpsertRequest("default", "health", &config.RequestDef{
		Method: "GET",
		URL:    "${BASE_URL}/health",
	})

	m := newModel(filepath.Join(t.TempDir(), config.FileName), file, func(ctx context.Context, request *executor.ResolvedRequest) (*executor.Response, error) {
		return &executor.Response{}, nil
	})
	m.width = 120
	m.height = 40
	m.resize()

	m.resetResponse("old response")
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyDown})
	next := updated.(*model)

	if next.currentRef() != "default/users" {
		t.Fatalf("expected current ref to move to default/users, got %q", next.currentRef())
	}

	updated, _ = next.Update(tea.KeyMsg{Type: tea.KeyUp})
	next = updated.(*model)
	if !strings.Contains(next.responseView.View(), "Selected") {
		t.Fatalf("expected placeholder response after selection changes, got %q", next.responseView.View())
	}
}

func TestEnterRunsRequestFromListFocus(t *testing.T) {
	m := newModel(filepath.Join(t.TempDir(), config.FileName), config.Default(), func(ctx context.Context, request *executor.ResolvedRequest) (*executor.Response, error) {
		return &executor.Response{
			Status:      "200 OK",
			StatusCode:  200,
			Body:        []byte(`{"ok":true}`),
			ContentType: "application/json",
		}, nil
	})
	m.width = 120
	m.height = 40
	m.resize()

	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	running := updated.(*model)
	if !running.busy {
		t.Fatal("expected enter to start the request")
	}

	msg := cmd()
	finalModel, _ := running.Update(msg)
	done := finalModel.(*model)
	if done.focus != paneResponse {
		t.Fatal("expected response pane to be focused after a run")
	}
	if !strings.Contains(done.responseView.View(), "\"ok\": true") {
		t.Fatalf("expected rendered response in viewport, got %q", done.responseView.View())
	}
}
