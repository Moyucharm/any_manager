package store

import "time"

type AppConfig struct {
	ID                int       `json:"id"`
	DownstreamKeyHash string    `json:"-"`
	AuthVersion       int       `json:"auth_version"`
	OutboundProxyURL  string    `json:"outbound_proxy_url"`
	UpstreamBaseURL   string    `json:"upstream_base_url"`
	UpstreamAuthMode  string    `json:"upstream_auth_mode"`
	FailoverThreshold int       `json:"failover_threshold"`
	CooldownSeconds   int       `json:"cooldown_seconds"`
	CreatedAt         time.Time `json:"created_at"`
	UpdatedAt         time.Time `json:"updated_at"`
}

type UpstreamKey struct {
	ID                        int64      `json:"id"`
	Alias                     string     `json:"alias"`
	KeyHint                   string     `json:"key_hint"`
	EncryptedAPIKey           string     `json:"-"`
	IsEnabled                 bool       `json:"is_enabled"`
	Priority                  int        `json:"priority"`
	ConsecutiveFailures       int        `json:"consecutive_failures"`
	CooldownUntil             *time.Time `json:"cooldown_until,omitempty"`
	LastBalanceTotalGranted   *float64   `json:"last_balance_total_granted,omitempty"`
	LastBalanceTotalUsed      *float64   `json:"last_balance_total_used,omitempty"`
	LastBalanceTotalAvailable *float64   `json:"last_balance_total_available,omitempty"`
	LastBalanceCheckedAt      *time.Time `json:"last_balance_checked_at,omitempty"`
	LastErrorSummary          string     `json:"last_error_summary"`
	CreatedAt                 time.Time  `json:"created_at"`
	UpdatedAt                 time.Time  `json:"updated_at"`
}

type UpstreamBalance struct {
	TotalGranted   *float64 `json:"total_granted,omitempty"`
	TotalUsed      *float64 `json:"total_used,omitempty"`
	TotalAvailable *float64 `json:"total_available,omitempty"`
}

type UpdateAppConfigInput struct {
	OutboundProxyURL  string
	UpstreamBaseURL   string
	UpstreamAuthMode  string
	FailoverThreshold int
	CooldownSeconds   int
}

type RequestLog struct {
	ID            int64     `json:"id"`
	RequestTS     time.Time `json:"request_ts"`
	Route         string    `json:"route"`
	Method        string    `json:"method"`
	UpstreamKeyID *int64    `json:"upstream_key_id,omitempty"`
	UpstreamAlias string    `json:"upstream_alias"`
	Model         string    `json:"model"`
	RequestID     string    `json:"request_id"`
	StatusCode    int       `json:"status_code"`
	Success       bool      `json:"success"`
	FailureReason string    `json:"failure_reason"`
	LatencyMS     int64     `json:"latency_ms"`
	InputTokens   int       `json:"input_tokens"`
	OutputTokens  int       `json:"output_tokens"`
	TotalTokens   int       `json:"total_tokens"`
	ClientIP      string    `json:"client_ip"`
	UserAgent     string    `json:"user_agent"`
}

type RequestLogFilter struct {
	Route  string
	Result string
	Limit  int
	Offset int
}

type RequestStats struct {
	Total     int64 `json:"total"`
	Successes int64 `json:"successes"`
	Failures  int64 `json:"failures"`
}
