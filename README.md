# Domain Lifecycle & Deployment Platform

> 域名生命週期與發布運維平台 — 一套以 **Artifact + Pull Agent + Release** 為核心的
> 企業級 HTML / Nginx 發布系統。管理 10+ 專案、1万+ 域名，支援灰度發布、探針驗證、
> 自動告警、快速 rollback、完整審計。

> **2026-04-09 重要架構轉折**：本專案於該日從 GFW failover 系統正式轉向 PRD 描述的
> 通用發布平台。詳見 [`docs/adr/0003-pivot-to-generic-release-platform-2026-04.md`](docs/adr/0003-pivot-to-generic-release-platform-2026-04.md)。
> 原 GFW 相關設計（switcher / standby pool / prefix-based subdomain）暫不開發，
> 留作未來 vertical 模組。

---

## 功能概覽

- **域名生命週期管理** — 域名申請 → 審批 → DNS 自動配置 → 上線 → 停用 → 退役，獨立模組
- **Template + Variable 渲染** — 同一份 template 透過不同變數產生多種 artifact
- **Artifact 為單位的不可變部署** — 每次發布產生帶 manifest + checksum + signature 的 immutable artifact
- **Pull Agent (Go binary)** — 每台 nginx 主機跑一個 agent，主動拉任務、驗證、寫入、`nginx -t`、reload、回報
- **HTML 與 Nginx 發布分開治理** — 不同 release_type、不同權限、不同灰度策略
- **Shard + Canary 灰度發布** — 200-500 domain/shard，canary 先行，95% 成功率才繼續
- **三層 Probe 部署驗證** — L1 連通性、L2 release-version meta tag、L3 業務邏輯
- **基於 Artifact 的 Rollback** — 不重 build，重新部署上一個 artifact
- **5 角色 RBAC + 審批流** — Viewer / Operator / Release Manager / Admin / Auditor
- **mTLS 安全模型** — 每個 agent 一張憑證，支援輪替與吊銷
- **Agent 自管理** — 註冊、heartbeat、drain、disable、quarantine、canary upgrade
- **完整審計** — 每個動作、每次狀態變更、每個 release 決策都有 audit log

---

## 技術棧

| 層級 | 技術 |
|------|------|
| 後端語言 | Go 1.22+（含 Pull Agent） |
| API 框架 | Gin |
| 資料庫存取 | sqlx（原生 SQL，無 ORM） |
| 任務佇列 | asynq（Redis-backed） |
| 模板引擎 | Go `text/template` |
| 資料庫 | PostgreSQL 16 + TimescaleDB（probe_results 用） |
| 快取 / 佇列 | Redis 7 |
| **Artifact 儲存** | **MinIO（S3-compatible）** |
| **Agent 認證** | **mTLS over HTTPS** |
| 前端 | Vue 3 + Naive UI + TypeScript |
| 建置工具 | Vite |
| 狀態管理 | Pinia |
| 反向代理 | Caddy |
| 日誌 | Zap（結構化 JSON） |
| 設定 | Viper（YAML + 環境變數） |
| 認證（管理介面） | JWT（golang-jwt/jwt/v5） |

---

## 4 層架構

```
┌─────────────────────────────────────────────────────────┐
│  CONTROL PLANE                                           │
│  Vue 3 SPA  ↔  REST API (Gin)  ↔  internal/* services    │
└─────────────────────────────┬───────────────────────────┘
                              │
        ┌─────────────────────┼──────────────────────┐
        ▼                     ▼                      ▼
┌─────────────┐     ┌──────────────┐       ┌────────────────┐
│ TASK & DATA │     │ ARTIFACT     │       │ EXECUTION      │
│             │     │ STORE        │       │ PLANE          │
│ PostgreSQL  │     │ MinIO / S3   │       │ asynq workers  │
│ + Timescale │     │ (immutable)  │       │ (cmd/worker)   │
│ Redis       │     │              │       └────────┬───────┘
└─────────────┘     └──────┬───────┘                │
                           │                        │
                           ▼ (download)             │ (dispatch)
                  ┌───────────────────────────────────┐
                  │   PULL AGENTS (cmd/agent)          │
                  │   one Go binary per Nginx host     │
                  │   mTLS, whitelist actions only     │
                  └───────────────────────────────────┘
```

---

## 專案結構

