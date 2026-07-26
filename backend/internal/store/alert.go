package store

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// TelegramConfig is what the notifier needs for one alert: bot token, optional
// operator template (Doc 3 §7), and the enabled TELEGRAM rules for the device.
type TelegramConfig struct {
	BotToken string
	Template string
	Rules    []TelegramRule
}

type TelegramRule struct {
	ChatID string
	OnDown bool
	OnUp   bool
}

// TelegramConfigFor loads settings + the enabled TELEGRAM rules that apply to
// deviceID (global rules with device_id NULL, plus device-scoped ones).
// The bot token is decrypted here — this is the only place it leaves the store
// in plaintext, and only toward the notifier, never toward the API.
func (s *Store) TelegramConfigFor(ctx context.Context, deviceID int64) (TelegramConfig, error) {
	var cfg TelegramConfig
	var token, tmpl *string
	err := s.pool.QueryRow(ctx,
		`SELECT telegram_bot_token, telegram_template FROM settings WHERE id = 1`).Scan(&token, &tmpl)
	if err != nil {
		return cfg, err
	}
	if token != nil {
		plain, err := s.secrets.Decrypt(*token)
		if err != nil {
			return cfg, fmt.Errorf("decrypt telegram_bot_token (key rotated?): %w", err)
		}
		cfg.BotToken = plain
	}
	if tmpl != nil {
		cfg.Template = *tmpl
	}
	if cfg.BotToken == "" {
		return cfg, nil // Telegram not configured; rules are irrelevant
	}

	rows, err := s.pool.Query(ctx, `
		SELECT target, on_down, on_up FROM alert_rules
		WHERE channel = 'TELEGRAM' AND enabled AND (device_id IS NULL OR device_id = $1)`, deviceID)
	if err != nil {
		return cfg, err
	}
	defer rows.Close()

	for rows.Next() {
		var r TelegramRule
		if err := rows.Scan(&r.ChatID, &r.OnDown, &r.OnUp); err != nil {
			return cfg, err
		}
		cfg.Rules = append(cfg.Rules, r)
	}
	return cfg, rows.Err()
}

// AlertRuleRow is one configurable alert rule (Doc 5 §8). device_id NULL = a
// global rule that applies to every device; device_name is joined for display.
type AlertRuleRow struct {
	ID         int64   `json:"id"`
	DeviceID   *int64  `json:"device_id"`
	DeviceName *string `json:"device_name"`
	Channel    string  `json:"channel"` // EMAIL | TELEGRAM
	Target     string  `json:"target"`
	OnDown     bool    `json:"on_down"`
	OnUp       bool    `json:"on_up"`
	OnFlapping bool    `json:"on_flapping"`
	OnOrphaned bool    `json:"on_orphaned"`
	Enabled    bool    `json:"enabled"`
}

func (s *Store) ListAlertRules(ctx context.Context) ([]AlertRuleRow, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT ar.id, ar.device_id, d.name, ar.channel::text, ar.target,
		       ar.on_down, ar.on_up, ar.on_flapping, ar.on_orphaned, ar.enabled
		FROM alert_rules ar
		LEFT JOIN devices d ON d.id = ar.device_id
		ORDER BY ar.device_id NULLS FIRST, ar.id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []AlertRuleRow{}
	for rows.Next() {
		var r AlertRuleRow
		if err := rows.Scan(&r.ID, &r.DeviceID, &r.DeviceName, &r.Channel, &r.Target,
			&r.OnDown, &r.OnUp, &r.OnFlapping, &r.OnOrphaned, &r.Enabled); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// CreateAlertRule inserts a rule and returns it with device_name filled in.
func (s *Store) CreateAlertRule(ctx context.Context, in AlertRuleRow) (*AlertRuleRow, error) {
	err := s.pool.QueryRow(ctx, `
		WITH ins AS (
			INSERT INTO alert_rules (device_id, channel, target, on_down, on_up, on_flapping, on_orphaned, enabled)
			VALUES ($1, $2::alert_channel, $3, $4, $5, $6, $7, $8)
			RETURNING id, device_id
		)
		SELECT ins.id, d.name FROM ins LEFT JOIN devices d ON d.id = ins.device_id`,
		in.DeviceID, in.Channel, in.Target, in.OnDown, in.OnUp, in.OnFlapping, in.OnOrphaned, in.Enabled,
	).Scan(&in.ID, &in.DeviceName)
	if err != nil {
		return nil, err
	}
	return &in, nil
}

// PatchAlertRuleInput carries only the mutable fields; nil = leave unchanged.
// device_id (scope) is not patchable — delete and re-create to move a rule.
type PatchAlertRuleInput struct {
	Channel    *string
	Target     *string
	OnDown     *bool
	OnUp       *bool
	OnFlapping *bool
	OnOrphaned *bool
	Enabled    *bool
}

func (s *Store) UpdateAlertRule(ctx context.Context, id int64, in PatchAlertRuleInput) (*AlertRuleRow, error) {
	var r AlertRuleRow
	err := s.pool.QueryRow(ctx, `
		UPDATE alert_rules ar SET
			channel     = COALESCE($2::alert_channel, ar.channel),
			target      = COALESCE($3, ar.target),
			on_down     = COALESCE($4, ar.on_down),
			on_up       = COALESCE($5, ar.on_up),
			on_flapping = COALESCE($6, ar.on_flapping),
			on_orphaned = COALESCE($7, ar.on_orphaned),
			enabled     = COALESCE($8, ar.enabled)
		WHERE ar.id = $1
		RETURNING ar.id, ar.device_id,
		          (SELECT name FROM devices WHERE id = ar.device_id),
		          ar.channel::text, ar.target, ar.on_down, ar.on_up, ar.on_flapping, ar.on_orphaned, ar.enabled`,
		id, in.Channel, in.Target, in.OnDown, in.OnUp, in.OnFlapping, in.OnOrphaned, in.Enabled,
	).Scan(&r.ID, &r.DeviceID, &r.DeviceName, &r.Channel, &r.Target,
		&r.OnDown, &r.OnUp, &r.OnFlapping, &r.OnOrphaned, &r.Enabled)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &r, nil
}

func (s *Store) DeleteAlertRule(ctx context.Context, id int64) error {
	ct, err := s.pool.Exec(ctx, `DELETE FROM alert_rules WHERE id = $1`, id)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// RecordAlertEvent audits one delivery attempt (alert_events, Doc 2 §1.7).
func (s *Store) RecordAlertEvent(ctx context.Context, deviceID int64, from, to string, flapping, delivered bool, errMsg string) error {
	var e *string
	if errMsg != "" {
		e = &errMsg
	}
	_, err := s.pool.Exec(ctx, `
		INSERT INTO alert_events (device_id, from_status, to_status, is_flapping, channel, delivered, error)
		VALUES ($1, NULLIF($2, '')::device_status, $3::device_status, $4, 'TELEGRAM', $5, $6)`,
		deviceID, from, to, flapping, delivered, e)
	return err
}
