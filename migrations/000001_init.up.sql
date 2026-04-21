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
    id                BIGSERIAL PRIMARY KEY,
    uuid              UUID NOT NULL DEFAULT gen_random_uuid(),
    policy_id         BIGINT NOT NULL REFERENCES probe_policies(id),
    domain_id         BIGINT NOT NULL REFERENCES domains(id),
    release_id        BIGINT REFERENCES releases(id),
    expected_artifact_id BIGINT REFERENCES artifacts(id),
    scheduled_for     TIMESTAMPTZ NOT NULL,
    status            VARCHAR(20) NOT NULL DEFAULT 'pending',
    created_at        TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT chk_probe_tasks_status CHECK (
        status IN ('pending', 'running', 'completed', 'cancelled')
    )
);
CREATE INDEX idx_probe_tasks_scheduled ON probe_tasks (scheduled_for) WHERE status = 'pending';

-- ============================================================
-- ALERTS                                                     [P3]
-- ============================================================
CREATE TABLE alert_events (
    id            BIGSERIAL PRIMARY KEY,
    uuid          UUID NOT NULL DEFAULT gen_random_uuid(),
    severity      VARCHAR(8) NOT NULL,
    target_kind   VARCHAR(32) NOT NULL,
    target_id     BIGINT,
    title         VARCHAR(200) NOT NULL,
    detail        JSONB,
    dedup_key     VARCHAR(200),
    notified_at   TIMESTAMPTZ,
    resolved_at   TIMESTAMPTZ,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT chk_alert_events_severity CHECK (severity IN ('P1', 'P2', 'P3', 'INFO'))
);
CREATE INDEX idx_alert_events_dedup       ON alert_events (dedup_key, created_at DESC);
CREATE INDEX idx_alert_events_unresolved  ON alert_events (severity, created_at DESC) WHERE resolved_at IS NULL;

CREATE TABLE notification_rules (
    id              BIGSERIAL PRIMARY KEY,
    uuid            UUID NOT NULL DEFAULT gen_random_uuid(),
    name            VARCHAR(100) NOT NULL,
    project_id      BIGINT REFERENCES projects(id),
    severity_filter VARCHAR(8),
    target_kind     VARCHAR(32),
    channel         VARCHAR(20) NOT NULL,
    config          JSONB NOT NULL,
    enabled         BOOLEAN NOT NULL DEFAULT true,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT chk_notification_rules_channel CHECK (channel IN ('telegram', 'webhook', 'slack'))
);

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
-- SEED DATA                                                  [P1]
-- ============================================================
-- Five roles per ADR-0003 D7
INSERT INTO roles (name, description) VALUES
    ('viewer',          'Read-only access to all resources'),
    ('operator',        'Can manage domains, templates, host groups, and trigger releases'),
    ('release_manager', 'Can approve releases and manage release policies'),
    ('admin',           'Full access including user management and system configuration'),
    ('auditor',         'Read-only access plus audit log visibility')
ON CONFLICT (name) DO NOTHING;
