package api

import (
	"net/http"
	"strconv"

	"echomap/internal/store"
)

// listLogs serves GET /logs with server-side filtering + pagination (Doc 5 §8.3).
// AUDIT rows are included only for Superadmin callers, regardless of the level filter.
func (s *server) listLogs(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	f := store.LogFilter{
		Level:        q.Get("level"),
		Category:     q.Get("category"),
		Search:       q.Get("search"),
		IncludeAudit: principalFrom(r.Context()).Role >= RoleSuperadmin,
		Page:         atoiDefault(q.Get("page"), 1),
		PageSize:     atoiDefault(q.Get("page_size"), 50),
	}
	if f.PageSize > 200 {
		f.PageSize = 200
	}
	if v := q.Get("device_id"); v != "" {
		if id, err := strconv.ParseInt(v, 10, 64); err == nil {
			f.DeviceID = &id
		}
	}
	if v := q.Get("user_id"); v != "" {
		if id, err := strconv.ParseInt(v, 10, 64); err == nil {
			f.UserID = &id
		}
	}

	logs, total, err := s.store.ListEventLogs(r.Context(), f)
	if err != nil {
		writeAuthErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"items": logs, "total": total, "page": f.Page, "page_size": f.PageSize,
	})
}
