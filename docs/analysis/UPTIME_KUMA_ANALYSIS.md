# Uptime Kuma Source Code Analysis

> **Date**: 2026-04-21
> **Source**: github.com/louislam/uptime-kuma (master branch, Node.js)
> **Files analyzed**: `db/knex_init_db.js`, `server/model/monitor.js`,
> `server/model/heartbeat.js`, `server/uptime-calculator.js`,
> notification model, status page model, maintenance model
> **Purpose**: Extract monitoring, alerting, and status page patterns for
> our platform's Phase 3 (Probe + Alert enhancement).

---

## 1. Architecture Overview

Uptime Kuma is a **single-node monitoring tool** (Node.js + SQLite):
- Single process runs all checks on intervals
- Real-time UI via Socket.IO (WebSocket)
- 90+ notification providers
- Public status pages with incidents

Relevant to us: monitor data model, heartbeat storage, notification
architecture, status page design, maintenance windows.

---

## 2. Monitor Schema (Exact from Source)

```javascript
// Core fields
id              INTEGER PRIMARY KEY AUTOINCREMENT
name            VARCHAR(150)
active          BOOLEAN DEFAULT true
user_id         INTEGER → user.id
type            VARCHAR(20)           // "http", "tcp", "ping", "dns", "docker", "push", "grpc", etc.
interval        INTEGER DEFAULT 20    // check interval in seconds
url             TEXT                  // target URL (for HTTP monitors)
hostname        VARCHAR(255)          // target hostname (for TCP/ping)
port            INTEGER

// HTTP-specific
method          TEXT DEFAULT "GET"
body            TEXT                  // request body
headers         TEXT                  // JSON headers
basic_auth_user TEXT
basic_auth_pass TEXT
maxredirects    INTEGER DEFAULT 10
accepted_statuscodes_json TEXT DEFAULT '["200-299"]'
keyword         VARCHAR(255)          // must appear in response
invert_keyword  BOOLEAN DEFAULT false // alert if keyword IS found
json_path       TEXT                  // JSONPath for response validation
expected_value  VARCHAR(255)

// Retry & timeout
maxretries      INTEGER DEFAULT 0    // retries before marking DOWN
retry_interval  INTEGER DEFAULT 0    // seconds between retries
timeout         DOUBLE DEFAULT 0     // request timeout

// TLS
ignore_tls      BOOLEAN DEFAULT false
expiry_notification BOOLEAN DEFAULT true  // alert on cert expiry
tls_ca          TEXT                 // custom CA cert
tls_cert        TEXT                 // client cert
tls_key         TEXT                 // client key

// DNS-specific
dns_resolve_type    VARCHAR(5)       // A, AAAA, MX, etc.
dns_resolve_server  VARCHAR(255)
dns_last_result     VARCHAR(255)

// Hierarchy
parent          INTEGER → monitor.id  // group/parent monitor

// Misc
upside_down     BOOLEAN DEFAULT false  // invert: DOWN = good
weight          INTEGER DEFAULT 2000   // sort order
push_token      VARCHAR(20)            // for push-type monitors
description     TEXT
created_date    DATETIME DEFAULT NOW()
```

**40+ additional fields** for: Docker, gRPC, MQTT, Radius, Kafka, OAuth,
database connections, SNMP, game servers, etc.

---

## 3. Heartbeat Schema (Exact from Source)

```javascript
id          INTEGER PRIMARY KEY AUTOINCREMENT
monitor_id  INTEGER NOT NULL → monitor.id (CASCADE DELETE)
status      SMALLINT NOT NULL       // 0=DOWN, 1=UP, 2=PENDING, 3=MAINTENANCE
important   BOOLEAN DEFAULT false   // true on status CHANGE (UP→DOWN, DOWN→UP)
msg         TEXT                     // status message / error detail
time        DATETIME NOT NULL        // check timestamp
ping        INTEGER                  // response time in ms
duration    INTEGER DEFAULT 0        // total check duration ms
down_count  INTEGER DEFAULT 0        // consecutive down count
end_time    DATETIME                 // for duration tracking
retries     INTEGER                  // retry attempts used
```

