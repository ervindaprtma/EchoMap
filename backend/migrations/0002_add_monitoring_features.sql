-- EchoMap schema 0002 — monitoring features from the expanded blueprint (Doc 2):
-- multi-site maps/submaps (§1.13), custom icons + draw.io import (§1.14),
-- device service launchers (§1.15), standalone designs (§1.16), plus devices
-- additions (hostname, dependency, placement, down cause — §1.3) and settings
-- additions (alert templates, DNS cadence, web-SSH gateway — §1.10).
--
-- Fresh deployments: runs automatically after 0001 (docker-entrypoint-initdb.d
-- applies the mounted directory in alphabetical order on first boot).
-- Existing databases: apply once by hand —
--   docker compose exec -T postgres psql -U echomap -d echomap \
--     < backend/migrations/0002_add_monitoring_features.sql

BEGIN;

-- ---- New enumerated types ----
CREATE TYPE service_kind AS ENUM ('HTTP', 'HTTPS', 'SSH', 'TELNET', 'CUSTOM_URL');
CREATE TYPE down_reason  AS ENUM ('PROBE', 'PARENT');  -- why a device is DOWN (Doc 3 §5); NULL while UP

-- ---- maps: multi-site / submap tree ----
CREATE TABLE maps (
    id            BIGSERIAL PRIMARY KEY,
    parent_map_id BIGINT REFERENCES maps(id) ON DELETE CASCADE,  -- NULL = root; tree only, cycles rejected app-side
    name          TEXT NOT NULL,
    pos_x         DOUBLE PRECISION,   -- submap-node position on the PARENT canvas
    pos_y         DOUBLE PRECISION,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT now(),

    UNIQUE (parent_map_id, name)
);

CREATE INDEX idx_maps_parent ON maps(parent_map_id);

-- ---- icons: custom icon library ----
-- draw.io .xml shape libraries are exploded at import time (POST /icons/import-drawio):
-- each shape lands here as one SVG/PNG row. Raw library XML is never stored.
CREATE TABLE icons (
    id         BIGSERIAL PRIMARY KEY,
    name       TEXT NOT NULL UNIQUE,
    category   TEXT,                  -- folder/library name, e.g. the imported draw.io library
    mime       TEXT NOT NULL CHECK (mime IN ('image/svg+xml', 'image/png')),
    data       BYTEA NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- ---- device_services: right-click launchers ----
-- NO credential columns — by design. SSH/Telnet entries only produce a client
-- handoff (ssh:// handler or web-SSH gateway); the user types credentials there.
CREATE TABLE device_services (
    id         BIGSERIAL PRIMARY KEY,
    device_id  BIGINT NOT NULL REFERENCES devices(id) ON DELETE CASCADE,
    label      TEXT NOT NULL,          -- 'Web UI', 'Mgmt SSH'
    kind       service_kind NOT NULL,
    port       INTEGER CHECK (port BETWEEN 1 AND 65535),  -- NULL = kind default (80/443/22/23)
    path       TEXT,                   -- optional HTTP(S) URL path, e.g. '/admin'
    url        TEXT,                   -- CUSTOM_URL only; may contain {ip} / {hostname}
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_services_device ON device_services(device_id);

-- ---- designs: standalone topology designer ----
-- Deliberately NO foreign keys to devices/maps/edges: the designer is a
-- drawing tool, fully decoupled from live monitoring.
CREATE TABLE designs (
    id         BIGSERIAL PRIMARY KEY,
    name       TEXT NOT NULL UNIQUE,
    graph      JSONB NOT NULL,         -- opaque React Flow document
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- ---- devices: hostname devices, dependency, placement, custom icon, down cause ----
ALTER TABLE devices
    ADD COLUMN hostname         TEXT UNIQUE,      -- optional FQDN; resolver keeps ip_address fresh (Doc 3 §6)
    ADD COLUMN down_reason      down_reason,      -- 'PROBE' | 'PARENT' while DOWN, else NULL
    ADD COLUMN parent_device_id BIGINT REFERENCES devices(id) ON DELETE SET NULL,  -- cycles rejected app-side (walk-up check on PATCH)
    ADD COLUMN map_id           BIGINT REFERENCES maps(id)  ON DELETE SET NULL,    -- NULL = root map
    ADD COLUMN icon_id          BIGINT REFERENCES icons(id) ON DELETE SET NULL;    -- falls back to builtin `icon` key

-- hostname gets its index from the UNIQUE constraint above.
CREATE INDEX idx_devices_parent ON devices(parent_device_id);
CREATE INDEX idx_devices_map    ON devices(map_id);

-- ---- settings: alert templates, DNS cadence, optional web-SSH gateway ----
ALTER TABLE settings
    ADD COLUMN telegram_template      TEXT,       -- Go text/template; NULL = built-in default (Doc 3 §7)
    ADD COLUMN email_subject_template TEXT,
    ADD COLUMN email_body_template    TEXT,
    ADD COLUMN dns_recheck_seconds    INTEGER NOT NULL DEFAULT 300,
    ADD COLUMN webssh_url             TEXT;       -- HTML5 SSH gateway base URL; NULL = ssh:// handler only

COMMIT;
