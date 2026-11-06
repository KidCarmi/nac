-- Operator session revocation. Every operator JWT carries the token_version
-- it was issued under; the API rejects a token whose version is not the
-- operator's current one. A password change (or a local reset) increments it,
-- which signs out every earlier session of that operator.
ALTER TABLE operators ADD COLUMN token_version INTEGER NOT NULL DEFAULT 0;
ALTER TABLE operators ADD COLUMN password_changed_at TIMESTAMPTZ;
