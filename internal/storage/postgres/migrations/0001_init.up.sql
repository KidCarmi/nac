-- 0001_init: foundational NAC schema.
--
-- Design notes:
--   * UUID primary keys (application-generated) avoid leaking row counts and
--     enable client-side ID generation without a round trip.
--   * TIMESTAMPTZ everywhere; the application writes UTC.
--   * JSONB for free-form attributes/metadata with GIN indexes for containment
--     queries.
--   * CHECK constraints encode the domain enums at the storage layer so the
--     database remains a source of truth even if a buggy client bypasses
--     service validation (defense in depth).

CREATE EXTENSION IF NOT EXISTS pgcrypto;

-- Operators: administrative users of the platform.
CREATE TABLE operators (
    id            UUID PRIMARY KEY,
    email         TEXT NOT NULL,
    password_hash TEXT NOT NULL,
    role          TEXT NOT NULL CHECK (role IN ('admin', 'operator', 'viewer')),
    disabled      BOOLEAN NOT NULL DEFAULT FALSE,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);
-- Case-insensitive uniqueness on email regardless of CITEXT availability.
CREATE UNIQUE INDEX operators_email_lower_uniq ON operators (lower(email));

-- Endpoints: devices observed on the network, keyed by MAC.
CREATE TABLE endpoints (
    id            UUID PRIMARY KEY,
    mac           TEXT NOT NULL,
    ip            TEXT,
    hostname      TEXT,
    device_type   TEXT,
    access_status TEXT NOT NULL DEFAULT 'unknown'
                  CHECK (access_status IN ('unknown', 'authorized', 'quarantined', 'denied')),
    posture       TEXT NOT NULL DEFAULT 'unknown'
                  CHECK (posture IN ('unknown', 'compliant', 'at_risk', 'noncompliant')),
    attributes    JSONB NOT NULL DEFAULT '{}'::jsonb,
    last_seen     TIMESTAMPTZ NOT NULL DEFAULT now(),
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX endpoints_mac_uniq ON endpoints (mac);
CREATE INDEX endpoints_access_status_idx ON endpoints (access_status);
CREATE INDEX endpoints_hostname_idx ON endpoints (hostname);
CREATE INDEX endpoints_attributes_gin ON endpoints USING GIN (attributes);

-- Policies: ordered access-control rules driving the decision engine.
CREATE TABLE policies (
    id          UUID PRIMARY KEY,
    name        TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    priority    INTEGER NOT NULL DEFAULT 100 CHECK (priority >= 0),
    enabled     BOOLEAN NOT NULL DEFAULT TRUE,
    conditions  JSONB NOT NULL DEFAULT '[]'::jsonb,
    action      TEXT NOT NULL CHECK (action IN ('allow', 'deny', 'quarantine')),
    segment     TEXT NOT NULL DEFAULT '',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX policies_name_uniq ON policies (lower(name));
-- The access engine reads enabled policies in priority order on the hot path.
CREATE INDEX policies_enabled_priority_idx ON policies (priority) WHERE enabled;

-- Audit events: append-only security log.
CREATE TABLE audit_events (
    id            UUID PRIMARY KEY,
    occurred_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    actor_id      UUID,
    actor_email   TEXT NOT NULL DEFAULT '',
    action        TEXT NOT NULL,
    resource_type TEXT NOT NULL,
    resource_id   TEXT NOT NULL DEFAULT '',
    outcome       TEXT NOT NULL,
    request_id    TEXT NOT NULL DEFAULT '',
    remote_addr   TEXT NOT NULL DEFAULT '',
    metadata      JSONB NOT NULL DEFAULT '{}'::jsonb
);
CREATE INDEX audit_events_occurred_at_idx ON audit_events (occurred_at DESC);
CREATE INDEX audit_events_actor_idx ON audit_events (actor_id);
CREATE INDEX audit_events_resource_idx ON audit_events (resource_type, resource_id);
