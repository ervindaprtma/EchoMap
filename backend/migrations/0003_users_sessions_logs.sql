-- 0003: users, sessions, event logs — PRD Phase 8 (Pillars 10 & 11; Doc 2 §4.1–4.3).
-- Applied on a fresh pgdata volume via docker-entrypoint-initdb.d (like 0001/0002);
-- for an existing DB, apply once by hand (psql -f) — there is no online migration runner yet.

-- Hierarchical roles (SUPERADMIN > ADMIN > OPERATOR), enforced server-side per route.
CREATE TYPE user_role AS ENUM ('SUPERADMIN', 'ADMIN', 'OPERATOR');

-- Event-log severities (Doc 2 §4.3). AUDIT rows are Superadmin-only at the API layer.
CREATE TYPE log_level AS ENUM ('INFO', 'NOTICE', 'ALERT', 'ERROR', 'AUDIT');

CREATE TABLE users (
    id            BIGSERIAL PRIMARY KEY,
    username      TEXT      NOT NULL UNIQUE,
    password_hash TEXT      NOT NULL,                    -- argon2id (never any other scheme)
    role          user_role NOT NULL DEFAULT 'OPERATOR',
    enabled       BOOLEAN   NOT NULL DEFAULT true,
    must_change_password BOOLEAN NOT NULL DEFAULT false, -- true on the seeded bootstrap Superadmin
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- The cookie carries an opaque 256-bit token; only its SHA-256 hash lands here,
-- so a DB leak leaks no usable sessions. Idle (12 h via last_seen_at) and absolute
-- (7 d via created_at) expiry are checked at auth time; a deleted row is instant revoke.
CREATE TABLE sessions (
    id           BIGSERIAL PRIMARY KEY,
    token_hash   BYTEA       NOT NULL UNIQUE,            -- sha256(raw token)
    user_id      BIGINT      NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    ip           INET,
    user_agent   TEXT,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_seen_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_sessions_user ON sessions(user_id);

-- Append-only. Written by status/monitor transitions (INFO), alert deliveries
-- (ALERT/ERROR), config edits + resolver IP changes + sync outcomes (NOTICE),
-- and auth/user administration (AUDIT). Pruned at 90 d by the housekeeping sweep.
CREATE TABLE event_logs (
    id         BIGSERIAL   PRIMARY KEY,
    ts         TIMESTAMPTZ NOT NULL DEFAULT now(),
    level      log_level   NOT NULL,
    category   TEXT        NOT NULL,                     -- 'device'|'monitor'|'alert'|'auth'|'config'|'system'
    message    TEXT        NOT NULL,
    device_id  BIGINT REFERENCES devices(id) ON DELETE SET NULL,
    user_id    BIGINT REFERENCES users(id)   ON DELETE SET NULL,
    meta       JSONB
);

CREATE INDEX idx_event_logs_ts       ON event_logs(ts DESC);
CREATE INDEX idx_event_logs_level_ts ON event_logs(level, ts DESC);
CREATE INDEX idx_event_logs_device   ON event_logs(device_id) WHERE device_id IS NOT NULL;
