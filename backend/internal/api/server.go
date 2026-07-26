// Package api runs the REST + WebSocket HTTP server (the `api` role).
package api

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"time"

	"echomap/internal/bus"
	"echomap/internal/config"
	"echomap/internal/notify"
	"echomap/internal/store"
	"echomap/internal/tsdb"
)

type server struct {
	cfg     config.Config
	store   *store.Store
	hub     *hub
	limiter *loginLimiter
	reader  *tsdb.Reader
	email   *notify.Email
}

// Run starts the HTTP server and blocks until ctx is cancelled, then drains.
// b may be nil (Redis unavailable) — the /ws endpoint still serves but gets no events.
func Run(ctx context.Context, cfg config.Config, st *store.Store, b *bus.Bus) error {
	reader := tsdb.NewReader(cfg.InfluxURL, cfg.InfluxToken, cfg.InfluxOrg, cfg.InfluxBucket)
	defer reader.Close()
	s := &server{cfg: cfg, store: st, hub: newHub(), limiter: newLoginLimiter(), reader: reader, email: notify.NewEmail(st)}

	// Phase 8: seed the bootstrap Superadmin from APP_ADMIN_PASSWORD on an empty DB.
	if err := s.bootstrapSuperadmin(ctx); err != nil {
		log.Printf("warning: superadmin bootstrap failed (%v) — check APP_ADMIN_PASSWORD / DB; automation token still works", err)
	}

	mux := http.NewServeMux()

	// Unauthenticated liveness probe.
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})

	// Login is unauthenticated. Registered on the outer mux so the method+path
	// pattern out-specifies "/api/v1/" and bypasses the authenticate wrapper.
	mux.HandleFunc("POST /api/v1/auth/login", s.login)

	// WebSocket hub (self-authenticates via session cookie or ?token=).
	mux.HandleFunc("GET /ws", s.handleWS)
	if b != nil {
		go s.runStatusSubscriber(ctx, b)
	}

	// Authenticated REST surface: identity resolved once, per-route role enforced by gate().
	v1 := http.NewServeMux()
	s.routes(v1)
	mux.Handle("/api/v1/", s.authenticate(v1))

	srv := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
	}

	go func() {
		<-ctx.Done()
		shutCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutCtx)
	}()

	log.Printf("api listening on %s", cfg.HTTPAddr)
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

