package api

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"net/netip"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5/pgconn"

	"echomap/internal/ping"
	"echomap/internal/store"
)

func (s *server) listDevices(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	f := store.ListFilter{
		Status:     q.Get("status"),
		Site:       q.Get("site"),
		SourceType: q.Get("source_type"),
		Search:     q.Get("search"),
		Page:       atoiDefault(q.Get("page"), 0),
		PageSize:   atoiDefault(q.Get("page_size"), 0),
	}
	f.Normalize()

	items, total, err := s.store.ListDevices(r.Context(), f)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"items": items, "total": total, "page": f.Page, "page_size": f.PageSize,
	})
}

func (s *server) createDevice(w http.ResponseWriter, r *http.Request) {
	var body struct {
		IPAddress      string `json:"ip_address"`
		Hostname       string `json:"hostname"`
		Name           string `json:"name"`
		SubnetID       *int64 `json:"subnet_id"`
		ParentDeviceID *int64 `json:"parent_device_id"`
		MapID          *int64 `json:"map_id"`
		IconID         *int64 `json:"icon_id"`
		SNMPEnabled    bool   `json:"snmp_enabled"`
	}
	if !readJSON(w, r, &body) {
		return
	}

	// Exactly one of ip_address / hostname identifies the device (Doc 5 §1).
	// A hostname is resolved synchronously here; the resolver loop keeps it fresh.
	ip, hostname := strings.TrimSpace(body.IPAddress), strings.TrimSpace(body.Hostname)
	switch {
	case ip == "" && hostname == "":
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "one of ip_address or hostname is required"})
		return
	case ip != "" && hostname != "":
		// Exactly one: with both, the resolver would silently overwrite the
		// typed IP on its next tick — reject instead of surprising later.
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "provide either ip_address or hostname, not both"})
		return
	case ip != "":
		if _, err := netip.ParseAddr(ip); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid ip_address"})
			return
		}
	default:
		resolved, err := ping.Resolve4(r.Context(), hostname)
		if err != nil {
			writeJSON(w, http.StatusUnprocessableEntity,
				map[string]string{"error": "hostname does not resolve to an IPv4 address: " + hostname})
			return
		}
		ip = resolved
	}

	in := store.CreateDeviceInput{
		IPAddress:      ip,
		SubnetID:       body.SubnetID,
		ParentDeviceID: body.ParentDeviceID,
		MapID:          body.MapID,
		IconID:         body.IconID,
		SNMPEnabled:    body.SNMPEnabled,
	}
	if hostname != "" {
		in.Hostname = &hostname
	}
	// Naming fallback: typed name wins as MANUAL, then hostname, then the IP.
	if name := strings.TrimSpace(body.Name); name != "" {
		in.Name, in.NameSource, in.ManualName = name, "MANUAL", &name
	} else if hostname != "" {
		in.Name, in.NameSource = hostname, "IP"
	} else {
		in.Name, in.NameSource = in.IPAddress, "IP"
	}

	dev, err := s.store.CreateDevice(r.Context(), in)
	if err != nil {
		writeErr(w, err)
		return
	}
	s.logEvent(r, "NOTICE", "config", "added device "+dev.Name+" ("+dev.IPAddress+")", &dev.ID, nil)
	writeJSON(w, http.StatusCreated, dev)
}

func (s *server) getDevice(w http.ResponseWriter, r *http.Request) {
	id, ok := idParam(w, r)
	if !ok {
		return
	}
	dev, err := s.store.GetDevice(r.Context(), id)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, dev)
}

func (s *server) patchDevice(w http.ResponseWriter, r *http.Request) {
	id, ok := idParam(w, r)
	if !ok {
		return
	}
	// For the nullable references, 0 clears the value (and "" clears hostname);
	// omitting the key leaves it unchanged.
	var body struct {
		Name           *string `json:"name"`
		Hostname       *string `json:"hostname"`
		ParentDeviceID *int64  `json:"parent_device_id"`
		MapID          *int64  `json:"map_id"`
		IconID         *int64  `json:"icon_id"`
		SNMPEnabled    *bool   `json:"snmp_enabled"`
	}
	if !readJSON(w, r, &body) {
		return
	}
	dev, err := s.store.PatchDevice(r.Context(), id, store.PatchDeviceInput{
		Name: body.Name, Hostname: body.Hostname,
		ParentDeviceID: body.ParentDeviceID, MapID: body.MapID, IconID: body.IconID,
		SNMPEnabled: body.SNMPEnabled,
	})
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, dev)
}

func (s *server) deleteDevice(w http.ResponseWriter, r *http.Request) {
	id, ok := idParam(w, r)
	if !ok {
		return
	}
	if err := s.store.DeleteDevice(r.Context(), id); err != nil {
		writeErr(w, err)
		return
	}
	s.logEvent(r, "NOTICE", "config", "deleted device", &id, nil)
	writeJSON(w, http.StatusOK, map[string]any{"id": id, "deleted": true})
}

// ---- helpers ----

func readJSON(w http.ResponseWriter, r *http.Request, dst any) bool {
	if err := json.NewDecoder(r.Body).Decode(dst); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON body"})
		return false
	}
	return true
}

func idParam(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid id"})
		return 0, false
	}
	return id, true
}

func atoiDefault(s string, def int) int {
	if n, err := strconv.Atoi(s); err == nil {
		return n
	}
	return def
}

// writeErr maps store/pg errors to HTTP status codes (Doc 5 §7).
func writeErr(w http.ResponseWriter, err error) {
	if errors.Is(err, store.ErrNotFound) {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
		return
	}
	if errors.Is(err, store.ErrParentCycle) {
		writeJSON(w, http.StatusBadRequest,
			map[string]string{"error": "parent_device_id would create a dependency cycle"})
		return
	}
	if errors.Is(err, store.ErrParentNotFound) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "parent device does not exist"})
		return
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		switch pgErr.Code {
		case "23505": // unique_violation — constraint name says which field collided
			writeJSON(w, http.StatusConflict, map[string]string{"error": "duplicate value (" + pgErr.ConstraintName + ")"})
			return
		case "23503": // foreign_key_violation: subnet/parent/map/icon reference
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "referenced " + pgErr.ConstraintName + " does not exist"})
			return
		case "23514": // check_violation (e.g. edge source == target, port range)
			writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": "constraint violated (" + pgErr.ConstraintName + ")"})
			return
		}
	}
	log.Printf("api error: %v", err)
	writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
}
