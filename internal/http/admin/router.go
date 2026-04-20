package admin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"anymanager/internal/auth"
	"anymanager/internal/config"
	"anymanager/internal/http/admin/web"
	"anymanager/internal/metrics"
	"anymanager/internal/proxy"
	"anymanager/internal/security"
	"anymanager/internal/store"
	"anymanager/internal/upstream"
)

type Server struct {
	repo      *store.Repository
	hasher    *security.Hasher
	sessions  *auth.SessionManager
	upstreams *upstream.Service
	metrics   *metrics.Service
	renderer  *web.Renderer
}

type modelRedirectInput struct {
	DownstreamModel string `json:"downstream_model"`
	UpstreamModel   string `json:"upstream_model"`
}

func NewServer(repo *store.Repository, hasher *security.Hasher, sessions *auth.SessionManager, upstreams *upstream.Service, metrics *metrics.Service) (*Server, error) {
	renderer, err := web.New()
	if err != nil {
		return nil, err
	}
	return &Server{repo: repo, hasher: hasher, sessions: sessions, upstreams: upstreams, metrics: metrics, renderer: renderer}, nil
}

func (s *Server) Router() http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.RealIP)
	r.Use(middleware.Recoverer)
	r.Get("/admin/health", s.handleHealth)
	r.Get("/admin/api/bootstrap-status", s.handleBootstrapStatus)
	r.Post("/admin/api/bootstrap", s.handleBootstrap)
	r.Post("/admin/api/login", s.handleLogin)
	r.Post("/admin/api/logout", s.handleLogout)

	r.Get("/admin", s.handleLoginPage)
	r.Mount("/admin/static/", s.renderer.StaticHandler())

	r.Group(func(pages chi.Router) {
		pages.Use(s.requireAuthPage)
		pages.Get("/admin/dashboard", s.handleDashboardPage)
		pages.Get("/admin/upstreams", s.handleUpstreamsPage)
		pages.Get("/admin/settings", s.handleSettingsPage)
		pages.Get("/admin/logs", s.handleLogsPage)
	})

	r.Group(func(protected chi.Router) {
		protected.Use(s.requireAuth)
		protected.Get("/admin/api/config", s.handleGetConfig)
		protected.Put("/admin/api/config", s.handleUpdateConfig)
		protected.Post("/admin/api/downstream/reset", s.handleResetDownstream)
		protected.Get("/admin/api/upstreams", s.handleListUpstreams)
		protected.Post("/admin/api/upstreams", s.handleCreateUpstream)
		protected.Post("/admin/api/upstreams/reorder", s.handleReorderUpstreams)
		protected.Put("/admin/api/upstreams/{id}", s.handleUpdateUpstream)
		protected.Delete("/admin/api/upstreams/{id}", s.handleDeleteUpstream)
		protected.Post("/admin/api/upstreams/{id}/enable", s.handleEnableUpstream)
		protected.Post("/admin/api/upstreams/{id}/disable", s.handleDisableUpstream)
		protected.Post("/admin/api/upstreams/{id}/refresh-balance", s.handleRefreshBalance)
		protected.Get("/admin/api/summary", s.handleSummary)
		protected.Get("/admin/api/logs", s.handleLogs)
	})
	return r
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) handleBootstrapStatus(w http.ResponseWriter, r *http.Request) {
	appConfig, err := s.repo.GetAppConfig(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load configuration")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"initialized": appConfig.DownstreamKeyHash != "",
	})
}

func (s *Server) handleBootstrap(w http.ResponseWriter, r *http.Request) {
	var request struct {
		DownstreamAPIKey string `json:"downstream_api_key"`
	}
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	request.DownstreamAPIKey = strings.TrimSpace(request.DownstreamAPIKey)
	if request.DownstreamAPIKey == "" {
		writeError(w, http.StatusBadRequest, "downstream_api_key is required")
		return
	}
	hash, err := s.hasher.Hash(request.DownstreamAPIKey)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to hash downstream API key")
		return
	}
	if err := s.repo.BootstrapDownstreamKey(r.Context(), hash); err != nil {
		if errors.Is(err, store.ErrAlreadyInitialized) {
			writeError(w, http.StatusConflict, "downstream API key already initialized")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to bootstrap downstream API key")
		return
	}
	if err := s.sessions.SetCookie(w, 1, time.Now().UTC()); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to issue admin session")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"ok": true})
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	var request struct {
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	appConfig, err := s.repo.GetAppConfig(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load configuration")
		return
	}
	if appConfig.DownstreamKeyHash == "" {
		writeError(w, http.StatusConflict, "downstream API key not initialized")
		return
	}
	valid, err := s.hasher.Verify(strings.TrimSpace(request.Password), appConfig.DownstreamKeyHash)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to verify password")
		return
	}
	if !valid {
		writeError(w, http.StatusUnauthorized, "invalid password")
		return
	}
	if err := s.sessions.SetCookie(w, appConfig.AuthVersion, time.Now().UTC()); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to issue admin session")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	s.sessions.ClearCookie(w)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) handleGetConfig(w http.ResponseWriter, r *http.Request) {
	appConfig, err := s.repo.GetAppConfig(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load configuration")
		return
	}
	configPayload, err := s.configPayload(r.Context(), appConfig)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load model redirects")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"has_downstream_key": appConfig.DownstreamKeyHash != "",
		"config":             configPayload,
	})
}

