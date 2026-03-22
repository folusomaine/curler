package format

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"postack/internal/executor"
)

func RenderResponse(response *executor.Response) string {
	var builder strings.Builder

	builder.WriteString(fmt.Sprintf("Status: %s\n", response.Status))
	builder.WriteString(fmt.Sprintf("Duration: %s\n", roundDuration(response.Duration)))
	builder.WriteString(fmt.Sprintf("Size: %s\n", humanizeBytes(len(response.Body))))

	if len(response.Header) > 0 {
		builder.WriteString("\nHeaders:\n")
		headerKeys := make([]string, 0, len(response.Header))
		for key := range response.Header {
			headerKeys = append(headerKeys, key)
		}
		sort.Strings(headerKeys)
		for _, key := range headerKeys {
			builder.WriteString(fmt.Sprintf("%s: %s\n", key, strings.Join(response.Header.Values(key), ", ")))
		}
	}

	builder.WriteString("\nBody:\n")
	builder.WriteString(renderBody(response))
	return strings.TrimSpace(builder.String())
}

func renderBody(response *executor.Response) string {
	if len(response.Body) == 0 {
		return "(empty body)"
	}
	if response.IsBinary {
		return fmt.Sprintf("[binary body omitted; use --output to save %s]", humanizeBytes(len(response.Body)))
	}
	if looksLikeJSON(response.ContentType, response.Body) {
		var pretty bytes.Buffer
		if err := json.Indent(&pretty, response.Body, "", "  "); err == nil {
			return pretty.String()
		}
	}
	return string(response.Body)
}

func roundDuration(value time.Duration) time.Duration {
	if value < time.Millisecond {
		return value
	}
	return value.Round(time.Millisecond)
}

func looksLikeJSON(contentType string, body []byte) bool {
	if strings.Contains(contentType, "json") {
		return true
	}
	trimmed := bytes.TrimSpace(body)
	if len(trimmed) == 0 {
		return false
	}
	return trimmed[0] == '{' || trimmed[0] == '['
}

func humanizeBytes(size int) string {
	const unit = 1024
	if size < unit {
		return fmt.Sprintf("%d B", size)
	}
	div, exp := unit, 0
	for n := size / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(size)/float64(div), "KMGTPE"[exp])
}
