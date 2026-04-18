package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"anymanager/internal/auth"
	adminhttp "anymanager/internal/http/admin"
	publichttp "anymanager/internal/http/public"
	"anymanager/internal/metrics"
	"anymanager/internal/proxy"
	"anymanager/internal/security"
	"anymanager/internal/store"
	"anymanager/internal/upstream"
)

type testEnv struct {
	repo         *store.Repository
	hasher       *security.Hasher
	upstreams    *upstream.Service
	publicServer *httptest.Server
	adminServer  *httptest.Server
	upstreamSrv  *httptest.Server
}

func TestAdminBootstrapLoginAndResetInvalidatesSession(t *testing.T) {
	t.Parallel()
	env := newTestEnv(t, func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	})
	client := newCookieClient(t)
	bootstrapResp := postJSON(t, client, env.adminServer.URL+"/admin/api/bootstrap", map[string]any{
		"downstream_api_key": "downstream-secret",
	})
	defer bootstrapResp.Body.Close()
	if bootstrapResp.StatusCode != http.StatusCreated {
		t.Fatalf("bootstrap status = %d, want %d", bootstrapResp.StatusCode, http.StatusCreated)
	}
	initialCookie := bootstrapResp.Cookies()[0]
	configResp := mustDo(t, client, mustNewRequest(t, http.MethodGet, env.adminServer.URL+"/admin/api/config", nil))
	defer configResp.Body.Close()
	if configResp.StatusCode != http.StatusOK {
		t.Fatalf("config status after bootstrap = %d, want %d", configResp.StatusCode, http.StatusOK)
	}
	resetResp := postJSONWithCookie(t, client, env.adminServer.URL+"/admin/api/downstream/reset", map[string]any{
		"new_api_key": "downstream-secret-new",
	}, initialCookie)
	defer resetResp.Body.Close()
	if resetResp.StatusCode != http.StatusOK {
		t.Fatalf("reset status = %d, want %d", resetResp.StatusCode, http.StatusOK)
	}
	oldSessionReq := mustNewRequest(t, http.MethodGet, env.adminServer.URL+"/admin/api/config", nil)
	oldSessionReq.AddCookie(initialCookie)
	oldSessionResp := mustDo(t, http.DefaultClient, oldSessionReq)
	defer oldSessionResp.Body.Close()
	if oldSessionResp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("config with old session status = %d, want %d", oldSessionResp.StatusCode, http.StatusUnauthorized)
	}
	oldLoginResp := postJSON(t, client, env.adminServer.URL+"/admin/api/login", map[string]any{"password": "downstream-secret"})
	defer oldLoginResp.Body.Close()
	if oldLoginResp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("login with old password status = %d, want %d", oldLoginResp.StatusCode, http.StatusUnauthorized)
	}
	newLoginResp := postJSON(t, client, env.adminServer.URL+"/admin/api/login", map[string]any{"password": "downstream-secret-new"})
	defer newLoginResp.Body.Close()
	if newLoginResp.StatusCode != http.StatusOK {
		t.Fatalf("login with new password status = %d, want %d", newLoginResp.StatusCode, http.StatusOK)
	}
}

func TestPublicProxyModelsAndMessages(t *testing.T) {
	t.Parallel()
	env := newTestEnv(t, func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer sk-primary" {
			t.Errorf("Authorization header = %q, want Bearer sk-primary", got)
		}
		switch r.URL.Path {
		case "/v1/models":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"data":[{"id":"claude-3-7-sonnet"}]}`))
		case "/v1/messages":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"id":"msg_1","model":"claude-3-7-sonnet","usage":{"input_tokens":12,"output_tokens":34}}`))
		default:
			http.NotFound(w, r)
		}
	})
	env.bootstrapDownstream(t, "downstream-secret")
	if _, err := env.upstreams.Create(context.Background(), "primary", "sk-primary", true); err != nil {
		t.Fatalf("Create(primary) error = %v", err)
	}
	modelsReq := mustNewRequest(t, http.MethodGet, env.publicServer.URL+"/v1/models", nil)
	modelsReq.Header.Set("x-api-key", "downstream-secret")
	modelsResp := mustDo(t, http.DefaultClient, modelsReq)
	defer modelsResp.Body.Close()
	if modelsResp.StatusCode != http.StatusOK {
		t.Fatalf("models status = %d, want %d", modelsResp.StatusCode, http.StatusOK)
	}
	modelsBody := readBody(t, modelsResp.Body)
	if !strings.Contains(modelsBody, "claude-3-7-sonnet") {
		t.Fatalf("models body = %s", modelsBody)
	}
	messageReq := mustNewRequest(t, http.MethodPost, env.publicServer.URL+"/v1/messages", strings.NewReader(`{"model":"claude-3-7-sonnet","messages":[]}`))
	messageReq.Header.Set("Content-Type", "application/json")
	messageReq.Header.Set("x-api-key", "downstream-secret")
	messageResp := mustDo(t, http.DefaultClient, messageReq)
	defer messageResp.Body.Close()
	if messageResp.StatusCode != http.StatusOK {
		t.Fatalf("messages status = %d, want %d", messageResp.StatusCode, http.StatusOK)
	}
	messageBody := readBody(t, messageResp.Body)
	if !strings.Contains(messageBody, `"input_tokens":12`) {
		t.Fatalf("message body = %s", messageBody)
	}
	logs, err := env.repo.ListRequestLogs(context.Background(), time.Now().UTC().Add(-24*time.Hour), store.RequestLogFilter{})
	if err != nil {
		t.Fatalf("ListRequestLogs() error = %v", err)
	}
	if len(logs) != 2 {
		t.Fatalf("log count = %d, want 2", len(logs))
	}
	if !logs[0].Success || !logs[1].Success {
		t.Fatalf("expected both request logs to be successful: %+v", logs)
	}
}