```
domain-platform/
├── cmd/
│   ├── server/        # Control Plane: API + Web server
│   ├── worker/        # Execution Plane: asynq workers
│   ├── agent/         # Pull Agent: Go binary on each Nginx host
│   ├── migrate/       # DB migration tool
│   └── scanner/       # PARKED — reserved for future GFW vertical
├── internal/
│   ├── project/       # Project management
│   ├── lifecycle/     # Domain lifecycle (requested → ... → retired)
│   ├── template/      # Template + TemplateVersion
│   ├── artifact/      # Artifact build pipeline + manifest + signature
│   ├── release/       # Release subsystem (state machine + dispatcher)
│   ├── deploy/        # Deploy orchestration
│   ├── agent/         # Agent management (control-plane side)
│   ├── probe/         # Probe orchestration (deployment verification)
│   ├── alert/         # Alert engine + dedup + notify
│   ├── approval/      # Approval flow (Phase 4)
│   └── audit/         # Audit log writes
├── pkg/
│   ├── agentprotocol/ # Wire protocol shared by server + agent
│   ├── storage/       # Artifact storage interface + MinIO impl
│   ├── provider/dns/  # DNS provider abstraction (lifecycle module uses)
│   ├── template/      # text/template helpers
│   └── notify/        # Telegram + Webhook + Slack
├── api/
│   ├── handler/       # Gin handlers
│   ├── middleware/    # Auth, mTLS, RBAC, Logger
│   └── router/        # Route registration
├── store/
│   ├── postgres/      # PostgreSQL queries (sqlx)
│   ├── timescale/     # TimescaleDB probe results
│   └── redis/         # Redis operations
├── migrations/        # SQL migration files
├── deploy/
│   ├── docker-compose.yml   # PG + Redis + MinIO + TimescaleDB
│   └── systemd/             # systemd unit files for server / worker / agent
├── web/               # Vue 3 frontend (SPA)
├── docs/              # Architecture docs + ADRs
├── configs/           # Config templates
└── Makefile
```

---

## 快速開始

### 環境需求

- Go 1.22+
- Docker & Docker Compose
- Node.js 20+ / npm 10+

### 啟動開發環境

```bash
# 1. 複製設定檔
cp configs/config.example.yaml configs/config.local.yaml
# 編輯 config.local.yaml，填入 DB / Redis / MinIO / JWT Secret 等設定

# 2. 啟動基礎設施（PostgreSQL + TimescaleDB + Redis + MinIO）
docker compose -f deploy/docker-compose.yml up -d

# 3. 執行資料庫 migration
make migrate-up

# 4. 啟動 Control Plane（需要安裝 air 熱重載）
make dev

# 5. 啟動前端開發伺服器
cd web && npm install && npm run dev
```

### 建置所有二進位

```bash
make build        # 建置 server、worker、migrate、agent
make agent        # 只交叉編譯 agent（linux/amd64）— 部署到 nginx 主機
make web          # 建置 Vue 前端
```

### 執行測試

```bash
make test                       # 所有單元測試 (含 -race)
make lint                       # golangci-lint + eslint
make check-lifecycle-writes     # CI gate: lifecycle 單一寫入路徑
make check-release-writes       # CI gate: release 單一寫入路徑
make check-agent-writes         # CI gate: agent 單一寫入路徑
make check-agent-safety         # CI gate: agent binary 安全邊界
```

---

## 環境變數

| 變數 | 說明 | 預設值 |
|------|------|--------|
| `DB_HOST` | PostgreSQL 主機 | `localhost` |
| `DB_PORT` | PostgreSQL 連接埠 | `5432` |
| `DB_NAME` | 資料庫名稱 | `domain_platform` |
| `DB_USER` | 資料庫使用者 | `postgres` |
| `DB_PASSWORD` | 資料庫密碼 | — |
| `REDIS_ADDR` | Redis 位址 | `localhost:6379` |
| `REDIS_PASSWORD` | Redis 密碼 | — |
| `S3_ENDPOINT` | MinIO/S3 endpoint | `http://localhost:9000` |
| `S3_BUCKET` | Artifact bucket | `domain-platform-artifacts` |
| `S3_ACCESS_KEY` | S3 access key | — |
| `S3_SECRET_KEY` | S3 secret key | — |
| `JWT_SECRET` | JWT 簽名金鑰 | — |
| `JWT_EXPIRY` | Token 有效期 | `24h` |
| `AGENT_CA_CERT_PATH` | Platform CA 公鑰路徑 | `/etc/domain-platform/ca.crt` |
| `AGENT_CA_KEY_PATH` | Platform CA 私鑰路徑 | `/etc/domain-platform/ca.key` |
| `AGENT_CERT_VALIDITY` | 每張 agent 憑證有效期 | `8760h`（1 年） |
| `TELEGRAM_BOT_TOKEN` | Telegram Bot Token | — |
| `TELEGRAM_CHAT_ID` | Telegram 頻道 ID | — |
| `WEBHOOK_URL` | Webhook 告警 URL | — |

