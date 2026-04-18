package proxy

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"strings"
	"time"

	"anymanager/internal/store"
	"anymanager/internal/upstream"
)

type Forwarder struct {
	repo      *store.Repository
	upstreams *upstream.Service
	clients   *ClientFactory
}

type ProxyResult struct {
	StatusCode    int
	Success       bool
	FailureReason string
	RequestID     string
	InputTokens   int
	OutputTokens  int
	TotalTokens   int
	UpstreamID    *int64
	UpstreamAlias string
}

func NewForwarder(repo *store.Repository, upstreams *upstream.Service, clients *ClientFactory) *Forwarder {
	return &Forwarder{repo: repo, upstreams: upstreams, clients: clients}
}

func (f *Forwarder) Proxy(w http.ResponseWriter, r *http.Request, route string, requestBody []byte, model string) {
	start := time.Now()
	logEntry := store.RequestLog{
		RequestTS: time.Now().UTC(),
		Route:     route,
		Method:    r.Method,
		Model:     model,
		ClientIP:  clientIP(r),
		UserAgent: r.UserAgent(),
	}
	defer func() {
		logEntry.LatencyMS = time.Since(start).Milliseconds()
		_ = f.repo.InsertRequestLog(context.Background(), logEntry)
	}()

	candidate, err := f.upstreams.Select(r.Context())
	if err != nil {
		statusCode := http.StatusInternalServerError
		failureReason := "upstream_selection_failed"
		message := "failed to select upstream API key"
		if errors.Is(err, upstream.ErrNoCandidate) {
			statusCode = http.StatusServiceUnavailable
			failureReason = "no_available_upstream"
			message = "no available upstream API keys"
		}
		logEntry.StatusCode = statusCode
		logEntry.Success = false
		logEntry.FailureReason = failureReason
		writeJSONError(w, statusCode, message)
		return
	}
	logEntry.UpstreamKeyID = &candidate.Key.ID
	logEntry.UpstreamAlias = candidate.Key.Alias

	client, err := f.clients.NewClient(candidate.Config.OutboundProxyURL)
	if err != nil {
		logEntry.StatusCode = http.StatusInternalServerError
		logEntry.Success = false
		logEntry.FailureReason = "invalid_outbound_proxy"
		writeJSONError(w, http.StatusInternalServerError, "invalid outbound proxy configuration")
		return
	}
	upstreamURL := *candidate.BaseURL
	upstreamURL.Path = joinUpstreamPath(candidate.BaseURL.Path, route)
	upstreamURL.RawQuery = r.URL.RawQuery

	var bodyReader io.Reader
	if requestBody != nil {
		bodyReader = bytes.NewReader(requestBody)
	}
	upstreamRequest, err := http.NewRequestWithContext(r.Context(), r.Method, upstreamURL.String(), bodyReader)
	if err != nil {
		logEntry.StatusCode = http.StatusInternalServerError
		logEntry.Success = false
		logEntry.FailureReason = "build_upstream_request"
		writeJSONError(w, http.StatusInternalServerError, "failed to build upstream request")
		return
	}
	CopyHeaders(upstreamRequest.Header, r.Header)
	StripAuthHeaders(upstreamRequest.Header)
	applyUpstreamAuth(upstreamRequest.Header, candidate.Config.UpstreamAuthMode, candidate.APIKey)

	response, err := client.Do(upstreamRequest)
	if err != nil {
		statusCode := mapProxyErrorStatus(err)
		logEntry.StatusCode = statusCode
		logEntry.Success = false
		logEntry.FailureReason = summarizeError(err)
		_, _ = f.upstreams.MarkResult(context.Background(), candidate.Key.ID, false, logEntry.FailureReason)
		writeJSONError(w, statusCode, "upstream request failed")
		return
	}
	defer response.Body.Close()

	CopyHeaders(w.Header(), response.Header)
	logEntry.StatusCode = response.StatusCode
	logEntry.RequestID = firstHeader(response.Header, "request-id", "x-request-id")
	isStream := strings.Contains(strings.ToLower(response.Header.Get("Content-Type")), "text/event-stream")
	if isStream {
		w.WriteHeader(response.StatusCode)
		_, copyErr := io.Copy(flushWriter{ResponseWriter: w}, response.Body)
		if copyErr != nil {
			logEntry.Success = false
			logEntry.FailureReason = summarizeError(copyErr)
			_, _ = f.upstreams.MarkResult(context.Background(), candidate.Key.ID, false, logEntry.FailureReason)
			return
		}
		logEntry.Success = response.StatusCode >= 200 && response.StatusCode < 300
		if !logEntry.Success {
			logEntry.FailureReason = response.Status
		}
		_, _ = f.upstreams.MarkResult(context.Background(), candidate.Key.ID, logEntry.Success, logEntry.FailureReason)
		return
	}

	body, readErr := io.ReadAll(response.Body)
	if readErr != nil {
		logEntry.Success = false
		logEntry.FailureReason = summarizeError(readErr)
		_, _ = f.upstreams.MarkResult(context.Background(), candidate.Key.ID, false, logEntry.FailureReason)
		writeJSONError(w, http.StatusBadGateway, "failed to read upstream response")
		return
	}
	if response.StatusCode >= 200 && response.StatusCode < 300 && route == "/v1/messages" {
		logEntry.InputTokens, logEntry.OutputTokens, logEntry.TotalTokens = extractUsageTokens(body)
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		logEntry.FailureReason = summarizeBody(body)
	}
	logEntry.Success = response.StatusCode >= 200 && response.StatusCode < 300
	_, _ = f.upstreams.MarkResult(context.Background(), candidate.Key.ID, logEntry.Success, logEntry.FailureReason)
	w.WriteHeader(response.StatusCode)
	_, _ = w.Write(body)
}

