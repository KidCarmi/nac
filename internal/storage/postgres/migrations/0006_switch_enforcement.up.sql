-- Bounded Aruba role path (ADR-0009). A switch states its platform and how
-- NAC enforces a decided segment on it:
--   vlan: RADIUS VLAN attributes (the Arista path, unchanged)
--   role: a local switch role named in Aruba-User-Role, keeping VLAN and IP
ALTER TABLE switches ADD COLUMN platform TEXT NOT NULL DEFAULT 'arista-eos'
    CHECK (platform IN ('arista-eos', 'aruba-aos-cx'));
ALTER TABLE switches ADD COLUMN enforcement TEXT NOT NULL DEFAULT 'vlan'
    CHECK (enforcement IN ('vlan', 'role'));
ALTER TABLE switches ADD COLUMN segment_roles JSONB NOT NULL DEFAULT '{}'::jsonb;
ALTER TABLE switches ADD COLUMN access_vlan INTEGER CHECK (access_vlan BETWEEN 1 AND 4094);
ALTER TABLE switches ADD CONSTRAINT switches_role_needs_aruba
    CHECK (enforcement = 'vlan' OR platform = 'aruba-aos-cx');

ALTER TABLE auth_decisions ADD COLUMN role TEXT NOT NULL DEFAULT '';
ALTER TABLE radius_sessions ADD COLUMN requested_role TEXT NOT NULL DEFAULT '';
ALTER TABLE coa_requests ADD COLUMN requested_role TEXT NOT NULL DEFAULT '';
