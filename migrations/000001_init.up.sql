-- 000001_init.up.sql — Phase 1 schema + P2-P4 tables (pre-launch exception).
-- See docs/DATABASE_SCHEMA.md for field-level docs and rationale.
-- Pre-launch exception (ADR-0003 D9): this file may be edited in place during P1.

-- ============================================================
-- Extensions
-- ============================================================
CREATE EXTENSION IF NOT EXISTS "pgcrypto";

-- ============================================================
-- USERS & ROLES                                              [P1]
-- ============================================================
CREATE TABLE users (
    id            BIGSERIAL PRIMARY KEY,
    uuid          UUID NOT NULL DEFAULT gen_random_uuid(),
    username      VARCHAR(64) NOT NULL,
    password_hash VARCHAR(255) NOT NULL,
    display_name  VARCHAR(100),
    status        VARCHAR(20) NOT NULL DEFAULT 'active',
    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    deleted_at    TIMESTAMPTZ,
    CONSTRAINT uq_users_username  UNIQUE (username),
    CONSTRAINT chk_users_username CHECK (username ~ '^[a-zA-Z0-9_.-]{3,64}$'),
    CONSTRAINT chk_users_status   CHECK (status IN ('active', 'disabled'))
);
CREATE INDEX idx_users_username ON users (username) WHERE deleted_at IS NULL;

-- Five roles per ADR-0003 D7
CREATE TABLE roles (
    id          BIGSERIAL PRIMARY KEY,
    name        VARCHAR(32) NOT NULL,
    description TEXT,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT uq_roles_name  UNIQUE (name),
    CONSTRAINT chk_roles_name CHECK (name IN ('viewer', 'operator', 'release_manager', 'admin', 'auditor'))
);

CREATE TABLE user_roles (
    user_id    BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    role_id    BIGINT NOT NULL REFERENCES roles(id) ON DELETE CASCADE,
    granted_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    granted_by BIGINT REFERENCES users(id),
    PRIMARY KEY (user_id, role_id)
);
CREATE INDEX idx_user_roles_role ON user_roles (role_id);

-- ============================================================
-- PROJECTS                                                   [P1]
-- ============================================================
CREATE TABLE projects (
    id          BIGSERIAL PRIMARY KEY,
    uuid        UUID NOT NULL DEFAULT gen_random_uuid(),
    name        VARCHAR(100) NOT NULL,
    slug        VARCHAR(100) NOT NULL,
    description TEXT,
    is_prod     BOOLEAN NOT NULL DEFAULT false,
    owner_id    BIGINT REFERENCES users(id),
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    deleted_at  TIMESTAMPTZ,
    CONSTRAINT uq_projects_name UNIQUE (name),
    CONSTRAINT uq_projects_slug UNIQUE (slug),
    CONSTRAINT chk_projects_slug CHECK (slug ~ '^[a-z0-9][a-z0-9-]{1,98}[a-z0-9]$')
);

-- ============================================================
-- REGISTRARS & DNS PROVIDERS (Domain Asset Layer)            [PA]
-- ============================================================
CREATE TABLE registrars (
    id            BIGSERIAL PRIMARY KEY,
    uuid          UUID NOT NULL DEFAULT gen_random_uuid(),
    name          VARCHAR(128) NOT NULL,
    url           VARCHAR(512),
    api_type      VARCHAR(64),
    capabilities  JSONB NOT NULL DEFAULT '{}',
    notes         TEXT,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    deleted_at    TIMESTAMPTZ,
    CONSTRAINT uq_registrars_name UNIQUE (name)
);

CREATE TABLE registrar_accounts (
    id              BIGSERIAL PRIMARY KEY,
    uuid            UUID NOT NULL DEFAULT gen_random_uuid(),
    registrar_id    BIGINT NOT NULL REFERENCES registrars(id),
    account_name    VARCHAR(256) NOT NULL,
    owner_user_id   BIGINT REFERENCES users(id),
    credentials     JSONB NOT NULL DEFAULT '{}',
    is_default      BOOLEAN NOT NULL DEFAULT false,
    notes           TEXT,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    deleted_at      TIMESTAMPTZ,
    CONSTRAINT uq_registrar_accounts UNIQUE (registrar_id, account_name)
);
CREATE INDEX idx_registrar_accounts_registrar ON registrar_accounts (registrar_id) WHERE deleted_at IS NULL;

CREATE TABLE dns_providers (
    id            BIGSERIAL PRIMARY KEY,
    uuid          UUID NOT NULL DEFAULT gen_random_uuid(),
    name          VARCHAR(128) NOT NULL,
    provider_type VARCHAR(64) NOT NULL,
    config        JSONB NOT NULL DEFAULT '{}',
    credentials   JSONB NOT NULL DEFAULT '{}',
    notes         TEXT,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    deleted_at    TIMESTAMPTZ,
    CONSTRAINT uq_dns_providers_name UNIQUE (name)
);

-- ============================================================
-- DOMAINS (Domain Lifecycle + Asset Layer)                   [P1+PA]
-- ============================================================
CREATE TABLE domains (
    id              BIGSERIAL PRIMARY KEY,
    uuid            UUID NOT NULL DEFAULT gen_random_uuid(),
    project_id      BIGINT NOT NULL REFERENCES projects(id),
    fqdn            VARCHAR(253) NOT NULL,
    lifecycle_state VARCHAR(20) NOT NULL DEFAULT 'requested',
    owner_user_id   BIGINT REFERENCES users(id),

    -- Asset: Registration & Provider binding
    tld                     VARCHAR(64),
    registrar_account_id    BIGINT REFERENCES registrar_accounts(id),
    dns_provider_id         BIGINT REFERENCES dns_providers(id),
    cdn_account_id          BIGINT,                        -- FK added after cdn_accounts is created (see bottom)
    origin_ips              TEXT[] NOT NULL DEFAULT '{}',  -- origin server IPs (PE.2)

    -- Asset: Registration dates & Expiry
    registration_date       DATE,
    expiry_date             DATE,
    auto_renew              BOOLEAN NOT NULL DEFAULT false,
    grace_end_date          DATE,
    expiry_status           VARCHAR(32),

    -- Asset: Status flags (orthogonal to lifecycle_state)
    transfer_lock           BOOLEAN NOT NULL DEFAULT true,
    hold                    BOOLEAN NOT NULL DEFAULT false,

    -- Asset: Transfer tracking
    transfer_status             VARCHAR(32),
    transfer_gaining_registrar  VARCHAR(128),
    transfer_requested_at       TIMESTAMPTZ,
    transfer_completed_at       TIMESTAMPTZ,
    last_transfer_at            TIMESTAMPTZ,
    last_renewed_at             TIMESTAMPTZ,

    -- Asset: DNS infrastructure
    nameservers             JSONB NOT NULL DEFAULT '[]',
    dnssec_enabled          BOOLEAN NOT NULL DEFAULT false,

    -- Asset: WHOIS & Contacts
    whois_privacy           BOOLEAN NOT NULL DEFAULT false,
    registrant_contact      JSONB,
    admin_contact           JSONB,
    tech_contact            JSONB,

    -- Asset: Financial
    annual_cost             DECIMAL(10,2),
    currency                VARCHAR(3) DEFAULT 'USD',
    purchase_price          DECIMAL(10,2),
    fee_fixed               BOOLEAN NOT NULL DEFAULT false,

    -- Asset: Metadata
    purpose                 VARCHAR(255),
    notes                   TEXT,
    metadata                JSONB NOT NULL DEFAULT '{}',

    -- Timestamps
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    deleted_at      TIMESTAMPTZ,

    CONSTRAINT uq_domains_fqdn UNIQUE (fqdn),
    CONSTRAINT chk_domains_lifecycle_state CHECK (
        lifecycle_state IN ('requested', 'approved', 'provisioned', 'active', 'disabled', 'retired')
    ),
    CONSTRAINT chk_domains_expiry_status CHECK (
        expiry_status IS NULL OR expiry_status IN ('expiring_90d', 'expiring_30d', 'expiring_7d', 'expired', 'grace', 'redemption')
    ),
    CONSTRAINT chk_domains_transfer_status CHECK (
        transfer_status IS NULL OR transfer_status IN ('pending', 'completed', 'failed', 'cancelled')
    )
);
CREATE INDEX idx_domains_project_state       ON domains (project_id, lifecycle_state) WHERE deleted_at IS NULL;
CREATE INDEX idx_domains_fqdn                ON domains (fqdn) WHERE deleted_at IS NULL;
CREATE INDEX idx_domains_registrar_account   ON domains (registrar_account_id) WHERE deleted_at IS NULL;
CREATE INDEX idx_domains_dns_provider        ON domains (dns_provider_id) WHERE deleted_at IS NULL;
CREATE INDEX idx_domains_expiry_date         ON domains (expiry_date) WHERE deleted_at IS NULL AND expiry_date IS NOT NULL;
CREATE INDEX idx_domains_tld                 ON domains (tld) WHERE deleted_at IS NULL;
-- cdn_account_id index added after FK constraint (see bottom of file)

