-- 0003_pilot_radius: switches, identity bindings, RADIUS sessions, decision
-- records, the authentication timeline and CoA actions.
--
-- The four things an operator must never confuse are separate tables:
--   endpoints          the DEVICE record (inventory, posture)
--   identity_bindings  the explicit mapping from an AUTHENTICATED certificate
--                      identity (issuer + serial) to a device
--   radius_sessions    an ACTIVE SESSION on a switch port
--   auth_decisions     an AUTHORIZATION DECISION with its inputs and version

-- Posture provenance: who reported it and when, so freshness can be judged.
ALTER TABLE endpoints
    ADD COLUMN posture_source      TEXT NOT NULL DEFAULT '',
    ADD COLUMN posture_observed_at TIMESTAMPTZ;

CREATE TABLE switches (
    id                 UUID PRIMARY KEY,
    name               TEXT NOT NULL,
    -- Source IP FreeRADIUS sees for this switch's RADIUS packets.
    radius_source_ip   TEXT NOT NULL,
    -- NAS-IP-Address sent in CoA requests (lab: same as source IP).
    nas_ip             TEXT NOT NULL,
    radius_secret_enc  TEXT NOT NULL,
    coa_port           INTEGER NOT NULL DEFAULT 3799 CHECK (coa_port BETWEEN 1 AND 65535),
    -- {"corp": 10, "quarantine": 20}
    segment_vlans      JSONB NOT NULL,
    access_ports       JSONB NOT NULL DEFAULT '[]'::jsonb,
    eos_version        TEXT NOT NULL DEFAULT '4.29.2F',
    radius_server_ip   TEXT NOT NULL DEFAULT '',
    eapi_url           TEXT NOT NULL DEFAULT '',
    eapi_username      TEXT NOT NULL DEFAULT '',
    eapi_password_enc  TEXT NOT NULL DEFAULT '',
    last_radius_at     TIMESTAMPTZ,
    last_probe_at      TIMESTAMPTZ,
    last_probe_ok      BOOLEAN,
    last_probe_detail  TEXT NOT NULL DEFAULT '',
    verified_at        TIMESTAMPTZ,
    verified_detail    TEXT NOT NULL DEFAULT '',
    verified_session_id UUID,
    created_at         TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at         TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX switches_name_uniq ON switches (lower(name));
CREATE UNIQUE INDEX switches_source_ip_uniq ON switches (radius_source_ip);

CREATE TABLE identity_bindings (
    id          UUID PRIMARY KEY,
    endpoint_id UUID NOT NULL REFERENCES endpoints (id) ON DELETE CASCADE,
    -- Issuer DN as FreeRADIUS reports it (TLS-Client-Cert-Issuer).
    issuer      TEXT NOT NULL,
    -- Normalised serial: upper-case hex, no separators or leading zeros.
    serial      TEXT NOT NULL,
    -- Optional pin on the certificate CN; empty = not checked.
    subject_cn  TEXT NOT NULL DEFAULT '',
    revision    INTEGER NOT NULL DEFAULT 1,
    note        TEXT NOT NULL DEFAULT '',
    created_by  TEXT NOT NULL DEFAULT '',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX identity_bindings_cert_uniq ON identity_bindings (issuer, serial);
CREATE INDEX identity_bindings_endpoint_idx ON identity_bindings (endpoint_id);

CREATE TABLE radius_sessions (
    id                UUID PRIMARY KEY,
    switch_id         UUID NOT NULL REFERENCES switches (id) ON DELETE CASCADE,
    nas_port_id       TEXT NOT NULL,
    mac               TEXT NOT NULL,
    -- Calling-Station-Id exactly as the switch sent it; CoA echoes it back.
    calling_station_id TEXT NOT NULL DEFAULT '',
    nas_ip_address    TEXT NOT NULL DEFAULT '',
    user_name         TEXT NOT NULL DEFAULT '',
    acct_session_id   TEXT NOT NULL DEFAULT '',
    cert_issuer       TEXT NOT NULL DEFAULT '',
    cert_serial       TEXT NOT NULL DEFAULT '',
    cert_cn           TEXT NOT NULL DEFAULT '',
    endpoint_id       UUID REFERENCES endpoints (id) ON DELETE SET NULL,
    binding_id        UUID REFERENCES identity_bindings (id) ON DELETE SET NULL,
    state             TEXT NOT NULL CHECK (state IN ('active', 'rejected', 'ended')),
    override          TEXT NOT NULL DEFAULT '' CHECK (override IN ('', 'quarantine')),
    override_by       TEXT NOT NULL DEFAULT '',
    override_at       TIMESTAMPTZ,
    last_decision_id  UUID,
    requested_action  TEXT NOT NULL DEFAULT '',
    requested_segment TEXT NOT NULL DEFAULT '',
    requested_vlan    INTEGER,
    started_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_auth_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    ended_at          TIMESTAMPTZ,
    observed_status   TEXT NOT NULL DEFAULT '',
    observed_vlan     TEXT NOT NULL DEFAULT '',
    observed_at       TIMESTAMPTZ,
    observed_source   TEXT NOT NULL DEFAULT '',
    observed_detail   TEXT NOT NULL DEFAULT ''
);
-- At most one non-ended session per switch port and MAC.
CREATE UNIQUE INDEX radius_sessions_current_uniq ON radius_sessions (switch_id, nas_port_id, mac) WHERE state <> 'ended';
CREATE INDEX radius_sessions_last_auth_idx ON radius_sessions (last_auth_at DESC);

CREATE TABLE auth_decisions (
    id                UUID PRIMARY KEY,
    occurred_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    session_id        UUID REFERENCES radius_sessions (id) ON DELETE SET NULL,
    switch_id         UUID REFERENCES switches (id) ON DELETE SET NULL,
    subject           TEXT NOT NULL DEFAULT '',
    inputs            JSONB NOT NULL,
    policy_version    INTEGER,
    policy_version_id UUID REFERENCES policy_versions (id),
    matched_rule      TEXT NOT NULL DEFAULT '',
    matched_rule_id   UUID,
    trace             JSONB NOT NULL DEFAULT '[]'::jsonb,
    insufficient      JSONB NOT NULL DEFAULT '[]'::jsonb,
    action            TEXT NOT NULL,
    segment           TEXT NOT NULL DEFAULT '',
    vlan              INTEGER,
    result            TEXT NOT NULL CHECK (result IN ('accept', 'reject')),
    reason            TEXT NOT NULL DEFAULT ''
);
CREATE INDEX auth_decisions_session_idx ON auth_decisions (session_id, occurred_at DESC);
CREATE INDEX auth_decisions_occurred_idx ON auth_decisions (occurred_at DESC);

-- Timeline: everything that happened to a session or a switch, including
-- certificate failures (no decision is made), outage rejects imported from
-- FreeRADIUS, CoA steps and switch observations.
CREATE TABLE auth_events (
    id          UUID PRIMARY KEY,
    occurred_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    session_id  UUID REFERENCES radius_sessions (id) ON DELETE SET NULL,
    switch_id   UUID REFERENCES switches (id) ON DELETE SET NULL,
    client_ip   TEXT NOT NULL DEFAULT '',
    nas_port_id TEXT NOT NULL DEFAULT '',
    mac         TEXT NOT NULL DEFAULT '',
    stage       TEXT NOT NULL CHECK (stage IN ('eap-tls', 'nac-decision', 'outage', 'coa', 'observation', 'accounting', 'operator')),
    result      TEXT NOT NULL CHECK (result IN ('accept', 'reject', 'info', 'error')),
    reason      TEXT NOT NULL DEFAULT '',
    source      TEXT NOT NULL DEFAULT '',
    decision_id UUID,
    coa_id      UUID,
    detail      JSONB NOT NULL DEFAULT '{}'::jsonb,
    -- Deduplicates imported lines (e.g. FreeRADIUS outage log offset).
    import_key  TEXT
);
CREATE INDEX auth_events_session_idx ON auth_events (session_id, occurred_at);
CREATE INDEX auth_events_occurred_idx ON auth_events (occurred_at DESC);
CREATE INDEX auth_events_mac_idx ON auth_events (mac, occurred_at DESC);
CREATE UNIQUE INDEX auth_events_import_uniq ON auth_events (import_key) WHERE import_key IS NOT NULL;

CREATE TABLE coa_requests (
    id                UUID PRIMARY KEY,
    session_id        UUID NOT NULL REFERENCES radius_sessions (id) ON DELETE CASCADE,
    switch_id         UUID NOT NULL REFERENCES switches (id) ON DELETE CASCADE,
    kind              TEXT NOT NULL CHECK (kind IN ('quarantine', 'restore', 'reauthenticate')),
    -- Exact identifiers captured when the action was requested.
    target            JSONB NOT NULL,
    requested_segment TEXT NOT NULL DEFAULT '',
    requested_vlan    INTEGER,
    state             TEXT NOT NULL CHECK (state IN ('requested', 'sent', 'acked', 'nak', 'timeout', 'failed', 'observed')),
    attempts          INTEGER NOT NULL DEFAULT 0,
    radius_id         INTEGER,
    error_cause       INTEGER,
    detail            TEXT NOT NULL DEFAULT '',
    created_by        TEXT NOT NULL DEFAULT '',
    created_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    sent_at           TIMESTAMPTZ,
    responded_at      TIMESTAMPTZ,
    observed_at       TIMESTAMPTZ,
    observed_decision_id UUID,
    updated_at        TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX coa_requests_session_idx ON coa_requests (session_id, created_at DESC);
