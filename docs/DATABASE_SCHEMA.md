# DATABASE_SCHEMA.md — Complete Schema Reference

> **Aligned with PRD + ADR-0003 (2026-04-09).** This is the authoritative schema
> for the Domain Lifecycle & Deployment Platform. Reference this when creating
> migrations or writing queries.
>
> **Pre-launch migration exception (carried over from ADR-0001/0002 and
> reaffirmed by ADR-0003 D9).** During Phase 1, the initial migration files
> (`000001_init.up.sql` and `000002_timescale.up.sql`) may be edited in place
> because no production data exists yet. **After Phase 1 cutover this exception
> closes permanently** — every subsequent schema change MUST be a new numbered
> migration file.

---

## Conventions

- All business tables include: `id BIGSERIAL PRIMARY KEY`,
  `uuid UUID NOT NULL DEFAULT gen_random_uuid()`,
  `created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()`,
  `updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()`,
  `deleted_at TIMESTAMPTZ` (where soft-delete applies)
- Foreign keys: `{table}_id BIGINT NOT NULL REFERENCES {table}(id)`
- Enums: stored as VARCHAR with CHECK constraint, validated in application
  layer (NOT as PostgreSQL ENUM — avoids migration pain)
- Timestamps: always TIMESTAMPTZ (UTC)
- JSONB for flexible payloads (variables, manifests, reports, audit detail)
- Text search: GIN index on frequently searched text columns if needed

### Phase tags

Each table is tagged with the phase it must exist by:

| Tag | Meaning |
|---|---|
| **P1** | Required for Phase 1 (initial control plane skeleton) |
| **P2** | Required for Phase 2 (sharding, rollback, dry-run, agent management UI) |
| **P3** | Required for Phase 3 (canary, probe, alert, agent upgrade) |
| **P4** | Required for Phase 4 (lifecycle approval, nginx artifact, HA) |

Tables tagged P2-P4 may exist in `000001_init.up.sql` from day one (it is
cheaper to create empty tables than to add tables later, especially during
the pre-launch exception window). Application code that touches them is
gated by phase.

---

## Initial Migration: `000001_init.up.sql`