CREATE TABLE domain_variables (
    id         BIGSERIAL PRIMARY KEY,
    domain_id  BIGINT NOT NULL REFERENCES domains(id) ON DELETE CASCADE,
    variables  JSONB NOT NULL DEFAULT '{}',
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_by BIGINT REFERENCES users(id),
    CONSTRAINT uq_domain_variables UNIQUE (domain_id)
);

CREATE TABLE domain_lifecycle_history (
    id           BIGSERIAL PRIMARY KEY,
    domain_id    BIGINT NOT NULL REFERENCES domains(id) ON DELETE CASCADE,
    from_state   VARCHAR(20),
    to_state     VARCHAR(20) NOT NULL,
    reason       TEXT,
    triggered_by VARCHAR(128) NOT NULL,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX idx_domain_lifecycle_history_domain ON domain_lifecycle_history (domain_id, created_at DESC);

-- ============================================================
-- TEMPLATES & VERSIONS                                        [P1]
-- ============================================================
CREATE TABLE templates (
    id          BIGSERIAL PRIMARY KEY,
    uuid        UUID NOT NULL DEFAULT gen_random_uuid(),
    project_id  BIGINT NOT NULL REFERENCES projects(id),
    name        VARCHAR(100) NOT NULL,
    description TEXT,
    kind        VARCHAR(20) NOT NULL DEFAULT 'full',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    deleted_at  TIMESTAMPTZ,
    CONSTRAINT uq_templates_project_name UNIQUE (project_id, name),
    CONSTRAINT chk_templates_kind CHECK (kind IN ('html', 'nginx', 'full'))
);

CREATE TABLE template_versions (
    id                 BIGSERIAL PRIMARY KEY,
    uuid               UUID NOT NULL DEFAULT gen_random_uuid(),
    template_id        BIGINT NOT NULL REFERENCES templates(id),
    version_label      VARCHAR(40) NOT NULL,
    content_html       TEXT,
    content_nginx      TEXT,
    default_variables  JSONB NOT NULL DEFAULT '{}',
    checksum           VARCHAR(80) NOT NULL,
    published_at       TIMESTAMPTZ,
    published_by       BIGINT REFERENCES users(id),
    created_at         TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    created_by         BIGINT REFERENCES users(id),
    CONSTRAINT uq_template_versions_label UNIQUE (template_id, version_label)
);
CREATE INDEX idx_template_versions_template ON template_versions (template_id, created_at DESC);

-- ============================================================
-- ARTIFACTS                                                  [P1]
-- ============================================================
CREATE TABLE artifacts (
    id                  BIGSERIAL PRIMARY KEY,
    uuid                UUID NOT NULL DEFAULT gen_random_uuid(),
    project_id          BIGINT NOT NULL REFERENCES projects(id),
    release_id          BIGINT,
    template_version_id BIGINT NOT NULL REFERENCES template_versions(id),
    artifact_id         VARCHAR(64) NOT NULL,
    storage_uri         TEXT NOT NULL,
    manifest            JSONB NOT NULL,
    checksum            VARCHAR(80) NOT NULL,
    signature           TEXT,
    domain_count        INT NOT NULL,
    file_count          INT NOT NULL,
    total_size_bytes    BIGINT NOT NULL,
    built_at            TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    built_by            BIGINT REFERENCES users(id),
    signed_at           TIMESTAMPTZ,
    CONSTRAINT uq_artifacts_artifact_id UNIQUE (artifact_id)
);
CREATE INDEX idx_artifacts_release  ON artifacts (release_id);
CREATE INDEX idx_artifacts_template ON artifacts (template_version_id);
CREATE INDEX idx_artifacts_project  ON artifacts (project_id, built_at DESC);

-- ============================================================
-- HOST GROUPS                                                [P1]
-- ============================================================
CREATE TABLE host_groups (
    id                      BIGSERIAL PRIMARY KEY,
    uuid                    UUID NOT NULL DEFAULT gen_random_uuid(),
    project_id              BIGINT NOT NULL REFERENCES projects(id),
    name                    VARCHAR(100) NOT NULL,
    description             TEXT,
    region                  VARCHAR(64),
    -- P2.5: per-host concurrency + nginx reload batching
    max_concurrency         INT NOT NULL DEFAULT 0,           -- 0 = unlimited
    reload_batch_size       INT NOT NULL DEFAULT 50,          -- domains per batch before reload
    reload_batch_wait_secs  INT NOT NULL DEFAULT 30,          -- seconds to buffer before reload
    created_at              TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at              TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    deleted_at              TIMESTAMPTZ,
    CONSTRAINT uq_host_groups_project_name UNIQUE (project_id, name)
);

-- ============================================================
-- AGENTS                                                     [P1]
-- ============================================================
CREATE TABLE agents (
    id              BIGSERIAL PRIMARY KEY,
    uuid            UUID NOT NULL DEFAULT gen_random_uuid(),
    agent_id        VARCHAR(64) NOT NULL,
    hostname        VARCHAR(253) NOT NULL,
    ip              INET,
    region          VARCHAR(64),
    datacenter      VARCHAR(64),
    host_group_id   BIGINT REFERENCES host_groups(id),
    agent_version   VARCHAR(40),
    capabilities    JSONB NOT NULL DEFAULT '[]',
    tags            JSONB NOT NULL DEFAULT '{}',
    cert_serial     VARCHAR(80),
    cert_expires_at TIMESTAMPTZ,
    status          VARCHAR(20) NOT NULL DEFAULT 'registered',
    last_seen_at    TIMESTAMPTZ,
    last_error      TEXT,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    deleted_at      TIMESTAMPTZ,
    CONSTRAINT uq_agents_agent_id UNIQUE (agent_id),
    CONSTRAINT chk_agents_status  CHECK (
        status IN ('registered', 'online', 'busy', 'idle', 'offline',
                   'draining', 'disabled', 'upgrading', 'error')
    )
);
CREATE INDEX idx_agents_status      ON agents (status) WHERE deleted_at IS NULL;
CREATE INDEX idx_agents_host_group  ON agents (host_group_id) WHERE deleted_at IS NULL;
CREATE INDEX idx_agents_last_seen   ON agents (last_seen_at);

CREATE TABLE agent_state_history (
    id           BIGSERIAL PRIMARY KEY,
    agent_id     BIGINT NOT NULL REFERENCES agents(id) ON DELETE CASCADE,
    from_state   VARCHAR(20),
    to_state     VARCHAR(20) NOT NULL,
    reason       TEXT,
    triggered_by VARCHAR(128) NOT NULL,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX idx_agent_state_history_agent ON agent_state_history (agent_id, created_at DESC);

CREATE TABLE agent_heartbeats (
    id              BIGSERIAL PRIMARY KEY,
    agent_id        BIGINT NOT NULL REFERENCES agents(id) ON DELETE CASCADE,
    status          VARCHAR(20) NOT NULL,
    current_task_id VARCHAR(64),
    agent_version   VARCHAR(40),
    load_avg_1      DOUBLE PRECISION,
    disk_free_pct   DOUBLE PRECISION,
    last_error      TEXT,
    received_at     TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX idx_agent_heartbeats_agent_time ON agent_heartbeats (agent_id, received_at DESC);

-- ============================================================
-- RELEASES                                                   [P1]
-- ============================================================
CREATE TABLE releases (
    id                  BIGSERIAL PRIMARY KEY,
    uuid                UUID NOT NULL DEFAULT gen_random_uuid(),
    release_id          VARCHAR(64) NOT NULL,
    project_id          BIGINT NOT NULL REFERENCES projects(id),
    template_version_id BIGINT NOT NULL REFERENCES template_versions(id),
    artifact_id         BIGINT REFERENCES artifacts(id),
    release_type        VARCHAR(20) NOT NULL,
    trigger_source      VARCHAR(20) NOT NULL DEFAULT 'ui',
    status              VARCHAR(20) NOT NULL DEFAULT 'pending',
    requires_approval   BOOLEAN NOT NULL DEFAULT false,
    canary_shard_size   INT NOT NULL DEFAULT 30,
    shard_size          INT NOT NULL DEFAULT 200,
    total_domains       INT,
    total_shards        INT,
    success_count       INT NOT NULL DEFAULT 0,
    failure_count       INT NOT NULL DEFAULT 0,
    description         TEXT,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    created_by          BIGINT REFERENCES users(id),
    started_at          TIMESTAMPTZ,
    ended_at            TIMESTAMPTZ,
    CONSTRAINT uq_releases_release_id UNIQUE (release_id),
    CONSTRAINT chk_releases_type      CHECK (release_type IN ('html', 'nginx', 'full')),
    CONSTRAINT chk_releases_status    CHECK (
        status IN ('pending', 'planning', 'ready', 'executing',
                   'paused', 'succeeded', 'failed',
                   'rolling_back', 'rolled_back', 'cancelled')
    )
);
CREATE INDEX idx_releases_project_status ON releases (project_id, status, created_at DESC);
CREATE INDEX idx_releases_artifact       ON releases (artifact_id);

CREATE TABLE release_state_history (
    id           BIGSERIAL PRIMARY KEY,
    release_id   BIGINT NOT NULL REFERENCES releases(id) ON DELETE CASCADE,
    from_state   VARCHAR(20),
    to_state     VARCHAR(20) NOT NULL,
    reason       TEXT,
    triggered_by VARCHAR(128) NOT NULL,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX idx_release_state_history_release ON release_state_history (release_id, created_at DESC);

CREATE TABLE release_scopes (
    id            BIGSERIAL PRIMARY KEY,
    release_id    BIGINT NOT NULL REFERENCES releases(id) ON DELETE CASCADE,
    domain_id     BIGINT NOT NULL REFERENCES domains(id),
    host_group_id BIGINT REFERENCES host_groups(id),
    CONSTRAINT uq_release_scopes UNIQUE (release_id, domain_id, host_group_id)
);
CREATE INDEX idx_release_scopes_release ON release_scopes (release_id);
CREATE INDEX idx_release_scopes_domain  ON release_scopes (domain_id);

-- Release shards                                                [P2 schema, P1 flat single shard]
CREATE TABLE release_shards (
    id              BIGSERIAL PRIMARY KEY,
    release_id      BIGINT NOT NULL REFERENCES releases(id) ON DELETE CASCADE,
    shard_index     INT NOT NULL,
    is_canary       BOOLEAN NOT NULL DEFAULT false,
    domain_count    INT NOT NULL,
    status          VARCHAR(20) NOT NULL DEFAULT 'pending',
    started_at      TIMESTAMPTZ,
    ended_at        TIMESTAMPTZ,
    success_count   INT NOT NULL DEFAULT 0,
    failure_count   INT NOT NULL DEFAULT 0,
    pause_reason    TEXT,
    CONSTRAINT uq_release_shards UNIQUE (release_id, shard_index),
    CONSTRAINT chk_release_shards_status CHECK (
        status IN ('pending', 'dispatching', 'running', 'paused',
                   'succeeded', 'failed', 'cancelled')
    )
);
CREATE INDEX idx_release_shards_release ON release_shards (release_id, shard_index);

-- ============================================================
-- DOMAIN TASKS & AGENT TASKS                                 [P1]
-- ============================================================
CREATE TABLE domain_tasks (
    id            BIGSERIAL PRIMARY KEY,
    uuid          UUID NOT NULL DEFAULT gen_random_uuid(),
    release_id    BIGINT NOT NULL REFERENCES releases(id),
    shard_id      BIGINT REFERENCES release_shards(id),
    domain_id     BIGINT NOT NULL REFERENCES domains(id),
    host_group_id BIGINT REFERENCES host_groups(id),
    task_type     VARCHAR(20) NOT NULL,
    status        VARCHAR(20) NOT NULL DEFAULT 'pending',
    started_at    TIMESTAMPTZ,
    ended_at      TIMESTAMPTZ,
    last_error    TEXT,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT chk_domain_tasks_status CHECK (
        status IN ('pending', 'dispatched', 'running', 'succeeded', 'failed', 'cancelled')
    ),
    CONSTRAINT chk_domain_tasks_task_type CHECK (
        task_type IN ('deploy_html', 'deploy_nginx', 'deploy_full', 'rollback', 'verify')
    )
);
CREATE INDEX idx_domain_tasks_release ON domain_tasks (release_id, status);
CREATE INDEX idx_domain_tasks_domain  ON domain_tasks (domain_id);

CREATE TABLE agent_tasks (
    id              BIGSERIAL PRIMARY KEY,
    uuid            UUID NOT NULL DEFAULT gen_random_uuid(),
    task_id         VARCHAR(64) NOT NULL,
    domain_task_id  BIGINT NOT NULL REFERENCES domain_tasks(id),
    agent_id        BIGINT NOT NULL REFERENCES agents(id),
    artifact_id     BIGINT NOT NULL REFERENCES artifacts(id),
    artifact_url    TEXT,
    payload         JSONB NOT NULL,
    status          VARCHAR(20) NOT NULL DEFAULT 'pending',
    claimed_at      TIMESTAMPTZ,
    started_at      TIMESTAMPTZ,
    ended_at        TIMESTAMPTZ,
    duration_ms     BIGINT,
    last_error      TEXT,
    retry_count     INT NOT NULL DEFAULT 0,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT uq_agent_tasks_task_id UNIQUE (task_id),
    CONSTRAINT chk_agent_tasks_status CHECK (
        status IN ('pending', 'claimed', 'running', 'succeeded', 'failed', 'timeout', 'cancelled')
    )
);
CREATE INDEX idx_agent_tasks_agent_status ON agent_tasks (agent_id, status);
CREATE INDEX idx_agent_tasks_domain_task  ON agent_tasks (domain_task_id);

CREATE TABLE deployment_logs (
    id            BIGSERIAL PRIMARY KEY,
    agent_task_id BIGINT NOT NULL REFERENCES agent_tasks(id) ON DELETE CASCADE,
    phase         VARCHAR(32) NOT NULL,
    status        VARCHAR(20) NOT NULL,
    duration_ms   BIGINT,
    detail        TEXT,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX idx_deployment_logs_task ON deployment_logs (agent_task_id, created_at);

-- ============================================================
-- ROLLBACK RECORDS                                           [P2]
-- ============================================================
CREATE TABLE rollback_records (
    id                       BIGSERIAL PRIMARY KEY,
    uuid                     UUID NOT NULL DEFAULT gen_random_uuid(),
    release_id               BIGINT NOT NULL REFERENCES releases(id),
    rollback_release_id      BIGINT REFERENCES releases(id),
    target_artifact_id       BIGINT NOT NULL REFERENCES artifacts(id),
    scope                    VARCHAR(20) NOT NULL,
    scope_target_id          BIGINT,
    reason                   TEXT NOT NULL,
    triggered_by             BIGINT REFERENCES users(id),
    triggered_at             TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    completed_at             TIMESTAMPTZ,
    success                  BOOLEAN,
    CONSTRAINT chk_rollback_scope CHECK (scope IN ('release', 'shard', 'domain'))
);
CREATE INDEX idx_rollback_records_release ON rollback_records (release_id);

-- ============================================================
-- PROBE                                                       [P3]
-- ============================================================
CREATE TABLE probe_policies (
    id                BIGSERIAL PRIMARY KEY,
    uuid              UUID NOT NULL DEFAULT gen_random_uuid(),
    project_id        BIGINT REFERENCES projects(id),
    name              VARCHAR(100) NOT NULL,
    tier              SMALLINT NOT NULL,
    interval_seconds  INT NOT NULL,
    timeout_seconds   INT NOT NULL DEFAULT 8,
    expected_status   INT,
    expected_keyword  TEXT,
    expected_meta_tag VARCHAR(64),
    target_filter     JSONB,
    enabled           BOOLEAN NOT NULL DEFAULT true,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at        TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT chk_probe_policies_tier CHECK (tier IN (1, 2, 3))
);

CREATE TABLE probe_tasks (
    id                   BIGSERIAL PRIMARY KEY,
    uuid                 UUID NOT NULL DEFAULT gen_random_uuid(),
    policy_id            BIGINT NOT NULL REFERENCES probe_policies(id),
    domain_id            BIGINT NOT NULL REFERENCES domains(id),
    release_id           BIGINT REFERENCES releases(id),
    expected_artifact_id BIGINT REFERENCES artifacts(id),
    scheduled_for        TIMESTAMPTZ NOT NULL,
    status               VARCHAR(20) NOT NULL DEFAULT 'pending',
    started_at           TIMESTAMPTZ,
    completed_at         TIMESTAMPTZ,
    error_message        TEXT,
    created_at           TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT chk_probe_tasks_status CHECK (
        status IN ('pending', 'running', 'completed', 'cancelled')
    )
);
CREATE INDEX idx_probe_tasks_scheduled ON probe_tasks (scheduled_for) WHERE status = 'pending';
CREATE INDEX idx_probe_tasks_domain    ON probe_tasks (domain_id, scheduled_for DESC);

-- ============================================================
-- ALERTS                                                     [P3]
-- ============================================================
CREATE TABLE alert_events (
    id                BIGSERIAL PRIMARY KEY,
    uuid              UUID NOT NULL DEFAULT gen_random_uuid(),
    severity          VARCHAR(8)   NOT NULL,
    source            VARCHAR(32)  NOT NULL DEFAULT 'system',  -- probe | drift | expiry | agent | manual
    target_kind       VARCHAR(32)  NOT NULL,
    target_id         BIGINT,
    title             VARCHAR(200) NOT NULL,
    detail            JSONB,
    dedup_key         VARCHAR(200),
    notified_at       TIMESTAMPTZ,
    resolved_at       TIMESTAMPTZ,
    acknowledged_at   TIMESTAMPTZ,
    acknowledged_by   BIGINT REFERENCES users(id),
    created_at        TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT chk_alert_events_severity CHECK (severity IN ('P1', 'P2', 'P3', 'INFO')),
    CONSTRAINT chk_alert_events_source   CHECK (source IN ('probe', 'drift', 'expiry', 'agent', 'manual', 'system', 'gfw'))
);
CREATE INDEX idx_alert_events_dedup       ON alert_events (dedup_key, created_at DESC);
CREATE INDEX idx_alert_events_unresolved  ON alert_events (severity, created_at DESC) WHERE resolved_at IS NULL;
CREATE INDEX idx_alert_events_target      ON alert_events (target_kind, target_id, created_at DESC);

-- notification_channels: reusable named channels with embedded config/credentials
CREATE TABLE notification_channels (
    id              BIGSERIAL PRIMARY KEY,
    uuid            UUID NOT NULL DEFAULT gen_random_uuid(),
    name            VARCHAR(128) NOT NULL,
    channel_type    VARCHAR(32)  NOT NULL,  -- "telegram" | "slack" | "webhook" | "email"
    config          JSONB        NOT NULL,  -- type-specific credentials/config
    is_default      BOOLEAN      NOT NULL DEFAULT false,
    enabled         BOOLEAN      NOT NULL DEFAULT true,
    created_by      BIGINT REFERENCES users(id),
    created_at      TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    CONSTRAINT chk_notification_channels_type CHECK (channel_type IN ('telegram', 'slack', 'webhook', 'email'))
);
CREATE INDEX idx_notification_channels_enabled ON notification_channels (enabled, channel_type);

-- notification_rules: many-to-many — which channels receive which alert types
CREATE TABLE notification_rules (
    id              BIGSERIAL PRIMARY KEY,
    channel_id      BIGINT NOT NULL REFERENCES notification_channels(id) ON DELETE CASCADE,
    alert_type      VARCHAR(64),   -- NULL = all alert types
    min_severity    VARCHAR(8)  NOT NULL DEFAULT 'P3',  -- P1 | P2 | P3 | INFO
    target_type     VARCHAR(32),   -- NULL = global; "project" | "domain"
    target_id       BIGINT,        -- specific project/domain ID, NULL = global
    enabled         BOOLEAN NOT NULL DEFAULT true,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT chk_notification_rules_severity CHECK (min_severity IN ('P1', 'P2', 'P3', 'INFO'))
);
CREATE INDEX idx_notification_rules_enabled ON notification_rules (enabled, channel_id) WHERE enabled = true;

-- notification_history: audit trail of every dispatch attempt
CREATE TABLE notification_history (
    id              BIGSERIAL PRIMARY KEY,
    channel_id      BIGINT NOT NULL REFERENCES notification_channels(id),
    alert_event_id  BIGINT REFERENCES alert_events(id),
    status          VARCHAR(32) NOT NULL,  -- "sent" | "failed" | "suppressed"
    message         TEXT,
    error           TEXT,
    sent_at         TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX idx_notification_history_channel ON notification_history (channel_id, sent_at DESC);
CREATE INDEX idx_notification_history_alert   ON notification_history (alert_event_id) WHERE alert_event_id IS NOT NULL;

-- ============================================================
-- AGENT VERSIONS & UPGRADES                                  [P3]
-- ============================================================
CREATE TABLE agent_versions (
    id            BIGSERIAL PRIMARY KEY,
    version       VARCHAR(40) NOT NULL,
    storage_uri   TEXT NOT NULL,
    checksum      VARCHAR(80) NOT NULL,
    signature     TEXT,
    released_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    released_by   BIGINT REFERENCES users(id),
    notes         TEXT,
    is_current    BOOLEAN NOT NULL DEFAULT false,
    CONSTRAINT uq_agent_versions UNIQUE (version)
);

CREATE TABLE agent_upgrade_jobs (
    id                BIGSERIAL PRIMARY KEY,
    uuid              UUID NOT NULL DEFAULT gen_random_uuid(),
    target_version_id BIGINT NOT NULL REFERENCES agent_versions(id),
    rollback_version_id BIGINT REFERENCES agent_versions(id),
    scope_filter      JSONB NOT NULL,
    canary_count      INT NOT NULL DEFAULT 3,
    status            VARCHAR(20) NOT NULL DEFAULT 'pending',
    succeeded_count   INT NOT NULL DEFAULT 0,
    failed_count      INT NOT NULL DEFAULT 0,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    started_at        TIMESTAMPTZ,
    ended_at          TIMESTAMPTZ,
    triggered_by      BIGINT REFERENCES users(id),
    CONSTRAINT chk_agent_upgrade_jobs_status CHECK (
        status IN ('pending', 'canary', 'expanding', 'succeeded', 'failed', 'rolled_back', 'cancelled')
    )
);

CREATE TABLE agent_logs (
    id          BIGSERIAL PRIMARY KEY,
    agent_id    BIGINT NOT NULL REFERENCES agents(id) ON DELETE CASCADE,
    task_id     VARCHAR(64),
    level       VARCHAR(8) NOT NULL,
    message     TEXT NOT NULL,
    fields      JSONB,
    received_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX idx_agent_logs_agent_time ON agent_logs (agent_id, received_at DESC);

-- ============================================================
-- APPROVAL REQUESTS                                          [P4]
-- ============================================================
CREATE TABLE approval_requests (
    id              BIGSERIAL PRIMARY KEY,
    uuid            UUID NOT NULL DEFAULT gen_random_uuid(),
    release_id      BIGINT NOT NULL REFERENCES releases(id),
    requested_by    BIGINT NOT NULL REFERENCES users(id),
    requested_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    required_role   VARCHAR(32) NOT NULL,
    status          VARCHAR(20) NOT NULL DEFAULT 'pending',
    reviewed_by     BIGINT REFERENCES users(id),
    reviewed_at     TIMESTAMPTZ,
    review_comment  TEXT,
    CONSTRAINT chk_approval_requests_status CHECK (
        status IN ('pending', 'granted', 'denied', 'expired', 'cancelled')
    )
);
CREATE INDEX idx_approval_requests_release ON approval_requests (release_id);
CREATE INDEX idx_approval_requests_pending ON approval_requests (status) WHERE status = 'pending';

-- ============================================================
-- AUDIT                                                      [P1]
-- ============================================================
CREATE TABLE audit_logs (
    id          BIGSERIAL PRIMARY KEY,
    user_id     BIGINT REFERENCES users(id),
    action      VARCHAR(64) NOT NULL,
    target_kind VARCHAR(32) NOT NULL,
    target_id   VARCHAR(64),
    detail      JSONB,
    ip_address  INET,
    user_agent  TEXT,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX idx_audit_logs_target ON audit_logs (target_kind, target_id, created_at DESC);
CREATE INDEX idx_audit_logs_user   ON audit_logs (user_id, created_at DESC);
CREATE INDEX idx_audit_logs_action ON audit_logs (action, created_at DESC);

-- ============================================================
-- SSL CERTIFICATES (Domain Asset Layer)                      [PA]
-- ============================================================
CREATE TABLE ssl_certificates (
    id              BIGSERIAL PRIMARY KEY,
    uuid            UUID NOT NULL DEFAULT gen_random_uuid(),
    domain_id       BIGINT NOT NULL REFERENCES domains(id) ON DELETE CASCADE,
    issuer          VARCHAR(256),
    cert_type       VARCHAR(32),
    serial_number   VARCHAR(128),
    issued_at       TIMESTAMPTZ,
    expires_at      TIMESTAMPTZ NOT NULL,
    auto_renew      BOOLEAN NOT NULL DEFAULT false,
    status          VARCHAR(32) NOT NULL DEFAULT 'active',
    last_check_at   TIMESTAMPTZ,
    notes           TEXT,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    deleted_at      TIMESTAMPTZ,
    CONSTRAINT chk_ssl_status CHECK (
        status IN ('active', 'expiring', 'expired', 'revoked', 'unknown')
    )
);
CREATE INDEX idx_ssl_certs_domain  ON ssl_certificates (domain_id) WHERE deleted_at IS NULL;
CREATE INDEX idx_ssl_certs_expires ON ssl_certificates (expires_at) WHERE deleted_at IS NULL;

-- ============================================================
-- DOMAIN FEE SCHEDULES & COST HISTORY (Domain Asset Layer)   [PA]
-- ============================================================
CREATE TABLE domain_fee_schedules (
    id               BIGSERIAL PRIMARY KEY,
    registrar_id     BIGINT NOT NULL REFERENCES registrars(id) ON DELETE CASCADE,
    tld              VARCHAR(64) NOT NULL,
    registration_fee DECIMAL(10,2) NOT NULL DEFAULT 0,
    renewal_fee      DECIMAL(10,2) NOT NULL DEFAULT 0,
    transfer_fee     DECIMAL(10,2) NOT NULL DEFAULT 0,
    privacy_fee      DECIMAL(10,2) NOT NULL DEFAULT 0,
    currency         VARCHAR(3) NOT NULL DEFAULT 'USD',
    created_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT uq_fee_schedule UNIQUE (registrar_id, tld)
);

CREATE TABLE domain_costs (
    id              BIGSERIAL PRIMARY KEY,
    domain_id       BIGINT NOT NULL REFERENCES domains(id) ON DELETE CASCADE,
    cost_type       VARCHAR(32) NOT NULL,
    amount          DECIMAL(10,2) NOT NULL,
    currency        VARCHAR(3) NOT NULL DEFAULT 'USD',
    period_start    DATE,
    period_end      DATE,
    paid_at         DATE,
    notes           TEXT,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT chk_cost_type CHECK (
        cost_type IN ('registration', 'renewal', 'transfer', 'restore', 'privacy', 'other')
    )
);
CREATE INDEX idx_domain_costs_domain ON domain_costs (domain_id);

-- ============================================================
-- TAGS (Domain Asset Layer)                                  [PA]
-- ============================================================
CREATE TABLE tags (
    id      BIGSERIAL PRIMARY KEY,
    name    VARCHAR(64) NOT NULL,
    color   VARCHAR(7),
    CONSTRAINT uq_tags_name UNIQUE (name),
    CONSTRAINT chk_tags_color CHECK (color IS NULL OR color ~ '^#[0-9a-fA-F]{6}$')
);

CREATE TABLE domain_tags (
    domain_id   BIGINT NOT NULL REFERENCES domains(id) ON DELETE CASCADE,
    tag_id      BIGINT NOT NULL REFERENCES tags(id) ON DELETE CASCADE,
    PRIMARY KEY (domain_id, tag_id)
);
CREATE INDEX idx_domain_tags_tag ON domain_tags (tag_id);

-- ============================================================
-- DOMAIN IMPORT JOBS (Domain Asset Layer)                    [PA]
-- ============================================================
CREATE TABLE domain_import_jobs (
    id                   BIGSERIAL PRIMARY KEY,
    uuid                 UUID NOT NULL DEFAULT gen_random_uuid(),
    registrar_account_id BIGINT REFERENCES registrar_accounts(id),
    project_id           BIGINT NOT NULL REFERENCES projects(id),
    source_type          VARCHAR(32) NOT NULL,
    status               VARCHAR(32) NOT NULL DEFAULT 'pending',
    total_count          INT NOT NULL DEFAULT 0,
    imported_count       INT NOT NULL DEFAULT 0,
    skipped_count        INT NOT NULL DEFAULT 0,
    failed_count         INT NOT NULL DEFAULT 0,
    error_details        JSONB,
    raw_csv              TEXT,                         -- original uploaded CSV content
    created_by           BIGINT REFERENCES users(id),
    started_at           TIMESTAMPTZ,
    completed_at         TIMESTAMPTZ,
    created_at           TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT chk_import_source CHECK (
        source_type IN ('csv_upload', 'api_sync', 'manual_bulk')
    ),
    CONSTRAINT chk_import_status CHECK (
        status IN ('pending', 'fetching', 'processing', 'completed', 'failed')
    )
);

-- ============================================================
-- DOMAIN PERMISSIONS (Zone-Level RBAC)                      [PB.6]
-- ============================================================
-- Per-domain permission grants for individual users.
-- Access resolution: global role (via user_roles) takes precedence,
-- then domain-level permission. Highest permission wins.
CREATE TABLE domain_permissions (
    id          BIGSERIAL PRIMARY KEY,
    domain_id   BIGINT NOT NULL REFERENCES domains(id) ON DELETE CASCADE,
    user_id     BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    permission  VARCHAR(32) NOT NULL DEFAULT 'viewer', -- viewer, editor, admin
    granted_by  BIGINT REFERENCES users(id),
    granted_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT uq_domain_permissions UNIQUE (domain_id, user_id),
    CONSTRAINT chk_domain_permission CHECK (
        permission IN ('viewer', 'editor', 'admin')
    )
);
CREATE INDEX idx_domain_permissions_domain ON domain_permissions (domain_id);
CREATE INDEX idx_domain_permissions_user   ON domain_permissions (user_id);

-- ============================================================
-- DNS RECORD TEMPLATES                                       [PB.7]
-- ============================================================
-- Reusable record blueprints with {{variable}} placeholders.
-- Applying a template = rendering + staging records for plan/apply.
CREATE TABLE dns_record_templates (
    id          BIGSERIAL PRIMARY KEY,
    uuid        UUID NOT NULL DEFAULT gen_random_uuid(),
    name        VARCHAR(128) NOT NULL,
    description TEXT,
    -- records: [{name:"@",type:"A",content:"{{ip}}",ttl:300,priority:0}, ...]
    records     JSONB NOT NULL DEFAULT '[]',
    -- variables: {"ip": "", "mx_host": ""} — keys are variable names, values are default/description
    variables   JSONB NOT NULL DEFAULT '{}',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT uq_dns_record_templates_name UNIQUE (name)
);

-- Extend domains with drift/sync tracking columns
ALTER TABLE domains ADD COLUMN IF NOT EXISTS last_sync_at  TIMESTAMPTZ;
ALTER TABLE domains ADD COLUMN IF NOT EXISTS last_drift_at TIMESTAMPTZ;

-- ============================================================
-- PHASE D: GFW Detection                                     [PD.1]
-- ============================================================

-- Probe nodes (CN + control vantage points).
-- role: "probe" = inside CN, "control" = uncensored (HK/JP/etc.)
-- status state machine: registered → online ↔ offline → error
CREATE TABLE gfw_probe_nodes (
    id              BIGSERIAL PRIMARY KEY,
    uuid            UUID NOT NULL DEFAULT gen_random_uuid(),
    node_id         VARCHAR(64) NOT NULL,       -- operator-assigned, e.g. "cn-beijing-01"
    region          VARCHAR(64) NOT NULL,        -- "cn-north", "cn-east", "hk", "jp"
    role            VARCHAR(16) NOT NULL,        -- "probe" | "control"
    status          VARCHAR(32) NOT NULL DEFAULT 'registered',
    last_seen_at    TIMESTAMPTZ,
    agent_version   VARCHAR(32),
    ip_address      VARCHAR(45),
    metadata        JSONB NOT NULL DEFAULT '{}', -- load_avg, disk_free, etc.
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT uq_gfw_probe_nodes_node_id UNIQUE (node_id),
    CONSTRAINT chk_gfw_probe_nodes_role   CHECK (role   IN ('probe', 'control')),
    CONSTRAINT chk_gfw_probe_nodes_status CHECK (status IN ('registered','online','offline','error'))
);

CREATE INDEX idx_gfw_probe_nodes_status  ON gfw_probe_nodes(status);
CREATE INDEX idx_gfw_probe_nodes_region  ON gfw_probe_nodes(region);

-- Check assignments: which probe + control nodes check which domains.
-- probe_node_ids / control_node_ids stored as JSONB arrays of node_id strings.
CREATE TABLE gfw_check_assignments (
    id                BIGSERIAL PRIMARY KEY,
    uuid              UUID NOT NULL DEFAULT gen_random_uuid(),
    domain_id         BIGINT NOT NULL REFERENCES domains(id) ON DELETE CASCADE,
    probe_node_ids    JSONB NOT NULL DEFAULT '[]',   -- ["cn-beijing-01", "cn-shanghai-01"]
    control_node_ids  JSONB NOT NULL DEFAULT '[]',   -- ["hk-01", "jp-01"]
    check_interval    INT NOT NULL DEFAULT 180,       -- seconds between check cycles
    enabled           BOOLEAN NOT NULL DEFAULT true,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at        TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT uq_gfw_check_assignments_domain UNIQUE (domain_id)
);

CREATE INDEX idx_gfw_check_assignments_enabled ON gfw_check_assignments(enabled);

-- Raw 4-layer measurements reported by probe + control nodes.           [PD.2]
-- TimescaleDB hypertable partitioned by measured_at; 180-day retention.
-- JSONB columns store per-layer result structs (probeprotocol wire format).
CREATE TABLE gfw_measurements (
    id              BIGSERIAL,
    domain_id       BIGINT NOT NULL,                    -- FK to domains(id), not enforced for TimescaleDB perf
    node_id         VARCHAR(64) NOT NULL,               -- FK to gfw_probe_nodes.node_id
    node_role       VARCHAR(16) NOT NULL,               -- "probe" | "control"
    region          VARCHAR(64) NOT NULL DEFAULT '',
    fqdn            VARCHAR(512) NOT NULL,
    dns_result      JSONB,                              -- DNSResult (including IsBogon, IsInjected flags)
    tcp_results     JSONB,                              -- []TCPResult
    tls_results     JSONB,                              -- []TLSResult
    http_result     JSONB,                              -- HTTPResult
    total_ms        INT,
    measured_at     TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (measured_at, id)
);

CREATE INDEX idx_gfw_measurements_domain ON gfw_measurements(domain_id, measured_at DESC);
CREATE INDEX idx_gfw_measurements_node   ON gfw_measurements(node_id,   measured_at DESC);

-- TimescaleDB hypertable + 180-day retention (non-TS environments: skip silently).
DO $$
BEGIN
  IF EXISTS (SELECT 1 FROM pg_extension WHERE extname = 'timescaledb') THEN
    PERFORM create_hypertable('gfw_measurements', 'measured_at',
                              if_not_exists => TRUE);
    PERFORM add_retention_policy('gfw_measurements', INTERVAL '180 days',
                                 if_not_exists => TRUE);
  END IF;
END
$$;

-- Known GFW bogon IPs — maintained by the platform, checked by probe nodes.   [PD.2]
-- source: "seeded" (built-in at install) | "operator" (added via admin API)
CREATE TABLE gfw_bogon_ips (
    id          BIGSERIAL PRIMARY KEY,
    ip_address  VARCHAR(45) NOT NULL,
    source      VARCHAR(32) NOT NULL DEFAULT 'seeded',  -- "seeded" | "operator"
    note        TEXT,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT uq_gfw_bogon_ips_ip UNIQUE (ip_address)
);

-- Seed with well-documented GFW injection/bogon IPs (as of 2025).
-- Sources: OONI data, Censorship Canary, citizenlab/filtering-data.
INSERT INTO gfw_bogon_ips (ip_address, source, note) VALUES
    ('1.2.3.4',          'seeded', 'Classic GFW bogon — used in DNS injection since ~2010'),
    ('37.235.1.174',     'seeded', 'GFW DNS injection — FreeDNS range hijacked'),
    ('8.7.198.45',       'seeded', 'GFW DNS injection — bogon returned for blocked domains'),
    ('46.82.174.68',     'seeded', 'GFW DNS injection — confirmed by OONI measurements'),
    ('78.16.49.15',      'seeded', 'GFW DNS injection — repeated in OONI CN data'),
    ('93.46.8.89',       'seeded', 'GFW DNS injection — CN ISP bogon range'),
    ('93.46.8.90',       'seeded', 'GFW DNS injection — CN ISP bogon range'),
    ('243.185.187.39',   'seeded', 'GFW DNS injection — non-routable bogon'),
    ('243.185.187.30',   'seeded', 'GFW DNS injection — non-routable bogon'),
    ('0.0.0.0',          'seeded', 'Null route — used by some CN ISPs for blocking'),
    ('127.0.0.1',        'seeded', 'Localhost — used by some CN ISPs for NXDOMAIN substitute')
ON CONFLICT (ip_address) DO NOTHING;

-- ============================================================
-- GFW VERDICTS                                              [PD.3]
-- ============================================================
-- One verdict row per (domain, probe_run) — the output of the OONI-style
-- decision tree comparing probe vs control measurements.
CREATE TABLE gfw_verdicts (
    id               BIGSERIAL PRIMARY KEY,
    domain_id        BIGINT NOT NULL REFERENCES domains(id),
    blocking         VARCHAR(32) NOT NULL DEFAULT '',  -- '' | 'dns' | 'tcp_ip' | 'tls_sni' | 'http-failure' | 'http-diff' | 'indeterminate'
    accessible       BOOLEAN NOT NULL,
    dns_consistency  VARCHAR(16),                      -- 'consistent' | 'inconsistent' | NULL (no DNS data)
    confidence       DECIMAL(3,2) NOT NULL DEFAULT 0,  -- 0.00 – 1.00
    probe_node_id    VARCHAR(64) NOT NULL,
    control_node_id  VARCHAR(64) NOT NULL DEFAULT '',
    detail           JSONB,                            -- VerdictDetail: dns/tcp/tls/http per-layer evidence
    measured_at      TIMESTAMPTZ NOT NULL,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT chk_gfw_verdicts_blocking CHECK (
        blocking IN ('', 'dns', 'tcp_ip', 'tls_sni', 'http-failure', 'http-diff', 'indeterminate')
    ),
    CONSTRAINT chk_gfw_verdicts_consistency CHECK (
        dns_consistency IS NULL OR dns_consistency IN ('consistent', 'inconsistent')
    ),
    CONSTRAINT chk_gfw_verdicts_confidence CHECK (confidence >= 0 AND confidence <= 1)
);
CREATE INDEX idx_gfw_verdicts_domain   ON gfw_verdicts(domain_id, measured_at DESC);
CREATE INDEX idx_gfw_verdicts_blocking ON gfw_verdicts(blocking)
    WHERE blocking != '' AND blocking != 'indeterminate';

-- ============================================================
-- GFW BLOCKING STATUS on domains                            [PD.4]
-- ============================================================
-- Denormalized summary updated by VerdictService on every verdict write.
-- Allows fast queries like "list all currently blocked domains" without
-- scanning gfw_verdicts.  The source-of-truth remains gfw_verdicts.
ALTER TABLE domains ADD COLUMN IF NOT EXISTS blocking_status     VARCHAR(32);       -- NULL | 'possibly_blocked' | 'blocked'
ALTER TABLE domains ADD COLUMN IF NOT EXISTS blocking_type       VARCHAR(32);       -- 'dns'|'tcp_ip'|'tls_sni'|'http-failure'|'http-diff'|'indeterminate'
ALTER TABLE domains ADD COLUMN IF NOT EXISTS blocking_since      TIMESTAMPTZ;       -- when this blocking episode started
ALTER TABLE domains ADD COLUMN IF NOT EXISTS blocking_confidence DECIMAL(3,2);      -- latest verdict confidence

CREATE INDEX idx_domains_blocking_status ON domains(blocking_status)
    WHERE blocking_status IS NOT NULL;

-- ============================================================
-- MAINTENANCE WINDOWS                                        [PC.4]
-- ============================================================
-- Planned downtime windows. While a domain is in maintenance:
--   • probe records status="maintenance" (not "down")
--   • alert engine suppresses alerts
--   • status page shows "Under Maintenance"
CREATE TABLE maintenance_windows (
    id              BIGSERIAL PRIMARY KEY,
    uuid            UUID NOT NULL DEFAULT gen_random_uuid(),
    title           VARCHAR(150) NOT NULL,
    description     TEXT,
    -- "single" = one-time, "recurring_weekly" = every week on given weekdays,
    -- "recurring_monthly" = every month on given day, "cron" = cron expression
    strategy        VARCHAR(32) NOT NULL DEFAULT 'single',
    start_at        TIMESTAMPTZ,                -- for single: exact start
    end_at          TIMESTAMPTZ,                -- for single: exact end
    -- for recurring: {"weekdays":[1,5],"start_time":"02:00","duration_minutes":120,"timezone":"UTC"}
    -- for cron:      {"expression":"0 2 * * 1","duration_minutes":120,"timezone":"UTC"}
    recurrence      JSONB,
    active          BOOLEAN NOT NULL DEFAULT true,
    created_by      BIGINT REFERENCES users(id),
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT chk_maintenance_strategy CHECK (
        strategy IN ('single', 'recurring_weekly', 'recurring_monthly', 'cron')
    )
);

CREATE INDEX idx_maintenance_windows_active    ON maintenance_windows(active);
CREATE INDEX idx_maintenance_windows_start_end ON maintenance_windows(start_at, end_at)
    WHERE strategy = 'single';

-- Targets: which domains/host_groups/projects are covered by this window.
-- A domain is in maintenance if it is directly targeted OR its host_group
-- or project is targeted.
CREATE TABLE maintenance_window_targets (
    id              BIGSERIAL PRIMARY KEY,
    maintenance_id  BIGINT NOT NULL REFERENCES maintenance_windows(id) ON DELETE CASCADE,
    target_type     VARCHAR(32) NOT NULL,   -- "domain", "host_group", "project"
    target_id       BIGINT NOT NULL,
    CONSTRAINT uq_maintenance_target UNIQUE (maintenance_id, target_type, target_id),
    CONSTRAINT chk_maintenance_target_type CHECK (
        target_type IN ('domain', 'host_group', 'project')
    )
);

CREATE INDEX idx_maintenance_targets_type_id ON maintenance_window_targets(target_type, target_id);

-- ============================================================
-- STATUS PAGES                                               [PC.3]
-- ============================================================
CREATE TABLE status_pages (
    id                   BIGSERIAL PRIMARY KEY,
    uuid                 UUID NOT NULL DEFAULT gen_random_uuid(),
    slug                 VARCHAR(128) NOT NULL,
    title                VARCHAR(255) NOT NULL,
    description          TEXT,
    published            BOOLEAN NOT NULL DEFAULT true,
    password_hash        VARCHAR(255),          -- bcrypt; NULL = public
    custom_domain        VARCHAR(255),
    theme                VARCHAR(32) DEFAULT 'default',
    logo_url             VARCHAR(512),
    footer_text          TEXT,
    custom_css           TEXT,
    auto_refresh_seconds INT NOT NULL DEFAULT 60,
    created_by           BIGINT REFERENCES users(id),
    created_at           TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at           TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT uq_status_pages_slug UNIQUE (slug)
);

CREATE TABLE status_page_groups (
    id             BIGSERIAL PRIMARY KEY,
    status_page_id BIGINT NOT NULL REFERENCES status_pages(id) ON DELETE CASCADE,
    name           VARCHAR(128) NOT NULL,
    sort_order     INT NOT NULL DEFAULT 0
);

CREATE INDEX idx_status_page_groups_page ON status_page_groups(status_page_id);

CREATE TABLE status_page_monitors (
    id           BIGSERIAL PRIMARY KEY,
    group_id     BIGINT NOT NULL REFERENCES status_page_groups(id) ON DELETE CASCADE,
    domain_id    BIGINT NOT NULL REFERENCES domains(id),
    display_name VARCHAR(128),    -- hides real FQDN if set
    sort_order   INT NOT NULL DEFAULT 0,
    CONSTRAINT uq_status_page_monitor UNIQUE (group_id, domain_id)
);

CREATE INDEX idx_status_page_monitors_group  ON status_page_monitors(group_id);
CREATE INDEX idx_status_page_monitors_domain ON status_page_monitors(domain_id);

CREATE TABLE status_page_incidents (
    id             BIGSERIAL PRIMARY KEY,
    status_page_id BIGINT NOT NULL REFERENCES status_pages(id) ON DELETE CASCADE,
    title          VARCHAR(255) NOT NULL,
    content        TEXT,              -- Markdown body
    severity       VARCHAR(32) NOT NULL DEFAULT 'info',  -- info, warning, danger
    pinned         BOOLEAN NOT NULL DEFAULT false,
    active         BOOLEAN NOT NULL DEFAULT true,
    created_by     BIGINT REFERENCES users(id),
    created_at     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT chk_incident_severity CHECK (severity IN ('info', 'warning', 'danger'))
);

CREATE INDEX idx_status_page_incidents_page   ON status_page_incidents(status_page_id);
CREATE INDEX idx_status_page_incidents_active ON status_page_incidents(status_page_id, active);

-- ============================================================
-- UPTIME ANALYTICS (PC.5)                                   [PC.5]
-- ============================================================
-- TimescaleDB continuous aggregates over probe_results.
-- Refresh policies are set here; the hypertable is created in
-- the TimescaleDB migration (000002_timescale.up.sql).
-- These views are created conditionally so plain PostgreSQL
-- (without TimescaleDB) does not error at migration time.

-- Hourly rollup: up/down/maintenance counts + response time stats.
-- Requires TimescaleDB to be installed; skipped otherwise.
DO $$
BEGIN
  IF EXISTS (
    SELECT 1 FROM pg_extension WHERE extname = 'timescaledb'
  ) THEN
    EXECUTE $sql$
      CREATE MATERIALIZED VIEW IF NOT EXISTS probe_stats_hourly
      WITH (timescaledb.continuous) AS
      SELECT
          domain_id,
          probe_type,
          time_bucket('1 hour', measured_at)         AS bucket,
          COUNT(*) FILTER (WHERE status = 'up')       AS up_count,
          COUNT(*) FILTER (WHERE status = 'down')     AS down_count,
          COUNT(*) FILTER (WHERE status = 'maintenance') AS maintenance_count,
          AVG(response_time_ms)                        AS avg_response_ms,
          MIN(response_time_ms)                        AS min_response_ms,
          MAX(response_time_ms)                        AS max_response_ms,
          PERCENTILE_CONT(0.95) WITHIN GROUP (ORDER BY response_time_ms) AS p95_response_ms
      FROM probe_results
      GROUP BY domain_id, probe_type, bucket
      WITH NO DATA
    $sql$;

    EXECUTE $sql$
      CREATE MATERIALIZED VIEW IF NOT EXISTS probe_stats_daily
      WITH (timescaledb.continuous) AS
      SELECT
          domain_id,
          probe_type,
          time_bucket('1 day', bucket)               AS bucket,
          SUM(up_count)                               AS up_count,
          SUM(down_count)                             AS down_count,
          SUM(maintenance_count)                      AS maintenance_count,
          AVG(avg_response_ms)                        AS avg_response_ms,
          MIN(min_response_ms)                        AS min_response_ms,
          MAX(max_response_ms)                        AS max_response_ms
      FROM probe_stats_hourly
      GROUP BY domain_id, probe_type, bucket
      WITH NO DATA
    $sql$;

    -- Refresh policies
    PERFORM add_continuous_aggregate_policy('probe_stats_hourly',
      start_offset => INTERVAL '2 hours',
      end_offset   => INTERVAL '1 hour',
      schedule_interval => INTERVAL '1 hour');

    PERFORM add_continuous_aggregate_policy('probe_stats_daily',
      start_offset => INTERVAL '2 days',
      end_offset   => INTERVAL '1 day',
      schedule_interval => INTERVAL '1 day');
  END IF;
END
$$;

-- ============================================================
-- CDN PROVIDERS + ACCOUNTS                                  [PE.1]
-- ============================================================
-- CDN/加速商供應商（與 Registrar、DNS Provider 並列）
CREATE TABLE IF NOT EXISTS cdn_providers (
    id              BIGSERIAL PRIMARY KEY,
    uuid            UUID NOT NULL DEFAULT gen_random_uuid(),
    name            VARCHAR(128) NOT NULL,          -- "Cloudflare", "聚合", "網宿"
    provider_type   VARCHAR(64)  NOT NULL,          -- "cloudflare"|"juhe"|"wangsu"|"baishan"|...
    description     TEXT,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    deleted_at      TIMESTAMPTZ,
    CONSTRAINT uq_cdn_providers_type_name UNIQUE (provider_type, name)
);

-- CDN 帳號（一個供應商可有多個帳號）
CREATE TABLE IF NOT EXISTS cdn_accounts (
    id              BIGSERIAL PRIMARY KEY,
    uuid            UUID NOT NULL DEFAULT gen_random_uuid(),
    cdn_provider_id BIGINT NOT NULL REFERENCES cdn_providers(id),
    account_name    VARCHAR(128) NOT NULL,          -- "直播2", "馬甲1"
    credentials     JSONB NOT NULL DEFAULT '{}',    -- {api_key, secret, token...}
    notes           TEXT,
    enabled         BOOLEAN NOT NULL DEFAULT true,
    created_by      BIGINT REFERENCES users(id),
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    deleted_at      TIMESTAMPTZ,
    CONSTRAINT uq_cdn_accounts_provider_name UNIQUE (cdn_provider_id, account_name)
);

CREATE INDEX IF NOT EXISTS idx_cdn_accounts_provider ON cdn_accounts(cdn_provider_id)
    WHERE deleted_at IS NULL;

-- FK from domains.cdn_account_id → cdn_accounts.id (PE.2)
-- Added here (after cdn_accounts is created) to satisfy forward-reference constraint.
ALTER TABLE domains
    ADD CONSTRAINT fk_domains_cdn_account
    FOREIGN KEY (cdn_account_id) REFERENCES cdn_accounts(id) ON DELETE SET NULL;

CREATE INDEX idx_domains_cdn_account ON domains (cdn_account_id)
    WHERE deleted_at IS NULL AND cdn_account_id IS NOT NULL;

-- ============================================================
-- SEED DATA                                                  [P1]
-- ============================================================
-- CDN 供應商預置清單 (PE.1)
INSERT INTO cdn_providers (name, provider_type, description) VALUES
    ('Cloudflare',    'cloudflare',   'Global CDN and DDoS protection'),
    ('聚合',           'juhe',         '中國聚合 CDN 加速服務'),
    ('網宿',           'wangsu',       '網宿科技 CDN'),
    ('白山雲',         'baishan',      '白山雲 CDN'),
    ('騰訊雲 CDN',     'tencent_cdn',  '騰訊雲內容分發網絡'),
    ('華為雲 CDN',     'huawei_cdn',   '華為雲內容分發網絡'),
    ('阿里雲 CDN',     'aliyun_cdn',   '阿里雲 CDN 加速服務'),
    ('Fastly',        'fastly',       'Fastly edge cloud platform')
ON CONFLICT (provider_type, name) DO NOTHING;

-- Five roles per ADR-0003 D7
INSERT INTO roles (name, description) VALUES
    ('viewer',          'Read-only access to all resources'),
    ('operator',        'Can manage domains, templates, host groups, and trigger releases'),
    ('release_manager', 'Can approve releases and manage release policies'),
    ('admin',           'Full access including user management and system configuration'),
    ('auditor',         'Read-only access plus audit log visibility')
ON CONFLICT (name) DO NOTHING;
