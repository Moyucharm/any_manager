package jobs

import (
	"context"
	"log"
	"time"

	"anymanager/internal/store"
)

type CleanupJob struct {
	repo     *store.Repository
	interval time.Duration
	logger   *log.Logger
}

func NewCleanupJob(repo *store.Repository, interval time.Duration, logger *log.Logger) *CleanupJob {
	return &CleanupJob{repo: repo, interval: interval, logger: logger}
}

func (j *CleanupJob) Run(ctx context.Context) {
	ticker := time.NewTicker(j.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if _, err := j.repo.CleanupRequestLogs(ctx, time.Now().UTC().Add(-24*time.Hour)); err != nil && j.logger != nil {
				j.logger.Printf("cleanup request logs: %v", err)
			}
		}
	}
}
