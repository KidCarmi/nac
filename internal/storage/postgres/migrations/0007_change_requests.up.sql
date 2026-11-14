-- 0007_change_requests: review-only change requests (ADR-0008 slice A).
--
-- A change request is a reviewed PROPOSAL. Nothing in these tables changes
-- the active policy, a session, a CoA or a switch: there is no activation,
-- approval or dispatch state here, and the state CHECK excludes them.
-- Revisions and validations are immutable history; a change request is
-- never deleted (it is cancelled).

CREATE TABLE change_requests (
    id           UUID PRIMARY KEY,
    title        TEXT NOT NULL CHECK (length(title) BETWEEN 1 AND 200),
    intent       TEXT NOT NULL DEFAULT '' CHECK (length(intent) <= 4000),
    workflow     TEXT NOT NULL DEFAULT 'review_only' CHECK (workflow = 'review_only'),
    state        TEXT NOT NULL CHECK (state IN ('draft', 'validated', 'simulated', 'cancelled')),
    revision     INTEGER NOT NULL CHECK (revision > 0),
    requested_by TEXT NOT NULL,
    created_at   TIMESTAMPTZ NOT NULL,
    updated_at   TIMESTAMPTZ NOT NULL
);
CREATE INDEX change_requests_updated_idx ON change_requests (updated_at DESC);

-- One row per revision. base_version_* is the immutable active policy
-- version the revision was written against (no activation records exist yet;
-- see the implementation plan, correction 2).
CREATE TABLE change_request_revisions (
    change_request_id   UUID NOT NULL REFERENCES change_requests (id),
    revision            INTEGER NOT NULL CHECK (revision > 0),
    rules               JSONB NOT NULL,
    default_action      TEXT NOT NULL CHECK (default_action IN ('allow', 'deny', 'quarantine')),
    content_digest      TEXT NOT NULL,
    base_version_id     UUID NOT NULL REFERENCES policy_versions (id),
    base_version_digest TEXT NOT NULL,
    author              TEXT NOT NULL,
    created_at          TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (change_request_id, revision)
);

CREATE TABLE change_request_validations (
    id                 UUID PRIMARY KEY,
    change_request_id  UUID NOT NULL,
    revision           INTEGER NOT NULL,
    content_digest     TEXT NOT NULL,
    environment_digest TEXT NOT NULL,
    valid              BOOLEAN NOT NULL,
    result             JSONB NOT NULL,
    created_by         TEXT NOT NULL,
    created_at         TIMESTAMPTZ NOT NULL,
    FOREIGN KEY (change_request_id, revision) REFERENCES change_request_revisions (change_request_id, revision)
);
CREATE INDEX change_request_validations_cr_idx ON change_request_validations (change_request_id, created_at);

CREATE TRIGGER change_request_revisions_immutable
    BEFORE UPDATE OR DELETE ON change_request_revisions
    FOR EACH ROW EXECUTE FUNCTION nac_forbid_history_change();
CREATE TRIGGER change_request_validations_immutable
    BEFORE UPDATE OR DELETE ON change_request_validations
    FOR EACH ROW EXECUTE FUNCTION nac_forbid_history_change();
-- Rows are updated (state, revision) but never deleted.
CREATE TRIGGER change_requests_no_delete
    BEFORE DELETE ON change_requests
    FOR EACH ROW EXECUTE FUNCTION nac_forbid_history_change();
