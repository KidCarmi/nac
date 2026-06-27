-- Rollback for 0001_init. Order matters only if foreign keys are later added;
-- today the tables are independent. pgcrypto is left installed because other
-- objects in the database may depend on it.
DROP TABLE IF EXISTS audit_events;
DROP TABLE IF EXISTS policies;
DROP TABLE IF EXISTS endpoints;
DROP TABLE IF EXISTS operators;
