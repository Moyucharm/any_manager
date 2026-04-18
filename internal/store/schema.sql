PRAGMA foreign_keys = ON;

CREATE TABLE IF NOT EXISTS app_config (
    id INTEGER PRIMARY KEY CHECK (id = 1),
    downstream_key_hash TEXT NOT NULL DEFAULT '',
    auth_version INTEGER NOT NULL DEFAULT 1,
    outbound_proxy_url TEXT NOT NULL DEFAULT '',
    upstream_base_url TEXT NOT NULL DEFAULT 'https://anyrouter.top',
    upstream_auth_mode TEXT NOT NULL DEFAULT 'authorization_bearer'
        CHECK (upstream_auth_mode IN ('authorization_bearer', 'x_api_key')),
    failover_threshold INTEGER NOT NULL DEFAULT 20 CHECK (failover_threshold >= 1),
    cooldown_seconds INTEGER NOT NULL DEFAULT 600 CHECK (cooldown_seconds >= 60),
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

INSERT INTO app_config (
    id,
    downstream_key_hash,
    auth_version,
    outbound_proxy_url,
    upstream_base_url,
    upstream_auth_mode,
    failover_threshold,
    cooldown_seconds
)
SELECT
    1,
    '',
    1,
    '',
    'https://anyrouter.top',
    'authorization_bearer',
    20,
    600
WHERE NOT EXISTS (
    SELECT 1 FROM app_config WHERE id = 1
);

CREATE TABLE IF NOT EXISTS upstream_keys (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    alias TEXT NOT NULL UNIQUE,
    key_hint TEXT NOT NULL DEFAULT '',
    encrypted_api_key TEXT NOT NULL,
    is_enabled INTEGER NOT NULL DEFAULT 1 CHECK (is_enabled IN (0, 1)),
    priority INTEGER NOT NULL,
    consecutive_failures INTEGER NOT NULL DEFAULT 0 CHECK (consecutive_failures >= 0),
    cooldown_until DATETIME NULL,
    last_balance_total_granted REAL NULL,
    last_balance_total_used REAL NULL,
    last_balance_total_available REAL NULL,
    last_balance_checked_at DATETIME NULL,
    last_error_summary TEXT NOT NULL DEFAULT '',
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_upstream_keys_priority
ON upstream_keys(priority);

CREATE TABLE IF NOT EXISTS request_logs (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    request_ts DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    route TEXT NOT NULL CHECK (route IN ('/v1/messages', '/v1/models')),
    method TEXT NOT NULL,
    upstream_key_id INTEGER NULL,
    upstream_alias TEXT NOT NULL DEFAULT '',
    model TEXT NOT NULL DEFAULT '',
    request_id TEXT NOT NULL DEFAULT '',
    status_code INTEGER NOT NULL DEFAULT 0,
    success INTEGER NOT NULL CHECK (success IN (0, 1)),
    failure_reason TEXT NOT NULL DEFAULT '',
    latency_ms INTEGER NOT NULL DEFAULT 0 CHECK (latency_ms >= 0),
    input_tokens INTEGER NOT NULL DEFAULT 0 CHECK (input_tokens >= 0),
    output_tokens INTEGER NOT NULL DEFAULT 0 CHECK (output_tokens >= 0),
    total_tokens INTEGER NOT NULL DEFAULT 0 CHECK (total_tokens >= 0),
    client_ip TEXT NOT NULL DEFAULT '',
    user_agent TEXT NOT NULL DEFAULT '',
    FOREIGN KEY (upstream_key_id) REFERENCES upstream_keys(id) ON DELETE SET NULL
);

CREATE INDEX IF NOT EXISTS idx_request_logs_request_ts
ON request_logs(request_ts DESC);

CREATE INDEX IF NOT EXISTS idx_request_logs_success_request_ts
ON request_logs(success, request_ts DESC);

CREATE INDEX IF NOT EXISTS idx_request_logs_route_request_ts
ON request_logs(route, request_ts DESC);
