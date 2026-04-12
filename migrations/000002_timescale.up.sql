-- 000002_timescale.up.sql — TimescaleDB hypertable for probe_results [P3]
-- Pre-launch exception (ADR-0003 D9): may be edited in place during P1.

CREATE EXTENSION IF NOT EXISTS timescaledb;

CREATE TABLE probe_results (
    id                BIGSERIAL,
    domain_id         BIGINT NOT NULL,
    policy_id         BIGINT,
    probe_task_id     BIGINT,
    tier              SMALLINT NOT NULL,
    status            VARCHAR(16) NOT NULL,
    http_status       INT,
    response_time_ms  INT,
    response_size_b   INT,
    tls_handshake_ok  BOOLEAN,
    cert_expires_at   TIMESTAMPTZ,
    content_hash      VARCHAR(80),
    expected_artifact_id BIGINT,
    detected_artifact_id BIGINT,
    error_message     TEXT,
    probe_runner      VARCHAR(64),
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
