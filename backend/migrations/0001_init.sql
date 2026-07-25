-- EchoMap initial schema (Doc 2). Applied automatically on first Postgres boot
-- via the docker-entrypoint-initdb.d mount in docker-compose.yml.

-- ---- Enumerated types ----
CREATE TYPE device_status AS ENUM ('UP', 'DOWN', 'ORPHANED', 'UNKNOWN');
CREATE TYPE source_type   AS ENUM ('MANUAL', 'NETBOX', 'DISCOVERY');
CREATE TYPE name_source   AS ENUM ('NETBOX', 'SNMP_SYSNAME', 'MANUAL', 'IP');
CREATE TYPE link_type     AS ENUM ('AUTO', 'MANUAL');
CREATE TYPE alert_channel AS ENUM ('EMAIL', 'TELEGRAM');
CREATE TYPE sync_status   AS ENUM ('SUCCESS', 'PARTIAL', 'FAILED');

-- ---- subnets ----
CREATE TABLE subnets (
    id          BIGSERIAL PRIMARY KEY,
    cidr        CIDR        NOT NULL UNIQUE,
    site        TEXT,
    description TEXT,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- ---- devices: core entity + state machine + topology coords ----
CREATE TABLE devices (
    id              BIGSERIAL PRIMARY KEY,
    ip_address      INET          NOT NULL UNIQUE,

    name            TEXT          NOT NULL,
    name_source     name_source   NOT NULL DEFAULT 'IP',
    manual_name     TEXT,

    source_type     source_type   NOT NULL DEFAULT 'MANUAL',
    netbox_id       INTEGER,
    subnet_id       BIGINT REFERENCES subnets(id) ON DELETE SET NULL,

    status          device_status NOT NULL DEFAULT 'UNKNOWN',
    previous_status device_status,
    is_flapping     BOOLEAN       NOT NULL DEFAULT false,
    up_streak       INTEGER       NOT NULL DEFAULT 0,   -- consecutive ALIVE probes (debounce)
    down_streak     INTEGER       NOT NULL DEFAULT 0,   -- consecutive DEAD probes (debounce)
    last_change_at  TIMESTAMPTZ,
    last_seen_at    TIMESTAMPTZ,

    snmp_enabled    BOOLEAN       NOT NULL DEFAULT false,
    snmp_version    TEXT,
    snmp_community  TEXT,                                -- encrypted at rest (app-layer)
    sys_object_id   TEXT,
    sys_descr       TEXT,
    sys_name        TEXT,
    vendor          TEXT,
    model           TEXT,
    icon            TEXT          NOT NULL DEFAULT 'host',

    pos_x           DOUBLE PRECISION,
    pos_y           DOUBLE PRECISION,
    is_locked       BOOLEAN       NOT NULL DEFAULT false,

    created_at      TIMESTAMPTZ   NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ   NOT NULL DEFAULT now()
);

CREATE INDEX idx_devices_status      ON devices(status);
CREATE INDEX idx_devices_source_type ON devices(source_type);
CREATE INDEX idx_devices_subnet      ON devices(subnet_id);
CREATE UNIQUE INDEX idx_devices_netbox_id ON devices(netbox_id) WHERE netbox_id IS NOT NULL;

-- ---- interfaces ----
CREATE TABLE interfaces (
    id           BIGSERIAL PRIMARY KEY,
    device_id    BIGINT NOT NULL REFERENCES devices(id) ON DELETE CASCADE,
    if_index     INTEGER,
    if_name      TEXT,
    ip_address   INET,
    subnet_id    BIGINT REFERENCES subnets(id) ON DELETE SET NULL,
    mac_address  MACADDR,
    is_uplink    BOOLEAN NOT NULL DEFAULT false,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),

    UNIQUE (device_id, if_index)
);

CREATE INDEX idx_interfaces_device ON interfaces(device_id);
CREATE INDEX idx_interfaces_subnet ON interfaces(subnet_id);

