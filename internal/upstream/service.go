package upstream

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"anymanager/internal/security"
	"anymanager/internal/store"
)

var ErrNoCandidate = errors.New("upstream: no candidate available")

type ClientFactory interface {
	NewClient(proxyURL string) (*http.Client, error)
}

type Candidate struct {
	Key     store.UpstreamKey
	APIKey  string
	Config  store.AppConfig
	BaseURL *url.URL
}

type Status struct {
	Availability      bool   `json:"availability"`
	ActiveAlias       string `json:"active_upstream_alias"`
	EnabledKeyCount   int    `json:"enabled_key_count"`
	AvailableKeyCount int    `json:"available_key_count"`
}

type Service struct {
	repo    *store.Repository
	cipher  *security.Cipher
	clients ClientFactory
}

func NewService(repo *store.Repository, cipher *security.Cipher, clients ClientFactory) *Service {
	return &Service{repo: repo, cipher: cipher, clients: clients}
}

func (s *Service) List(ctx context.Context) ([]store.UpstreamKey, error) {
	return s.repo.ListUpstreamKeys(ctx)
}

func (s *Service) Create(ctx context.Context, alias, apiKey string, enabled bool) (store.UpstreamKey, error) {
	encrypted, err := s.cipher.Encrypt(strings.TrimSpace(apiKey))
	if err != nil {
		return store.UpstreamKey{}, fmt.Errorf("encrypt upstream key: %w", err)
	}
	return s.repo.CreateUpstreamKey(ctx, strings.TrimSpace(alias), keyHint(apiKey), encrypted, enabled)
}

func (s *Service) Update(ctx context.Context, id int64, alias string, apiKey *string, enabled bool) (store.UpstreamKey, error) {
	current, err := s.repo.GetUpstreamKeyByID(ctx, id)
	if err != nil {
		return store.UpstreamKey{}, err
	}
	encrypted := current.EncryptedAPIKey
	hint := current.KeyHint
	if apiKey != nil {
		encrypted, err = s.cipher.Encrypt(strings.TrimSpace(*apiKey))
		if err != nil {
			return store.UpstreamKey{}, fmt.Errorf("encrypt updated upstream key: %w", err)
		}
		hint = keyHint(*apiKey)
	}
	return s.repo.ReplaceUpstreamKey(ctx, id, strings.TrimSpace(alias), hint, encrypted, enabled)
}

func (s *Service) Delete(ctx context.Context, id int64) error {
	return s.repo.DeleteUpstreamKey(ctx, id)
}

func (s *Service) SetEnabled(ctx context.Context, id int64, enabled bool) (store.UpstreamKey, error) {
	return s.repo.SetUpstreamEnabled(ctx, id, enabled)
}

func (s *Service) Reorder(ctx context.Context, ids []int64) error {
	return s.repo.ReorderUpstreamKeys(ctx, ids)
}

func (s *Service) Select(ctx context.Context) (Candidate, error) {
	config, err := s.repo.GetAppConfig(ctx)
	if err != nil {
		return Candidate{}, err
	}
	keys, err := s.repo.ListUpstreamKeys(ctx)
	if err != nil {
		return Candidate{}, err
	}
	available := filterAvailable(keys, time.Now().UTC())
	if len(available) == 0 {
		return Candidate{}, ErrNoCandidate
	}
	selected := available[0]
	decrypted, err := s.cipher.Decrypt(selected.EncryptedAPIKey)
	if err != nil {
		return Candidate{}, fmt.Errorf("decrypt upstream key: %w", err)
	}
	baseURL, err := url.Parse(config.UpstreamBaseURL)
	if err != nil {
		return Candidate{}, fmt.Errorf("parse upstream base url: %w", err)
	}
	return Candidate{Key: selected, APIKey: decrypted, Config: config, BaseURL: baseURL}, nil
}

func (s *Service) MarkResult(ctx context.Context, id int64, success bool, failureSummary string) (store.UpstreamKey, error) {
	config, err := s.repo.GetAppConfig(ctx)
	if err != nil {
		return store.UpstreamKey{}, err
	}
	return s.repo.RecordUpstreamResult(ctx, id, success, config.FailoverThreshold, time.Duration(config.CooldownSeconds)*time.Second, failureSummary)
}

