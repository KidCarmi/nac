-- 0004_posture_backfill: posture recorded before 0003 has no observation
-- time. The engine no longer falls back to endpoints.updated_at for posture
-- (an unrelated inventory edit would make an old result look fresh), so give
-- such rows the best known upper bound -- their last update before this
-- upgrade -- and mark the source so operators can tell.
UPDATE endpoints
SET posture_observed_at = updated_at,
    posture_source = 'pre-provenance'
WHERE posture <> 'unknown' AND posture_observed_at IS NULL;
