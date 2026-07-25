package api

import (
	"net/http"
	"strings"

	"echomap/internal/auth"
)

var validRoles = map[string]bool{"SUPERADMIN": true, "ADMIN": true, "OPERATOR": true}

func (s *server) listUsers(w http.ResponseWriter, r *http.Request) {
	users, err := s.store.ListUsers(r.Context())
	if err != nil {
		writeAuthErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": users})
}

func (s *server) createUser(w http.ResponseWriter, r *http.Request) {
	var body struct{ Username, Password, Role string }
	if !readJSON(w, r, &body) {
		return
	}
	body.Username = strings.TrimSpace(body.Username)
	if body.Username == "" || len(body.Password) < 8 {
		writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": "username required and password must be at least 8 characters"})
		return
	}
	if body.Role == "" {
		body.Role = "OPERATOR"
	}
	if !validRoles[body.Role] {
		writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": "role must be SUPERADMIN, ADMIN or OPERATOR"})
		return
	}
	hash, err := auth.HashPassword(body.Password)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
		return
	}
	u, err := s.store.CreateUser(r.Context(), body.Username, hash, body.Role)
	if err != nil {
		writeAuthErr(w, err)
		return
	}
	s.logEvent(r, "AUDIT", "auth", "created user "+u.Username+" ("+u.Role+")", nil, map[string]any{"target_user_id": u.ID})
	writeJSON(w, http.StatusCreated, u)
}

func (s *server) patchUser(w http.ResponseWriter, r *http.Request) {
	id, ok := idParam(w, r)
	if !ok {
		return
	}
	var body struct {
		Role     *string `json:"role"`
		Enabled  *bool   `json:"enabled"`
		Password *string `json:"password"`
	}
	if !readJSON(w, r, &body) {
		return
	}
	if body.Role != nil && !validRoles[*body.Role] {
		writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": "invalid role"})
		return
	}

	// Self-guard: a Superadmin can't demote or disable their own account (the
	// store separately blocks removing the last enabled Superadmin).
	if p := principalFrom(r.Context()); p.UserID != nil && *p.UserID == id {
		if (body.Role != nil && *body.Role != "SUPERADMIN") || (body.Enabled != nil && !*body.Enabled) {
			writeJSON(w, http.StatusConflict, map[string]string{"error": "you cannot demote or disable your own account"})
			return
		}
	}

	var hash *string
	if body.Password != nil {
		if len(*body.Password) < 8 {
			writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": "password must be at least 8 characters"})
			return
		}
		h, err := auth.HashPassword(*body.Password)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
			return
		}
		hash = &h
	}
	u, err := s.store.UpdateUser(r.Context(), id, body.Role, body.Enabled, hash, false)
	if err != nil {
		writeAuthErr(w, err)
		return
	}
	// A disabled or password-reset account should not keep live sessions.
	if (body.Enabled != nil && !*body.Enabled) || hash != nil {
		_, _ = s.store.DeleteUserSessions(r.Context(), id, 0)
	}
	s.logEvent(r, "AUDIT", "auth", "updated user "+u.Username, nil, map[string]any{"target_user_id": u.ID})
	writeJSON(w, http.StatusOK, u)
}

func (s *server) deleteUser(w http.ResponseWriter, r *http.Request) {
	id, ok := idParam(w, r)
	if !ok {
		return
	}
	if p := principalFrom(r.Context()); p.UserID != nil && *p.UserID == id {
		writeJSON(w, http.StatusConflict, map[string]string{"error": "you cannot delete your own account"})
		return
	}
	if err := s.store.DeleteUser(r.Context(), id); err != nil {
		writeAuthErr(w, err)
		return
	}
	s.logEvent(r, "AUDIT", "auth", "deleted user", nil, map[string]any{"target_user_id": id})
	writeJSON(w, http.StatusOK, map[string]any{"id": id, "deleted": true})
}