// routes registers every /api/v1 route with its minimum role (PRD §2b P11 matrix):
//   - reads + on-demand ping/traceroute/DNS + own account/sessions → OPERATOR
//   - all mutations, settings, device-service launchers → ADMIN
//   - user management, all-session admin → SUPERADMIN
func (s *server) routes(mux *http.ServeMux) {
	// --- account & sessions (any authenticated user) ---
	mux.HandleFunc("POST /api/v1/auth/logout", s.gate(RoleOperator, s.logout))
	mux.HandleFunc("GET /api/v1/auth/me", s.gate(RoleOperator, s.me))
	mux.HandleFunc("POST /api/v1/auth/change-password", s.gate(RoleOperator, s.changePassword))
	mux.HandleFunc("GET /api/v1/sessions", s.gate(RoleOperator, s.listSessions))
	mux.HandleFunc("DELETE /api/v1/sessions/{id}", s.gate(RoleOperator, s.deleteSession))

	// --- users (Superadmin only) ---
	mux.HandleFunc("GET /api/v1/users", s.gate(RoleSuperadmin, s.listUsers))
	mux.HandleFunc("POST /api/v1/users", s.gate(RoleSuperadmin, s.createUser))
	mux.HandleFunc("PATCH /api/v1/users/{id}", s.gate(RoleSuperadmin, s.patchUser))
	mux.HandleFunc("DELETE /api/v1/users/{id}", s.gate(RoleSuperadmin, s.deleteUser))

	// --- event logs (Admin; AUDIT rows filtered to Superadmin in the handler) ---
	mux.HandleFunc("GET /api/v1/logs", s.gate(RoleAdmin, s.listLogs))

	// --- devices (read = Operator, write = Admin) ---
	mux.HandleFunc("GET /api/v1/devices", s.gate(RoleOperator, s.listDevices))
	mux.HandleFunc("POST /api/v1/devices", s.gate(RoleAdmin, s.createDevice))
	mux.HandleFunc("GET /api/v1/devices/{id}", s.gate(RoleOperator, s.getDevice))
	mux.HandleFunc("PATCH /api/v1/devices/{id}", s.gate(RoleAdmin, s.patchDevice))
	mux.HandleFunc("DELETE /api/v1/devices/{id}", s.gate(RoleAdmin, s.deleteDevice))

	// --- settings (Admin) ---
	mux.HandleFunc("GET /api/v1/settings/channels", s.gate(RoleAdmin, s.getChannelSettings))
	mux.HandleFunc("PUT /api/v1/settings/channels", s.gate(RoleAdmin, s.putChannelSettings))
	mux.HandleFunc("POST /api/v1/settings/channels/test-email", s.gate(RoleAdmin, s.testEmail))

	// --- alert sounds (Phase 11, Pillar 14): read/playback = Operator, manage = Admin ---
	mux.HandleFunc("GET /api/v1/sounds", s.gate(RoleOperator, s.listSounds))
	mux.HandleFunc("GET /api/v1/sounds/{id}/audio", s.gate(RoleOperator, s.getSoundAudio))
	mux.HandleFunc("POST /api/v1/sounds", s.gate(RoleAdmin, s.createSound))
	mux.HandleFunc("DELETE /api/v1/sounds/{id}", s.gate(RoleAdmin, s.deleteSound))
	mux.HandleFunc("GET /api/v1/settings/sounds", s.gate(RoleOperator, s.getSoundAssignments))
	mux.HandleFunc("PUT /api/v1/settings/sounds", s.gate(RoleAdmin, s.putSoundAssignments))

	// --- alert rules (Phase 11): read = Operator, write = Admin (Doc 5 §8) ---
	mux.HandleFunc("GET /api/v1/alert-rules", s.gate(RoleOperator, s.listAlertRules))
	mux.HandleFunc("POST /api/v1/alert-rules", s.gate(RoleAdmin, s.createAlertRule))
	mux.HandleFunc("PATCH /api/v1/alert-rules/{id}", s.gate(RoleAdmin, s.patchAlertRule))
	mux.HandleFunc("DELETE /api/v1/alert-rules/{id}", s.gate(RoleAdmin, s.deleteAlertRule))

	// --- maps & topology (read = Operator, write = Admin) ---
	mux.HandleFunc("GET /api/v1/maps", s.gate(RoleOperator, s.listMaps))
	mux.HandleFunc("POST /api/v1/maps", s.gate(RoleAdmin, s.createMap))
	mux.HandleFunc("PATCH /api/v1/maps/{id}", s.gate(RoleAdmin, s.patchMap))
	mux.HandleFunc("GET /api/v1/topology", s.gate(RoleOperator, s.getTopology))
	mux.HandleFunc("POST /api/v1/topology/nodes/positions/bulk", s.gate(RoleAdmin, s.savePositions))
	mux.HandleFunc("POST /api/v1/topology/edges", s.gate(RoleAdmin, s.createEdge))
	mux.HandleFunc("DELETE /api/v1/topology/edges/{id}", s.gate(RoleAdmin, s.deleteEdge))

	// --- on-demand tools (Operator per matrix: read-only diagnostics) ---
	mux.HandleFunc("POST /api/v1/tools/ping", s.gate(RoleOperator, s.toolPing))
	mux.HandleFunc("POST /api/v1/tools/traceroute", s.gate(RoleOperator, s.toolTraceroute))
	mux.HandleFunc("POST /api/v1/tools/dns-lookup", s.gate(RoleOperator, s.toolDNSLookup))

	// --- icons (read = Operator, write = Admin) ---
	mux.HandleFunc("GET /api/v1/icons", s.gate(RoleOperator, s.listIcons))
	mux.HandleFunc("POST /api/v1/icons", s.gate(RoleAdmin, s.createIcon))
	mux.HandleFunc("DELETE /api/v1/icons/{id}", s.gate(RoleAdmin, s.deleteIcon))

	// --- device services (launchers are Admin+ per the matrix) ---
	mux.HandleFunc("GET /api/v1/devices/{id}/services", s.gate(RoleAdmin, s.listServices))
	mux.HandleFunc("POST /api/v1/devices/{id}/services", s.gate(RoleAdmin, s.createService))
	mux.HandleFunc("DELETE /api/v1/services/{id}", s.gate(RoleAdmin, s.deleteService))

	// --- custom monitors (Phase 9): read = Operator, write/test = Admin (Doc 5 §8.4) ---
	mux.HandleFunc("GET /api/v1/devices/{id}/monitors", s.gate(RoleOperator, s.listMonitors))
	mux.HandleFunc("POST /api/v1/devices/{id}/monitors", s.gate(RoleAdmin, s.createMonitor))
	mux.HandleFunc("PATCH /api/v1/monitors/{id}", s.gate(RoleAdmin, s.patchMonitor))
	mux.HandleFunc("DELETE /api/v1/monitors/{id}", s.gate(RoleAdmin, s.deleteMonitor))
	mux.HandleFunc("POST /api/v1/monitors/{id}/test", s.gate(RoleAdmin, s.testMonitor))

	// History metrics reads (Phase 10, §8.5–8.6) — read-only, Operator+.
	mux.HandleFunc("GET /api/v1/devices/{id}/metrics/ping", s.gate(RoleOperator, s.metricsPing))
	mux.HandleFunc("GET /api/v1/monitors/{id}/metrics", s.gate(RoleOperator, s.metricsMonitor))
	// TODO(phase11+): discovery ingestion, alert-sounds.
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}