```sql
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
    password_hash VARCHAR(255) NOT NULL,           -- bcrypt
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
    is_prod     BOOLEAN NOT NULL DEFAULT false,    -- true → releases require approval
    owner_id    BIGINT REFERENCES users(id),
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    deleted_at  TIMESTAMPTZ,
    CONSTRAINT uq_projects_name UNIQUE (name),
    CONSTRAINT uq_projects_slug UNIQUE (slug),
    CONSTRAINT chk_projects_slug CHECK (slug ~ '^[a-z0-9][a-z0-9-]{1,98}[a-z0-9]$')
);

-- ============================================================
-- DOMAINS (Domain Lifecycle module)                          [P1]
-- ============================================================
CREATE TABLE domains (
    id              BIGSERIAL PRIMARY KEY,
    uuid            UUID NOT NULL DEFAULT gen_random_uuid(),
    project_id      BIGINT NOT NULL REFERENCES projects(id),
    fqdn            VARCHAR(253) NOT NULL,
    lifecycle_state VARCHAR(20) NOT NULL DEFAULT 'requested',
    owner_user_id   BIGINT REFERENCES users(id),
    dns_provider    VARCHAR(32),                  -- which DNS provider to use for provisioning
    dns_zone        VARCHAR(253),                 -- the zone the FQDN belongs to (e.g. "example.com")
    -- Per CLAUDE.md Critical Rule #1, lifecycle_state is only written by
    -- internal/lifecycle.Service.Transition().
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    deleted_at      TIMESTAMPTZ,
    CONSTRAINT uq_domains_fqdn UNIQUE (fqdn),
    CONSTRAINT chk_domains_lifecycle_state CHECK (
        lifecycle_state IN ('requested', 'approved', 'provisioned', 'active', 'disabled', 'retired')
    )
);
CREATE INDEX idx_domains_project_state ON domains (project_id, lifecycle_state) WHERE deleted_at IS NULL;
CREATE INDEX idx_domains_fqdn          ON domains (fqdn) WHERE deleted_at IS NULL;

-- Per-domain variables, merged with template defaults at artifact build time
CREATE TABLE domain_variables (
    id         BIGSERIAL PRIMARY KEY,
    domain_id  BIGINT NOT NULL REFERENCES domains(id) ON DELETE CASCADE,
    variables  JSONB NOT NULL DEFAULT '{}',
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_by BIGINT REFERENCES users(id),
    CONSTRAINT uq_domain_variables UNIQUE (domain_id)
);

-- Domain lifecycle history (audit trail; written by Transition())
CREATE TABLE domain_lifecycle_history (
    id           BIGSERIAL PRIMARY KEY,
    domain_id    BIGINT NOT NULL REFERENCES domains(id) ON DELETE CASCADE,
    from_state   VARCHAR(20),                      -- nullable for initial insert
    to_state     VARCHAR(20) NOT NULL,
    reason       TEXT,
    triggered_by VARCHAR(128) NOT NULL,            -- "user:{uuid}" | "system" | "approval:{id}"
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
    kind        VARCHAR(20) NOT NULL DEFAULT 'full',  -- html | nginx | full
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    deleted_at  TIMESTAMPTZ,
    CONSTRAINT uq_templates_project_name UNIQUE (project_id, name),
    CONSTRAINT chk_templates_kind CHECK (kind IN ('html', 'nginx', 'full'))
);

-- Template versions are IMMUTABLE once published_at IS NOT NULL.
-- Editing a template = publishing a new version (CLAUDE.md Critical Rule #9).
CREATE TABLE template_versions (
    id                 BIGSERIAL PRIMARY KEY,
    uuid               UUID NOT NULL DEFAULT gen_random_uuid(),
    template_id        BIGINT NOT NULL REFERENCES templates(id),
    version_label      VARCHAR(40) NOT NULL,       -- e.g. "v23" or "2026-04-09-001"
    content_html       TEXT,                       -- Go text/template source for HTML
    content_nginx      TEXT,                       -- Go text/template source for nginx conf
    default_variables  JSONB NOT NULL DEFAULT '{}',
    checksum           VARCHAR(80) NOT NULL,       -- sha256:... of (content_html || content_nginx || default_variables)
    published_at       TIMESTAMPTZ,                -- NULL = draft
    published_by       BIGINT REFERENCES users(id),
    created_at         TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    created_by         BIGINT REFERENCES users(id),
    CONSTRAINT uq_template_versions_label UNIQUE (template_id, version_label)
);
CREATE INDEX idx_template_versions_template ON template_versions (template_id, created_at DESC);

-- ============================================================
-- ARTIFACTS                                                  [P1]
-- ============================================================
-- An artifact is the immutable product of rendering a template version
-- against a domain set. CLAUDE.md Critical Rule #2: once signed_at is set,
-- nothing about an artifact may change.
CREATE TABLE artifacts (
    id                  BIGSERIAL PRIMARY KEY,
    uuid                UUID NOT NULL DEFAULT gen_random_uuid(),
    project_id          BIGINT NOT NULL REFERENCES projects(id),
    release_id          BIGINT,                    -- set when bound to a release; nullable for ad-hoc builds
    template_version_id BIGINT NOT NULL REFERENCES template_versions(id),
    artifact_id         VARCHAR(64) NOT NULL,      -- e.g. "art_01HXYZ..."
    storage_uri         TEXT NOT NULL,             -- e.g. "s3://bucket/project-a/rel_xxx/"
    manifest            JSONB NOT NULL,            -- the manifest.json contents
    checksum            VARCHAR(80) NOT NULL,      -- sha256:... of manifest.json + checksums.txt
    signature           TEXT,                      -- per ADR-0004 scheme
    domain_count        INT NOT NULL,
    file_count          INT NOT NULL,
    total_size_bytes    BIGINT NOT NULL,
    built_at            TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    built_by            BIGINT REFERENCES users(id),
    signed_at           TIMESTAMPTZ,               -- set after signature applied; row is immutable after this
    CONSTRAINT uq_artifacts_artifact_id UNIQUE (artifact_id)
);
CREATE INDEX idx_artifacts_release  ON artifacts (release_id);
CREATE INDEX idx_artifacts_template ON artifacts (template_version_id);
CREATE INDEX idx_artifacts_project  ON artifacts (project_id, built_at DESC);

-- ============================================================
-- HOST GROUPS                                                [P1]
-- ============================================================
-- A host_group is a logical group of nginx servers (and therefore agents)
-- that share the same nginx conf set. Releases scope to host_groups.
CREATE TABLE host_groups (
    id          BIGSERIAL PRIMARY KEY,
    uuid        UUID NOT NULL DEFAULT gen_random_uuid(),
    project_id  BIGINT NOT NULL REFERENCES projects(id),
    name        VARCHAR(100) NOT NULL,
    description TEXT,
    region      VARCHAR(64),
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    deleted_at  TIMESTAMPTZ,
    CONSTRAINT uq_host_groups_project_name UNIQUE (project_id, name)
);

-- ============================================================
-- AGENTS                                                     [P1]
-- ============================================================
CREATE TABLE agents (
    id              BIGSERIAL PRIMARY KEY,
    uuid            UUID NOT NULL DEFAULT gen_random_uuid(),
    agent_id        VARCHAR(64) NOT NULL,         -- assigned at registration: "agt_01HXYZ..."
    hostname        VARCHAR(253) NOT NULL,
    ip              INET,
    region          VARCHAR(64),
    datacenter      VARCHAR(64),
    host_group_id   BIGINT REFERENCES host_groups(id),
    agent_version   VARCHAR(40),
    capabilities    JSONB NOT NULL DEFAULT '[]',
    tags            JSONB NOT NULL DEFAULT '{}',
    cert_serial     VARCHAR(80),                  -- mTLS client cert serial → agent_id
    cert_expires_at TIMESTAMPTZ,
    status          VARCHAR(20) NOT NULL DEFAULT 'registered',
    last_seen_at    TIMESTAMPTZ,
    last_error      TEXT,
    -- Per CLAUDE.md Critical Rule #1, status is only written by
    -- internal/agent.Service.TransitionAgent().
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

-- Agent state history (written by TransitionAgent())
CREATE TABLE agent_state_history (
    id           BIGSERIAL PRIMARY KEY,
    agent_id     BIGINT NOT NULL REFERENCES agents(id) ON DELETE CASCADE,
    from_state   VARCHAR(20),
    to_state     VARCHAR(20) NOT NULL,
    reason       TEXT,
    triggered_by VARCHAR(128) NOT NULL,           -- "agent" | "user:{uuid}" | "system:health_check"
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX idx_agent_state_history_agent ON agent_state_history (agent_id, created_at DESC);

-- Agent heartbeats (lightweight, may be heavily inserted; consider TimescaleDB if it grows)
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
    release_id          VARCHAR(64) NOT NULL,     -- "rel_01HXYZ..."
    project_id          BIGINT NOT NULL REFERENCES projects(id),
    template_version_id BIGINT NOT NULL REFERENCES template_versions(id),
    artifact_id         BIGINT REFERENCES artifacts(id),
    release_type        VARCHAR(20) NOT NULL,     -- html | nginx | full
    trigger_source      VARCHAR(20) NOT NULL DEFAULT 'ui',  -- ui | api | webhook | scheduler
    status              VARCHAR(20) NOT NULL DEFAULT 'pending',
    requires_approval   BOOLEAN NOT NULL DEFAULT false,
    canary_shard_size   INT NOT NULL DEFAULT 30,  -- min(30, 5%) clamp applied at planning
    shard_size          INT NOT NULL DEFAULT 200,
    total_domains       INT,                       -- populated during planning
    total_shards        INT,
    success_count       INT NOT NULL DEFAULT 0,
    failure_count       INT NOT NULL DEFAULT 0,
    description         TEXT,
    -- Per CLAUDE.md Critical Rule #1, status is only written by
    -- internal/release.Service.TransitionRelease().
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

-- Release state history (written by TransitionRelease())
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

-- Scope of a release: which domains × which host_groups (a single release can target multiple)
CREATE TABLE release_scopes (
    id            BIGSERIAL PRIMARY KEY,
    release_id    BIGINT NOT NULL REFERENCES releases(id) ON DELETE CASCADE,
    domain_id     BIGINT NOT NULL REFERENCES domains(id),
    host_group_id BIGINT REFERENCES host_groups(id),
    CONSTRAINT uq_release_scopes UNIQUE (release_id, domain_id, host_group_id)
);
CREATE INDEX idx_release_scopes_release ON release_scopes (release_id);
CREATE INDEX idx_release_scopes_domain  ON release_scopes (domain_id);

-- Release shards                                                [P2 (schema P1, logic P2)]
CREATE TABLE release_shards (
    id              BIGSERIAL PRIMARY KEY,
    release_id      BIGINT NOT NULL REFERENCES releases(id) ON DELETE CASCADE,
    shard_index     INT NOT NULL,                 -- 0 = canary
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
-- DOMAIN TASKS & AGENT TASKS                                 [P1 schema, P1 basic logic]
-- ============================================================
-- A DomainTask represents the work needed for one (release, shard, domain).
-- It expands into one or more AgentTasks (one per agent that needs to act).
CREATE TABLE domain_tasks (
    id            BIGSERIAL PRIMARY KEY,
    uuid          UUID NOT NULL DEFAULT gen_random_uuid(),
    release_id    BIGINT NOT NULL REFERENCES releases(id),
    shard_id      BIGINT REFERENCES release_shards(id),
    domain_id     BIGINT NOT NULL REFERENCES domains(id),
    host_group_id BIGINT REFERENCES host_groups(id),
    task_type     VARCHAR(20) NOT NULL,           -- deploy_html | deploy_nginx | rollback | verify
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

-- AgentTask = the actual instruction sent to one agent
CREATE TABLE agent_tasks (
    id              BIGSERIAL PRIMARY KEY,
    uuid            UUID NOT NULL DEFAULT gen_random_uuid(),
    task_id         VARCHAR(64) NOT NULL,         -- sent to agent in TaskEnvelope
    domain_task_id  BIGINT NOT NULL REFERENCES domain_tasks(id),
    agent_id        BIGINT NOT NULL REFERENCES agents(id),
    artifact_id     BIGINT NOT NULL REFERENCES artifacts(id),
    artifact_url    TEXT,                          -- presigned S3 URL (regenerated on retry)
    payload         JSONB NOT NULL,                -- TaskEnvelope serialized
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

-- Per-task structured execution log (deployment_logs in PRD §24)
CREATE TABLE deployment_logs (
    id            BIGSERIAL PRIMARY KEY,
    agent_task_id BIGINT NOT NULL REFERENCES agent_tasks(id) ON DELETE CASCADE,
    phase         VARCHAR(32) NOT NULL,           -- download | verify | write | nginx_test | swap | reload | local_verify
    status        VARCHAR(20) NOT NULL,           -- ok | failed | skipped
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
    rollback_release_id      BIGINT REFERENCES releases(id),     -- the new release that did the rollback
    target_artifact_id       BIGINT NOT NULL REFERENCES artifacts(id),
    scope                    VARCHAR(20) NOT NULL,                -- 'release' | 'shard' | 'domain'
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
-- probe_policies define WHAT to check; probe_tasks are scheduled instances;
-- probe_results store outcomes (in TimescaleDB hypertable, see migration 000002).
CREATE TABLE probe_policies (
    id                BIGSERIAL PRIMARY KEY,
    uuid              UUID NOT NULL DEFAULT gen_random_uuid(),
    project_id        BIGINT REFERENCES projects(id),  -- nullable for global policies
    name              VARCHAR(100) NOT NULL,
    tier              SMALLINT NOT NULL,              -- 1, 2, or 3
    interval_seconds  INT NOT NULL,
    timeout_seconds   INT NOT NULL DEFAULT 8,
    expected_status   INT,
    expected_keyword  TEXT,
    expected_meta_tag VARCHAR(64),                    -- e.g. "release-version" for L2
    target_filter     JSONB,                          -- e.g. {"tags": ["core"]}
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
    release_id        BIGINT REFERENCES releases(id),  -- NULL for L1 standing checks
    expected_artifact_id BIGINT REFERENCES artifacts(id),  -- L2 only
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
    severity      VARCHAR(8) NOT NULL,                -- P1 | P2 | P3 | INFO
    target_kind   VARCHAR(32) NOT NULL,               -- 'release' | 'agent' | 'domain' | 'host_group'
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
    project_id      BIGINT REFERENCES projects(id),  -- NULL = global
    severity_filter VARCHAR(8),
    target_kind     VARCHAR(32),
    channel         VARCHAR(20) NOT NULL,            -- 'telegram' | 'webhook' | 'slack'
    config          JSONB NOT NULL,                  -- {chat_id: "..."} or {url: "..."}
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
    storage_uri   TEXT NOT NULL,                     -- s3://bucket/agent-bin/v1.2.3-linux-amd64
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
    scope_filter      JSONB NOT NULL,                -- {host_group: "...", tags: {...}}
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

-- Bulk agent log uploads (Phase 3+); could become a hypertable later
CREATE TABLE agent_logs (
    id          BIGSERIAL PRIMARY KEY,
    agent_id    BIGINT NOT NULL REFERENCES agents(id) ON DELETE CASCADE,
    task_id     VARCHAR(64),
    level       VARCHAR(8) NOT NULL,                 -- DEBUG | INFO | WARN | ERROR
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
    required_role   VARCHAR(32) NOT NULL,            -- 'release_manager' | 'admin'
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
    user_id     BIGINT REFERENCES users(id),         -- nullable for system actions
    action      VARCHAR(64) NOT NULL,                -- 'release.created', 'domain.transition.requested→approved', etc.
    target_kind VARCHAR(32) NOT NULL,                -- 'release', 'domain', 'agent', etc.
    target_id   VARCHAR(64),                         -- UUID of target
    detail      JSONB,
    ip_address  INET,
    user_agent  TEXT,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX idx_audit_logs_target ON audit_logs (target_kind, target_id, created_at DESC);
CREATE INDEX idx_audit_logs_user   ON audit_logs (user_id, created_at DESC);
CREATE INDEX idx_audit_logs_action ON audit_logs (action, created_at DESC);
```