DNS provider API 金鑰透過 `configs/providers.yaml` 設定，支援環境變數替換：

```yaml
dns:
  cloudflare:
    api_token: "${CLOUDFLARE_API_TOKEN}"
  aliyun:
    access_key_id:     "${ALIYUN_ACCESS_KEY_ID}"
    access_key_secret: "${ALIYUN_ACCESS_KEY_SECRET}"
```

---

## 部署架構（Phase 1 最小規格）

```
┌── 平台側 ─────────────────────────────────────────────┐
│                                                         │
│  ┌───────────────────────────────────────────────────┐ │
│  │ Caddy → cmd/server (REST :8080 + mTLS :8443)      │ │
│  │         cmd/worker (asynq)                        │ │
│  └───────────────────────────────────────────────────┘ │
│                                                         │
│  ┌───────────────────────────────────────────────────┐ │
│  │ PostgreSQL 16 + TimescaleDB                       │ │
│  │ Redis 7                                           │ │
│  │ MinIO (artifact storage)                          │ │
│  └───────────────────────────────────────────────────┘ │
└─────────────────────────────────────────────────────────┘
                           │
                           │ mTLS (HTTPS + client cert)
                           │
        ┌──────────────────┼──────────────────┐
        ▼                  ▼                  ▼
┌─────────────┐    ┌─────────────┐    ┌─────────────┐
│ Nginx 主機 1 │    │ Nginx 主機 2 │    │ Nginx 主機 N │
│ + cmd/agent │    │ + cmd/agent │    │ + cmd/agent │
└─────────────┘    └─────────────┘    └─────────────┘
```

完整部署拓撲、HA、備份策略見 `docs/ARCHITECTURE.md` §6。

---

## 三個狀態機

平台有三個一級業務物件，各自擁有獨立的狀態機與單一寫入路徑（CLAUDE.md Critical Rule #1）。

### Domain Lifecycle

```
requested → approved → provisioned → active ←→ disabled
                                       │           │
                                       └───────────┴──→ retired (終態)
```

### Release

```
pending → planning → ready → executing → succeeded
                                │
                                ├→ paused → executing
                                │
                                └→ failed → rolling_back → rolled_back
```

### Agent

```
registered → online ⇄ busy / idle / draining / disabled / upgrading / offline
                                                              │
                                                              └→ error
```

---

## API 概覽

所有管理介面 API 回應格式統一：

```json
{ "code": 0, "data": { ... }, "message": "ok" }
```

主要端點（管理介面 — JWT auth）：

| 方法 | 路徑 | 說明 |
|------|------|------|
| `POST` | `/api/v1/auth/login` | 登入取得 JWT |
| `GET`  | `/api/v1/projects` | 列出專案 |
| `GET`  | `/api/v1/domains` | 列出域名 |
| `POST` | `/api/v1/domains` | 註冊域名（state = requested）|
| `POST` | `/api/v1/domains/:id/transition` | 域名狀態轉換（呼叫 Lifecycle.Transition） |
| `POST` | `/api/v1/templates/:id/versions/publish` | 發布 template 新版本（immutable） |
| `GET`  | `/api/v1/releases` | 列出 release |
| `POST` | `/api/v1/releases` | 建立 release |
| `POST` | `/api/v1/releases/:id/pause` | 暫停 release |
| `POST` | `/api/v1/releases/:id/resume` | 繼續 release |
| `POST` | `/api/v1/releases/:id/rollback` | Rollback（需 release_manager） |
| `GET`  | `/api/v1/agents` | 列出 agent |
| `POST` | `/api/v1/agents/:id/drain` | Drain agent |

Agent 協定端點（mTLS auth — agent 專用）：