func (s *Server) handleUpdateConfig(w http.ResponseWriter, r *http.Request) {
	current, err := s.repo.GetAppConfig(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load configuration")
		return
	}
	var request struct {
		OutboundProxyURL  *string               `json:"outbound_proxy_url"`
		UpstreamBaseURL   *string               `json:"upstream_base_url"`
		UpstreamAuthMode  *string               `json:"upstream_auth_mode"`
		FailoverThreshold *int                  `json:"failover_threshold"`
		CooldownSeconds   *int                  `json:"cooldown_seconds"`
		ModelRedirects    *[]modelRedirectInput `json:"model_redirects"`
	}
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	var redirects []store.ModelRedirect
	if request.ModelRedirects != nil {
		redirects, err = normalizeModelRedirects(*request.ModelRedirects)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
	}
	input := store.UpdateAppConfigInput{
		OutboundProxyURL:  current.OutboundProxyURL,
		UpstreamBaseURL:   current.UpstreamBaseURL,
		UpstreamAuthMode:  current.UpstreamAuthMode,
		FailoverThreshold: current.FailoverThreshold,
		CooldownSeconds:   current.CooldownSeconds,
	}
	if request.OutboundProxyURL != nil {
		input.OutboundProxyURL = strings.TrimSpace(*request.OutboundProxyURL)
		if input.OutboundProxyURL != "" {
			if _, err := proxy.ValidateProxyURL(input.OutboundProxyURL); err != nil {
				writeError(w, http.StatusBadRequest, err.Error())
				return
			}
		}
	}
	if request.UpstreamBaseURL != nil {
		input.UpstreamBaseURL = strings.TrimSpace(*request.UpstreamBaseURL)
		parsed, err := url.Parse(input.UpstreamBaseURL)
		if err != nil || parsed.Scheme == "" || parsed.Host == "" {
			writeError(w, http.StatusBadRequest, "upstream_base_url must be a valid absolute URL")
			return
		}
	}
	if request.UpstreamAuthMode != nil {
		mode, err := config.NormalizeAuthMode(*request.UpstreamAuthMode)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		input.UpstreamAuthMode = mode
	}
	if request.FailoverThreshold != nil {
		if *request.FailoverThreshold < 1 {
			writeError(w, http.StatusBadRequest, "failover_threshold must be >= 1")
			return
		}
		input.FailoverThreshold = *request.FailoverThreshold
	}
	if request.CooldownSeconds != nil {
		if *request.CooldownSeconds < 60 {
			writeError(w, http.StatusBadRequest, "cooldown_seconds must be >= 60")
			return
		}
		input.CooldownSeconds = *request.CooldownSeconds
	}
	updated, err := s.repo.UpdateAppConfig(r.Context(), input)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update configuration")
		return
	}
	if request.ModelRedirects != nil {
		if err := s.repo.ReplaceModelRedirects(r.Context(), redirects); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to update model redirects")
			return
		}
	}
	configPayload, err := s.configPayload(r.Context(), updated)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load model redirects")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"config": configPayload})
}

func (s *Server) handleResetDownstream(w http.ResponseWriter, r *http.Request) {
	var request struct {
		NewAPIKey string `json:"new_api_key"`
	}
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	request.NewAPIKey = strings.TrimSpace(request.NewAPIKey)
	if request.NewAPIKey == "" {
		writeError(w, http.StatusBadRequest, "new_api_key is required")
		return
	}
	hash, err := s.hasher.Hash(request.NewAPIKey)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to hash downstream API key")
		return
	}
	updated, err := s.repo.UpdateDownstreamKey(r.Context(), hash)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update downstream API key")
		return
	}
	configPayload, err := s.configPayload(r.Context(), updated)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load model redirects")
		return
	}
	s.sessions.ClearCookie(w)
	writeJSON(w, http.StatusOK, map[string]any{"config": configPayload, "sessions_invalidated": true})
}

func (s *Server) handleListUpstreams(w http.ResponseWriter, r *http.Request) {
	keys, err := s.upstreams.List(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list upstream keys")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": keys})
}