---

## Migration `000001_init.down.sql`

```sql
DROP TABLE IF EXISTS audit_logs            CASCADE;
DROP TABLE IF EXISTS approval_requests     CASCADE;
DROP TABLE IF EXISTS agent_logs            CASCADE;
DROP TABLE IF EXISTS agent_upgrade_jobs    CASCADE;
DROP TABLE IF EXISTS agent_versions        CASCADE;
DROP TABLE IF EXISTS notification_rules    CASCADE;
DROP TABLE IF EXISTS alert_events          CASCADE;
DROP TABLE IF EXISTS probe_tasks           CASCADE;
DROP TABLE IF EXISTS probe_policies        CASCADE;
DROP TABLE IF EXISTS rollback_records      CASCADE;
DROP TABLE IF EXISTS deployment_logs       CASCADE;
DROP TABLE IF EXISTS agent_tasks           CASCADE;
DROP TABLE IF EXISTS domain_tasks          CASCADE;
DROP TABLE IF EXISTS release_shards        CASCADE;
DROP TABLE IF EXISTS release_scopes        CASCADE;
DROP TABLE IF EXISTS release_state_history CASCADE;
DROP TABLE IF EXISTS releases              CASCADE;
DROP TABLE IF EXISTS agent_heartbeats      CASCADE;
DROP TABLE IF EXISTS agent_state_history   CASCADE;
DROP TABLE IF EXISTS agents                CASCADE;
DROP TABLE IF EXISTS host_groups           CASCADE;
DROP TABLE IF EXISTS artifacts             CASCADE;
DROP TABLE IF EXISTS template_versions     CASCADE;
DROP TABLE IF EXISTS templates             CASCADE;
DROP TABLE IF EXISTS domain_lifecycle_history CASCADE;
DROP TABLE IF EXISTS domain_variables      CASCADE;
DROP TABLE IF EXISTS domains               CASCADE;
DROP TABLE IF EXISTS projects              CASCADE;
DROP TABLE IF EXISTS user_roles            CASCADE;
DROP TABLE IF EXISTS roles                 CASCADE;
DROP TABLE IF EXISTS users                 CASCADE;
DROP EXTENSION IF EXISTS "pgcrypto";
```

