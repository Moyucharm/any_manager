package metrics

import (
	"context"
	"time"

	"anymanager/internal/store"
	"anymanager/internal/upstream"
)

type Summary struct {
	TotalRequests24h    int64   `json:"total_requests_24h"`
	SuccessCount24h     int64   `json:"success_count_24h"`
	FailureCount24h     int64   `json:"failure_count_24h"`
	SuccessRate24h      float64 `json:"success_rate_24h"`
	Availability        bool    `json:"availability"`
	ActiveUpstreamAlias string  `json:"active_upstream_alias"`
	EnabledKeyCount     int     `json:"enabled_key_count"`
	AvailableKeyCount   int     `json:"available_key_count"`
}

type Service struct {
	repo      *store.Repository
	upstreams *upstream.Service
}

func NewService(repo *store.Repository, upstreams *upstream.Service) *Service {
	return &Service{repo: repo, upstreams: upstreams}
}

func (s *Service) Get(ctx context.Context) (Summary, error) {
	stats, err := s.repo.Get24hStats(ctx, time.Now().UTC().Add(-24*time.Hour))
	if err != nil {
		return Summary{}, err
	}
	status, err := s.upstreams.Status(ctx)
	if err != nil {
		return Summary{}, err
	}
	summary := Summary{
		TotalRequests24h:    stats.Total,
		SuccessCount24h:     stats.Successes,
		FailureCount24h:     stats.Failures,
		Availability:        status.Availability,
		ActiveUpstreamAlias: status.ActiveAlias,
		EnabledKeyCount:     status.EnabledKeyCount,
		AvailableKeyCount:   status.AvailableKeyCount,
	}
	if stats.Total > 0 {
		summary.SuccessRate24h = float64(stats.Successes) / float64(stats.Total) * 100
	}
	return summary, nil
}
