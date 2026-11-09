ALTER TABLE coa_requests DROP COLUMN requested_role;
ALTER TABLE radius_sessions DROP COLUMN requested_role;
ALTER TABLE auth_decisions DROP COLUMN role;
ALTER TABLE switches DROP CONSTRAINT switches_role_needs_aruba;
ALTER TABLE switches DROP COLUMN access_vlan;
ALTER TABLE switches DROP COLUMN segment_roles;
ALTER TABLE switches DROP COLUMN enforcement;
ALTER TABLE switches DROP COLUMN platform;
