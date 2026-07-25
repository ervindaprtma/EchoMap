-- 0004: custom device monitors + alert sounds — Phase 9 (Pillar 13; alert-sounds
-- groundwork for Pillar 14). Doc 2 §4.4–4.6. Applied on a fresh pgdata volume via
-- docker-entrypoint-initdb.d (like 0001–0003); existing DB = manual psql -f.

CREATE TYPE monitor_kind   AS ENUM ('TCP', 'HTTP', 'HTTPS');
CREATE TYPE monitor_status AS ENUM ('UP', 'DOWN', 'UNKNOWN');

CREATE TABLE device_monitors (
    id               BIGSERIAL PRIMARY KEY,
    device_id        BIGINT NOT NULL REFERENCES devices(id) ON DELETE CASCADE,
    label            TEXT   NOT NULL,
    kind             monitor_kind NOT NULL,
    port             INTEGER CHECK (port BETWEEN 1 AND 65535),   -- required for TCP; kind default for HTTP(S)
    path             TEXT,                            -- HTTP(S) path on the device
    url_override     TEXT,                            -- absolute URL health check; wins over ip+port+path
    expect_status_lo INTEGER NOT NULL DEFAULT 200,
    expect_status_hi INTEGER NOT NULL DEFAULT 399,
    interval_seconds INTEGER NOT NULL DEFAULT 60 CHECK (interval_seconds BETWEEN 10 AND 86400),
    timeout_ms       INTEGER NOT NULL DEFAULT 5000,
    enabled          BOOLEAN NOT NULL DEFAULT true,

    -- state machine (mirrors devices; Doc 3 §2 debounce)
    status           monitor_status NOT NULL DEFAULT 'UNKNOWN',
    up_streak        INTEGER NOT NULL DEFAULT 0,
    down_streak      INTEGER NOT NULL DEFAULT 0,
    last_change_at   TIMESTAMPTZ,
    last_check_at    TIMESTAMPTZ,
    next_check_at    TIMESTAMPTZ,                     -- scheduler cursor: due when NULL or <= now()
    last_duration_ms DOUBLE PRECISION,
    last_http_status INTEGER,
    cert_expires_at  TIMESTAMPTZ,                     -- HTTPS only; feeds days-to-expiry chart

    created_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT now(),

    UNIQUE (device_id, label)
);

CREATE INDEX idx_monitors_due    ON device_monitors(next_check_at) WHERE enabled;
CREATE INDEX idx_monitors_device ON device_monitors(device_id);

CREATE TABLE alert_sounds (
    id         BIGSERIAL PRIMARY KEY,
    name       TEXT  NOT NULL UNIQUE,
    data       BYTEA NOT NULL,                  -- audio/wav, <= 1 MB (enforced at upload, Phase 11)
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Which sound plays per event class; NULL = built-in WebAudio chime (assignment UI is Phase 11).
ALTER TABLE settings
    ADD COLUMN sound_device_down_id  BIGINT REFERENCES alert_sounds(id) ON DELETE SET NULL,
    ADD COLUMN sound_device_up_id    BIGINT REFERENCES alert_sounds(id) ON DELETE SET NULL,
    ADD COLUMN sound_monitor_down_id BIGINT REFERENCES alert_sounds(id) ON DELETE SET NULL,
    ADD COLUMN sound_monitor_up_id   BIGINT REFERENCES alert_sounds(id) ON DELETE SET NULL;