func (s *Service) RefreshBalance(ctx context.Context, id int64) (store.UpstreamKey, error) {
	config, err := s.repo.GetAppConfig(ctx)
	if err != nil {
		return store.UpstreamKey{}, err
	}
	key, err := s.repo.GetUpstreamKeyByID(ctx, id)
	if err != nil {
		return store.UpstreamKey{}, err
	}
	apiKey, err := s.cipher.Decrypt(key.EncryptedAPIKey)
	if err != nil {
		return store.UpstreamKey{}, fmt.Errorf("decrypt upstream key: %w", err)
	}
	client, err := s.clients.NewClient(config.OutboundProxyURL)
	if err != nil {
		return store.UpstreamKey{}, err
	}
	baseURL, err := url.Parse(config.UpstreamBaseURL)
	if err != nil {
		return store.UpstreamKey{}, fmt.Errorf("parse upstream base url: %w", err)
	}
	usageURL := *baseURL
	usageURL.Path = joinURLPath(baseURL.Path, "/api/usage/token")
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, usageURL.String(), nil)
	if err != nil {
		return store.UpstreamKey{}, fmt.Errorf("build usage request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	resp, err := client.Do(req)
	if err != nil {
		return store.UpstreamKey{}, fmt.Errorf("request usage endpoint: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return store.UpstreamKey{}, fmt.Errorf("read usage response: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return store.UpstreamKey{}, fmt.Errorf("usage endpoint returned %d: %s", resp.StatusCode, summarizeBody(body))
	}
	balance, err := decodeUsageBalance(body)
	if err != nil {
		return store.UpstreamKey{}, err
	}
	return s.repo.UpdateUpstreamBalance(ctx, id, balance)
}

func (s *Service) Status(ctx context.Context) (Status, error) {
	keys, err := s.repo.ListUpstreamKeys(ctx)
	if err != nil {
		return Status{}, err
	}
	now := time.Now().UTC()
	status := Status{}
	available := filterAvailable(keys, now)
	status.Availability = len(available) > 0
	status.AvailableKeyCount = len(available)
	if status.Availability {
		status.ActiveAlias = available[0].Alias
	}
	for _, key := range keys {
		if key.IsEnabled {
			status.EnabledKeyCount++
		}
	}
	return status, nil
}

func filterAvailable(keys []store.UpstreamKey, now time.Time) []store.UpstreamKey {
	filtered := make([]store.UpstreamKey, 0, len(keys))
	for _, key := range keys {
		if !key.IsEnabled {
			continue
		}
		if key.CooldownUntil != nil && key.CooldownUntil.After(now) {
			continue
		}
		filtered = append(filtered, key)
	}
	sort.SliceStable(filtered, func(i, j int) bool {
		if filtered[i].Priority == filtered[j].Priority {
			return filtered[i].ID < filtered[j].ID
		}
		return filtered[i].Priority < filtered[j].Priority
	})
	return filtered
}

func decodeUsageBalance(body []byte) (store.UpstreamBalance, error) {
	var raw map[string]any
	if err := json.Unmarshal(body, &raw); err != nil {
		return store.UpstreamBalance{}, fmt.Errorf("decode usage response: %w", err)
	}
	if balance, ok := extractUsageBalance(raw); ok {
		return balance, nil
	}
	if nested, ok := raw["data"].(map[string]any); ok {
		if balance, ok := extractUsageBalance(nested); ok {
			return balance, nil
		}
	}
	return store.UpstreamBalance{}, fmt.Errorf("usage response missing balance fields")
}

func extractUsageBalance(raw map[string]any) (store.UpstreamBalance, bool) {
	granted, ok := toFloat(raw["total_granted"])
	if !ok {
		return store.UpstreamBalance{}, false
	}
	used, ok := toFloat(raw["total_used"])
	if !ok {
		return store.UpstreamBalance{}, false
	}
	available, ok := toFloat(raw["total_available"])
	if !ok {
		return store.UpstreamBalance{}, false
	}
	return store.UpstreamBalance{
		TotalGranted:   granted,
		TotalUsed:      used,
		TotalAvailable: available,
	}, true
}

func toFloat(value any) (float64, bool) {
	switch typed := value.(type) {
	case float64:
		return typed, true
	case float32:
		return float64(typed), true
	case int:
		return float64(typed), true
	case int64:
		return float64(typed), true
	case json.Number:
		parsed, err := typed.Float64()
		if err != nil {
			return 0, false
		}
		return parsed, true
	case string:
		parsed, err := json.Number(typed).Float64()
		if err != nil {
			return 0, false
		}
		return parsed, true
	default:
		return 0, false
	}
}

func keyHint(value string) string {
	trimmed := strings.TrimSpace(value)
	if len(trimmed) <= 8 {
		return "****"
	}
	return trimmed[:4] + "..." + trimmed[len(trimmed)-4:]
}

func joinURLPath(basePath, suffix string) string {
	basePath = strings.TrimSuffix(basePath, "/")
	suffix = strings.TrimPrefix(suffix, "/")
	if basePath == "" {
		return "/" + suffix
	}
	return basePath + "/" + suffix
}

func summarizeBody(body []byte) string {
	text := strings.TrimSpace(string(body))
	if len(text) <= 200 {
		return text
	}
	return text[:200]
}
