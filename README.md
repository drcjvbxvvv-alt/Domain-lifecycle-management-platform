# Domain Lifecycle Management Platform

> 域名全生命週期管理平台 — 管理 12,000+ 個域名，確保中國大陸持續可訪問性，< 2 分鐘偵測、< 5 分鐘自動切換。

---

## 功能概覽

- **域名自動化部署** — DNS 建立 → CDN 配置 → nginx conf 渲染 → SVN 提交 → Agent 部署 → 探針驗證，全流程自動化
- **三層探針監控** — 從中國大陸節點（電信/聯通/移動，Phase 1 每 ISP 一台共 3 台；Phase 2 擴展到 6 台）持續監控，60 秒一輪
- **GFW 封鎖偵測** — 識別 DNS 毒化、TCP 封鎖、SNI 封鎖、HTTP 劫持
- **自動切換（< 5 分鐘）** — 偵測到封鎖後自動從備用域名池取出域名，完整切換並驗證
- **批次發布（Canary）** — Shard 分批發布，成功率 < 95% 自動暫停，支援獨立 rollback
- **備用域名池** — 預熱備用域名（DNS + CDN 預先建立），確保切換時立即可用
- **多廠商支援** — DNS: Cloudflare、Alibaba Cloud、Tencent Cloud、GoDaddy；CDN: Cloudflare、Alibaba Cloud、Tencent Cloud、Huawei Cloud

---

## 技術棧

| 層級 | 技術 |
|------|------|
| 後端語言 | Go 1.22+ |
| API 框架 | Gin |
| 資料庫存取 | sqlx（原生 SQL，無 ORM） |
| 任務佇列 | asynq（Redis-backed） |
| 資料庫 | PostgreSQL 16 + TimescaleDB |
| 快取 / 佇列 | Redis 7 |
| 前端 | Vue 3 + Naive UI + TypeScript |
| 建置工具 | Vite |
| 狀態管理 | Pinia |
| 反向代理 | Caddy |
| 日誌 | Zap（結構化 JSON） |
| 設定 | Viper（YAML + 環境變數） |
| 認證 | JWT（golang-jwt/jwt/v5） |

---

## 專案結構

```
domain-platform/
├── cmd/
│   ├── server/        # API + Web server
│   ├── worker/        # asynq task worker
│   ├── scanner/       # 探針 scanner（部署到中國大陸節點）
│   └── migrate/       # DB migration 工具
├── internal/
│   ├── domain/        # 域名業務邏輯 + 狀態機
│   ├── project/       # 專案管理 + Prefix Rules
│   ├── release/       # 批次發布 + Shard + Reload Buffer
│   ├── probe/         # 探針接收邏輯
│   ├── alert/         # 告警引擎（Telegram + Webhook）
│   ├── switcher/      # 自動切換引擎
│   └── pool/          # 備用域名池管理
├── pkg/
│   ├── provider/
│   │   ├── dns/       # DNS Provider 介面 + 各廠商實作
│   │   └── cdn/       # CDN Provider 介面 + 各廠商實作
│   ├── svnagent/      # SVN Agent HTTP client
│   ├── template/      # nginx conf 模板引擎
│   └── notify/        # 通知（Telegram + Webhook）
├── api/
│   ├── handler/       # Gin handlers
│   ├── middleware/    # Auth、RBAC、Logger
│   └── router/        # 路由注冊
├── store/
│   ├── postgres/      # PostgreSQL queries（sqlx）
│   ├── timescale/     # TimescaleDB 探針資料
│   └── redis/         # Redis 操作
├── migrations/        # SQL migration 檔案
├── templates/         # nginx conf 模板（.conf.tmpl）
├── deploy/
│   ├── docker-compose.yml
│   ├── systemd/       # systemd service 檔案
│   └── svn-agent/     # Python SVN Agent（目標機器）
├── web/               # Vue 3 前端
│   └── src/
│       ├── api/       # API client + TypeScript types
│       ├── views/     # 頁面元件
│       ├── stores/    # Pinia stores
│       └── ...
├── docs/              # 架構文件 + ADR
├── configs/           # 設定範本
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
cp configs/config.yaml configs/config.local.yaml
# 編輯 config.local.yaml，填入 DB / Redis / JWT Secret 等設定

# 2. 啟動基礎設施（PostgreSQL + TimescaleDB + Redis）
docker compose -f deploy/docker-compose.yml up -d

# 3. 執行資料庫 migration
make migrate-up

# 4. 啟動 API server（需要安裝 air 熱重載）
make dev

# 5. 啟動前端開發伺服器
cd web && npm install && npm run dev
```

### 建置所有二進位

```bash
make build        # 建置 server、worker、migrate
make scanner      # 交叉編譯 scanner（linux/amd64）
make web          # 建置 Vue 前端
```

### 執行測試