-- ---- edges (AUTO + MANUAL topology links) ----
CREATE TABLE edges (
    id                  BIGSERIAL PRIMARY KEY,
    source_device_id    BIGINT NOT NULL REFERENCES devices(id) ON DELETE CASCADE,
    target_device_id    BIGINT NOT NULL REFERENCES devices(id) ON DELETE CASCADE,
    source_interface_id BIGINT REFERENCES interfaces(id) ON DELETE SET NULL,
    target_interface_id BIGINT REFERENCES interfaces(id) ON DELETE SET NULL,
    link_type           link_type NOT NULL DEFAULT 'AUTO',
    label               TEXT,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT now(),

    CHECK (source_device_id <> target_device_id),
    UNIQUE (source_device_id, target_device_id, link_type)
);

CREATE INDEX idx_edges_source ON edges(source_device_id);
CREATE INDEX idx_edges_target ON edges(target_device_id);
CREATE INDEX idx_edges_type   ON edges(link_type);

-- ---- alert_rules ----
CREATE TABLE alert_rules (
    id            BIGSERIAL PRIMARY KEY,
    device_id     BIGINT REFERENCES devices(id) ON DELETE CASCADE,   -- NULL = global rule
    channel       alert_channel NOT NULL,
    target        TEXT NOT NULL,
    on_down       BOOLEAN NOT NULL DEFAULT true,
    on_up         BOOLEAN NOT NULL DEFAULT true,
    on_flapping   BOOLEAN NOT NULL DEFAULT true,
    on_orphaned   BOOLEAN NOT NULL DEFAULT true,
    enabled       BOOLEAN NOT NULL DEFAULT true,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_alert_rules_device ON alert_rules(device_id);

-- ---- alert_events (fired-alert audit) ----
CREATE TABLE alert_events (
    id           BIGSERIAL PRIMARY KEY,
    device_id    BIGINT NOT NULL REFERENCES devices(id) ON DELETE CASCADE,
    from_status  device_status,
    to_status    device_status NOT NULL,
    is_flapping  BOOLEAN NOT NULL DEFAULT false,
    channel      alert_channel NOT NULL,
    delivered    BOOLEAN NOT NULL DEFAULT false,
    error        TEXT,
    fired_at     TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_alert_events_device ON alert_events(device_id, fired_at DESC);

-- ---- status_transitions (sliding window for flapping detection) ----
CREATE TABLE status_transitions (
    id           BIGSERIAL PRIMARY KEY,
    device_id    BIGINT NOT NULL REFERENCES devices(id) ON DELETE CASCADE,
    to_status    device_status NOT NULL,
    changed_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_transitions_device_time ON status_transitions(device_id, changed_at DESC);

-- ---- netbox_sync_logs ----
CREATE TABLE netbox_sync_logs (
    id            BIGSERIAL PRIMARY KEY,
    run_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    status        sync_status NOT NULL,
    ips_added     INTEGER NOT NULL DEFAULT 0,
    ips_updated   INTEGER NOT NULL DEFAULT 0,
    ips_orphaned  INTEGER NOT NULL DEFAULT 0,
    ips_restored  INTEGER NOT NULL DEFAULT 0,
    duration_ms   INTEGER,
    error         TEXT
);

CREATE INDEX idx_sync_logs_run ON netbox_sync_logs(run_at DESC);

-- ---- settings (singleton) ----
CREATE TABLE settings (
    id            SMALLINT PRIMARY KEY DEFAULT 1 CHECK (id = 1),
    netbox_url    TEXT,
    netbox_token  TEXT,                          -- encrypted at rest
    netbox_sync_enabled BOOLEAN NOT NULL DEFAULT true,
    snmp_default_version   TEXT DEFAULT 'v2c',
    snmp_default_community TEXT,                  -- encrypted at rest
    ping_interval_seconds  INTEGER NOT NULL DEFAULT 20,
    smtp_config   JSONB,
    telegram_bot_token TEXT,                      -- encrypted at rest
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Ensure the singleton row exists.
INSERT INTO settings (id) VALUES (1) ON CONFLICT (id) DO NOTHING;