func (s *Server) handleCreateUpstream(w http.ResponseWriter, r *http.Request) {
	var request struct {
		Alias     string `json:"alias"`
		APIKey    string `json:"api_key"`
		IsEnabled *bool  `json:"is_enabled"`
	}
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	request.Alias = strings.TrimSpace(request.Alias)
	request.APIKey = strings.TrimSpace(request.APIKey)
	if request.Alias == "" || request.APIKey == "" {
		writeError(w, http.StatusBadRequest, "alias and api_key are required")
		return
	}
	enabled := true
	if request.IsEnabled != nil {
		enabled = *request.IsEnabled
	}
	key, err := s.upstreams.Create(r.Context(), request.Alias, request.APIKey, enabled)
	if err != nil {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("failed to create upstream key: %v", err))
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"item": key})
}

func (s *Server) handleUpdateUpstream(w http.ResponseWriter, r *http.Request) {
	id, err := parseIDParam(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	current, err := s.repo.GetUpstreamKeyByID(r.Context(), id)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "upstream key not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to load upstream key")
		return
	}
	var request struct {
		Alias     string  `json:"alias"`
		APIKey    *string `json:"api_key"`
		IsEnabled *bool   `json:"is_enabled"`
	}
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	alias := strings.TrimSpace(request.Alias)
	if alias == "" {
		writeError(w, http.StatusBadRequest, "alias is required")
		return
	}
	enabled := current.IsEnabled
	if request.IsEnabled != nil {
		enabled = *request.IsEnabled
	}
	if request.APIKey != nil {
		trimmed := strings.TrimSpace(*request.APIKey)
		request.APIKey = &trimmed
		if trimmed == "" {
			writeError(w, http.StatusBadRequest, "api_key cannot be empty when provided")
			return
		}
	}
	updated, err := s.upstreams.Update(r.Context(), id, alias, request.APIKey, enabled)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "upstream key not found")
			return
		}
		writeError(w, http.StatusBadRequest, fmt.Sprintf("failed to update upstream key: %v", err))
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"item": updated})
}

func (s *Server) handleDeleteUpstream(w http.ResponseWriter, r *http.Request) {
	id, err := parseIDParam(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := s.upstreams.Delete(r.Context(), id); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "upstream key not found")
			return
		}
		writeError(w, http.StatusBadRequest, fmt.Sprintf("failed to delete upstream key: %v", err))
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) handleEnableUpstream(w http.ResponseWriter, r *http.Request) {
	s.handleSetEnabled(w, r, true)
}

func (s *Server) handleDisableUpstream(w http.ResponseWriter, r *http.Request) {
	s.handleSetEnabled(w, r, false)
}

func (s *Server) handleSetEnabled(w http.ResponseWriter, r *http.Request, enabled bool) {
	id, err := parseIDParam(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	updated, err := s.upstreams.SetEnabled(r.Context(), id, enabled)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "upstream key not found")
			return
		}
		writeError(w, http.StatusBadRequest, fmt.Sprintf("failed to update upstream key state: %v", err))
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"item": updated})
}

func (s *Server) handleReorderUpstreams(w http.ResponseWriter, r *http.Request) {
	var request struct {
		IDs []int64 `json:"ids"`
	}
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if len(request.IDs) == 0 {
		writeError(w, http.StatusBadRequest, "ids is required")
		return
	}
	if err := s.upstreams.Reorder(r.Context(), request.IDs); err != nil {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("failed to reorder upstream keys: %v", err))
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) handleRefreshBalance(w http.ResponseWriter, r *http.Request) {
	id, err := parseIDParam(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	updated, err := s.upstreams.RefreshBalance(r.Context(), id)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "upstream key not found")
			return
		}
		writeError(w, http.StatusBadGateway, fmt.Sprintf("failed to refresh upstream balance: %v", err))
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"item": updated})
}

func (s *Server) handleSummary(w http.ResponseWriter, r *http.Request) {
	summary, err := s.metrics.Get(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load summary")
		return
	}
	writeJSON(w, http.StatusOK, summary)
}

func (s *Server) handleLogs(w http.ResponseWriter, r *http.Request) {
	filter := store.RequestLogFilter{
		Route:  strings.TrimSpace(r.URL.Query().Get("route")),
		Result: strings.TrimSpace(r.URL.Query().Get("result")),
	}
	if limit := strings.TrimSpace(r.URL.Query().Get("limit")); limit != "" {
		parsed, err := strconv.Atoi(limit)
		if err != nil {
			writeError(w, http.StatusBadRequest, "limit must be an integer")
			return
		}
		filter.Limit = parsed
	}
	if offset := strings.TrimSpace(r.URL.Query().Get("offset")); offset != "" {
		parsed, err := strconv.Atoi(offset)
		if err != nil {
			writeError(w, http.StatusBadRequest, "offset must be an integer")
			return
		}
		filter.Offset = parsed
	}
	logs, err := s.repo.ListRequestLogs(r.Context(), time.Now().UTC().Add(-24*time.Hour), filter)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list logs")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": logs})
}

