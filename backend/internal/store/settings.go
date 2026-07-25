package store

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

// ChannelSettings is the API-safe view of alert-channel settings: it reports
// whether a token exists but never carries the token itself.
type ChannelSettings struct {
	TelegramTokenSet bool
	TelegramTemplate string
	WebsshURL        string
}

func (s *Store) GetChannelSettings(ctx context.Context) (ChannelSettings, error) {
	var token, tmpl, webssh *string
	err := s.pool.QueryRow(ctx,
		`SELECT telegram_bot_token, telegram_template, webssh_url FROM settings WHERE id = 1`).
		Scan(&token, &tmpl, &webssh)
	if err != nil {
		return ChannelSettings{}, err
	}
	cs := ChannelSettings{TelegramTokenSet: token != nil && *token != ""}
	if tmpl != nil {
		cs.TelegramTemplate = *tmpl
	}
	if webssh != nil {
		cs.WebsshURL = *webssh
	}
	return cs, nil
}

// UpdateChannelSettings applies write-only channel settings. nil = unchanged,
// "" = clear. A non-empty token is sealed with AES-256-GCM before it touches
// the database; plaintext never lands in a row.
func (s *Store) UpdateChannelSettings(ctx context.Context, token, tmpl, websshURL *string) error {
	sets, args := []string{}, []any{}
	if websshURL != nil {
		args = append(args, nilIfZero(strings.TrimSpace(*websshURL)))
		sets = append(sets, fmt.Sprintf("webssh_url = $%d", len(args)))
	}
	if token != nil {
		var val any
		if *token != "" {
			if s.secrets == nil {
				return errors.New("encryption not configured")
			}
			enc, err := s.secrets.Encrypt(*token)
			if err != nil {
				return err
			}
			val = enc
		}
		args = append(args, val)
		sets = append(sets, fmt.Sprintf("telegram_bot_token = $%d", len(args)))
	}
	if tmpl != nil {
		args = append(args, nilIfZero(*tmpl))
		sets = append(sets, fmt.Sprintf("telegram_template = $%d", len(args)))
	}
	if len(sets) == 0 {
		return nil
	}
	sets = append(sets, "updated_at = now()")
	_, err := s.pool.Exec(ctx, "UPDATE settings SET "+strings.Join(sets, ", ")+" WHERE id = 1", args...)
	return err
}
