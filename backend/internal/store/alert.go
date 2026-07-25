package store

import (
	"context"
	"fmt"
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