**Indexes**: `(monitor_id, time)`, `(monitor_id, important, time)`, `important`

### Retention Strategy

- **Important heartbeats** (status changes): kept FOREVER
- **Non-important** (regular checks): pruned to last 24h or last 100 per
  monitor (whichever is more)
- Statistical aggregation stored separately (see §6)

---

## 4. Notification Architecture

### Notification Table

```javascript
id          INTEGER PRIMARY KEY
name        VARCHAR(255)
active      BOOLEAN DEFAULT true
user_id     INTEGER
is_default  BOOLEAN DEFAULT false
config      TEXT (LONGTEXT)          // JSON blob with provider-specific config
```

### Many-to-Many Link

```javascript
// monitor_notification
monitor_id      INTEGER → monitor.id
notification_id INTEGER → notification.id
```

### Notification Dispatch (Conceptual)

```javascript
// On status change (important heartbeat):
if (previousStatus !== currentStatus) {
    for (notification of monitor.notifications) {
        const provider = getProvider(notification.config.type);
        await provider.send(notification.config, {
            monitor, heartbeat, previousStatus
        });
    }
}
```

### Dedup via State-Change Model

No time-based dedup needed — notifications fire ONLY on state transitions:
- UP → DOWN: "Monitor X is DOWN"
- DOWN → UP: "Monitor X is back UP"

Optional: `resend_interval` — re-notify every N checks if still DOWN.

### Notification Sent History (Dedup for Special Cases)

```javascript
// notification_sent_history
type        VARCHAR(50)       // e.g., "cert_expiry"
monitor_id  INTEGER
days        INTEGER           // days until expiry when sent
UNIQUE(type, monitor_id, days)
```

Prevents duplicate cert expiry notifications (only notify once per threshold).

---

## 5. Status Page Model

### Status Page Table

```javascript
id          INTEGER PRIMARY KEY
slug        VARCHAR(255) UNIQUE      // URL path
title       VARCHAR(255)
description TEXT
icon        VARCHAR(255)
theme       VARCHAR(30)
published   BOOLEAN DEFAULT true
password    VARCHAR                   // optional password protection
show_tags   BOOLEAN DEFAULT false
search_engine_index BOOLEAN DEFAULT true
footer_text TEXT
custom_css  TEXT
```

### Status Page Structure

```
Status Page
  └── Groups (ordered by weight)
        └── Monitors (ordered by weight, many-to-many)
              └── Current status + uptime bars
```

Related tables:
- `group`: `{ id, name, weight, status_page_id, public, active }`
- `monitor_group`: `{ monitor_id, group_id, weight, send_url }`
- `incident`: `{ id, title, content (Markdown), style (info/warning/danger), pin, active, status_page_id }`
- `status_page_cname`: `{ status_page_id, domain }` — custom domain

### Incident Management

Incidents are simple manual posts:
- Title + Markdown content + severity style
- Pin to top of status page
- Active/inactive toggle
- No automated incident creation from monitor state (manual only)

---

## 6. Uptime Calculation (Exact from Source)

### Three-Level Aggregation

| Table | Granularity | Retention | Fields |
|-------|-------------|-----------|--------|
| `stat_minutely` | 1 minute | 24 hours | monitor_id, timestamp, ping (avg/min/max), up_count, down_count |
| `stat_hourly` | 1 hour | 30 days | same |
| `stat_daily` | 1 day | 365 days | same |

### Formula

```
uptime_pct = up_count / (up_count + down_count) * 100
```

**Status mapping for calculation**:
- UP (1) + MAINTENANCE (3) → counted as UP
- DOWN (0) + PENDING (2) → counted as DOWN

Maintenance windows do NOT count against uptime.

### Time Window Accessors

```javascript
get24Hour()   → sum 1440 minutely buckets
get7Day()     → sum 168 hourly buckets
get30Day()    → sum 30 daily buckets
get1Year()    → sum 365 daily buckets
```

### Bucket Update (On Each Heartbeat)

