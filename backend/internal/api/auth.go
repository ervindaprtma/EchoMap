package api

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"log"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"echomap/internal/auth"
	"echomap/internal/store"
)

// ---- login rate limiter (PRD §4.6) ----
// ponytail: in-memory fixed-window counter keyed by IP. Single-node deployment,
// so a shared store isn't worth it; entries self-expire, no background sweeper.
type loginLimiter struct {
	mu     sync.Mutex
	hits   map[string]*window
	max    int
	window time.Duration
}

type window struct {
	count int
	reset time.Time
}

func newLoginLimiter() *loginLimiter {
	return &loginLimiter{hits: map[string]*window{}, max: 10, window: time.Minute}
}

// allow reports whether this key may attempt now, counting the attempt.
func (l *loginLimiter) allow(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now()
	w := l.hits[key]
	if w == nil || now.After(w.reset) {
		l.hits[key] = &window{count: 1, reset: now.Add(l.window)}
		return true
	}
	w.count++
	return w.count <= l.max
}

func clientIP(r *http.Request) string {
	if host, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
		return host
	}
	return r.RemoteAddr
}

// setSessionCookie issues the HttpOnly session cookie. Secure tracks the actual
// connection so it works over plain-HTTP dev yet is Secure behind TLS in prod.
func setSessionCookie(w http.ResponseWriter, r *http.Request, raw string) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookie,
		Value:    raw,
		Path:     "/",
		HttpOnly: true,
		Secure:   r.TLS != nil,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   int(store.SessionMaxLifetime.Seconds()),
	})
}

func clearSessionCookie(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookie,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		Secure:   r.TLS != nil,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   -1,
	})
}

func randomToken() (string, error) {
	b := make([]byte, 32) // 256 bits
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// meResponse is the SPA's identity source (drives useRole).
type meResponse struct {
	ID                 *int64 `json:"id"`
	Username           string `json:"username"`
	Role               string `json:"role"`
	MustChangePassword bool   `json:"must_change_password"`
}

// login is unauthenticated (registered outside the authenticated subtree).
func (s *server) login(w http.ResponseWriter, r *http.Request) {
	ip := clientIP(r)
	if !s.limiter.allow(ip) {
		writeJSON(w, http.StatusTooManyRequests, map[string]string{"error": "too many attempts, try again later"})
		return
	}
	var body struct{ Username, Password string }
	if !readJSON(w, r, &body) {
		return
	}
	body.Username = strings.TrimSpace(body.Username)

	// One generic error for every failure mode: no username-existence oracle.
	fail := func() {
		s.logEvent(r, "AUDIT", "auth", "failed login for "+body.Username, nil, map[string]any{"ip": ip})
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "invalid username or password"})
	}

	u, err := s.store.UserByUsername(r.Context(), body.Username)
	if err != nil || !u.Enabled {
		fail()
		return
	}
	if err := auth.VerifyPassword(body.Password, u.PasswordHash); err != nil {
		fail()
		return
	}

	raw, err := randomToken()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
		return
	}
	if err := s.store.CreateSession(r.Context(), raw, u.ID, ip, r.UserAgent()); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
		return
	}
	setSessionCookie(w, r, raw)
	_ = s.store.InsertEventLog(r.Context(), "AUDIT", "auth", "login", nil, &u.ID, map[string]any{"ip": ip})
	writeJSON(w, http.StatusOK, meResponse{ID: &u.ID, Username: u.Username, Role: u.Role, MustChangePassword: u.MustChangePassword})
}

func (s *server) logout(w http.ResponseWriter, r *http.Request) {
	p := principalFrom(r.Context())
	if p.SessionID != nil {
		_ = s.store.DeleteSession(r.Context(), *p.SessionID, nil)
	}
	clearSessionCookie(w, r)
	s.logEvent(r, "AUDIT", "auth", "logout", nil, nil)
	w.WriteHeader(http.StatusNoContent)
}