| 方法 | 路徑 | 說明 |
|------|------|------|
| `POST` | `/agent/v1/register` | Agent 首次註冊 |
| `POST` | `/agent/v1/heartbeat` | Heartbeat |
| `GET`  | `/agent/v1/tasks` | Long-poll 拉取任務 |
| `POST` | `/agent/v1/tasks/:id/claim` | 認領任務 |
| `POST` | `/agent/v1/tasks/:id/report` | 回報執行結果 |
| `POST` | `/agent/v1/logs` | 上傳日誌 |

完整 API 規格見 `docs/ARCHITECTURE.md` §3.4 與 OpenAPI（待 Phase 1 中產出）。

---

## 開發文件

| 文件 | 說明 |
|------|------|
| `CLAUDE.md` | 編碼規範、三個狀態機、Critical Rules、tech stack |
| `docs/CLAUDE_CODE_INSTRUCTIONS.md` | Claude Code session 入口 + Model Selection Policy |
| `docs/PHASE1_TASKLIST.md` | **Phase 1 工作清單**（12 張任務卡，含 owner model / scope / 驗收條件） |
| `docs/PHASE1_EFFORT.md` | **Phase 1 開發週期粗估**（每任務 Lo/Hi 工作天、風險標記、週計畫 — 非承諾，P1.3 後需重新校準） |
| `docs/ARCHITECTURE.md` | 4 層架構、subsystem 說明、agent 協定、queue layout |
| `docs/DATABASE_SCHEMA.md` | 完整 schema、index 策略、Phase 標記 |
| `docs/DEVELOPMENT_PLAYBOOK.md` | API endpoint / state transition / artifact build / agent task / migration / Vue page 範本 |
| `docs/FRONTEND_GUIDE.md` | 前端設計系統、共用組件、token、狀態色 |
| `docs/TESTING.md` | 測試策略、mock 模式、覆蓋率要求 |

### Architecture Decision Records（ADR）

| ADR | 標題 | 狀態 |
|-----|------|------|
| [ADR-0001](docs/adr/0001-architecture-revision-2026-04.md) | Architecture revision 2026-04（GFW failover system）| **Superseded by ADR-0003** |
| [ADR-0002](docs/adr/0002-pre-implementation-adjustments-2026-04.md) | Pre-implementation adjustments（GFW failover）| **Superseded by ADR-0003** |
| [ADR-0003](docs/adr/0003-pivot-to-generic-release-platform-2026-04.md) | **Pivot to PRD-aligned generic release platform** | **Accepted (2026-04-09)** |

> ADR-0001 / ADR-0002 為歷史紀錄，**不是**當前事實來源。當前架構以 ADR-0003 + PRD 為準。

---

## Makefile 指令

```bash
make dev                       # 啟動開發模式（air 熱重載）
make build                     # 建置所有 Go 二進位 (server / worker / migrate / agent)
make agent                     # 交叉編譯 agent（linux/amd64）
make web                       # 建置 Vue 前端
make test                      # 執行所有單元測試（含 -race）
make lint                      # golangci-lint + eslint
make migrate-up                # 執行 DB migration
make migrate-down              # 回滾最後一次 migration
make check-lifecycle-writes    # CI gate: domains.lifecycle_state 單一寫入路徑
make check-release-writes      # CI gate: releases.status 單一寫入路徑
make check-agent-writes        # CI gate: agents.status 單一寫入路徑
make check-agent-safety        # CI gate: cmd/agent/ 結構安全邊界
make clean                     # 清除 bin/
```

---

## 工程紀律

平台奉行以下幾條核心紀律（細節見 `CLAUDE.md` §"Critical Business Rules"）：

1. **狀態機只有單一寫入路徑** — 三個狀態機，三個 CI gate，無例外
2. **Artifact 是不可變的** — 簽名後不能改，rollback = 重新部署舊 artifact
3. **Agent 只能做白名單動作** — 結構性強制，不靠設定檔
4. **Release 只屬於一個 project** — 不支援跨專案發布
5. **production release 需要審批** — Release Manager 或 Admin 簽核
6. **每次部署前 snapshot 上一版本** — agent 內建 `.previous/` 機制
7. **nginx reload 按主機聚合** — 30s buffer 或 50 domains，緊急 rollback 跳過
8. **告警去重** — 同 target + type + severity，1 小時 1 則
9. **template 版本一旦發布即 immutable** — 編輯 = 發新版本
10. **mTLS 給 agent，JWT 給管理介面** — 兩套 auth 互不相通

---

## License

MIT