```javascript
// Pseudocode
function update(status, ping, date) {
    flatStatus = (status == UP || status == MAINTENANCE) ? UP : DOWN;
    
    minuteBucket = getOrCreateBucket('minutely', truncateToMinute(date));
    minuteBucket[flatStatus == UP ? 'up' : 'down']++;
    minuteBucket.ping = runningAvg(minuteBucket.ping, ping);
    
    // Same for hourly and daily buckets
    // Prune old data beyond retention
}
```

---

## 7. Maintenance Window Model

```javascript
id          INTEGER PRIMARY KEY
title       VARCHAR(150)
description TEXT
active      BOOLEAN DEFAULT true
strategy    VARCHAR(50)     // "manual", "single", "cron", "recurring-interval",
                            // "recurring-weekday", "recurring-day-of-month"
start_date  DATETIME
end_date    DATETIME
start_time  TIME            // for recurring: daily start
end_time    TIME            // for recurring: daily end
weekdays    VARCHAR(250)    // JSON array [1,2,3,4,5]
days_of_month TEXT          // JSON array [1,15]
interval_day INTEGER        // for recurring-interval
cron        TEXT            // generated cron expression
timezone    VARCHAR(255)
duration    INTEGER         // seconds
```

**Link tables**:
- `monitor_maintenance`: which monitors are affected
- `maintenance_status_page`: which status pages show this maintenance

**Effect during maintenance**:
- Heartbeats recorded with `status = 3` (MAINTENANCE)
- Notifications suppressed
- Status page shows "Under Maintenance"
- Does NOT count against uptime percentage

---

## 8. Applicability to Our Platform

### Phase 3: Probe + Alert Enhancement

| Uptime Kuma Pattern | Our Adaptation |
|---|---|
| Monitor model (type + interval + retry + keyword) | `probe_policies` table enhancement — add keyword match, cert expiry check |
| Heartbeat model (status + ping + msg) | `probe_results` TimescaleDB hypertable (already planned) |
| State-change alerting (fire on transition) | Matches our Critical Rule #8 dedup model |
| 3-level aggregation (minutely/hourly/daily) | TimescaleDB continuous aggregates (built-in feature) |
| Maintenance windows | New `maintenance_windows` table + suppress during window |
| Status page with groups + incidents | New public-facing status page feature |

### Proposed Maintenance Window for Our Platform

```sql
CREATE TABLE maintenance_windows (
    id              BIGSERIAL PRIMARY KEY,
    uuid            UUID NOT NULL DEFAULT gen_random_uuid(),
    title           VARCHAR(150) NOT NULL,
    description     TEXT,
    strategy        VARCHAR(32) NOT NULL DEFAULT 'single',  -- single, cron, recurring
    start_at        TIMESTAMPTZ,
    end_at          TIMESTAMPTZ,
    recurrence      JSONB,       -- {"weekdays": [1,5], "start_time": "02:00", "duration_minutes": 120}
    timezone        VARCHAR(64) DEFAULT 'UTC',
    active          BOOLEAN NOT NULL DEFAULT true,
    created_by      BIGINT REFERENCES users(id),
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE maintenance_window_targets (
    maintenance_id  BIGINT NOT NULL REFERENCES maintenance_windows(id),
    target_type     VARCHAR(32) NOT NULL,  -- 'domain', 'host_group', 'project'
    target_id       BIGINT NOT NULL,
    PRIMARY KEY (maintenance_id, target_type, target_id)
);
```

### Proposed Status Page for Our Platform