---

## Migration `000002_timescale.up.sql`

```sql
CREATE EXTENSION IF NOT EXISTS timescaledb;

CREATE TABLE probe_results (
    id                BIGSERIAL,
    domain_id         BIGINT NOT NULL,
    policy_id         BIGINT,
    probe_task_id     BIGINT,
    tier              SMALLINT NOT NULL,
    status            VARCHAR(16) NOT NULL,            -- ok | dns_fail | tcp_fail | tls_fail | http_fail | content_mismatch | timeout
    http_status       INT,
    response_time_ms  INT,
    response_size_b   INT,
    tls_handshake_ok  BOOLEAN,
    cert_expires_at   TIMESTAMPTZ,
    content_hash      VARCHAR(80),
    expected_artifact_id BIGINT,
    detected_artifact_id BIGINT,
    error_message     TEXT,
    probe_runner      VARCHAR(64),                     -- which probe runner host did this check
    checked_at        TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (id, checked_at),
    CONSTRAINT chk_probe_results_tier CHECK (tier IN (1, 2, 3))
);

SELECT create_hypertable('probe_results', 'checked_at', chunk_time_interval => INTERVAL '1 day');

-- 90-day retention
SELECT add_retention_policy('probe_results', INTERVAL '90 days');

-- Compress chunks older than 7 days
ALTER TABLE probe_results SET (
    timescaledb.compress,
    timescaledb.compress_segmentby = 'domain_id',
    timescaledb.compress_orderby   = 'checked_at DESC'
);
SELECT add_compression_policy('probe_results', INTERVAL '7 days');

CREATE INDEX idx_probe_results_domain_time ON probe_results (domain_id, checked_at DESC);
CREATE INDEX idx_probe_results_policy_time ON probe_results (policy_id, checked_at DESC) WHERE policy_id IS NOT NULL;
```