func TestPublicProxyStreamingAndFailoverThreshold(t *testing.T) {
	t.Parallel()
	var primaryHits atomic.Int32
	var secondaryHits atomic.Int32
	env := newTestEnv(t, func(w http.ResponseWriter, r *http.Request) {
		token := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		switch token {
		case "sk-primary":
			primaryHits.Add(1)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(`{"error":{"message":"primary failure"}}`))
		case "sk-secondary":
			secondaryHits.Add(1)
			w.Header().Set("Content-Type", "text/event-stream")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("event: message\ndata: {\"type\":\"message_start\"}\n\n"))
		default:
			w.WriteHeader(http.StatusUnauthorized)
		}
	})
	env.bootstrapDownstream(t, "downstream-secret")
	primary, err := env.upstreams.Create(context.Background(), "primary", "sk-primary", true)
	if err != nil {
		t.Fatalf("Create(primary) error = %v", err)
	}
	if _, err := env.upstreams.Create(context.Background(), "secondary", "sk-secondary", true); err != nil {
		t.Fatalf("Create(secondary) error = %v", err)
	}
	body := `{"model":"claude-3-7-sonnet","stream":true,"messages":[]}`
	for i := 0; i < 20; i++ {
		resp := postPublicMessage(t, env.publicServer.URL, "downstream-secret", body)
		resp.Body.Close()
		if resp.StatusCode != http.StatusInternalServerError {
			t.Fatalf("failure request %d status = %d, want %d", i, resp.StatusCode, http.StatusInternalServerError)
		}
	}
	streamResp := postPublicMessage(t, env.publicServer.URL, "downstream-secret", body)
	defer streamResp.Body.Close()
	if streamResp.StatusCode != http.StatusOK {
		t.Fatalf("stream status after failover = %d, want %d", streamResp.StatusCode, http.StatusOK)
	}
	streamBody := readBody(t, streamResp.Body)
	if !strings.Contains(streamBody, "message_start") {
		t.Fatalf("stream body = %s", streamBody)
	}
	if primaryHits.Load() != 20 {
		t.Fatalf("primary hits = %d, want 20", primaryHits.Load())
	}
	if secondaryHits.Load() != 1 {
		t.Fatalf("secondary hits = %d, want 1", secondaryHits.Load())
	}
	updatedPrimary, err := env.repo.GetUpstreamKeyByID(context.Background(), primary.ID)
	if err != nil {
		t.Fatalf("GetUpstreamKeyByID(primary) error = %v", err)
	}
	if updatedPrimary.CooldownUntil == nil {
		t.Fatalf("primary cooldown was not set after 20 failures")
	}
}