```sql
CREATE TABLE status_pages (
    id              BIGSERIAL PRIMARY KEY,
    uuid            UUID NOT NULL DEFAULT gen_random_uuid(),
    slug            VARCHAR(128) NOT NULL UNIQUE,
    title           VARCHAR(255) NOT NULL,
    description     TEXT,
    published       BOOLEAN NOT NULL DEFAULT true,
    password_hash   VARCHAR(255),          -- optional protection
    custom_domain   VARCHAR(255),
    theme           VARCHAR(32) DEFAULT 'default',
    footer_text     TEXT,
    custom_css      TEXT,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE status_page_groups (
    id              BIGSERIAL PRIMARY KEY,
    status_page_id  BIGINT NOT NULL REFERENCES status_pages(id),
    name            VARCHAR(128) NOT NULL,
    sort_order      INT NOT NULL DEFAULT 0
);

CREATE TABLE status_page_monitors (
    group_id        BIGINT NOT NULL REFERENCES status_page_groups(id),
    domain_id       BIGINT NOT NULL REFERENCES domains(id),
    sort_order      INT NOT NULL DEFAULT 0,
    PRIMARY KEY (group_id, domain_id)
);

CREATE TABLE status_page_incidents (
    id              BIGSERIAL PRIMARY KEY,
    status_page_id  BIGINT NOT NULL REFERENCES status_pages(id),
    title           VARCHAR(255) NOT NULL,
    content         TEXT,                  -- Markdown
    severity        VARCHAR(32) NOT NULL DEFAULT 'info',  -- info, warning, danger
    pinned          BOOLEAN NOT NULL DEFAULT false,
    active          BOOLEAN NOT NULL DEFAULT true,
    created_by      BIGINT REFERENCES users(id),
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
```

### Proposed Uptime Aggregation (TimescaleDB)

Instead of Uptime Kuma's manual 3-table aggregation, we use TimescaleDB
continuous aggregates:

```sql
-- Raw probe results (already in ARCHITECTURE.md)
-- probe_results hypertable with 90-day retention

-- Continuous aggregate: hourly
CREATE MATERIALIZED VIEW probe_stats_hourly
WITH (timescaledb.continuous) AS
SELECT
    domain_id,
    time_bucket('1 hour', measured_at) AS bucket,
    COUNT(*) FILTER (WHERE status = 'up') AS up_count,
    COUNT(*) FILTER (WHERE status = 'down') AS down_count,
    AVG(response_time_ms) AS avg_response_ms,
    MIN(response_time_ms) AS min_response_ms,
    MAX(response_time_ms) AS max_response_ms
FROM probe_results
GROUP BY domain_id, bucket;

-- Continuous aggregate: daily
CREATE MATERIALIZED VIEW probe_stats_daily
WITH (timescaledb.continuous) AS
SELECT
    domain_id,
    time_bucket('1 day', bucket) AS bucket,
    SUM(up_count) AS up_count,
    SUM(down_count) AS down_count,
    AVG(avg_response_ms) AS avg_response_ms,
    MIN(min_response_ms) AS min_response_ms,
    MAX(max_response_ms) AS max_response_ms
FROM probe_stats_hourly
GROUP BY domain_id, bucket;
```

This gives us Uptime Kuma's 3-level aggregation for free via TimescaleDB.

### What We Should NOT Adopt

| Uptime Kuma Pattern | Why Not |
|---|---|
| SQLite single-file DB | We use PostgreSQL + TimescaleDB |
| Socket.IO real-time | We use polling (Phase 2 decision, WebSocket in Phase 3+) |
| 90+ notification providers in one codebase | We use `pkg/notify` with Telegram + Webhook + Slack (extensible) |
| Single-node architecture | We have distributed probe runners (Phase 3+) |
| Manual-only incidents | We'll auto-create incidents from probe state changes |

---

## 9. Summary

Uptime Kuma provides a complete reference for monitoring UX and data model.
Key takeaways for our platform:

1. **State-change alerting is the right model** — already matches our
   Critical Rule #8. No time-based dedup needed if you fire on transitions.

2. **3-level aggregation is essential** — we get this for free with
   TimescaleDB continuous aggregates instead of manual bucketing.

3. **Maintenance windows are critical** — suppress alerts + show correct
   status on public pages during planned downtime.

4. **Status pages are a product feature** — public visibility builds
   trust. Sections (groups) + incidents + custom domain support.

5. **Heartbeat retention strategy** — keep status changes forever, prune
   regular checks. TimescaleDB retention policy handles this.

6. **Monitor hierarchy (parent/child)** — useful for grouping checks
   under a domain or project.
