package executor

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"postack/internal/config"
)

const defaultTimeout = 30 * time.Second

type OverrideOptions struct {
	Headers map[string]string
	Query   map[string]string
	Body    *string
	Timeout time.Duration
}

type ResolvedRequest struct {
	Method       string
	URL          string
	Headers      map[string]string
	Auth         config.AuthConfig
	Body         config.BodyConfig
	Timeout      time.Duration
	Environment  map[string]string
	RequestLabel string
}

type Response struct {
	Status      string
	StatusCode  int
	Header      http.Header
	Body        []byte
	Duration    time.Duration
	ContentType string
	IsBinary    bool
}

func PrepareRequest(def *config.RequestDef, env map[string]string, overrides OverrideOptions) (*ResolvedRequest, error) {
	work := config.CloneRequest(def)

	for key, value := range overrides.Headers {
		work.Headers[http.CanonicalHeaderKey(strings.TrimSpace(key))] = value
	}
	for key, value := range overrides.Query {
		work.Query[strings.TrimSpace(key)] = value
	}
	if overrides.Body != nil {
		work.Body.Content = *overrides.Body
		if work.Body.Mode == "" || work.Body.Mode == config.BodyModeNone {
			work.Body.Mode = config.BodyModeRaw
		}
	}
	if overrides.Timeout > 0 {
		work.Timeout = overrides.Timeout.String()
	}

	if err := config.ValidateAuthType(work.Auth.Type); err != nil {
		return nil, err
	}
	if err := config.ValidateBodyMode(work.Body.Mode); err != nil {
		return nil, err
	}

	method, err := config.InterpolateString(strings.ToUpper(strings.TrimSpace(work.Method)), env)
	if err != nil {
		return nil, err
	}
	rawURL, err := config.InterpolateString(strings.TrimSpace(work.URL), env)
	if err != nil {
		return nil, err
	}
	if rawURL == "" {
		return nil, fmt.Errorf("request url is required")
	}
	parsedURL, err := url.Parse(rawURL)
	if err != nil {
		return nil, err
	}
	queryValues := parsedURL.Query()
	keys := make([]string, 0, len(work.Query))
	for key := range work.Query {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		resolvedKey, err := config.InterpolateString(key, env)
		if err != nil {
			return nil, err
		}
		resolvedValue, err := config.InterpolateString(work.Query[key], env)
		if err != nil {
			return nil, err
		}
		queryValues.Set(resolvedKey, resolvedValue)
	}
	parsedURL.RawQuery = queryValues.Encode()

	headers := make(map[string]string, len(work.Headers))
	for key, value := range work.Headers {
		resolvedKey, err := config.InterpolateString(key, env)
		if err != nil {
			return nil, err
		}
		resolvedValue, err := config.InterpolateString(value, env)
		if err != nil {
			return nil, err
		}
		headers[http.CanonicalHeaderKey(resolvedKey)] = resolvedValue
	}

	auth := work.Auth
	auth.Type = strings.ToLower(strings.TrimSpace(auth.Type))
	auth.Username, err = config.InterpolateString(auth.Username, env)
	if err != nil {
		return nil, err
	}
	auth.Password, err = config.InterpolateString(auth.Password, env)
	if err != nil {
		return nil, err
	}
	auth.Token, err = config.InterpolateString(auth.Token, env)
	if err != nil {
		return nil, err
	}

	body := work.Body
	body.Mode = strings.ToLower(strings.TrimSpace(body.Mode))
	body.Content, err = config.InterpolateString(body.Content, env)
	if err != nil {
		return nil, err
	}

	timeout := defaultTimeout
	if strings.TrimSpace(work.Timeout) != "" {
		timeoutValue, err := config.InterpolateString(work.Timeout, env)
		if err != nil {
			return nil, err
		}
		timeout, err = time.ParseDuration(timeoutValue)
		if err != nil {
			return nil, fmt.Errorf("invalid timeout %q: %w", timeoutValue, err)
		}
	}

	return &ResolvedRequest{
		Method:      method,
		URL:         parsedURL.String(),
		Headers:     headers,
		Auth:        auth,
		Body:        body,
		Timeout:     timeout,
		Environment: cloneMap(env),
	}, nil
}

func Execute(ctx context.Context, request *ResolvedRequest) (*Response, error) {
	var body io.Reader
	switch request.Body.Mode {
	case config.BodyModeNone:
		body = nil
	case config.BodyModeJSON, config.BodyModeRaw:
		body = bytes.NewBufferString(request.Body.Content)
	default:
		return nil, fmt.Errorf("unsupported body mode %q", request.Body.Mode)
	}

	httpRequest, err := http.NewRequestWithContext(ctx, request.Method, request.URL, body)
	if err != nil {
		return nil, err
	}

	for key, value := range request.Headers {
		httpRequest.Header.Set(key, value)
	}
	switch request.Auth.Type {
	case config.AuthTypeBearer:
		if httpRequest.Header.Get("Authorization") == "" {
			httpRequest.Header.Set("Authorization", "Bearer "+request.Auth.Token)
		}
	case config.AuthTypeBasic:
		if httpRequest.Header.Get("Authorization") == "" {
			httpRequest.SetBasicAuth(request.Auth.Username, request.Auth.Password)
		}
	}
	if request.Body.Mode == config.BodyModeJSON && httpRequest.Header.Get("Content-Type") == "" {
		httpRequest.Header.Set("Content-Type", "application/json")
	}

	client := &http.Client{Timeout: request.Timeout}
	start := time.Now()
	httpResponse, err := client.Do(httpRequest)
	if err != nil {
		return nil, err
	}
	defer httpResponse.Body.Close()

	responseBody, err := io.ReadAll(httpResponse.Body)
	if err != nil {
		return nil, err
	}

	contentType := httpResponse.Header.Get("Content-Type")
	return &Response{
		Status:      httpResponse.Status,
		StatusCode:  httpResponse.StatusCode,
		Header:      httpResponse.Header.Clone(),
		Body:        responseBody,
		Duration:    time.Since(start),
		ContentType: contentType,
		IsBinary:    isBinaryBody(contentType, responseBody),
	}, nil
}

func isBinaryBody(contentType string, body []byte) bool {
	if len(body) == 0 {
		return false
	}
	if isTextContentType(contentType) {
		return false
	}
	return !utf8.Valid(body)
}

func isTextContentType(contentType string) bool {
	if contentType == "" {
		return false
	}
	mediaType, _, err := mime.ParseMediaType(contentType)
	if err != nil {
		mediaType = contentType
	}
	if strings.HasPrefix(mediaType, "text/") {
		return true
	}
	if strings.HasSuffix(mediaType, "+json") || strings.HasSuffix(mediaType, "+xml") {
		return true
	}
	switch mediaType {
	case "application/json", "application/xml", "application/javascript", "application/x-www-form-urlencoded":
		return true
	default:
		return false
	}
}

func cloneMap(values map[string]string) map[string]string {
	cloned := make(map[string]string, len(values))
	for key, value := range values {
		cloned[key] = value
	}
	return cloned
}
