# EchoMap — Network Topology & Monitoring

**EchoMap** is an open-source network topology and monitoring web app — think **The Dude (MikroTik), but web-based**. It continuously pings your fleet (by IP **or domain name**), tracks each device through a clear state machine (`UP` / `DOWN` / `ORPHANED` / `UNKNOWN`), draws an editable PNETLab-style topology with multi-level submaps and parent/child dependencies, and alerts on confirmed state changes — all from a single `docker-compose` deployment.

- **Scale target:** ~500 IPs across DC & DRC sites (comfortably; the architecture has plenty of headroom).
- **Deploy:** one `docker-compose.yml`, one image, role-selected workers.
- **Ping without root:** the ping path uses `cap_add: NET_RAW` for unprivileged raw-socket ICMP.

---

## What works today (built & live-tested)

- **Real-time monitoring** — ICMP polling every 15–30 s with debounce + flap detection; metrics written to InfluxDB on every probe, but a WebSocket push fires **only on a confirmed transition**, so ~500 devices never flood the socket.
- **Devices by IP or domain** — hostnames are resolved to an IPv4 address at creation (`422` if unresolvable), then re-resolved by the worker every `dns_recheck_seconds` (default 300): when the record moves, the device's IP is updated and a `device.updated` event repaints the table and canvas live. A DNS hiccup keeps the last-known IP, and an address another device already monitors is refused and logged rather than clobbered.
- **Parent/child dependencies (The Dude-style)** — set a device's parent; a confirmed parent DOWN turns every descendant red instantly with a "down via parent" reason, pauses their probes, and fires **one** aggregated Telegram alert with the blast radius. Children re-probe and go green when the parent recovers. Cycle creation is rejected server-side (transactional walk-up check → `400`).
- **Topology canvas** — React Flow (v11) wrapped in shadcn/ui: View/Edit modes, drag + explicit "Save Layout" (saved coordinates are canonical), handle-drag **manual links** (never auto-deleted), submap folder nodes, per-map breadcrumb.
- **Multi-site / submap tree** — nested maps (Site → Building → Rack); each submap node is colored by its **worst descendant status**, aggregated over the whole subtree, so a branch problem is visible from the root map.
- **Custom icons** — upload SVG/PNG to the icon library API and assign per device (picker in the edit dialog). *draw.io `.xml` library import + the Icon Library management page are pending.*
- **Right-click services & tools** — configure per-device services (HTTP / HTTPS / custom port / SSH / Telnet / custom URL); right-click a node to launch them (URLs open a new tab; SSH hands off to `ssh://` handlers like PuTTY, an optional web-SSH gateway, or copy-command). Built-in on-demand **ping, traceroute, DNS lookup**, plus an ad-hoc **Telnet** handoff (Admin+). **Credentials are never stored.**
- **Telegram alerting with custom templates** — operator-editable Go templates with parse validation; the bot token is **AES-256-GCM encrypted at rest** and only ever returned masked.
- **Browser sound & desktop pop-up alerts** — two toggles in the topbar (stored per browser, no server state): a synthesized chime (falling tones for DOWN, rising for recovery) and native desktop notifications, one per device. They fire on any page and stay **silent for devices down via a dependency parent** — only the device that actually failed interrupts you.
- **Users, roles & sessions (Phase 8)** — Superadmin / Administrator / Operator, enforced **server-side on every route** (the UI just hides what a role can't use). argon2id passwords, opaque tokens stored SHA-256-hashed, **HttpOnly session cookies** (retires the dev bearer for browsers; the static token stays for automation), 12 h idle / 7 d absolute expiry, per-session revoke, login rate-limiting, a bootstrap Superadmin seeded from `APP_ADMIN_PASSWORD` with a forced first-login password change.
- **Event logging (Phase 8)** — an Admin **Logs** page: searchable, filterable, paginated table over INFO / NOTICE / ALERT / ERROR / AUDIT levels (AUDIT rows Superadmin-only). Login/config/transition/alert events are recorded; the page refreshes on a short poll.
- **Custom monitors (Phase 9)** — per-device **TCP / HTTP(S)** checks (with an absolute-URL override for endpoint health), each on its own interval (seconds/minutes/hours). A worker scheduler runs the same debounce as ping; a confirmed transition writes a log row, fires a Telegram alert + browser pop-up/sound, and pushes a `monitor.status` WS frame. Monitors pause while their device is DOWN, HTTPS records cert-expiry, and an on-demand **"Test now"** runs one check immediately. Configured in the device dialog's **Monitoring** tab (Admin+).
- **Alert configuration from the UI (Phase 11)** — an Admin **Settings → Alerts** page: create/toggle/delete **alert rules** (per-device or global; per-event switches for down / up / flapping / orphaned) and set delivery-channel credentials. Both **Telegram** and **email (SMTP)** deliver: configure a bot token or SMTP server (host/port/username/password/from + TLS mode, with a **"Send test"** button), and route each rule to a chat id or email address. Secrets are AES-256-GCM encrypted at rest. Previously alert rules could only be created via SQL.
- **History charts (Phase 10)** — a **History** action (Devices row · node context menu) opens `/devices/:id/history`: a range picker (1h/6h/24h/7d/30d) over a ping group (latency avg+p95, packet loss, read-time RFC 3550 jitter, availability strip) plus one group per custom monitor (connect/response time, HTTP status-code band, availability, TLS days-to-expiry for HTTPS). Read-only Recharts; degrades to a clean "no samples" state when InfluxDB isn't configured.

## Designed, pending implementation (see [PRD.md](./PRD.md) §5 roadmap)

- **Custom alert sounds (Phase 11)** — upload `.wav` files (≤ 1 MB) under Settings → Alerts and assign one per event class (device down/recovery, monitor down/recovery); unassigned classes fall back to the built-in synthesized chime. Playback fetches the file with the session cookie → blob → `Audio`.

**v1.1 — next in line:**

- **Phase 11 is complete** (Telnet tool, alert-rules CRUD + Settings→Alerts tab, email/SMTP channel, flapping alert, custom `.wav` sounds). Next: the **housekeeping retention sweep** (Phase 11.5, priority — nothing prunes the log/session tables yet), then **System Resource Monitoring** (v1.2, Pillar 16 — design approved).

**Original roadmap (Slices 3–7):**

- **Bulk discovery** (subnet CIDR sweep + Netbox import, skip-on-fail, live progress) — Slice 3.
- **SNMP fingerprinting** (vendor/model/icon from `sysObjectID`, naming fallback chain) — Slice 4.
- **Netbox hourly sync + ORPHANED safety flow** (never hard-delete; two-step, type-to-confirm resolution UI) — Slice 6.
- **Alerting** — ✅ complete as of Phase 11 (Telegram + email/SMTP + templates + sound/pop-up + edge-triggered flapping alert).
- **Engineer utilities** — IP calculator and the standalone drawing-only **Topology Designer** (the `designs` table/API groundwork exists).
- **Dashboard & metrics UI**, auth/login (dev bearer token today), retention sweeps, dagre auto-arrange, maps management UI (rename/delete).
- **System Resource Monitoring** (v1.2, PRD Pillar 16 — ✅ design approved 2026-07-26, not yet built): a `/system` page (Admin+) monitoring EchoMap itself — host CPU/mem/load (real host values via `/proc`, no privileges needed), per-container Docker stats (read-only `docker.sock` on the worker), InfluxDB `/health`+`/metrics`, nginx `stub_status`, and Go-role self-stats — collected by the worker into the existing InfluxDB, charted with the existing Recharts stack. No cAdvisor/Prometheus/Grafana.

---

## Requirements → status

The nine product requirements and where each stands (details per pillar in [PRD.md](./PRD.md) §2):

| # | Requirement | Status |
|---|-------------|--------|
| 1 | Ping monitoring + topology creation | ✅ Done |
| 2 | Web-based "The Dude" alternative | ✅ Core done |
| 3 | Custom icons, draw.io-compatible | 🟡 Upload/assign done; `.xml` import + library page pending |
| 4 | Tools (SSH/traceroute/DNS), domain devices, manual links | ✅ Done (incl. periodic DNS re-resolution) |
| 5 | Telegram alerts + templates + sound/pop-up | ✅ Done — Telegram + **email/SMTP**, templates, sound/pop-up, alert-rules config UI, **edge-triggered flapping alert** (all Phase 11) |
| 6 | Multi-site / submap tree | ✅ Done |
| 7 | Parent/child cascade (red down, green recover) | ✅ Done |
| 8 | Device services, zero-credential launch | ✅ Done |
| 9 | IP calculator + standalone Topology Designer | ⬜ Pending (schema/API groundwork only) |

**v1.1 requirements** (added 2026-07-25 — all ⬜ designed; build order Phases 8–11, see [PRD.md](./PRD.md) §5):

| Requirement | Status |
|-------------|--------|
| Event logging — searchable table, levels INFO / NOTICE / ALERT / ERROR / AUDIT | ✅ Done (Phase 8; live WS prepend deferred, page polls) |
| User management + sessions + roles (Superadmin / Administrator / Operator) | ✅ Done (Phase 8; argon2id, cookie sessions, server-side RBAC) |
| Telnet tool (ad-hoc, zero-credential handoff) | ✅ Done (Phase 11) |
| Custom port / protocol / URL monitors (per-device, custom interval) | ✅ Done (Phase 9; TCP/HTTP(S), scheduler, `monitor.status`, test endpoint) |
| Custom uploaded `.wav` alert sounds + URL health checks | ✅ Done — URL health checks (Phase 9, monitor `url_override`) + `.wav` upload/assign/playback (Phase 11) |
| Historical graphs per device (latency / loss / jitter; monitor charts) | ✅ Done (Phase 10; metrics read APIs + `/devices/:id/history` Recharts page, live-tested) |

---

## Documents

| # | Document | Contents |
|---|----------|----------|
| — | [**PRD.md**](./PRD.md) | **Single source of truth**: vision, the 9 requirement pillars with status, tech stack, security model, phased roadmap, UI/UX rules (shadcn/ui mandatory) |
| 1 | [System Architecture & Tech Stack](./01-system-architecture.md) | Worker roles (incl. DNS resolver), real-time ping→WS→frontend flow, dependency cascade path, bulk-add queue (skip-on-fail), `docker-compose.yml` with `NET_RAW` + healthchecks, feature-to-component map |
| 2 | [Database Schema](./02-database-schema.md) | PostgreSQL DDL + Prisma (device state machine, dependency & maps, services, custom icons, designs, alert rules, retention) and InfluxDB time-series schema |
| 3 | [Backend Logic & Pseudocode](./03-backend-logic.md) | Go snippets: skip-on-fail discovery, state-change + flapping alerting, parent/child cascade, hostname resolver, alert templates, Netbox ORPHANED diffing, retention sweeps |
| 4 | [Frontend UI/UX (shadcn/ui)](./04-frontend-uiux.md) | Pages/components, React Flow topology canvas, Edit/View modes, right-click services/tools menu, submap navigation, icon library, IP calculator & designer, orphan resolution |
| 5 | [API Endpoints & WebSocket Events](./05-api-websocket.md) | Authentication, REST endpoints (ingestion, topology & maps, services, tools, icons, designs, settings) and WS event payloads |
| 6 | [Menu Design Gallery & Stack Verification](./06-menu-design-gallery.md) | ASCII design mockups of every menu (with build status) + a stack cross-check matrix verifying doc claims against package.json / go.mod / compose |

---

## Architecture at a glance

EchoMap is a **worker-oriented monolith-of-services**: one Go binary that boots into a different *role* via `--role` / `APP_ROLE`, so the deployment stays a single image and compose file while workloads stay cleanly separated.

| Role | Responsibility |
|------|----------------|
| `api` | REST API + WebSocket hub |
| `ping-worker` | Scheduled ICMP polling (15–30 s) |
| `snmp-worker` | Device fingerprinting |
| `discovery-worker` | Bulk-add jobs, skip-on-fail |
| `netbox-sync-worker` | Hourly diff + ORPHANED state machine |
| `notifier` | Email + Telegram alerts (custom templates) |
| `resolver` | Auto DNS re-resolution for domain-based devices |

> Today the binary implements two roles, `api` and `worker`; the rows above are the *workloads*, and the ping loop and resolver both run inside `worker` (each on its own cadence). They split into separate roles only if one starves the other — the compose file already runs them as separate containers by role, so that is a one-line change.

**Data flow:** ping-worker writes every probe to InfluxDB, but only publishes a Redis event on a *confirmed* transition → the `api` hub fans that out over WebSocket → the browser recolors the node and patches the table row. The API server never pings, so it scales and restarts independently of the polling load.

---

## Stack summary

- **Backend:** Go (goroutines for concurrent ping/SNMP). A Python (FastAPI + Celery) mapping is documented per section.
- **Frontend:** React + Vite, `shadcn/ui` (Radix + Tailwind), React Flow (topology), Recharts (metrics), TanStack Query, native WebSocket.
- **Data:** PostgreSQL (source of truth), Redis (Pub/Sub + cache; the Asynq queue arrives with Slice 3), InfluxDB 2.x (time-series; Prometheus is a documented alternative).
- **Deploy:** single `docker-compose.yml`; ping containers get `cap_add: NET_RAW` for unprivileged ICMP; stores are health-gated via `depends_on: condition: service_healthy`.

---

## System requirements (server / VM)

EchoMap runs as one `docker-compose` stack of five processes: the Go binary in two roles (`api`, `worker`) plus **PostgreSQL**, **Redis**, and **InfluxDB**. The Go roles are lightweight; **InfluxDB is the dominant consumer** of both RAM and disk, so sizing tracks how much time-series you keep, not how busy the app UI is.

**Sizing tiers** (estimates; the middle tier is the design target of ~500 monitored devices):

| Tier | Devices | vCPU | RAM | Disk (SSD) |
|------|---------|------|-----|-----------|
| Minimum (lab / PoC) | ≤ 100 | 2 | 4 GB | 20 GB |
| **Recommended** | ~500 | **4** | **8 GB** | **40 GB** |
| Comfortable (growth / heavy history use) | 500–1500 | 4–8 | 16 GB | 80 GB+ |

**Where the resources go** (roughly, at the ~500-device target):

| Component | RAM | Notes |
|-----------|-----|-------|
| Go `api` + `worker` | ~150–300 MB total | ICMP at concurrency 32 is cheap; bursty per sweep |
| PostgreSQL 16 | ~256–512 MB | small DB — relational tables are retention-bounded |
| Redis 7 | ~64–128 MB | Pub/Sub + cache (+ Asynq queue in Slice 3) |
| InfluxDB 2.x | ~0.5–1 GB | the driver; give it room or it's the first to feel tight |

**Disk is dominated by InfluxDB retention.** Rough model: `devices × samples/day × retention days`. At 500 devices probing every ~20 s over a 30-day window that's ~65M points, which TSM compresses to a few GB; the relational DB and Docker images/volumes add a few more. The **v1.1 custom monitors (P13)** add a second time-series measurement (`monitor_metrics`), so budget extra disk proportional to how many monitors you configure. **Use SSD/NVMe** — Postgres and InfluxDB are I/O-sensitive; avoid spinning disks or throttled network block storage.

**Platform:**

- **OS:** any modern Linux (kernel ≥ 5.x) with **Docker Engine 24+** and **Compose v2**. Also runs on macOS/Windows via Docker Desktop and on WSL2 (the dev/test environment) for evaluation.
- **CPU architecture:** **x86-64 or arm64** — all base images (postgres/redis/influxdb) are multi-arch and the Go binary cross-compiles, so small ARM VMs work for lab tiers.
- **Raw sockets:** the host must allow the `NET_RAW` capability on containers (standard Docker; some locked-down managed-container platforms forbid it). Alternative on such hosts: the `net.ipv4.ping_group_range` sysctl for unprivileged ICMP without `NET_RAW` (Doc 1 §5).
- **Network:** L3 reachability from the host to everything it monitors (ICMP echo, plus TCP/HTTP for v1.1 monitors); outbound HTTPS to `api.telegram.org` for alerts; inbound `:80` served by the frontend container (which reverse-proxies `/api` + `/ws` to the `app` role) — plus `:8080` if you expose the API directly. (Optional host HMR dev runs Vite on `:5173`.)

**Not covered by these numbers:** high availability (this is a single-host deployment — no clustering/failover), and the Node.js toolchain, which is a **build-time** dependency for the frontend only, not part of the runtime footprint.

---

## Quickstart

**All-container (see [PRD.md](./PRD.md) §3b):** the whole stack — frontend included — comes up with one command and no host build:

```bash
cp .env.example .env                  # fill in the secrets (INFLUX_TOKEN, APP_ENCRYPTION_KEY, APP_API_TOKEN, APP_ADMIN_PASSWORD)
docker compose up -d --build          # builds every image (incl. the frontend) and starts everything
```

No Node, no `npm install`, no `npm run build` on the host — each service builds inside its own image. Open **https://localhost/** (TLS) or **http://localhost/** — nginx serves the SPA and reverse-proxies `/api` + `/ws` to the `app` role, so it's single-origin. A **self-signed TLS cert** is auto-generated on first start (the browser will warn — expected for internal use); to use a real cert, drop `tls.crt` + `tls.key` into `./certs/`.

**Custom ports.** The published host ports are env-driven — set any of these in `.env` to expose EchoMap wherever you like (the containers still listen on 80/443/8080/8086 internally):

```bash
WEB_HTTP_PORT=80      WEB_HTTPS_PORT=443     # SPA over HTTP / TLS  (e.g. WEB_HTTPS_PORT=8443)
API_PORT=8080         INFLUX_PORT=8086       # direct API / InfluxDB UI
```

**Raw metrics.** The InfluxDB UI is at **http://localhost:8086** (or your `INFLUX_PORT`) — sign in as `echomap` (`DOCKER_INFLUXDB_INIT_PASSWORD`), org `echomap`, and use **Data Explorer** to browse the `ping_metrics` bucket (`ping_metrics` + `monitor_metrics` measurements). The admin API token is your `INFLUX_TOKEN`.

The Postgres schema in `backend/migrations/` (`0001` + `0002` + `0003` + `0004`) is applied automatically on the container's **first boot** (via the `docker-entrypoint-initdb.d` mount) — no manual migration step. There is no online migration runner yet, so applying a new migration to an **existing** database is a manual `psql -f` (an upgrade-hardening item). The API listens on `:8080`: `GET /healthz` and `POST /api/v1/auth/login` are open; everything else authenticates by the `echomap_session` cookie (browsers) or the static `APP_API_TOKEN` bearer (automation). On first boot, sign in as **`admin`** with `APP_ADMIN_PASSWORD` and set a new password when prompted.

## Repository layout

```
EchoMap/
├── docker-compose.yml        # frontend + app + worker + postgres + redis + influxdb (all healthchecked)
├── .env.example              # secrets + tunables (copy to .env)
├── backend/                  # Go — single binary, role-dispatched (api | worker)
│   ├── cmd/echomap/          #   main: --role / APP_ROLE dispatch; fatal without APP_ENCRYPTION_KEY
│   ├── internal/api/         #   REST + WS hub: devices, topology, maps, icons, services, tools, settings
│   ├── internal/monitor/     #   debounce/flap state machine + parent/child cascade (+ tests)
│   ├── internal/worker/      #   ping loop (discovery/snmp/sync attach in Slices 3-6)
│   ├── internal/{auth,moncheck,store,bus,ping,tools,notify,secrets,tsdb,config,db}/
│   ├── migrations/           #   0001..0004 (Doc 2 schema: core, monitoring, users/logs, monitors/sounds)
│   ├── prisma/schema.prisma  #   read-only mirror of the SQL schema for Prisma tooling
│   └── Dockerfile            #   multi-stage; setcap NET_RAW on a non-root binary
├── frontend/                 # React + Vite + TS + Tailwind + shadcn/ui
│   ├── src/                  #   pages/{Devices,Topology}, components/{devices,topology,ui}, lib (+ vitest)
│   ├── Dockerfile            #   multi-stage node build → nginx (serves bundle, proxies /api,/ws)
│   └── nginx.conf            #   static serve + reverse proxy to the app service
├── PRD.md                    # product source of truth (status, roadmap, UI/UX rules)
└── 01..05-*.md               # the design blueprint (see status banners inside)
```

Unbuilt areas are marked `ponytail:`/`TODO(slice…)` in code and ⬜ in [PRD.md](./PRD.md). Add shadcn components as you build: `cd frontend && npx shadcn@2 add <component>` — **all new UI must use shadcn/ui primitives** (PRD §6).

## Local development (optional — not the deploy path)

The deployment model is all-container (see Quickstart). These host-run shortcuts exist **only** for tighter dev loops (Go rebuilds, frontend HMR) and are never how EchoMap ships:

- **Stores only:** `docker compose up -d postgres redis influxdb`
- **Backend:** `cd backend && go run ./cmd/echomap --role=api` (or `--role=worker`)
- **Frontend (HMR):** `cd frontend && npm run dev`

---

## Required environment variables

Put these in a **git-ignored `.env`** next to `docker-compose.yml`. Credentials in the DB are the runtime source of truth; the `NETBOX_*` vars only **seed** an empty database on first boot.

| Variable | Purpose |
|----------|---------|
| `INFLUX_TOKEN` | InfluxDB admin/write token (time-series store) |
| `APP_ENCRYPTION_KEY` | AES-256-GCM key for secrets at rest (Netbox/SNMP/SMTP/Telegram). **Never** stored in the DB |
| `APP_API_TOKEN` | Static bearer token for **headless automation** on `/api/v1/*` + `/ws` (browsers use cookie sessions since Phase 8) |
| `APP_ADMIN_PASSWORD` | Seeds the bootstrap Superadmin `admin` on an empty DB (Phase 8); forced password change on first login. Unset = no seeded account (automation token only) |
| `NETBOX_URL` | Netbox base URL — first-boot seed only (then edit in Settings) |
| `NETBOX_TOKEN` | Netbox API token — first-boot seed only |

Tunables (have sensible defaults in compose): `PING_INTERVAL_SECONDS` (15–30), `DISCOVERY_CONCURRENCY` (e.g. 32).

---

## Security notes

- **Auth is mandatory.** Every REST route and the WebSocket upgrade require `Authorization: Bearer <token>`; missing/invalid → `401`. See Doc 5 §0.
- **Secrets are encrypted at rest** (AES-256-GCM) and returned **masked** (`••••`), accepted write-only.
- **Device credentials are never stored.** SSH/Telnet service launchers only hand off to the user's own client (`ssh://` handler or web-SSH gateway) — the user types credentials there each session, so a breach of EchoMap exposes no device logins.
- **Destructive actions are guarded** *(designed — lands with Slice 6)* — hard delete of a device becomes a two-step, type-to-confirm flow, only valid on an `ORPHANED` device via the safety path. Today's delete is a plain guarded endpoint.
- **Retention is bounded** *(designed — lands with Slice 7)* — `status_transitions` (24 h), `alert_events` / `netbox_sync_logs` (90 d), InfluxDB (30 d raw + rollups) pruned by an hourly sweep.