func (s *server) me(w http.ResponseWriter, r *http.Request) {
	p := principalFrom(r.Context())
	resp := meResponse{Username: p.Username, Role: roleName(p.Role)}
	if p.UserID != nil {
		// reload so must_change_password reflects a mid-session admin reset
		if u, err := s.store.UserByID(r.Context(), *p.UserID); err == nil {
			resp.ID = &u.ID
			resp.Username = u.Username
			resp.Role = u.Role
			resp.MustChangePassword = u.MustChangePassword
		}
	}
	writeJSON(w, http.StatusOK, resp)
}

func (s *server) changePassword(w http.ResponseWriter, r *http.Request) {
	p := principalFrom(r.Context())
	if p.UserID == nil {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "automation token cannot change a password"})
		return
	}
	var body struct{ Current, Next string }
	if !readJSON(w, r, &body) {
		return
	}
	if len(body.Next) < 8 {
		writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": "new password must be at least 8 characters"})
		return
	}
	u, err := s.store.UserByID(r.Context(), *p.UserID)
	if err != nil {
		writeAuthErr(w, err)
		return
	}
	if err := auth.VerifyPassword(body.Current, u.PasswordHash); err != nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "current password is incorrect"})
		return
	}
	hash, err := auth.HashPassword(body.Next)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
		return
	}
	if _, err := s.store.UpdateUser(r.Context(), u.ID, nil, nil, &hash, true); err != nil {
		writeAuthErr(w, err)
		return
	}
	// Rotate: invalidate this user's OTHER sessions after a password change.
	except := int64(0)
	if p.SessionID != nil {
		except = *p.SessionID
	}
	_, _ = s.store.DeleteUserSessions(r.Context(), u.ID, except)
	s.logEvent(r, "AUDIT", "auth", "password changed", nil, nil)
	w.WriteHeader(http.StatusNoContent)
}

func (s *server) listSessions(w http.ResponseWriter, r *http.Request) {
	p := principalFrom(r.Context())
	var scope *int64
	// Default: only my own sessions. Superadmin may request all.
	if !(r.URL.Query().Get("scope") == "all" && p.Role >= RoleSuperadmin) {
		scope = p.UserID
	}
	cur := int64(0)
	if p.SessionID != nil {
		cur = *p.SessionID
	}
	sessions, err := s.store.ListSessions(r.Context(), scope, cur)
	if err != nil {
		writeAuthErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": sessions})
}

func (s *server) deleteSession(w http.ResponseWriter, r *http.Request) {
	id, ok := idParam(w, r)
	if !ok {
		return
	}
	p := principalFrom(r.Context())
	var owner *int64 // nil = unscoped (Superadmin may revoke anyone's)
	if p.Role < RoleSuperadmin {
		owner = p.UserID
	}
	if err := s.store.DeleteSession(r.Context(), id, owner); err != nil {
		writeAuthErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func roleName(r Role) string {
	switch r {
	case RoleSuperadmin:
		return "SUPERADMIN"
	case RoleAdmin:
		return "ADMIN"
	case RoleOperator:
		return "OPERATOR"
	default:
		return ""
	}
}

// bootstrapSuperadmin seeds the first account (username "admin") from
// APP_ADMIN_PASSWORD on an empty users table. Called once at api boot; a no-op
// once any user exists. Without the env var nothing is seeded and only the
// static automation token works — deliberate, so an unset password never yields
// a default-credential account.
func (s *server) bootstrapSuperadmin(ctx context.Context) error {
	if s.cfg.AdminPassword == "" {
		return nil
	}
	hash, err := auth.HashPassword(s.cfg.AdminPassword)
	if err != nil {
		return err
	}
	created, err := s.store.EnsureBootstrapSuperadmin(ctx, "admin", hash)
	if err != nil {
		return err
	}
	if created {
		log.Printf("auth: seeded bootstrap Superadmin 'admin' (must change password on first login)")
	}
	return nil
}
