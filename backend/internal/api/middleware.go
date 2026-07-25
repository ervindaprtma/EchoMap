package api

import (
	"context"
	"crypto/subtle"
	"errors"
	"net/http"
	"strings"

	"echomap/internal/store"
)

// Role is the hierarchical RBAC level (PRD §2b P11). Higher wins.
type Role int

const (
	RoleNone Role = iota
	RoleOperator
	RoleAdmin
	RoleSuperadmin
)

func parseRole(s string) Role {
	switch s {
	case "SUPERADMIN":
		return RoleSuperadmin
	case "ADMIN":
		return RoleAdmin
	case "OPERATOR":
		return RoleOperator
	default:
		return RoleNone
	}
}

// principal is the authenticated caller attached to the request context. For the
// static automation token, UserID/SessionID are nil and Role is Superadmin.
type principal struct {
	UserID    *int64
	Username  string
	Role      Role
	SessionID *int64
}

const sessionCookie = "echomap_session"

type ctxKey int

const principalKey ctxKey = 0

func principalFrom(ctx context.Context) principal {
	p, _ := ctx.Value(principalKey).(principal)
	return p
}

// authenticate resolves identity from the session cookie first, then the static
// bearer token (headless automation), and attaches a principal to the context.
// It does NOT authorize — per-route minimums are enforced by gate().
func (s *server) authenticate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if c, err := r.Cookie(sessionCookie); err == nil && c.Value != "" {
			u, sid, err := s.store.SessionUser(r.Context(), c.Value)
			if err == nil {
				p := principal{UserID: &u.ID, Username: u.Username, Role: parseRole(u.Role), SessionID: &sid}
				next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), principalKey, p)))
				return
			}
			// fall through to token auth; an invalid cookie isn't fatal on its own
		}

		token := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		if s.cfg.APIToken != "" && token != "" &&
			subtle.ConstantTimeCompare([]byte(token), []byte(s.cfg.APIToken)) == 1 {
			p := principal{Username: "automation", Role: RoleSuperadmin}
			next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), principalKey, p)))
			return
		}

		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "authentication required"})
	})
}

// gate wraps a handler with a minimum-role check. authenticate must run first.
func (s *server) gate(min Role, h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if principalFrom(r.Context()).Role < min {
			writeJSON(w, http.StatusForbidden, map[string]string{"error": "insufficient role"})
			return
		}
		h(w, r)
	}
}

// logEvent writes an event-log row, swallowing errors so logging never breaks the
// operation it records (Doc 3 §10).
func (s *server) logEvent(r *http.Request, level, category, message string, deviceID *int64, meta map[string]any) {
	var userID *int64
	if p := principalFrom(r.Context()); p.UserID != nil {
		userID = p.UserID
	}
	if err := s.store.InsertEventLog(r.Context(), level, category, message, deviceID, userID, meta); err != nil {
		// best-effort; the request itself already succeeded
		_ = err
	}
}

// writeAuthErr maps auth/store errors to status codes for the Phase 8 handlers.
func writeAuthErr(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, store.ErrNotFound):
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
	case errors.Is(err, store.ErrConflict):
		writeJSON(w, http.StatusConflict, map[string]string{"error": "already exists"})
	case errors.Is(err, store.ErrLastSuperadmin):
		writeJSON(w, http.StatusConflict, map[string]string{"error": err.Error()})
	default:
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
	}
}