---

## Migration `000002_timescale.down.sql`

```sql
DROP TABLE IF EXISTS probe_results CASCADE;
DROP EXTENSION IF EXISTS timescaledb;
```

---

## Schema Notes

### Why no `prefix_rules`, `main_domains`, `subdomains`, `main_domain_pool`?

These tables existed in the previous design (superseded by ADR-0003) and were
specific to the GFW failover system:

- `prefix_rules` — coupled DNS provider, CDN provider, nginx template, and
  HTML template via subdomain prefix string. Replaced by `templates` +
  `template_versions` + `domain_variables`.
- `main_domains` + `subdomains` — hierarchical model. The new platform has
  flat `domains` (each row is one fully-qualified domain).
- `main_domain_pool` — standby domain warmup pool for GFW failover. Out of
  scope.

If GFW failover returns as a future vertical, those tables (or replacements)
will be added in a new migration with their own ADR.

### Why `releases.release_type` instead of one type?

Per ADR-0003 D5 (PRD §10), HTML and Nginx releases must be governed
separately. `release_type IN ('html', 'nginx', 'full')` is the explicit
distinction at the database level. Code paths that filter by release type:

- Approval flow: `release_type='nginx'` always requires Release Manager
- Frequency limits: `release_type='nginx'` may have stricter
  `max_concurrent_per_project`
