# DATABASE_SCHEMA.md — Complete Schema Reference

> Claude Code: reference this when creating migrations or writing queries.

> **Pre-launch migration exception (continuation).** ADR-0001 (2026-04-08)
> established a one-time exception allowing the initial migration file
> `000001_init.up.sql` to be modified in place because the project had not
> shipped production data yet. ADR-0002 (same day) applies that same
> exception to add `releases.kind` and `chk_task_type`. After Phase 1
> cutover this exception window closes permanently — every subsequent
> schema change MUST be a new numbered migration file.

---

## Conventions

- All tables: `id BIGSERIAL PRIMARY KEY`, `uuid UUID NOT NULL DEFAULT gen_random_uuid()`, `created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()`, `updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()`, `deleted_at TIMESTAMPTZ`
- Foreign keys: `{table}_id BIGINT NOT NULL REFERENCES {table}(id)`
- Enums: stored as VARCHAR, validated in application layer (NOT as PostgreSQL ENUM — avoids migration pain)
- Timestamps: always TIMESTAMPTZ (UTC)
- Text search: GIN index on frequently searched text columns if needed

---

## Initial Migration: 000001_init.up.sql

```sql
-- Enable extensions
CREATE EXTENSION IF NOT EXISTS "pgcrypto";

-- ============================================================
-- PROJECTS
-- ============================================================
CREATE TABLE projects (
    id          BIGSERIAL PRIMARY KEY,
    uuid        UUID NOT NULL DEFAULT gen_random_uuid(),
    name        VARCHAR(100) NOT NULL,
    slug        VARCHAR(100) NOT NULL,
    description TEXT,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    deleted_at  TIMESTAMPTZ,
    CONSTRAINT uq_projects_name UNIQUE (name),
    CONSTRAINT uq_projects_slug UNIQUE (slug)
);

-- ============================================================
-- PREFIX RULES
-- ============================================================
CREATE TABLE prefix_rules (
    id              BIGSERIAL PRIMARY KEY,
    project_id      BIGINT REFERENCES projects(id),  -- NULL = system-wide default
    prefix          VARCHAR(50) NOT NULL,
    purpose         VARCHAR(100) NOT NULL,
    dns_provider    VARCHAR(50) NOT NULL,
    cdn_provider    VARCHAR(50) NOT NULL,
    nginx_template  VARCHAR(100) NOT NULL,
    html_template   VARCHAR(100),
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT uq_prefix_rules UNIQUE (project_id, prefix)
);

-- System-wide defaults have project_id = NULL
CREATE UNIQUE INDEX uq_prefix_rules_global ON prefix_rules (prefix) WHERE project_id IS NULL;

-- ============================================================
-- MAIN DOMAINS
-- ============================================================
CREATE TABLE main_domains (
    id          BIGSERIAL PRIMARY KEY,
    uuid        UUID NOT NULL DEFAULT gen_random_uuid(),
    domain      VARCHAR(253) NOT NULL,
    project_id  BIGINT NOT NULL REFERENCES projects(id),
    status      VARCHAR(20) NOT NULL DEFAULT 'inactive',
    conf_path   VARCHAR(500),
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    deleted_at  TIMESTAMPTZ,
    CONSTRAINT uq_main_domains_domain UNIQUE (domain),
    CONSTRAINT chk_main_domains_status CHECK (
        status IN ('inactive','deploying','active','degraded','switching','suspended','failed','blocked','retired')
    )
);

CREATE INDEX idx_main_domains_project_id ON main_domains (project_id);
CREATE INDEX idx_main_domains_status ON main_domains (status);

-- ============================================================
-- SUBDOMAINS
-- ============================================================
CREATE TABLE subdomains (
    id              BIGSERIAL PRIMARY KEY,
    uuid            UUID NOT NULL DEFAULT gen_random_uuid(),
    main_domain_id  BIGINT NOT NULL REFERENCES main_domains(id),
    prefix          VARCHAR(50) NOT NULL,
    fqdn            VARCHAR(253) NOT NULL,
    dns_provider    VARCHAR(50) NOT NULL,
    cdn_provider    VARCHAR(50) NOT NULL,
    nginx_template  VARCHAR(100) NOT NULL,
    html_template   VARCHAR(100),
    dns_record_id   VARCHAR(200),
    cdn_domain_id   VARCHAR(200),
    ssl_expiry      TIMESTAMPTZ,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    deleted_at      TIMESTAMPTZ,
    CONSTRAINT uq_subdomains_fqdn UNIQUE (fqdn)
);

CREATE INDEX idx_subdomains_main_domain_id ON subdomains (main_domain_id);

-- ============================================================
-- MAIN DOMAIN POOL (standby domains)
-- ============================================================
CREATE TABLE main_domain_pool (
    id          BIGSERIAL PRIMARY KEY,
    uuid        UUID NOT NULL DEFAULT gen_random_uuid(),
    project_id  BIGINT NOT NULL REFERENCES projects(id),
    domain      VARCHAR(253) NOT NULL,
    status      VARCHAR(20) NOT NULL DEFAULT 'pending',
    priority    INT NOT NULL DEFAULT 0,
    warmup_attempts INT NOT NULL DEFAULT 0,
    warmup_last_error TEXT,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT uq_pool_domain UNIQUE (domain),
    -- Status names are deliberately distinct from main_domains.status (see ARCHITECTURE.md §2.6)
    CONSTRAINT chk_pool_status CHECK (
        status IN ('pending','warming','ready','promoted','blocked','retired')
    )
);

CREATE INDEX idx_pool_project_status ON main_domain_pool (project_id, status, priority DESC);

-- ============================================================
-- DOMAIN STATE HISTORY (audit)
-- ============================================================
CREATE TABLE domain_state_history (
    id              BIGSERIAL PRIMARY KEY,
    main_domain_id  BIGINT NOT NULL REFERENCES main_domains(id),
    from_status     VARCHAR(20) NOT NULL,
    to_status       VARCHAR(20) NOT NULL,
    reason          TEXT,
    triggered_by    VARCHAR(50),   -- 'system', 'user:{uuid}', 'probe:{node}'
    changed_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_state_history_domain ON domain_state_history (main_domain_id, changed_at DESC);

-- ============================================================
-- SWITCH HISTORY
-- ============================================================
CREATE TABLE switch_history (
    id                  BIGSERIAL PRIMARY KEY,
    main_domain_id      BIGINT NOT NULL REFERENCES main_domains(id),
    from_domain         VARCHAR(253) NOT NULL,
    to_domain           VARCHAR(253) NOT NULL,
    trigger_type        VARCHAR(20) NOT NULL,  -- 'auto', 'manual'
    trigger_reason      TEXT,
    status              VARCHAR(20) NOT NULL DEFAULT 'in_progress',
    started_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    completed_at        TIMESTAMPTZ,
    rollback_at         TIMESTAMPTZ,
    error_detail        TEXT
);

CREATE INDEX idx_switch_history_domain ON switch_history (main_domain_id, started_at DESC);

-- ============================================================
-- SERVERS (target machines)
-- ============================================================
CREATE TABLE servers (
    id          BIGSERIAL PRIMARY KEY,
    uuid        UUID NOT NULL DEFAULT gen_random_uuid(),
    hostname    VARCHAR(253) NOT NULL,
    ip_address  INET NOT NULL,
    agent_port  INT NOT NULL DEFAULT 9090,
    project_id  BIGINT NOT NULL REFERENCES projects(id),
    status      VARCHAR(20) NOT NULL DEFAULT 'active',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    deleted_at  TIMESTAMPTZ,
    CONSTRAINT uq_servers_hostname UNIQUE (hostname)
);

-- ============================================================
-- RELEASES
-- ============================================================
-- `kind` distinguishes normal deploy releases from prefix-rule rebuild releases
-- (see ADR-0002 D3). Both use the same Shard / DomainTask pipeline, so the
-- column is informational for filtering/audit but load-bearing for the
-- rebuild flow because the service layer reads it to decide which subdomains
-- to enumerate.
CREATE TABLE releases (
    id              BIGSERIAL PRIMARY KEY,
    uuid            UUID NOT NULL DEFAULT gen_random_uuid(),
    project_id      BIGINT NOT NULL REFERENCES projects(id),
    title           VARCHAR(200),
    kind            VARCHAR(20) NOT NULL DEFAULT 'deploy',
    status          VARCHAR(20) NOT NULL DEFAULT 'pending',
    total_domains   INT NOT NULL DEFAULT 0,
    shard_size      INT NOT NULL DEFAULT 200,
    canary_shard_size INT NOT NULL DEFAULT 30,   -- see ARCHITECTURE.md §2.3: min(30, 2%), hard min 10
    canary_threshold FLOAT NOT NULL DEFAULT 0.95,
    created_by      BIGINT,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT chk_release_kind CHECK (
        kind IN ('deploy','rebuild')
    ),
    CONSTRAINT chk_release_status CHECK (
        status IN ('pending','running','paused','completed','failed','rolled_back')
    )
);

CREATE INDEX idx_releases_project ON releases (project_id, created_at DESC);

-- ============================================================
-- RELEASE SHARDS
-- ============================================================
CREATE TABLE release_shards (
    id          BIGSERIAL PRIMARY KEY,
    release_id  BIGINT NOT NULL REFERENCES releases(id),
    shard_index INT NOT NULL,
    status      VARCHAR(20) NOT NULL DEFAULT 'pending',
    domain_count INT NOT NULL DEFAULT 0,
    success_count INT NOT NULL DEFAULT 0,
    fail_count  INT NOT NULL DEFAULT 0,
    started_at  TIMESTAMPTZ,
    completed_at TIMESTAMPTZ,
    CONSTRAINT chk_shard_status CHECK (
        status IN ('pending','running','completed','failed','rolled_back')
    )
);

CREATE INDEX idx_shards_release ON release_shards (release_id, shard_index);

-- ============================================================
-- DOMAIN TASKS
-- ============================================================
CREATE TABLE domain_tasks (
    id              BIGSERIAL PRIMARY KEY,
    uuid            UUID NOT NULL DEFAULT gen_random_uuid(),
    release_id      BIGINT REFERENCES releases(id),
    shard_id        BIGINT REFERENCES release_shards(id),
    main_domain_id  BIGINT NOT NULL REFERENCES main_domains(id),
    task_type       VARCHAR(30) NOT NULL,  -- 'deploy','rebuild','switch','rollback'
    status          VARCHAR(20) NOT NULL DEFAULT 'pending',
    step            VARCHAR(30),
    error_detail    TEXT,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    completed_at    TIMESTAMPTZ,
    CONSTRAINT chk_task_type CHECK (
        task_type IN ('deploy','rebuild','switch','rollback')
    ),
    CONSTRAINT chk_task_status CHECK (
        status IN ('pending','dns','cdn','render','svn','deploy','verify','completed','failed','rolled_back')
    )
);

CREATE INDEX idx_domain_tasks_release ON domain_tasks (release_id, status);
CREATE INDEX idx_domain_tasks_domain ON domain_tasks (main_domain_id, created_at DESC);

-- ============================================================
-- CONF SNAPSHOTS
-- ============================================================
CREATE TABLE conf_snapshots (
    id              BIGSERIAL PRIMARY KEY,
    main_domain_id  BIGINT NOT NULL REFERENCES main_domains(id),
    domain_task_id  BIGINT REFERENCES domain_tasks(id),
    conf_content    TEXT NOT NULL,
    checksum        VARCHAR(64) NOT NULL,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_snapshots_domain ON conf_snapshots (main_domain_id, created_at DESC);

-- ============================================================
-- ALERT EVENTS
-- ============================================================
CREATE TABLE alert_events (
    id          BIGSERIAL PRIMARY KEY,
    uuid        UUID NOT NULL DEFAULT gen_random_uuid(),
    severity    VARCHAR(10) NOT NULL,  -- 'P0','P1','P2','P3','INFO'
    domain      VARCHAR(253),
    probe_node  VARCHAR(32),
    alert_type  VARCHAR(50) NOT NULL,
    message     TEXT NOT NULL,
    acked_by    BIGINT,
    acked_at    TIMESTAMPTZ,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_alerts_severity ON alert_events (severity, created_at DESC);
CREATE INDEX idx_alerts_domain ON alert_events (domain, created_at DESC);

-- ============================================================
-- USERS
-- ============================================================
-- Internal operator console: login identifier is `username`, NOT email.
-- See ARCHITECTURE.md §4 Management Console.
CREATE TABLE users (
    id          BIGSERIAL PRIMARY KEY,
    uuid        UUID NOT NULL DEFAULT gen_random_uuid(),
    username    VARCHAR(64) NOT NULL,
    password_hash VARCHAR(200) NOT NULL,
    display_name VARCHAR(100),
    role        VARCHAR(30) NOT NULL DEFAULT 'viewer',
    last_login  TIMESTAMPTZ,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    deleted_at  TIMESTAMPTZ,
    CONSTRAINT uq_users_username UNIQUE (username),
    CONSTRAINT chk_users_username CHECK (username ~ '^[a-zA-Z0-9_.-]{3,64}$'),
    CONSTRAINT chk_users_role CHECK (
        role IN ('viewer','operator','release_manager','admin','auditor')
    )
);

-- ============================================================
-- AUDIT LOGS
-- ============================================================
CREATE TABLE audit_logs (
    id          BIGSERIAL PRIMARY KEY,
    user_id     BIGINT REFERENCES users(id),
    action      VARCHAR(100) NOT NULL,
    target_type VARCHAR(50),
    target_id   VARCHAR(100),
    detail      JSONB,
    ip_address  INET,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_audit_logs_user ON audit_logs (user_id, created_at DESC);
CREATE INDEX idx_audit_logs_action ON audit_logs (action, created_at DESC);
CREATE INDEX idx_audit_logs_target ON audit_logs (target_type, target_id);

-- ============================================================
-- PROBE RESULTS (TimescaleDB hypertable)
-- ============================================================
-- See ARCHITECTURE.md §2.4 / §3 for tier semantics and block detection logic.
CREATE TABLE probe_results (
    probe_node       VARCHAR(32)  NOT NULL,
    isp              VARCHAR(16)  NOT NULL,
    domain           VARCHAR(253) NOT NULL,
    tier             SMALLINT     NOT NULL,        -- 1=L1, 2=L2, 3=L3
    status           VARCHAR(16)  NOT NULL,        -- ok/dns_poison/tcp_block/sni_block/http_hijack/content_tamper/timeout
    block_reason     VARCHAR(64),                  -- NULL when status='ok'
    dns_ok           BOOLEAN      NOT NULL DEFAULT FALSE,
    dns_ips          TEXT[],
    tcp_latency_ms   FLOAT,
    tls_handshake_ok BOOLEAN,                      -- L2+ only
    tls_sni_ok       BOOLEAN,                      -- L2+ only
    tls_cert_expiry  TIMESTAMPTZ,                  -- L3 only
    http_code        SMALLINT,
    http_hijacked    BOOLEAN DEFAULT FALSE,
    content_hash     BYTEA,                        -- L3 only, for content_tamper detection
    checked_at       TIMESTAMPTZ  NOT NULL,
    CONSTRAINT chk_probe_tier CHECK (tier IN (1,2,3))
);

SELECT create_hypertable('probe_results', 'checked_at');

CREATE INDEX idx_probe_results_domain ON probe_results (domain, checked_at DESC);
CREATE INDEX idx_probe_results_status ON probe_results (status, checked_at DESC);
CREATE INDEX idx_probe_results_tier ON probe_results (tier, checked_at DESC);

SELECT add_retention_policy('probe_results', INTERVAL '90 days');
```

