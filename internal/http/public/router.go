package public

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"anymanager/internal/proxy"
	"anymanager/internal/security"
	"anymanager/internal/store"
)

type Server struct {
	repo      *store.Repository
	hasher    *security.Hasher
	forwarder *proxy.Forwarder
}

func NewServer(repo *store.Repository, hasher *security.Hasher, forwarder *proxy.Forwarder) *Server {
	return &Server{repo: repo, hasher: hasher, forwarder: forwarder}
}

func (s *Server) Router() http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.RealIP)
	r.Use(middleware.Recoverer)
	r.Post("/v1/messages", s.handleMessages)
	r.Get("/v1/models", s.handleModels)
	return r
}

func (s *Server) handleMessages(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		s.logFailure(r.Context(), store.RequestLog{
			RequestTS:     time.Now().UTC(),
			Route:         "/v1/messages",
			Method:        r.Method,
			StatusCode:    http.StatusBadRequest,
			Success:       false,
			FailureReason: "failed to read request body",
			ClientIP:      clientIP(r),
			UserAgent:     r.UserAgent(),
		})
		writeJSONError(w, http.StatusBadRequest, "failed to read request body")
		return
	}
	model := extractModel(body)
	if ok := s.authenticate(w, r, "/v1/messages", model); !ok {
		return
	}
	r.Body = io.NopCloser(bytes.NewReader(body))
	s.forwarder.Proxy(w, r, "/v1/messages", body, model)
}

func (s *Server) handleModels(w http.ResponseWriter, r *http.Request) {
	if ok := s.authenticate(w, r, "/v1/models", ""); !ok {
		return
	}
	s.forwarder.Proxy(w, r, "/v1/models", nil, "")
}

func (s *Server) authenticate(w http.ResponseWriter, r *http.Request, route, model string) bool {
	config, err := s.repo.GetAppConfig(r.Context())
	if err != nil {
		s.logFailure(r.Context(), baseFailureLog(r, route, model, http.StatusInternalServerError, "failed to load app configuration"))
		writeJSONError(w, http.StatusInternalServerError, "failed to load app configuration")
		return false
	}
	if config.DownstreamKeyHash == "" {
		s.logFailure(r.Context(), baseFailureLog(r, route, model, http.StatusServiceUnavailable, "downstream API key not configured"))
		writeJSONError(w, http.StatusServiceUnavailable, "downstream API key not configured")
		return false
	}
	providedKey := extractDownstreamKey(r)
	if providedKey == "" {
		s.logFailure(r.Context(), baseFailureLog(r, route, model, http.StatusUnauthorized, "missing downstream API key"))
		writeJSONError(w, http.StatusUnauthorized, "missing downstream API key")
		return false
	}
	valid, err := s.hasher.Verify(providedKey, config.DownstreamKeyHash)
	if err != nil {
		s.logFailure(r.Context(), baseFailureLog(r, route, model, http.StatusInternalServerError, "failed to verify downstream API key"))
		writeJSONError(w, http.StatusInternalServerError, "failed to verify downstream API key")
		return false
	}
	if !valid {
		s.logFailure(r.Context(), baseFailureLog(r, route, model, http.StatusUnauthorized, "invalid downstream API key"))
		writeJSONError(w, http.StatusUnauthorized, "invalid downstream API key")
		return false
	}
	return true
}

func (s *Server) logFailure(ctx context.Context, logEntry store.RequestLog) {
	_ = s.repo.InsertRequestLog(ctx, logEntry)
}

func baseFailureLog(r *http.Request, route, model string, statusCode int, reason string) store.RequestLog {
	return store.RequestLog{
		RequestTS:     time.Now().UTC(),
		Route:         route,
		Method:        r.Method,
		Model:         model,
		StatusCode:    statusCode,
		Success:       false,
		FailureReason: reason,
		ClientIP:      clientIP(r),
		UserAgent:     r.UserAgent(),
	}
}

func extractDownstreamKey(r *http.Request) string {
	if value := strings.TrimSpace(r.Header.Get("x-api-key")); value != "" {
		return value
	}
	if value := strings.TrimSpace(r.Header.Get("Authorization")); strings.HasPrefix(strings.ToLower(value), "bearer ") {
		return strings.TrimSpace(value[7:])
	}
	return ""
}

func extractModel(body []byte) string {
	var payload struct {
		Model string `json:"model"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return ""
	}
	return strings.TrimSpace(payload.Model)
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

func clientIP(r *http.Request) string {
	if forwarded := strings.TrimSpace(r.Header.Get("X-Forwarded-For")); forwarded != "" {
		parts := strings.Split(forwarded, ",")
		return strings.TrimSpace(parts[0])
	}
	if value := strings.TrimSpace(r.Header.Get("X-Real-IP")); value != "" {
		return value
	}
	return strings.TrimSpace(r.RemoteAddr)
}
