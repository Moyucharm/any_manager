package app

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"anymanager/internal/auth"
	"anymanager/internal/config"
	adminhttp "anymanager/internal/http/admin"
	publichttp "anymanager/internal/http/public"
	"anymanager/internal/jobs"
	"anymanager/internal/metrics"
	"anymanager/internal/proxy"
	"anymanager/internal/security"
	"anymanager/internal/store"
	"anymanager/internal/upstream"
)

type App struct {
	repo         *store.Repository
	publicServer *http.Server
	adminServer  *http.Server
	cleanupJob   *jobs.CleanupJob
	logger       *log.Logger
}

func New(ctx context.Context, cfg config.Config) (*App, error) {
	logger := log.New(os.Stdout, "[anymanager] ", log.LstdFlags|log.LUTC)
	_, statErr := os.Stat(filepath.Clean(cfg.DBPath))
	newDatabase := errors.Is(statErr, os.ErrNotExist)
	db, err := store.Open(ctx, cfg.DBPath)
	if err != nil {
		return nil, err
	}
	if err := store.Migrate(ctx, db); err != nil {
		_ = db.Close()
		return nil, err
	}
	repo := store.NewRepository(db)
	if newDatabase {
		if _, err := repo.UpdateAppConfig(ctx, store.UpdateAppConfigInput{
			OutboundProxyURL:  cfg.OutboundProxyURL,
			UpstreamBaseURL:   cfg.UpstreamBaseURL,
			UpstreamAuthMode:  cfg.UpstreamAuthMode,
			FailoverThreshold: cfg.FailoverThreshold,
			CooldownSeconds:   int(cfg.Cooldown.Seconds()),
		}); err != nil {
			_ = repo.Close()
			return nil, fmt.Errorf("seed runtime defaults: %w", err)
		}
	}
	cipher, err := security.NewCipher(cfg.MasterKey)
	if err != nil {
		_ = repo.Close()
		return nil, err
	}
	hasher := security.NewHasher()
	sessions := auth.NewSessionManager(cfg.MasterKey, cfg.SessionCookieName, cfg.SessionCookieSecure, cfg.SessionTTL)
	transportBuilder := proxy.NewTransportBuilder()
	clientFactory := proxy.NewClientFactory(transportBuilder)
	upstreamService := upstream.NewService(repo, cipher, clientFactory)
	metricsService := metrics.NewService(repo, upstreamService)
	forwarder := proxy.NewForwarder(repo, upstreamService, clientFactory)
	publicHandler := publichttp.NewServer(repo, hasher, forwarder).Router()
	adminServer, err := adminhttp.NewServer(repo, hasher, sessions, upstreamService, metricsService)
	if err != nil {
		_ = repo.Close()
		return nil, fmt.Errorf("init admin server: %w", err)
	}
	adminHandler := adminServer.Router()

	publicMux := http.NewServeMux()
	publicMux.Handle("/admin", adminHandler)
	publicMux.Handle("/admin/", adminHandler)
	publicMux.Handle("/", publicHandler)

	return &App{
		repo: repo,
		publicServer: &http.Server{
			Addr:              cfg.PublicListenAddr,
			Handler:           publicMux,
			ReadHeaderTimeout: 15 * time.Second,
		},
		adminServer: &http.Server{
			Addr:              cfg.AdminListenAddr,
			Handler:           adminHandler,
			ReadHeaderTimeout: 15 * time.Second,
		},
		cleanupJob: jobs.NewCleanupJob(repo, cfg.CleanupInterval, logger),
		logger:     logger,
	}, nil
}

func (a *App) Run(ctx context.Context) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	errCh := make(chan error, 2)

	go a.cleanupJob.Run(ctx)
	go func() {
		a.logger.Printf("public listener on %s", a.publicServer.Addr)
		if err := a.publicServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- fmt.Errorf("public server: %w", err)
		}
	}()
	go func() {
		a.logger.Printf("admin listener on %s", a.adminServer.Addr)
		if err := a.adminServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- fmt.Errorf("admin server: %w", err)
		}
	}()

	var runErr error
	select {
	case <-ctx.Done():
		runErr = ctx.Err()
	case err := <-errCh:
		runErr = err
	}

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()
	_ = a.publicServer.Shutdown(shutdownCtx)
	_ = a.adminServer.Shutdown(shutdownCtx)
	if closeErr := a.repo.Close(); closeErr != nil && runErr == nil {
		runErr = closeErr
	}
	if errors.Is(runErr, context.Canceled) {
		return nil
	}
	return runErr
}
