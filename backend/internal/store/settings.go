package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// ChannelSettings is the API-safe view of alert-channel settings: it reports
// whether secrets exist but never carries them.
type ChannelSettings struct {
	TelegramTokenSet bool
	TelegramTemplate string
	WebsshURL        string

	// SMTP / email (non-secret fields + whether a password is stored).
	SMTPHost        string
	SMTPPort        int
	SMTPUsername    string
	SMTPFrom        string
	SMTPTLS         string // "none" | "starttls" | "tls"
	SMTPPasswordSet bool
	EmailSubject    string
	EmailBody       string
}

// smtpConfigJSON is the shape stored in settings.smtp_config (JSONB). The
// password holds the AES-GCM ciphertext (enc:v1:…), never plaintext.
type smtpConfigJSON struct {
	Host     string `json:"host"`
	Port     int    `json:"port"`
	Username string `json:"username"`
	From     string `json:"from"`
	TLS      string `json:"tls"`
	Password string `json:"password"` // enc:v1:… ciphertext, or ""
}

func (s *Store) GetChannelSettings(ctx context.Context) (ChannelSettings, error) {
	var token, tmpl, webssh, emailSubj, emailBody *string
	var smtpRaw []byte
	err := s.pool.QueryRow(ctx, `
		SELECT telegram_bot_token, telegram_template, webssh_url,
		       email_subject_template, email_body_template, smtp_config
		FROM settings WHERE id = 1`).
		Scan(&token, &tmpl, &webssh, &emailSubj, &emailBody, &smtpRaw)
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
	if emailSubj != nil {
		cs.EmailSubject = *emailSubj
	}
	if emailBody != nil {
		cs.EmailBody = *emailBody
	}
	if len(smtpRaw) > 0 {
		var sc smtpConfigJSON
		if err := json.Unmarshal(smtpRaw, &sc); err == nil {
			cs.SMTPHost, cs.SMTPPort, cs.SMTPUsername = sc.Host, sc.Port, sc.Username
			cs.SMTPFrom, cs.SMTPTLS = sc.From, sc.TLS
			cs.SMTPPasswordSet = sc.Password != ""
		}
	}
	return cs, nil
}

// SMTPUpdate is a whole-object SMTP write. Password: nil = keep the stored one
// (masked echo), "" = clear, else the new plaintext (sealed before it lands).
type SMTPUpdate struct {
	Host     string
	Port     int
	Username string
	From     string
	TLS      string
	Password *string
}

// ChannelUpdate carries optional channel settings; a nil field is left unchanged.
type ChannelUpdate struct {
	TelegramToken    *string
	TelegramTemplate *string
	WebsshURL        *string
	EmailSubject     *string
	EmailBody        *string
	SMTP             *SMTPUpdate
}

// UpdateChannelSettings applies write-only channel settings. Secrets (Telegram
// token, SMTP password) are sealed with AES-256-GCM before touching the DB;
// plaintext never lands in a row.
func (s *Store) UpdateChannelSettings(ctx context.Context, u ChannelUpdate) error {
	sets, args := []string{}, []any{}
	add := func(col string, val any) {
		args = append(args, val)
		sets = append(sets, fmt.Sprintf("%s = $%d", col, len(args)))
	}

	if u.WebsshURL != nil {
		add("webssh_url", nilIfZero(strings.TrimSpace(*u.WebsshURL)))
	}
	if u.TelegramToken != nil {
		val, err := s.sealOrNil(*u.TelegramToken)
		if err != nil {
			return err
		}
		add("telegram_bot_token", val)
	}
	if u.TelegramTemplate != nil {
		add("telegram_template", nilIfZero(*u.TelegramTemplate))
	}
	if u.EmailSubject != nil {
		add("email_subject_template", nilIfZero(*u.EmailSubject))
	}
	if u.EmailBody != nil {
		add("email_body_template", nilIfZero(*u.EmailBody))
	}
	if u.SMTP != nil {
		raw, err := s.buildSMTPConfig(ctx, *u.SMTP)
		if err != nil {
			return err
		}
		add("smtp_config", raw)
	}

	if len(sets) == 0 {
		return nil
	}
	sets = append(sets, "updated_at = now()")
	_, err := s.pool.Exec(ctx, "UPDATE settings SET "+strings.Join(sets, ", ")+" WHERE id = 1", args...)
	return err
}

// buildSMTPConfig assembles the smtp_config JSONB, preserving the stored password
// when the caller passes nil (masked echo) and sealing a new one otherwise.
func (s *Store) buildSMTPConfig(ctx context.Context, in SMTPUpdate) ([]byte, error) {
	sc := smtpConfigJSON{
		Host: strings.TrimSpace(in.Host), Port: in.Port,
		Username: strings.TrimSpace(in.Username), From: strings.TrimSpace(in.From),
		TLS: in.TLS,
	}
	switch {
	case in.Password == nil: // keep whatever is stored
		var raw []byte
		if err := s.pool.QueryRow(ctx, `SELECT smtp_config FROM settings WHERE id = 1`).Scan(&raw); err != nil {
			return nil, err
		}
		if len(raw) > 0 {
			var cur smtpConfigJSON
			if err := json.Unmarshal(raw, &cur); err == nil {
				sc.Password = cur.Password
			}
		}
	case *in.Password == "": // explicit clear
		sc.Password = ""
	default:
		sealed, err := s.sealOrNil(*in.Password)
		if err != nil {
			return nil, err
		}
		if sealed != nil {
			sc.Password = sealed.(string)
		}
	}
	return json.Marshal(sc)
}

// sealOrNil returns nil for an empty string (a cleared secret) or the AES-GCM
// ciphertext otherwise.
func (s *Store) sealOrNil(plaintext string) (any, error) {
	if plaintext == "" {
		return nil, nil
	}
	if s.secrets == nil {
		return nil, errors.New("encryption not configured")
	}
	enc, err := s.secrets.Encrypt(plaintext)
	if err != nil {
		return nil, err
	}
	return enc, nil
}