## 000001_init.down.sql

```sql
DROP TABLE IF EXISTS probe_results CASCADE;
DROP TABLE IF EXISTS audit_logs CASCADE;
DROP TABLE IF EXISTS users CASCADE;
DROP TABLE IF EXISTS alert_events CASCADE;
DROP TABLE IF EXISTS conf_snapshots CASCADE;
DROP TABLE IF EXISTS domain_tasks CASCADE;
DROP TABLE IF EXISTS release_shards CASCADE;
DROP TABLE IF EXISTS releases CASCADE;
DROP TABLE IF EXISTS servers CASCADE;
DROP TABLE IF EXISTS switch_history CASCADE;
DROP TABLE IF EXISTS domain_state_history CASCADE;
DROP TABLE IF EXISTS main_domain_pool CASCADE;
DROP TABLE IF EXISTS subdomains CASCADE;
DROP TABLE IF EXISTS main_domains CASCADE;
DROP TABLE IF EXISTS prefix_rules CASCADE;
DROP TABLE IF EXISTS projects CASCADE;
```

---

## Query Patterns

### `main_domains.status` single write path (ADR-0002 D2)

`main_domains.status` is mutated in exactly one query constant in the
codebase:

```go
// store/postgres/domain.go
const updateStatusTx = `
    UPDATE main_domains
    SET status = $2, updated_at = NOW()
    WHERE id = $1`
```

