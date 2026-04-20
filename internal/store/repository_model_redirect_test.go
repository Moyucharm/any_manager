package store

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
)

func TestReplaceAndLookupModelRedirects(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db, err := Open(ctx, filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer db.Close()
	if err := Migrate(ctx, db); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}
	repo := NewRepository(db)
	defer repo.Close()

	if err := repo.ReplaceModelRedirects(ctx, []ModelRedirect{
		{DownstreamModel: "claude-4-6-opus", UpstreamModel: "claude-4-7-opus"},
		{DownstreamModel: "claude-3-7-sonnet", UpstreamModel: "claude-3-8-sonnet"},
	}); err != nil {
		t.Fatalf("ReplaceModelRedirects() error = %v", err)
	}

	redirect, err := repo.LookupModelRedirect(ctx, "claude-4-6-opus")
	if err != nil {
		t.Fatalf("LookupModelRedirect() error = %v", err)
	}
	if redirect.UpstreamModel != "claude-4-7-opus" {
		t.Fatalf("upstream model = %q, want %q", redirect.UpstreamModel, "claude-4-7-opus")
	}

	redirects, err := repo.ListModelRedirects(ctx)
	if err != nil {
		t.Fatalf("ListModelRedirects() error = %v", err)
	}
	if len(redirects) != 2 {
		t.Fatalf("redirect count = %d, want 2", len(redirects))
	}

	_, err = repo.LookupModelRedirect(ctx, "claude-4-6-opus-preview")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("LookupModelRedirect() error = %v, want ErrNotFound", err)
	}

	if err := repo.ReplaceModelRedirects(ctx, nil); err != nil {
		t.Fatalf("ReplaceModelRedirects(nil) error = %v", err)
	}
	redirects, err = repo.ListModelRedirects(ctx)
	if err != nil {
		t.Fatalf("ListModelRedirects() after clear error = %v", err)
	}
	if len(redirects) != 0 {
		t.Fatalf("redirect count after clear = %d, want 0", len(redirects))
	}
}
