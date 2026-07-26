package api

import (
	"net/http"
	"strings"

	"echomap/internal/nbsync"
	"echomap/internal/netbox"
	"echomap/internal/store"
)

// GET /settings/netbox — url + whether a token is stored (masked) + sync toggle.
func (s *server) getNetboxSettings(w http.ResponseWriter, r *http.Request) {
	ns, err := s.store.GetNetboxSettings(r.Context())
	if err != nil {
		writeErr(w, err)
		return
	}
	token := ""
	if ns.TokenSet {
		token = maskedToken
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"url":          ns.URL,
		"token":        token,
		"sync_enabled": ns.SyncEnabled,
	})
}

// PUT /settings/netbox — write-only token; masked echo = unchanged.
func (s *server) putNetboxSettings(w http.ResponseWriter, r *http.Request) {
	var body struct {
		URL         *string `json:"url"`
		Token       *string `json:"token"`
		SyncEnabled *bool   `json:"sync_enabled"`
	}
	if !readJSON(w, r, &body) {
		return
	}
	u := store.NetboxUpdate{URL: body.URL, SyncEnabled: body.SyncEnabled}
	if body.Token != nil && *body.Token != maskedToken {
		u.Token = body.Token
	}
	if err := s.store.UpdateNetboxSettings(r.Context(), u); err != nil {
		writeErr(w, err)
		return
	}
	s.logEvent(r, "NOTICE", "config", "updated Netbox settings", nil, nil)
	s.getNetboxSettings(w, r)
}

// POST /settings/netbox/test — verify URL + token reach Netbox.
func (s *server) testNetbox(w http.ResponseWriter, r *http.Request) {
	url, token, _, err := s.store.NetboxCredentials(r.Context())
	if err != nil {
		writeErr(w, err)
		return
	}
	if err := netbox.New(url, token).Test(r.Context()); err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "message": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "message": "connection OK"})
}

// POST /settings/netbox/sync — run one sync now (Admin), returning its result.
// Same diff the hourly worker runs; handy for "I just changed Netbox, sync now".
func (s *server) syncNetboxNow(w http.ResponseWriter, r *http.Request) {
	url, token, _, err := s.store.NetboxCredentials(r.Context())
	if err != nil {
		writeErr(w, err)
		return
	}
	if url == "" || token == "" {
		writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": "Netbox url and token are required"})
		return
	}
	l, err := nbsync.Run(r.Context(), s.store, s.bus, netbox.New(url, token))
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
		return
	}
	s.logEvent(r, "NOTICE", "config", "ran Netbox sync (manual)", nil, nil)
	writeJSON(w, http.StatusOK, l)
}

// GET /sync-logs — recent Netbox sync runs.
func (s *server) listSyncLogs(w http.ResponseWriter, r *http.Request) {
	logs, err := s.store.ListSyncLogs(r.Context(), 50)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": logs})
}

// POST /devices/{id}/detach-netbox — keep monitoring, convert NETBOX → MANUAL,
// lift out of ORPHANED (Doc 3 §3 resolution). Reversible-ish: re-import re-links.
func (s *server) detachNetbox(w http.ResponseWriter, r *http.Request) {
	id, ok := idParam(w, r)
	if !ok {
		return
	}
	if _, err := s.store.GetDevice(r.Context(), id); err != nil {
		writeErr(w, err)
		return
	}
	if err := s.store.DetachNetbox(r.Context(), id); err != nil {
		writeErr(w, err)
		return
	}
	s.logEvent(r, "NOTICE", "config", "detached device from Netbox", &id, nil)
	dev, err := s.store.GetDevice(r.Context(), id)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, dev)
}

// POST /devices/{id}/confirm-delete — hard-delete, but ONLY an ORPHANED device
// (safety: the 2-step orphan resolution, Doc 3 §3 / Doc 5). UI double-confirms.
func (s *server) confirmDeleteOrphan(w http.ResponseWriter, r *http.Request) {
	id, ok := idParam(w, r)
	if !ok {
		return
	}
	dev, err := s.store.GetDevice(r.Context(), id)
	if err != nil {
		writeErr(w, err)
		return
	}
	if !strings.EqualFold(dev.Status, "ORPHANED") {
		writeJSON(w, http.StatusUnprocessableEntity,
			map[string]string{"error": "confirm-delete is only valid for an ORPHANED device"})
		return
	}
	if err := s.store.DeleteDevice(r.Context(), id); err != nil {
		writeErr(w, err)
		return
	}
	s.logEvent(r, "NOTICE", "config", "confirm-deleted orphaned device "+dev.Name, nil, nil)
	writeJSON(w, http.StatusOK, map[string]any{"id": id, "deleted": true})
}