```bash
make test                           # 所有單元測試
make lint                           # golangci-lint + eslint
go test -tags=integration ./...     # 整合測試（需要 DB + Redis）
cd web && npm run test              # 前端測試
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
| `JWT_SECRET` | JWT 簽名金鑰 | — |
| `JWT_EXPIRY` | Token 有效期 | `24h` |
| `TELEGRAM_BOT_TOKEN` | Telegram Bot Token | — |
| `TELEGRAM_CHAT_ID` | Telegram 頻道 ID | — |
| `WEBHOOK_URL` | Webhook 告警 URL | — |

Provider API 金鑰透過 `configs/providers.yaml` 設定，支援環境變數替換：

```yaml
dns:
  cloudflare:
    api_token: "${CLOUDFLARE_API_TOKEN}"
  aliyun:
    access_key_id: "${ALIYUN_ACCESS_KEY_ID}"
    access_key_secret: "${ALIYUN_ACCESS_KEY_SECRET}"
```

---

## 部署架構（Phase 1）

```
┌─── 台灣 ──────────────────────────────────────────┐
│  主節點（8C/32G）                                   │
│  ┌─────────────────────────────────────────────┐  │
│  │ Caddy  →  domain-platform（API :8080）       │  │
│  │           domain-worker（asynq worker）      │  │
│  │           PostgreSQL 16 + TimescaleDB        │  │
│  │           Redis 7                            │  │
│  └─────────────────────────────────────────────┘  │
│                                                    │
│  探針接收節點（2C/4G）                               │
│  ┌─────────────────────────────────────────────┐  │
│  │ probe-receiver  +  Alert Engine             │  │
│  │ Auto-Switch Engine  +  Telegram Bot         │  │
│  └─────────────────────────────────────────────┘  │
└────────────────────────────────────────────────────┘

┌─── 中國大陸 ───────────────────────────────────────┐
│  cn-probe-ct（電信）  →  domain-scanner            │
│  cn-probe-cu（聯通）  →  domain-scanner            │
│  cn-probe-cm（移動）  →  domain-scanner            │
└────────────────────────────────────────────────────┘
```

---

## 域名狀態機

```
inactive ──→ deploying ──→ active ──→ degraded ──→ switching ──→ active
                │                                       │
                ▼                                       ▼
              failed ──→ deploying (retry)            failed
                │
active ──→ suspended ──→ active (手動恢復)
blocked ──→ retired（終態）
```

---

## API 概覽

所有 API 回應格式統一：

```json
{ "code": 0, "data": { ... }, "message": "ok" }
```

主要端點：

| 方法 | 路徑 | 說明 |
|------|------|------|
| `POST` | `/api/v1/auth/login` | 登入取得 JWT |
| `GET` | `/api/v1/projects` | 列出所有專案 |
| `GET` | `/api/v1/domains` | 列出域名（cursor 分頁） |
| `POST` | `/api/v1/domains` | 新增域名 |
| `POST` | `/api/v1/domains/:id/deploy` | 部署域名 |
| `GET` | `/api/v1/releases` | 列出發布記錄 |
| `POST` | `/api/v1/releases/:id/pause` | 暫停發布 |
| `POST` | `/api/v1/probe/push` | 探針結果上報（mTLS） |

完整 API 文件見 `docs/ARCHITECTURE.md`。

---

## 開發文件

| 文件 | 說明 |
|------|------|
| `docs/CLAUDE_CODE_INSTRUCTIONS.md` | 實作指南、優先順序、Phase 規格 |
| `docs/ARCHITECTURE.md` | 系統架構、資料流、部署拓撲 |
| `docs/DATABASE_SCHEMA.md` | 完整 DB Schema、索引策略 |
| `docs/DEVELOPMENT_PLAYBOOK.md` | 新增 API / Provider / Task / 頁面的步驟範本 |
| `docs/TESTING.md` | 測試策略、mock 模式、覆蓋率要求 |

### Architecture Decision Records（ADR）

實作前的所有重大決策以 ADR 形式記錄於 `docs/adr/`。**ADR 為最新事實來源**；若 ADR 與其他文件衝突，以 ADR 為準。

| ADR | 標題 | 狀態 |
|-----|------|------|
| [ADR-0001](docs/adr/0001-architecture-revision-2026-04.md) | Architecture revision 2026-04（initial platform architecture） | Accepted |
| [ADR-0002](docs/adr/0002-pre-implementation-adjustments-2026-04.md) | Pre-implementation adjustments（state machine 單一寫入路徑、switch dual lock、prefix_rules soft-freeze、CDN clone 冪等、asynq queue 優先級、pool promoted lifecycle） | Accepted (2026-04-08) |

---

## Makefile 指令

```bash
make dev           # 啟動開發模式（air 熱重載）
make build         # 建置所有 Go 二進位
make scanner       # 交叉編譯 scanner（linux/amd64）
make web           # 建置 Vue 前端
make test          # 執行所有單元測試
make lint          # golangci-lint + eslint
make migrate-up    # 執行 DB migration
make migrate-down  # 回滾最後一次 migration
make clean         # 清除 bin/
```

---

## License

MIT
