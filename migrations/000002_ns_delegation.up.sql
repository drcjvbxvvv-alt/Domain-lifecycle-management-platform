-- Phase B.1: NS delegation status tracking on the domains table.
-- Tracks whether the domain's nameservers have been delegated to the
-- configured DNS provider and what the current propagation state is.
--
-- States: unset | pending | verified | mismatch
--   unset    : no dns_provider_id is set (default)
--   pending  : dns_provider_id just set, waiting for NS propagation
--   verified : live NS matches expected NS from provider
--   mismatch : live NS differs from expected NS (or check timed out)

ALTER TABLE domains
  ADD COLUMN IF NOT EXISTS ns_delegation_status  VARCHAR(30)  NOT NULL DEFAULT 'unset',
  ADD COLUMN IF NOT EXISTS ns_verified_at         TIMESTAMPTZ,
  ADD COLUMN IF NOT EXISTS ns_last_checked_at     TIMESTAMPTZ,
  ADD COLUMN IF NOT EXISTS ns_actual              TEXT[]       NOT NULL DEFAULT '{}';

COMMENT ON COLUMN domains.ns_delegation_status IS
  'unset: no DNS provider set; pending: set, awaiting NS propagation; '
  'verified: NS matches provider; mismatch: NS mismatch or check error';

-- Index to efficiently find domains that need a periodic NS check.
CREATE INDEX IF NOT EXISTS idx_domains_ns_delegation_status
  ON domains (ns_delegation_status)
  WHERE deleted_at IS NULL;
