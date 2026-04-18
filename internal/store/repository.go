package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"
)

var (
	ErrNotFound           = errors.New("store: not found")
	ErrAlreadyInitialized = errors.New("store: already initialized")
)

type Repository struct {
	db *sql.DB
}

func NewRepository(db *sql.DB) *Repository {
	return &Repository{db: db}
}

func (r *Repository) Close() error {
	return r.db.Close()
}

func (r *Repository) GetAppConfig(ctx context.Context) (AppConfig, error) {
	row := r.db.QueryRowContext(ctx, `
		SELECT id, downstream_key_hash, auth_version, outbound_proxy_url, upstream_base_url, upstream_auth_mode,
		       failover_threshold, cooldown_seconds, created_at, updated_at
		FROM app_config
		WHERE id = 1
	`)
	return scanAppConfig(row)
}

func (r *Repository) BootstrapDownstreamKey(ctx context.Context, keyHash string) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin bootstrap transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	var existing string
	if err := tx.QueryRowContext(ctx, `SELECT downstream_key_hash FROM app_config WHERE id = 1`).Scan(&existing); err != nil {
		return fmt.Errorf("load downstream hash: %w", err)
	}
	if existing != "" {
		return ErrAlreadyInitialized
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE app_config
		SET downstream_key_hash = ?, auth_version = 1, updated_at = CURRENT_TIMESTAMP
		WHERE id = 1
	`, keyHash); err != nil {
		return fmt.Errorf("bootstrap downstream hash: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit bootstrap transaction: %w", err)
	}
	return nil
}

func (r *Repository) UpdateDownstreamKey(ctx context.Context, keyHash string) (AppConfig, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return AppConfig{}, fmt.Errorf("begin downstream update transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.ExecContext(ctx, `
		UPDATE app_config
		SET downstream_key_hash = ?, auth_version = auth_version + 1, updated_at = CURRENT_TIMESTAMP
		WHERE id = 1
	`, keyHash); err != nil {
		return AppConfig{}, fmt.Errorf("update downstream hash: %w", err)
	}
	config, err := scanAppConfig(tx.QueryRowContext(ctx, `
		SELECT id, downstream_key_hash, auth_version, outbound_proxy_url, upstream_base_url, upstream_auth_mode,
		       failover_threshold, cooldown_seconds, created_at, updated_at
		FROM app_config
		WHERE id = 1
	`))
	if err != nil {
		return AppConfig{}, err
	}
	if err := tx.Commit(); err != nil {
		return AppConfig{}, fmt.Errorf("commit downstream update transaction: %w", err)
	}
	return config, nil
}

func (r *Repository) UpdateAppConfig(ctx context.Context, input UpdateAppConfigInput) (AppConfig, error) {
	if _, err := r.db.ExecContext(ctx, `
		UPDATE app_config
		SET outbound_proxy_url = ?,
		    upstream_base_url = ?,
		    upstream_auth_mode = ?,
		    failover_threshold = ?,
		    cooldown_seconds = ?,
		    updated_at = CURRENT_TIMESTAMP
		WHERE id = 1
	`, input.OutboundProxyURL, input.UpstreamBaseURL, input.UpstreamAuthMode, input.FailoverThreshold, input.CooldownSeconds); err != nil {
		return AppConfig{}, fmt.Errorf("update app config: %w", err)
	}
	return r.GetAppConfig(ctx)
}

func (r *Repository) ListUpstreamKeys(ctx context.Context) ([]UpstreamKey, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, alias, key_hint, encrypted_api_key, is_enabled, priority, consecutive_failures, cooldown_until,
		       last_balance_total_granted, last_balance_total_used, last_balance_total_available,
		       last_balance_checked_at, last_error_summary, created_at, updated_at
		FROM upstream_keys
		ORDER BY priority ASC, id ASC
	`)
	if err != nil {
		return nil, fmt.Errorf("list upstream keys: %w", err)
	}
	defer rows.Close()

	var keys []UpstreamKey
	for rows.Next() {
		key, err := scanUpstreamKey(rows)
		if err != nil {
			return nil, err
		}
		keys = append(keys, key)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate upstream keys: %w", err)
	}
	return keys, nil
}