This constant is called from exactly one place:
`internal/domain/service.go::Transition()`. The full transaction template is:

```go
func (s *Service) Transition(ctx context.Context, id int64, from, to, reason, triggeredBy string) error {
    tx, err := s.db.BeginTxx(ctx, nil)
    if err != nil { return fmt.Errorf("begin tx: %w", err) }
    defer tx.Rollback()

    var current string
    if err := tx.GetContext(ctx, &current,
        `SELECT status FROM main_domains WHERE id = $1 FOR UPDATE`, id); err != nil {
        return fmt.Errorf("lock main_domain %d: %w", id, err)
    }

    if current != from {
        return ErrStatusRaceCondition
    }
    if !CanTransition(current, to) {
        return ErrInvalidTransition
    }

    if _, err := tx.ExecContext(ctx, updateStatusTx, id, to); err != nil {
        return fmt.Errorf("update status: %w", err)
    }

    if _, err := tx.ExecContext(ctx, insertHistoryTx,
        id, from, to, reason, triggeredBy); err != nil {
        return fmt.Errorf("insert history: %w", err)
    }

    return tx.Commit()
}
```

CI gate:
```bash
grep -rn 'UPDATE main_domains SET status' --include='*.go' .
# Must return exactly one line, inside store/postgres/domain.go
```

