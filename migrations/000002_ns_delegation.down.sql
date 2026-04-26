-- Reverse Phase B.1: NS delegation status tracking on the domains table.

DROP INDEX IF EXISTS idx_domains_ns_delegation_status;

ALTER TABLE domains
  DROP COLUMN IF EXISTS ns_delegation_status,
  DROP COLUMN IF EXISTS ns_verified_at,
  DROP COLUMN IF EXISTS ns_last_checked_at,
  DROP COLUMN IF EXISTS ns_actual;
