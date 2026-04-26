-- Phase B.3: Domain DNS records — local snapshot table.
-- Mirrors DNS records held in the provider's zone, keyed by
-- (domain_id, record_type, name, content). Used for CRUD operations
-- that first call the provider API and then persist locally on success.

CREATE TABLE IF NOT EXISTS domain_dns_records (
  id                   BIGSERIAL PRIMARY KEY,
  uuid                 UUID         NOT NULL DEFAULT gen_random_uuid(),
  domain_id            BIGINT       NOT NULL REFERENCES domains(id),
  dns_provider_id      BIGINT       REFERENCES dns_providers(id),
  provider_record_id   VARCHAR(255),                            -- provider-side record ID
  record_type          VARCHAR(10)  NOT NULL,
  name                 VARCHAR(255) NOT NULL,
  content              TEXT         NOT NULL,
  ttl                  INT          NOT NULL DEFAULT 300,
  priority             INT,                                     -- MX / SRV
  proxied              BOOLEAN      NOT NULL DEFAULT FALSE,     -- Cloudflare
  extra                JSONB        NOT NULL DEFAULT '{}',      -- provider-specific fields
  synced_at            TIMESTAMPTZ,                             -- last provider sync time
  created_at           TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
  updated_at           TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
  deleted_at           TIMESTAMPTZ
);

COMMENT ON TABLE domain_dns_records IS
  'Local snapshot of DNS records managed via DNS provider APIs (B.3). '
  'Records are written after a successful provider API call. '
  'Use provider_record_id to map back to the provider-side record. '
  'Unique constraint is enforced in application code (soft-delete rows are excluded).';

CREATE INDEX IF NOT EXISTS idx_domain_dns_records_domain
  ON domain_dns_records (domain_id)
  WHERE deleted_at IS NULL;

CREATE INDEX IF NOT EXISTS idx_domain_dns_records_provider_record
  ON domain_dns_records (provider_record_id)
  WHERE deleted_at IS NULL AND provider_record_id IS NOT NULL;
