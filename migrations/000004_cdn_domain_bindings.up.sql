-- migrations/000004_cdn_domain_bindings.up.sql
-- Phase C.2: CDN domain lifecycle — bindings, config snapshots, content tasks.

-- ── domain_cdn_bindings ───────────────────────────────────────────────────────
-- Records the binding between a platform domain and a CDN account.
-- One domain may be bound to multiple CDN accounts (e.g. Aliyun + Cloudflare),
-- but each (domain, cdn_account) pair must be unique at any point in time.
CREATE TABLE IF NOT EXISTS domain_cdn_bindings (
    id              BIGSERIAL PRIMARY KEY,
    uuid            UUID        NOT NULL DEFAULT gen_random_uuid(),
    domain_id       BIGINT      NOT NULL REFERENCES domains(id),
    cdn_account_id  BIGINT      NOT NULL REFERENCES cdn_accounts(id),
    -- CNAME assigned by the CDN provider — set when the domain is successfully added.
    cdn_cname       VARCHAR(500),
    -- CDN acceleration type: web | download | media
    business_type   VARCHAR(30) NOT NULL DEFAULT 'web',
    -- Mirrors cdn.DomainStatus*: online | offline | configuring | checking
    status          VARCHAR(30) NOT NULL DEFAULT 'offline',
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    deleted_at      TIMESTAMPTZ
);

-- Partial unique index: one active binding per (domain, cdn_account) pair.
-- Soft-deleted rows are excluded, so a domain can be re-bound after unbinding.
CREATE UNIQUE INDEX IF NOT EXISTS uq_active_domain_cdn_binding
    ON domain_cdn_bindings (domain_id, cdn_account_id)
    WHERE deleted_at IS NULL;

CREATE INDEX IF NOT EXISTS idx_domain_cdn_bindings_domain
    ON domain_cdn_bindings (domain_id)
    WHERE deleted_at IS NULL;

CREATE INDEX IF NOT EXISTS idx_domain_cdn_bindings_account
    ON domain_cdn_bindings (cdn_account_id)
    WHERE deleted_at IS NULL;

-- ── cdn_domain_configs ────────────────────────────────────────────────────────
-- Stores the last-known configuration for each config category (cache, origin,
-- access_control, https, performance) per binding.  The config column holds the
-- provider response as a JSONB snapshot; synced_at records when it was fetched.
CREATE TABLE IF NOT EXISTS cdn_domain_configs (
    id           BIGSERIAL   PRIMARY KEY,
    binding_id   BIGINT      NOT NULL REFERENCES domain_cdn_bindings(id),
    -- cache | origin | access_control | https | performance
    config_type  VARCHAR(30) NOT NULL,
    config       JSONB       NOT NULL DEFAULT '{}',
    synced_at    TIMESTAMPTZ,
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT uq_cdn_domain_config UNIQUE (binding_id, config_type)
);

CREATE INDEX IF NOT EXISTS idx_cdn_domain_configs_binding
    ON cdn_domain_configs (binding_id);

-- ── cdn_content_tasks ─────────────────────────────────────────────────────────
-- Tracks purge and prefetch jobs submitted to a CDN provider.
-- provider_task_id is the task ID returned by the CDN API (used for polling).
CREATE TABLE IF NOT EXISTS cdn_content_tasks (
    id               BIGSERIAL    PRIMARY KEY,
    uuid             UUID         NOT NULL DEFAULT gen_random_uuid(),
    binding_id       BIGINT       NOT NULL REFERENCES domain_cdn_bindings(id),
    -- purge_url | purge_dir | prefetch
    task_type        VARCHAR(20)  NOT NULL,
    provider_task_id VARCHAR(255),
    -- pending | processing | done | failed
    status           VARCHAR(20)  NOT NULL DEFAULT 'pending',
    targets          TEXT[]       NOT NULL DEFAULT '{}',
    created_by       BIGINT       REFERENCES users(id),
    created_at       TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    completed_at     TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS idx_cdn_content_tasks_binding
    ON cdn_content_tasks (binding_id);

-- Partial index to efficiently scan in-progress tasks for polling.
CREATE INDEX IF NOT EXISTS idx_cdn_content_tasks_pending
    ON cdn_content_tasks (status)
    WHERE status NOT IN ('done', 'failed');
