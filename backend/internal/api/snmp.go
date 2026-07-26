package api

import (
	"net/http"
	"strings"
	"time"

	"echomap/internal/snmp"
	"echomap/internal/store"
)

var snmpVersions = map[string]bool{"v2c": true, "v3": true}

// On-demand fingerprints get a shorter timeout than the scanner — a human is waiting.
const snmpProbeTimeout = 3 * time.Second

// GET /settings/snmp — default version + whether a community is stored (masked).
func (s *server) getSNMPSettings(w http.ResponseWriter, r *http.Request) {
	ss, err := s.store.GetSNMPSettings(r.Context())
	if err != nil {
		writeErr(w, err)
		return
	}
	community := ""
	if ss.CommunitySet {
		community = maskedToken
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"default_version":   ss.DefaultVersion,
		"default_community": community,
	})
}

// PUT /settings/snmp — write-only. A masked-echo community means "unchanged".
func (s *server) putSNMPSettings(w http.ResponseWriter, r *http.Request) {
	var body struct {
		DefaultVersion   *string `json:"default_version"`
		DefaultCommunity *string `json:"default_community"`
	}
	if !readJSON(w, r, &body) {
		return
	}
	var u store.SNMPUpdate
	if body.DefaultVersion != nil {
		v := strings.TrimSpace(*body.DefaultVersion)
		if v != "" && !snmpVersions[v] {
			writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": "version must be v2c or v3"})
			return
		}
		u.DefaultVersion = &v
	}
	if body.DefaultCommunity != nil && *body.DefaultCommunity != maskedToken {
		u.Community = body.DefaultCommunity // masked echo = leave unchanged
	}
	if err := s.store.UpdateSNMPSettings(r.Context(), u); err != nil {
		writeErr(w, err)
		return
	}
	s.logEvent(r, "NOTICE", "config", "updated SNMP settings", nil, nil)
	s.getSNMPSettings(w, r)
}

// GET /devices/{id}/interfaces — the device's SNMP-discovered interfaces.
func (s *server) listInterfaces(w http.ResponseWriter, r *http.Request) {
	id, ok := idParam(w, r)
	if !ok {
		return
	}
	rows, err := s.store.ListInterfaces(r.Context(), id)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": rows})
}

// POST /devices/{id}/snmp/fingerprint — run one fingerprint on demand and persist
// it (Admin). Uses the settings-level community; 422 if none is configured or the
// device is unreachable over SNMP.
func (s *server) fingerprintDevice(w http.ResponseWriter, r *http.Request) {
	id, ok := idParam(w, r)
	if !ok {
		return
	}
	dev, err := s.store.GetDevice(r.Context(), id)
	if err != nil {
		writeErr(w, err)
		return
	}
	version, community, err := s.store.SNMPCredentials(r.Context())
	if err != nil {
		writeErr(w, err)
		return
	}
	if community == "" {
		writeJSON(w, http.StatusUnprocessableEntity,
			map[string]string{"error": "no SNMP community configured — set one in Settings → SNMP"})
		return
	}
	res, err := snmp.Fingerprint(r.Context(), dev.IPAddress, version, community, snmpProbeTimeout)
	if err != nil {
		writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": err.Error()})
		return
	}
	if err := s.store.SaveFingerprint(r.Context(), id, res); err != nil {
		writeErr(w, err)
		return
	}
	s.logEvent(r, "NOTICE", "config", "SNMP fingerprint (manual): "+res.Vendor+" "+res.Model, &id, nil)
	writeJSON(w, http.StatusOK, res)
}