- Audit: nginx releases are flagged in audit_logs with elevated visibility

### Why is `template_versions` immutable?

Per CLAUDE.md Critical Rule #9, once `published_at IS NOT NULL` the row is
frozen. This makes "what was deployed in release X" a fully reproducible
question: look up `releases.template_version_id`, fetch the immutable row,
re-render. There is no version drift, no "we updated the template after the
fact and now nothing matches".

Editing a published template means publishing a new version. Old releases
remain pinned to old versions forever.

### Why one big `audit_logs` table plus per-object `*_state_history` tables?

Two patterns serve different purposes:

- **`audit_logs`** is the global, queryable timeline. "Show me everything user
  X did this week" → one query, one table, one index.
- **`*_state_history`** tables (`domain_lifecycle_history`,
  `release_state_history`, `agent_state_history`) are state-machine audit
  trails written by each `Transition()` method. They are the ground truth for
  "what state was this object in at time T", with full from/to/reason/triggered_by.

The two are kept because they answer different questions efficiently. State
history rows are also written into `audit_logs` (the Transition() helpers do
both writes in the same transaction) so global queries don't miss anything.

### Index strategy

- Every FK has either an explicit index or is part of a composite where it
  appears first
- Status / state columns get partial indexes filtered by `WHERE deleted_at IS NULL`
- Time-series-ish fields (state history, audit, heartbeats) are indexed by
  `(scope_id, time DESC)` for efficient "most recent N" queries
- `probe_results` (TimescaleDB) is partitioned by `checked_at` chunks of 1 day

### Single-write-path enforcement (CI gates)

Per CLAUDE.md Critical Rule #1, three CI grep gates exist:

```bash
make check-lifecycle-writes  # exactly one hit: store/postgres/lifecycle.go::updateLifecycleStateTx
make check-release-writes    # exactly one hit: store/postgres/release.go::updateReleaseStatusTx
make check-agent-writes      # exactly one hit: store/postgres/agent.go::updateAgentStatusTx
```

Each gate is a `grep -rn 'UPDATE {table} SET (lifecycle_state|status)' --include='*.go'`
that whitelists the one allowed file path. Adding a second hit fails CI.
