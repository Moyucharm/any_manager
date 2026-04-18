package upstream

import (
	"context"
	"net/http"
	"path/filepath"
	"testing"

	"anymanager/internal/security"
	"anymanager/internal/store"
)

type noopClientFactory struct{}

func (noopClientFactory) NewClient(string) (*http.Client, error) {
	return &http.Client{}, nil
}

func TestSelectAfterThresholdFailuresSwitchesCandidate(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db, err := store.Open(ctx, filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("store.Open() error = %v", err)
	}
	defer db.Close()
	if err := store.Migrate(ctx, db); err != nil {
		t.Fatalf("store.Migrate() error = %v", err)
	}
	repo := store.NewRepository(db)
	defer repo.Close()
	cipher, err := security.NewCipher("test-master-key")
	if err != nil {
		t.Fatalf("NewCipher() error = %v", err)
	}
	service := NewService(repo, cipher, noopClientFactory{})
	if _, err := repo.UpdateAppConfig(ctx, store.UpdateAppConfigInput{
		OutboundProxyURL:  "",
		UpstreamBaseURL:   "https://example.com",
		UpstreamAuthMode:  "authorization_bearer",
		FailoverThreshold: 20,
		CooldownSeconds:   600,
	}); err != nil {
		t.Fatalf("UpdateAppConfig() error = %v", err)
	}
	primary, err := service.Create(ctx, "primary", "sk-primary", true)
	if err != nil {
		t.Fatalf("Create(primary) error = %v", err)
	}
	secondary, err := service.Create(ctx, "secondary", "sk-secondary", true)
	if err != nil {
		t.Fatalf("Create(secondary) error = %v", err)
	}
	for i := 0; i < 19; i++ {
		updated, err := service.MarkResult(ctx, primary.ID, false, "upstream failure")
		if err != nil {
			t.Fatalf("MarkResult() error = %v", err)
		}
		if updated.CooldownUntil != nil {
			t.Fatalf("CooldownUntil set before threshold on iteration %d", i)
		}
		candidate, err := service.Select(ctx)
		if err != nil {
			t.Fatalf("Select() error before threshold = %v", err)
		}
		if candidate.Key.ID != primary.ID {
			t.Fatalf("Select() chose %d before threshold, want %d", candidate.Key.ID, primary.ID)
		}
	}
	updated, err := service.MarkResult(ctx, primary.ID, false, "threshold failure")
	if err != nil {
		t.Fatalf("MarkResult() threshold error = %v", err)
	}
	if updated.CooldownUntil == nil {
		t.Fatalf("CooldownUntil = nil after threshold")
	}
	candidate, err := service.Select(ctx)
	if err != nil {
		t.Fatalf("Select() error after threshold = %v", err)
	}
	if candidate.Key.ID != secondary.ID {
		t.Fatalf("Select() chose %d after threshold, want %d", candidate.Key.ID, secondary.ID)
	}
}
