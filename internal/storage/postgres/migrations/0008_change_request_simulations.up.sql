-- 0008_change_request_simulations: frozen-input historical replay for
-- review-only change requests (slice A, milestone 3).
--
-- A simulation runs on a persisted INPUT SET, not on a cutoff time: the
-- recorded decision inputs and the session overrides are COPIED (sessions
-- and overrides change), the active policy version is an immutable
-- reference plus its digest, the segment environment and the input catalog
-- are copied with their digests. Re-running a simulation from its input set
-- reproduces its canonical output; refreshing the inputs creates a new input
-- set and a new simulation. Both are immutable.

CREATE TABLE change_request_input_sets (
    id                    UUID PRIMARY KEY,
    change_request_id     UUID NOT NULL,
    revision              INTEGER NOT NULL,
    content_digest        TEXT NOT NULL,
    input_set_digest      TEXT NOT NULL,
    active_version_id     UUID NOT NULL REFERENCES policy_versions (id),
    active_version_number INTEGER NOT NULL,
    active_version_digest TEXT NOT NULL,
    environment           JSONB NOT NULL,
    environment_digest    TEXT NOT NULL,
    catalog               JSONB NOT NULL,
    catalog_digest        TEXT NOT NULL,
    window_since          TIMESTAMPTZ NOT NULL,
    case_limit            INTEGER NOT NULL CHECK (case_limit > 0),
    cases                 JSONB NOT NULL,
    created_by            TEXT NOT NULL,
    created_at            TIMESTAMPTZ NOT NULL,
    FOREIGN KEY (change_request_id, revision) REFERENCES change_request_revisions (change_request_id, revision)
);

CREATE TABLE change_request_simulations (
    id                UUID PRIMARY KEY,
    change_request_id UUID NOT NULL,
    revision          INTEGER NOT NULL,
    content_digest    TEXT NOT NULL,
    input_set_id      UUID NOT NULL REFERENCES change_request_input_sets (id),
    input_set_digest  TEXT NOT NULL,
    rows              JSONB NOT NULL,
    summary           JSONB NOT NULL,
    coverage          JSONB NOT NULL,
    output_digest     TEXT NOT NULL,
    label             TEXT NOT NULL,
    created_by        TEXT NOT NULL,
    created_at        TIMESTAMPTZ NOT NULL,
    FOREIGN KEY (change_request_id, revision) REFERENCES change_request_revisions (change_request_id, revision)
);
CREATE INDEX change_request_simulations_cr_idx ON change_request_simulations (change_request_id, created_at);

CREATE TRIGGER change_request_input_sets_immutable
    BEFORE UPDATE OR DELETE ON change_request_input_sets
    FOR EACH ROW EXECUTE FUNCTION nac_forbid_history_change();
CREATE TRIGGER change_request_simulations_immutable
    BEFORE UPDATE OR DELETE ON change_request_simulations
    FOR EACH ROW EXECUTE FUNCTION nac_forbid_history_change();