func (r *Repository) GetUpstreamKeyByID(ctx context.Context, id int64) (UpstreamKey, error) {
	row := r.db.QueryRowContext(ctx, `
		SELECT id, alias, key_hint, encrypted_api_key, is_enabled, priority, consecutive_failures, cooldown_until,
		       last_balance_total_granted, last_balance_total_used, last_balance_total_available,
		       last_balance_checked_at, last_error_summary, created_at, updated_at
		FROM upstream_keys
		WHERE id = ?
	`, id)
	key, err := scanUpstreamKey(row)
	if err != nil {
		return UpstreamKey{}, err
	}
	return key, nil
}

func (r *Repository) CreateUpstreamKey(ctx context.Context, alias, keyHint, encryptedAPIKey string, isEnabled bool) (UpstreamKey, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return UpstreamKey{}, fmt.Errorf("begin create upstream transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	priority, err := nextPriority(ctx, tx)
	if err != nil {
		return UpstreamKey{}, err
	}
	result, err := tx.ExecContext(ctx, `
		INSERT INTO upstream_keys (alias, key_hint, encrypted_api_key, is_enabled, priority)
		VALUES (?, ?, ?, ?, ?)
	`, alias, keyHint, encryptedAPIKey, boolToInt(isEnabled), priority)
	if err != nil {
		return UpstreamKey{}, fmt.Errorf("insert upstream key: %w", err)
	}
	id, err := result.LastInsertId()
	if err != nil {
		return UpstreamKey{}, fmt.Errorf("get inserted upstream key id: %w", err)
	}
	key, err := scanUpstreamKey(tx.QueryRowContext(ctx, `
		SELECT id, alias, key_hint, encrypted_api_key, is_enabled, priority, consecutive_failures, cooldown_until,
		       last_balance_total_granted, last_balance_total_used, last_balance_total_available,
		       last_balance_checked_at, last_error_summary, created_at, updated_at
		FROM upstream_keys
		WHERE id = ?
	`, id))
	if err != nil {
		return UpstreamKey{}, err
	}
	if err := tx.Commit(); err != nil {
		return UpstreamKey{}, fmt.Errorf("commit create upstream transaction: %w", err)
	}
	return key, nil
}

func (r *Repository) ReplaceUpstreamKey(ctx context.Context, id int64, alias, keyHint, encryptedAPIKey string, isEnabled bool) (UpstreamKey, error) {
	result, err := r.db.ExecContext(ctx, `
		UPDATE upstream_keys
		SET alias = ?, key_hint = ?, encrypted_api_key = ?, is_enabled = ?, updated_at = CURRENT_TIMESTAMP
		WHERE id = ?
	`, alias, keyHint, encryptedAPIKey, boolToInt(isEnabled), id)
	if err != nil {
		return UpstreamKey{}, fmt.Errorf("replace upstream key: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return UpstreamKey{}, fmt.Errorf("check replace upstream key rows affected: %w", err)
	}
	if affected == 0 {
		return UpstreamKey{}, ErrNotFound
	}
	return r.GetUpstreamKeyByID(ctx, id)
}

func (r *Repository) SetUpstreamEnabled(ctx context.Context, id int64, enabled bool) (UpstreamKey, error) {
	result, err := r.db.ExecContext(ctx, `
		UPDATE upstream_keys
		SET is_enabled = ?, updated_at = CURRENT_TIMESTAMP
		WHERE id = ?
	`, boolToInt(enabled), id)
	if err != nil {
		return UpstreamKey{}, fmt.Errorf("update upstream enabled state: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return UpstreamKey{}, fmt.Errorf("check upstream enabled rows affected: %w", err)
	}
	if affected == 0 {
		return UpstreamKey{}, ErrNotFound
	}
	return r.GetUpstreamKeyByID(ctx, id)
}

func (r *Repository) DeleteUpstreamKey(ctx context.Context, id int64) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin delete upstream transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	result, err := tx.ExecContext(ctx, `DELETE FROM upstream_keys WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("delete upstream key: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("check delete upstream rows affected: %w", err)
	}
	if affected == 0 {
		return ErrNotFound
	}
	if err := normalizePrioritiesTx(ctx, tx); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit delete upstream transaction: %w", err)
	}
	return nil
}

func (r *Repository) ReorderUpstreamKeys(ctx context.Context, ids []int64) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin reorder upstream transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	rows, err := tx.QueryContext(ctx, `SELECT id FROM upstream_keys ORDER BY priority ASC, id ASC`)
	if err != nil {
		return fmt.Errorf("list upstream ids: %w", err)
	}
	defer rows.Close()

	var existing []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return fmt.Errorf("scan upstream id: %w", err)
		}
		existing = append(existing, id)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate upstream ids: %w", err)
	}
	if len(existing) != len(ids) {
		return fmt.Errorf("reorder payload must include every upstream key exactly once")
	}
	seen := make(map[int64]struct{}, len(ids))
	for _, id := range ids {
		seen[id] = struct{}{}
	}
	for _, id := range existing {
		if _, ok := seen[id]; !ok {
			return fmt.Errorf("reorder payload missing upstream id %d", id)
		}
	}

	if _, err := tx.ExecContext(ctx, `UPDATE upstream_keys SET priority = priority + 1000000`); err != nil {
		return fmt.Errorf("shift upstream priorities: %w", err)
	}
	for index, id := range ids {
		if _, err := tx.ExecContext(ctx, `
			UPDATE upstream_keys
			SET priority = ?, updated_at = CURRENT_TIMESTAMP
			WHERE id = ?
		`, index+1, id); err != nil {
			return fmt.Errorf("update upstream priority for id %d: %w", id, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit reorder upstream transaction: %w", err)
	}
	return nil
}

func (r *Repository) RecordUpstreamResult(ctx context.Context, id int64, success bool, threshold int, cooldown time.Duration, failureSummary string) (UpstreamKey, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return UpstreamKey{}, fmt.Errorf("begin upstream result transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	key, err := scanUpstreamKey(tx.QueryRowContext(ctx, `
		SELECT id, alias, key_hint, encrypted_api_key, is_enabled, priority, consecutive_failures, cooldown_until,
		       last_balance_total_granted, last_balance_total_used, last_balance_total_available,
		       last_balance_checked_at, last_error_summary, created_at, updated_at
		FROM upstream_keys
		WHERE id = ?
	`, id))
	if err != nil {
		return UpstreamKey{}, err
	}

	if success {
		if _, err := tx.ExecContext(ctx, `
			UPDATE upstream_keys
			SET consecutive_failures = 0,
			    cooldown_until = NULL,
			    last_error_summary = '',
			    updated_at = CURRENT_TIMESTAMP
			WHERE id = ?
		`, id); err != nil {
			return UpstreamKey{}, fmt.Errorf("reset upstream failures: %w", err)
		}
	} else {
		newFailures := key.ConsecutiveFailures + 1
		var cooldownUntil any
		if newFailures >= threshold {
			cooldownUntil = time.Now().UTC().Add(cooldown)
		} else {
			cooldownUntil = key.CooldownUntil
		}
		if _, err := tx.ExecContext(ctx, `
			UPDATE upstream_keys
			SET consecutive_failures = ?,
			    cooldown_until = ?,
			    last_error_summary = ?,
			    updated_at = CURRENT_TIMESTAMP
			WHERE id = ?
		`, newFailures, cooldownUntil, truncateText(failureSummary, 512), id); err != nil {
			return UpstreamKey{}, fmt.Errorf("record upstream failure: %w", err)
		}
	}
	updated, err := scanUpstreamKey(tx.QueryRowContext(ctx, `
		SELECT id, alias, key_hint, encrypted_api_key, is_enabled, priority, consecutive_failures, cooldown_until,
		       last_balance_total_granted, last_balance_total_used, last_balance_total_available,
		       last_balance_checked_at, last_error_summary, created_at, updated_at
		FROM upstream_keys
		WHERE id = ?
	`, id))
	if err != nil {
		return UpstreamKey{}, err
	}
	if err := tx.Commit(); err != nil {
		return UpstreamKey{}, fmt.Errorf("commit upstream result transaction: %w", err)
	}
	return updated, nil
}

func (r *Repository) UpdateUpstreamBalance(ctx context.Context, id int64, balance UpstreamBalance) (UpstreamKey, error) {
	result, err := r.db.ExecContext(ctx, `
		UPDATE upstream_keys
		SET last_balance_total_granted = ?,
		    last_balance_total_used = ?,
		    last_balance_total_available = ?,
		    last_balance_checked_at = CURRENT_TIMESTAMP,
		    updated_at = CURRENT_TIMESTAMP
		WHERE id = ?
	`, balance.TotalGranted, balance.TotalUsed, balance.TotalAvailable, id)
	if err != nil {
		return UpstreamKey{}, fmt.Errorf("update upstream balance: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return UpstreamKey{}, fmt.Errorf("check update upstream balance rows affected: %w", err)
	}
	if affected == 0 {
		return UpstreamKey{}, ErrNotFound
	}
	return r.GetUpstreamKeyByID(ctx, id)
}

func (r *Repository) InsertRequestLog(ctx context.Context, log RequestLog) error {
	if log.StatusCode == http.StatusUnauthorized {
		return nil
	}
	var upstreamKeyID any
	if log.UpstreamKeyID != nil {
		upstreamKeyID = *log.UpstreamKeyID
	}
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO request_logs (
			request_ts, route, method, upstream_key_id, upstream_alias, model, request_id,
			status_code, success, failure_reason, latency_ms,
			input_tokens, output_tokens, total_tokens, client_ip, user_agent
		)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, log.RequestTS.UTC(), log.Route, log.Method, upstreamKeyID, log.UpstreamAlias, log.Model, log.RequestID,
		log.StatusCode, boolToInt(log.Success), truncateText(log.FailureReason, 512), log.LatencyMS,
		log.InputTokens, log.OutputTokens, log.TotalTokens, truncateText(log.ClientIP, 128), truncateText(log.UserAgent, 512))
	if err != nil {
		return fmt.Errorf("insert request log: %w", err)
	}
	return nil
}

func (r *Repository) ListRequestLogs(ctx context.Context, since time.Time, filter RequestLogFilter) ([]RequestLog, error) {
	limit := filter.Limit
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	offset := filter.Offset
	if offset < 0 {
		offset = 0
	}

	var query strings.Builder
	query.WriteString(`
		SELECT id, request_ts, route, method, upstream_key_id, upstream_alias, model, request_id,
		       status_code, success, failure_reason, latency_ms,
		       input_tokens, output_tokens, total_tokens, client_ip, user_agent
		FROM request_logs
		WHERE request_ts >= ?
	`)
	args := []any{since.UTC()}
	if filter.Route != "" {
		query.WriteString(` AND route = ?`)
		args = append(args, filter.Route)
	}
	switch filter.Result {
	case "success":
		query.WriteString(` AND success = 1`)
	case "failure":
		query.WriteString(` AND success = 0`)
	}
	query.WriteString(` ORDER BY request_ts DESC, id DESC LIMIT ? OFFSET ?`)
	args = append(args, limit, offset)

	rows, err := r.db.QueryContext(ctx, query.String(), args...)
	if err != nil {
		return nil, fmt.Errorf("list request logs: %w", err)
	}
	defer rows.Close()

	var logs []RequestLog
	for rows.Next() {
		logEntry, err := scanRequestLog(rows)
		if err != nil {
			return nil, err
		}
		logs = append(logs, logEntry)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate request logs: %w", err)
	}
	return logs, nil
}

func (r *Repository) Get24hStats(ctx context.Context, since time.Time) (RequestStats, error) {
	row := r.db.QueryRowContext(ctx, `
		SELECT COUNT(*), COALESCE(SUM(CASE WHEN success = 1 THEN 1 ELSE 0 END), 0),
		       COALESCE(SUM(CASE WHEN success = 0 THEN 1 ELSE 0 END), 0)
		FROM request_logs
		WHERE request_ts >= ?
	`, since.UTC())
	var stats RequestStats
	if err := row.Scan(&stats.Total, &stats.Successes, &stats.Failures); err != nil {
		return RequestStats{}, fmt.Errorf("load 24h stats: %w", err)
	}
	return stats, nil
}

func (r *Repository) CleanupRequestLogs(ctx context.Context, olderThan time.Time) (int64, error) {
	result, err := r.db.ExecContext(ctx, `DELETE FROM request_logs WHERE request_ts < ?`, olderThan.UTC())
	if err != nil {
		return 0, fmt.Errorf("cleanup request logs: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("check cleanup request log rows affected: %w", err)
	}
	return affected, nil
}

func nextPriority(ctx context.Context, tx *sql.Tx) (int, error) {
	var maxPriority sql.NullInt64
	if err := tx.QueryRowContext(ctx, `SELECT MAX(priority) FROM upstream_keys`).Scan(&maxPriority); err != nil {
		return 0, fmt.Errorf("load max upstream priority: %w", err)
	}
	if !maxPriority.Valid {
		return 1, nil
	}
	return int(maxPriority.Int64) + 1, nil
}

func normalizePrioritiesTx(ctx context.Context, tx *sql.Tx) error {
	rows, err := tx.QueryContext(ctx, `SELECT id FROM upstream_keys ORDER BY priority ASC, id ASC`)
	if err != nil {
		return fmt.Errorf("list upstream ids for normalization: %w", err)
	}
	defer rows.Close()

	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return fmt.Errorf("scan upstream id for normalization: %w", err)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate upstream ids for normalization: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `UPDATE upstream_keys SET priority = priority + 1000000`); err != nil {
		return fmt.Errorf("shift priorities for normalization: %w", err)
	}
	for index, id := range ids {
		if _, err := tx.ExecContext(ctx, `UPDATE upstream_keys SET priority = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?`, index+1, id); err != nil {
			return fmt.Errorf("normalize upstream priority for id %d: %w", id, err)
		}
	}
	return nil
}

func scanAppConfig(scanner interface{ Scan(dest ...any) error }) (AppConfig, error) {
	var config AppConfig
	if err := scanner.Scan(
		&config.ID,
		&config.DownstreamKeyHash,
		&config.AuthVersion,
		&config.OutboundProxyURL,
		&config.UpstreamBaseURL,
		&config.UpstreamAuthMode,
		&config.FailoverThreshold,
		&config.CooldownSeconds,
		&config.CreatedAt,
		&config.UpdatedAt,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return AppConfig{}, ErrNotFound
		}
		return AppConfig{}, fmt.Errorf("scan app config: %w", err)
	}
	return config, nil
}

func scanUpstreamKey(scanner interface{ Scan(dest ...any) error }) (UpstreamKey, error) {
	var (
		key                  UpstreamKey
		isEnabled            int
		cooldownUntil        sql.NullTime
		balanceGranted       sql.NullFloat64
		balanceUsed          sql.NullFloat64
		balanceAvailable     sql.NullFloat64
		lastBalanceCheckedAt sql.NullTime
	)
	if err := scanner.Scan(
		&key.ID,
		&key.Alias,
		&key.KeyHint,
		&key.EncryptedAPIKey,
		&isEnabled,
		&key.Priority,
		&key.ConsecutiveFailures,
		&cooldownUntil,
		&balanceGranted,
		&balanceUsed,
		&balanceAvailable,
		&lastBalanceCheckedAt,
		&key.LastErrorSummary,
		&key.CreatedAt,
		&key.UpdatedAt,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return UpstreamKey{}, ErrNotFound
		}
		return UpstreamKey{}, fmt.Errorf("scan upstream key: %w", err)
	}
	key.IsEnabled = isEnabled == 1
	if cooldownUntil.Valid {
		key.CooldownUntil = &cooldownUntil.Time
	}
	if balanceGranted.Valid {
		key.LastBalanceTotalGranted = &balanceGranted.Float64
	}
	if balanceUsed.Valid {
		key.LastBalanceTotalUsed = &balanceUsed.Float64
	}
	if balanceAvailable.Valid {
		key.LastBalanceTotalAvailable = &balanceAvailable.Float64
	}
	if lastBalanceCheckedAt.Valid {
		key.LastBalanceCheckedAt = &lastBalanceCheckedAt.Time
	}
	return key, nil
}

func scanRequestLog(scanner interface{ Scan(dest ...any) error }) (RequestLog, error) {
	var (
		entry         RequestLog
		upstreamKeyID sql.NullInt64
		success       int
	)
	if err := scanner.Scan(
		&entry.ID,
		&entry.RequestTS,
		&entry.Route,
		&entry.Method,
		&upstreamKeyID,
		&entry.UpstreamAlias,
		&entry.Model,
		&entry.RequestID,
		&entry.StatusCode,
		&success,
		&entry.FailureReason,
		&entry.LatencyMS,
		&entry.InputTokens,
		&entry.OutputTokens,
		&entry.TotalTokens,
		&entry.ClientIP,
		&entry.UserAgent,
	); err != nil {
		return RequestLog{}, fmt.Errorf("scan request log: %w", err)
	}
	if upstreamKeyID.Valid {
		entry.UpstreamKeyID = &upstreamKeyID.Int64
	}
	entry.Success = success == 1
	return entry, nil
}

func boolToInt(value bool) int {
	if value {
		return 1
	}
	return 0
}

func truncateText(value string, max int) string {
	value = strings.TrimSpace(value)
	if len(value) <= max {
		return value
	}
	return value[:max]
}