func (s *Server) requireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie(s.sessions.CookieName())
		if err != nil {
			writeError(w, http.StatusUnauthorized, "missing admin session")
			return
		}
		session, err := s.sessions.Decode(cookie.Value, time.Now().UTC())
		if err != nil {
			writeError(w, http.StatusUnauthorized, "invalid admin session")
			return
		}
		appConfig, err := s.repo.GetAppConfig(r.Context())
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to load configuration")
			return
		}
		if session.AuthVersion != appConfig.AuthVersion {
			writeError(w, http.StatusUnauthorized, "admin session expired")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) requireAuthPage(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie(s.sessions.CookieName())
		if err != nil {
			http.Redirect(w, r, "/admin", http.StatusFound)
			return
		}
		session, err := s.sessions.Decode(cookie.Value, time.Now().UTC())
		if err != nil {
			http.Redirect(w, r, "/admin", http.StatusFound)
			return
		}
		appConfig, err := s.repo.GetAppConfig(r.Context())
		if err != nil {
			http.Error(w, "failed to load configuration", http.StatusInternalServerError)
			return
		}
		if session.AuthVersion != appConfig.AuthVersion {
			http.Redirect(w, r, "/admin", http.StatusFound)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) handleLoginPage(w http.ResponseWriter, r *http.Request) {
	s.renderer.RenderLogin(w)
}

func (s *Server) handleDashboardPage(w http.ResponseWriter, r *http.Request) {
	s.renderer.RenderPage(w, "dashboard", "仪表盘")
}

func (s *Server) handleUpstreamsPage(w http.ResponseWriter, r *http.Request) {
	s.renderer.RenderPage(w, "upstreams", "上游 Key")
}

func (s *Server) handleSettingsPage(w http.ResponseWriter, r *http.Request) {
	s.renderer.RenderPage(w, "settings", "系统设置")
}

func (s *Server) handleLogsPage(w http.ResponseWriter, r *http.Request) {
	s.renderer.RenderPage(w, "logs", "请求日志")
}

func sanitizeConfig(appConfig store.AppConfig) map[string]any {
	return map[string]any{
		"id":                 appConfig.ID,
		"auth_version":       appConfig.AuthVersion,
		"outbound_proxy_url": appConfig.OutboundProxyURL,
		"upstream_base_url":  appConfig.UpstreamBaseURL,
		"upstream_auth_mode": appConfig.UpstreamAuthMode,
		"failover_threshold": appConfig.FailoverThreshold,
		"cooldown_seconds":   appConfig.CooldownSeconds,
		"created_at":         appConfig.CreatedAt,
		"updated_at":         appConfig.UpdatedAt,
	}
}

func (s *Server) configPayload(ctx context.Context, appConfig store.AppConfig) (map[string]any, error) {
	redirects, err := s.repo.ListModelRedirects(ctx)
	if err != nil {
		return nil, err
	}
	config := sanitizeConfig(appConfig)
	config["model_redirects"] = redirects
	return config, nil
}

func normalizeModelRedirects(inputs []modelRedirectInput) ([]store.ModelRedirect, error) {
	redirects := make([]store.ModelRedirect, 0, len(inputs))
	seen := make(map[string]struct{}, len(inputs))
	for index, input := range inputs {
		downstreamModel := strings.TrimSpace(input.DownstreamModel)
		upstreamModel := strings.TrimSpace(input.UpstreamModel)
		if downstreamModel == "" || upstreamModel == "" {
			return nil, fmt.Errorf("model_redirects[%d] requires downstream_model and upstream_model", index)
		}
		if _, ok := seen[downstreamModel]; ok {
			return nil, fmt.Errorf("duplicate downstream_model %q", downstreamModel)
		}
		seen[downstreamModel] = struct{}{}
		redirects = append(redirects, store.ModelRedirect{
			DownstreamModel: downstreamModel,
			UpstreamModel:   upstreamModel,
		})
	}
	return redirects, nil
}

func parseIDParam(r *http.Request, key string) (int64, error) {
	value := chi.URLParam(r, key)
	parsed, err := strconv.ParseInt(strings.TrimSpace(value), 10, 64)
	if err != nil || parsed <= 0 {
		return 0, fmt.Errorf("%s must be a positive integer", key)
	}
	return parsed, nil
}

func writeJSON(w http.ResponseWriter, statusCode int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	_ = json.NewEncoder(w).Encode(payload)
}

func writeError(w http.ResponseWriter, statusCode int, message string) {
	writeJSON(w, statusCode, map[string]any{
		"error": map[string]any{
			"type":    http.StatusText(statusCode),
			"message": message,
		},
	})
}
