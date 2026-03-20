package format

import (
	"strings"
	"testing"
	"time"

	"curler/internal/executor"
)

func TestRenderResponsePrettyPrintsJSON(t *testing.T) {
	output := RenderResponse(&executor.Response{
		Status:      "200 OK",
		StatusCode:  200,
		Header:      map[string][]string{"Content-Type": {"application/json"}},
		Body:        []byte(`{"ok":true,"items":[1,2]}`),
		Duration:    1250 * time.Millisecond,
		ContentType: "application/json",
	})

	if !strings.Contains(output, "\"ok\": true") {
		t.Fatalf("expected pretty JSON output, got %s", output)
	}
	if !strings.Contains(output, "Duration: 1.25s") {
		t.Fatalf("expected rounded duration, got %s", output)
	}
}

func TestRenderResponseOmitsBinaryBody(t *testing.T) {
	output := RenderResponse(&executor.Response{
		Status:      "200 OK",
		StatusCode:  200,
		Header:      map[string][]string{"Content-Type": {"application/octet-stream"}},
		Body:        []byte{0x00, 0x01, 0x02},
		Duration:    time.Millisecond,
		ContentType: "application/octet-stream",
		IsBinary:    true,
	})

	if !strings.Contains(output, "binary body omitted") {
		t.Fatalf("expected binary placeholder, got %s", output)
	}
}
