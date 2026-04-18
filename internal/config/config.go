package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

const (
	defaultPublicListenAddr  = ":8080"
	defaultAdminListenAddr   = ":8081"
	defaultDBPath            = "./data/anymanager.db"
	defaultSessionCookieName = "anymanager_admin_session"
	defaultUpstreamBaseURL   = "https://anyrouter.top"
	defaultUpstreamAuthMode  = "authorization_bearer"
	defaultFailoverThreshold = 20
	defaultCooldownSeconds   = 600
	defaultCleanupInterval   = 5 * time.Minute
	defaultSessionTTL        = 24 * time.Hour
)

type Config struct {
	PublicListenAddr    string
	AdminListenAddr     string
	DBPath              string
	MasterKey           string
	SessionCookieName   string
	SessionCookieSecure bool
	SessionTTL          time.Duration
	CleanupInterval     time.Duration
	UpstreamBaseURL     string
	UpstreamAuthMode    string
	FailoverThreshold   int
	Cooldown            time.Duration
	OutboundProxyURL    string
}

func Load() (Config, error) {
	cfg := Config{
		PublicListenAddr:  getEnv("PUBLIC_LISTEN_ADDR", defaultPublicListenAddr),
		AdminListenAddr:   getEnv("ADMIN_LISTEN_ADDR", defaultAdminListenAddr),
		DBPath:            getEnv("DB_PATH", defaultDBPath),
		MasterKey:         os.Getenv("APP_MASTER_KEY"),
		SessionCookieName: getEnv("SESSION_COOKIE_NAME", defaultSessionCookieName),
		UpstreamBaseURL:   getEnv("UPSTREAM_BASE_URL", defaultUpstreamBaseURL),
		UpstreamAuthMode:  getEnv("UPSTREAM_AUTH_MODE", defaultUpstreamAuthMode),
		OutboundProxyURL:  strings.TrimSpace(os.Getenv("OUTBOUND_PROXY_URL")),
		CleanupInterval:   defaultCleanupInterval,
		SessionTTL:        defaultSessionTTL,
	}

	if cfg.MasterKey == "" {
		return Config{}, fmt.Errorf("APP_MASTER_KEY is required")
	}

	secureCookie, err := getEnvBool("SESSION_COOKIE_SECURE", false)
	if err != nil {
		return Config{}, fmt.Errorf("invalid SESSION_COOKIE_SECURE: %w", err)
	}
	cfg.SessionCookieSecure = secureCookie

	failoverThreshold, err := getEnvInt("FAILOVER_THRESHOLD", defaultFailoverThreshold)
	if err != nil {
		return Config{}, fmt.Errorf("invalid FAILOVER_THRESHOLD: %w", err)
	}
	if failoverThreshold < 1 {
		return Config{}, fmt.Errorf("FAILOVER_THRESHOLD must be >= 1")
	}
	cfg.FailoverThreshold = failoverThreshold

	cooldownSeconds, err := getEnvInt("COOLDOWN_SECONDS", defaultCooldownSeconds)
	if err != nil {
		return Config{}, fmt.Errorf("invalid COOLDOWN_SECONDS: %w", err)
	}
	if cooldownSeconds < 60 {
		return Config{}, fmt.Errorf("COOLDOWN_SECONDS must be >= 60")
	}
	cfg.Cooldown = time.Duration(cooldownSeconds) * time.Second

	if _, err := normalizeAuthMode(cfg.UpstreamAuthMode); err != nil {
		return Config{}, err
	}

	return cfg, nil
}

func NormalizeAuthMode(value string) (string, error) {
	return normalizeAuthMode(value)
}

func normalizeAuthMode(value string) (string, error) {
	mode := strings.TrimSpace(strings.ToLower(value))
	switch mode {
	case "authorization_bearer", "x_api_key":
		return mode, nil
	default:
		return "", fmt.Errorf("UPSTREAM_AUTH_MODE must be authorization_bearer or x_api_key")
	}
}

func getEnv(key, fallback string) string {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	return value
}

func getEnvInt(key string, fallback int) (int, error) {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback, nil
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return 0, err
	}
	return parsed, nil
}

func getEnvBool(key string, fallback bool) (bool, error) {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback, nil
	}
	parsed, err := strconv.ParseBool(value)
	if err != nil {
		return false, err
	}
	return parsed, nil
}
