DROP TABLE IF EXISTS coa_requests;
DROP TABLE IF EXISTS auth_events;
DROP TABLE IF EXISTS auth_decisions;
DROP TABLE IF EXISTS radius_sessions;
DROP TABLE IF EXISTS identity_bindings;
DROP TABLE IF EXISTS switches;
ALTER TABLE endpoints DROP COLUMN IF EXISTS posture_observed_at, DROP COLUMN IF EXISTS posture_source;