func joinUpstreamPath(basePath, route string) string {
	basePath = strings.TrimSuffix(basePath, "/")
	if basePath == "" {
		return route
	}
	return basePath + route
}

func applyUpstreamAuth(headers http.Header, mode, apiKey string) {
	switch mode {
	case "x_api_key":
		headers.Set("x-api-key", apiKey)
	default:
		headers.Set("Authorization", "Bearer "+apiKey)
	}
}

func mapProxyErrorStatus(err error) int {
	if errors.Is(err, context.DeadlineExceeded) {
		return http.StatusGatewayTimeout
	}
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return http.StatusGatewayTimeout
	}
	return http.StatusBadGateway
}

func summarizeError(err error) string {
	text := strings.TrimSpace(err.Error())
	if len(text) <= 256 {
		return text
	}
	return text[:256]
}

func writeJSONError(w http.ResponseWriter, statusCode int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"error": map[string]any{
			"type":    http.StatusText(statusCode),
			"message": message,
		},
	})
}

func firstHeader(headers http.Header, names ...string) string {
	for _, name := range names {
		if value := headers.Get(name); value != "" {
			return value
		}
	}
	return ""
}

func extractUsageTokens(body []byte) (int, int, int) {
	var payload struct {
		Usage struct {
			InputTokens  int `json:"input_tokens"`
			OutputTokens int `json:"output_tokens"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return 0, 0, 0
	}
	return payload.Usage.InputTokens, payload.Usage.OutputTokens, payload.Usage.InputTokens + payload.Usage.OutputTokens
}

func summarizeBody(body []byte) string {
	trimmed := strings.TrimSpace(string(body))
	if trimmed == "" {
		return ""
	}
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err == nil {
		if errorValue, ok := payload["error"].(map[string]any); ok {
			if message, ok := errorValue["message"].(string); ok && message != "" {
				trimmed = message
			}
		}
	}
	if len(trimmed) <= 256 {
		return trimmed
	}
	return trimmed[:256]
}

type flushWriter struct {
	http.ResponseWriter
}

func (w flushWriter) Write(p []byte) (int, error) {
	n, err := w.ResponseWriter.Write(p)
	if flusher, ok := w.ResponseWriter.(http.Flusher); ok {
		flusher.Flush()
	}
	return n, err
}

func clientIP(r *http.Request) string {
	if forwarded := r.Header.Get("X-Forwarded-For"); forwarded != "" {
		parts := strings.Split(forwarded, ",")
		return strings.TrimSpace(parts[0])
	}
	if value := r.Header.Get("X-Real-IP"); value != "" {
		return strings.TrimSpace(value)
	}
	host, _, err := net.SplitHostPort(strings.TrimSpace(r.RemoteAddr))
	if err == nil {
		return host
	}
	return strings.TrimSpace(r.RemoteAddr)
}