func TestRefreshBalanceEndpoint(t *testing.T) {
	t.Parallel()
	env := newTestEnv(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/usage/token" {
			http.NotFound(w, r)
			return
		}
		if got := r.Header.Get("Authorization"); got != "Bearer sk-primary" {
			t.Errorf("Authorization header = %q, want Bearer sk-primary", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"total_granted":100,"total_used":40,"total_available":60}`))
	})
	env.bootstrapDownstream(t, "downstream-secret")
	key, err := env.upstreams.Create(context.Background(), "primary", "sk-primary", true)
	if err != nil {
		t.Fatalf("Create(primary) error = %v", err)
	}
	client := newCookieClient(t)
	loginResp := postJSON(t, client, env.adminServer.URL+"/admin/api/login", map[string]any{"password": "downstream-secret"})
	defer loginResp.Body.Close()
	if loginResp.StatusCode != http.StatusOK {
		t.Fatalf("login status = %d, want %d", loginResp.StatusCode, http.StatusOK)
	}
	refreshResp := postJSON(t, client, fmt.Sprintf("%s/admin/api/upstreams/%d/refresh-balance", env.adminServer.URL, key.ID), map[string]any{})
	defer refreshResp.Body.Close()
	if refreshResp.StatusCode != http.StatusOK {
		t.Fatalf("refresh balance status = %d, want %d", refreshResp.StatusCode, http.StatusOK)
	}
	body := readBody(t, refreshResp.Body)
	if !strings.Contains(body, `"last_balance_total_available":60`) {
		t.Fatalf("refresh balance body = %s", body)
	}
}

func newTestEnv(t *testing.T, upstreamHandler http.HandlerFunc) *testEnv {
	t.Helper()
	ctx := context.Background()
	upstreamServer := httptest.NewServer(http.HandlerFunc(upstreamHandler))
	t.Cleanup(upstreamServer.Close)
	db, err := store.Open(ctx, filepath.Join(t.TempDir(), "anymanager.db"))
	if err != nil {
		t.Fatalf("store.Open() error = %v", err)
	}
	if err := store.Migrate(ctx, db); err != nil {
		t.Fatalf("store.Migrate() error = %v", err)
	}
	repo := store.NewRepository(db)
	t.Cleanup(func() { _ = repo.Close() })
	if _, err := repo.UpdateAppConfig(ctx, store.UpdateAppConfigInput{
		OutboundProxyURL:  "",
		UpstreamBaseURL:   upstreamServer.URL,
		UpstreamAuthMode:  "authorization_bearer",
		FailoverThreshold: 20,
		CooldownSeconds:   600,
	}); err != nil {
		t.Fatalf("UpdateAppConfig() error = %v", err)
	}
	hasher := security.NewHasher()
	cipher, err := security.NewCipher("test-master-key")
	if err != nil {
		t.Fatalf("NewCipher() error = %v", err)
	}
	sessions := auth.NewSessionManager("test-master-key", "test_admin_session", false, time.Hour)
	clientFactory := proxy.NewClientFactory(proxy.NewTransportBuilder())
	upstreamService := upstream.NewService(repo, cipher, clientFactory)
	metricsService := metrics.NewService(repo, upstreamService)
	forwarder := proxy.NewForwarder(repo, upstreamService, clientFactory)
	publicServer := httptest.NewServer(publichttp.NewServer(repo, hasher, forwarder).Router())
	t.Cleanup(publicServer.Close)
	adminServer := httptest.NewServer(adminhttp.NewServer(repo, hasher, sessions, upstreamService, metricsService).Router())
	t.Cleanup(adminServer.Close)
	return &testEnv{
		repo:         repo,
		hasher:       hasher,
		upstreams:    upstreamService,
		publicServer: publicServer,
		adminServer:  adminServer,
		upstreamSrv:  upstreamServer,
	}
}

func (e *testEnv) bootstrapDownstream(t *testing.T, apiKey string) {
	t.Helper()
	hash, err := e.hasher.Hash(apiKey)
	if err != nil {
		t.Fatalf("Hash() error = %v", err)
	}
	if err := e.repo.BootstrapDownstreamKey(context.Background(), hash); err != nil {
		t.Fatalf("BootstrapDownstreamKey() error = %v", err)
	}
}

func newCookieClient(t *testing.T) *http.Client {
	t.Helper()
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("cookiejar.New() error = %v", err)
	}
	return &http.Client{Jar: jar}
}

func postJSON(t *testing.T, client *http.Client, url string, payload any) *http.Response {
	t.Helper()
	return doJSON(t, client, http.MethodPost, url, payload, nil)
}

func postJSONWithCookie(t *testing.T, client *http.Client, url string, payload any, cookie *http.Cookie) *http.Response {
	t.Helper()
	return doJSON(t, client, http.MethodPost, url, payload, cookie)
}

func doJSON(t *testing.T, client *http.Client, method, url string, payload any, cookie *http.Cookie) *http.Response {
	t.Helper()
	body, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}
	req := mustNewRequest(t, method, url, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if cookie != nil {
		req.AddCookie(cookie)
	}
	return mustDo(t, client, req)
}

func postPublicMessage(t *testing.T, baseURL, downstreamKey, body string) *http.Response {
	t.Helper()
	req := mustNewRequest(t, http.MethodPost, baseURL+"/v1/messages", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", downstreamKey)
	return mustDo(t, http.DefaultClient, req)
}

func mustNewRequest(t *testing.T, method, url string, body io.Reader) *http.Request {
	t.Helper()
	req, err := http.NewRequest(method, url, body)
	if err != nil {
		t.Fatalf("http.NewRequest() error = %v", err)
	}
	return req
}

func mustDo(t *testing.T, client *http.Client, req *http.Request) *http.Response {
	t.Helper()
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("client.Do() error = %v", err)
	}
	return resp
}

func readBody(t *testing.T, reader io.Reader) string {
	t.Helper()
	body, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("io.ReadAll() error = %v", err)
	}
	return string(body)
}
