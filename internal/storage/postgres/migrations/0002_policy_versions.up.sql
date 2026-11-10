-- 0002_policy_versions: immutable policy versions and an append-only
-- activation log. The existing `policies` table becomes the editable draft
-- (workspace); publishing snapshots it into a new version atomically.

CREATE TABLE policy_versions (
    id             UUID PRIMARY KEY,
    number         INTEGER NOT NULL UNIQUE CHECK (number > 0),
    rules          JSONB NOT NULL,
    default_action TEXT NOT NULL CHECK (default_action IN ('allow', 'deny', 'quarantine')),
    digest         TEXT NOT NULL,
    note           TEXT NOT NULL DEFAULT '',
    created_by     TEXT NOT NULL DEFAULT '',
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE policy_activations (
    id                  BIGSERIAL PRIMARY KEY,
    version_id          UUID NOT NULL REFERENCES policy_versions (id),
    kind                TEXT NOT NULL CHECK (kind IN ('initial', 'publish', 'restore')),
    previous_version_id UUID REFERENCES policy_versions (id),
    activated_by        TEXT NOT NULL DEFAULT '',
    activated_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    note                TEXT NOT NULL DEFAULT ''
);
CREATE INDEX policy_activations_version_idx ON policy_activations (version_id);

-- Draft-level settings (single row).
CREATE TABLE policy_draft_settings (
    singleton      BOOLEAN PRIMARY KEY DEFAULT TRUE CHECK (singleton),
    default_action TEXT CHECK (default_action IN ('allow', 'deny', 'quarantine'))
);
INSERT INTO policy_draft_settings (singleton) VALUES (TRUE);

-- Published versions and activations are history: forbid rewriting them, even
-- by a buggy client that bypasses the service layer (defense in depth).
CREATE FUNCTION nac_forbid_history_change() RETURNS trigger AS $$
BEGIN
    RAISE EXCEPTION '% is append-only', TG_TABLE_NAME;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER policy_versions_immutable
    BEFORE UPDATE OR DELETE ON policy_versions
    FOR EACH ROW EXECUTE FUNCTION nac_forbid_history_change();
CREATE TRIGGER policy_activations_immutable
    BEFORE UPDATE OR DELETE ON policy_activations
    FOR EACH ROW EXECUTE FUNCTION nac_forbid_history_change();