### Switch lock query (ADR-0002 D1)

```go
// internal/switcher/service.go
func (s *Service) acquireSwitchLock(ctx context.Context, mainDomainID int64) (*sqlx.Tx, error) {
    // Step 1: Redis fast path
    workerID := s.workerID
    ok, redisErr := s.redis.SetNX(ctx,
        fmt.Sprintf("switch:lock:%d", mainDomainID),
        workerID,
        600*time.Second,
    ).Result()
    if redisErr != nil {
        s.logger.Warn("redis unreachable, falling back to PG row lock only",
            zap.Int64("main_domain_id", mainDomainID),
            zap.Error(redisErr),
        )
        // Fall through to PG lock
    } else if !ok {
        return nil, ErrSwitchInProgress
    }

    // Step 2: Postgres row lock (authoritative)
    tx, err := s.db.BeginTxx(ctx, nil)
    if err != nil {
        return nil, fmt.Errorf("begin tx: %w", err)
    }

    var row struct {
        ID     int64
        Status string
    }
    if err := tx.GetContext(ctx, &row,
        `SELECT id, status FROM main_domains WHERE id = $1 FOR UPDATE`,
        mainDomainID,
    ); err != nil {
        tx.Rollback()
        return nil, fmt.Errorf("lock main_domain %d: %w", mainDomainID, err)
    }

    if !CanTransition(row.Status, "switching") {
        tx.Rollback()
        return nil, ErrInvalidTransition
    }

    return tx, nil  // caller holds the tx for the duration of the switch
}
```

Release order: `DEL switch:lock:{id}`, then `tx.Commit()`. If the process
crashes between the two, the Redis TTL (600s) cleans up the orphan lock and
the Postgres transaction is rolled back by connection termination.
