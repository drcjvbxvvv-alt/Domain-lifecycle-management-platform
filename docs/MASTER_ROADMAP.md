# MASTER ROADMAP — 域名生命週期與發布運維平台
# Domain Lifecycle & Deployment Platform — Complete Architecture & Development Guide

> **版本**: v2.0 — 2026-04-26
> **狀態**: 現行主控文件（取代舊版 ARCHITECTURE_ROADMAP.md）
> **說明**: 本文件是整個平台從設計→開發→測試→上線的唯一真實來源（Single Source of Truth）。
> 所有 Phase 的依賴關係、任務拆解、驗收標準、技術規格均在此定義。

---

## 目錄

1. [平台願景與目標](#1-平台願景與目標)
2. [系統架構總覽](#2-系統架構總覽)
3. [Phase 依賴關係圖](#3-phase-依賴關係圖)
4. [完整進度總表](#4-完整進度總表)
5. [Phase 1-2：發布平台（已完成）](#5-phase-1-2發布平台已完成)
6. [Phase A：域名資產層](#6-phase-a域名資產層)
7. [Phase B：DNS 操作層](#7-phase-bdns-操作層)
8. [Phase C：監控與告警](#8-phase-c監控與告警)
9. [Phase D：GFW 偵測與自動切換](#9-phase-dgfw-偵測與自動切換)
10. [Phase E：CDN 帳號管理與域名資產補全](#10-phase-ecdn-帳號管理與域名資產補全)
11. [技術棧規格](#11-技術棧規格)
12. [資料庫 Schema 演進](#12-資料庫-schema-演進)
13. [測試策略](#13-測試策略)
14. [開發工作流程](#14-開發工作流程)
15. [已知問題與技術債](#15-已知問題與技術債)

---

## 1. 平台願景與目標

### 1.1 一句話定義

> 一個能管理 **10+ 個專案、1萬+ 個域名** 的企業級平台，涵蓋：
> 域名資產 → DNS 操作 → 內容發布 → 可用性監控 → 防牆偵測與切換。

### 1.2 核心用戶場景

| 角色 | 場景 | 平台功能 |
|------|------|----------|
| **域名管理員** | 管理跨多家 Registrar/DNS 供應商的域名資產 | Phase A + E |
| **DNS 工程師** | 安全地批量變更 DNS 記錄，防止誤刪 | Phase B |
| **發布工程師** | 將 HTML + Nginx 配置部署到所有 Nginx 機器 | Phase 1-2 |
| **運維工程師** | 監控域名可用性、設定維護窗口、查看狀態頁 | Phase C |
| **安全工程師** | 偵測 GFW 封鎖，自動切換備用域名 | Phase D |

### 1.3 設計原則（不可妥協）

1. **狀態機有且只有一條寫入路徑** — 所有 lifecycle/release/agent 狀態變更都走對應 Service
2. **Artifact 不可變** — 簽名後的 artifact 不得修改，回滾是重新部署舊 artifact
3. **Agent 白名單操作** — Agent binary 結構上不含任意 shell 執行
4. **mTLS 隔離** — Agent 流量與管理控制台流量使用不同 auth scheme，互相無法越權
5. **告警去重** — 同一域名/相同嚴重度最多 1 條/小時
6. **DNS 安全臨界** — 變更超過區域的 33% 記錄數，Plan/Apply 強制暫停

---

## 2. 系統架構總覽

### 2.1 四層架構

```
┌─────────────────────────────────────────────────────────────────┐
│                    管理控制台 (Vue 3 SPA)                         │
│  域名資產  │  DNS操作  │  發布管理  │  監控  │  GFW偵測  │  帳號  │
└─────────────────────────────────────────────────────────────────┘
                              │ HTTPS + JWT
┌─────────────────────────────────────────────────────────────────┐
│                  控制平面 cmd/server (Gin)                         │
│  Domain API │ DNS API │ Release API │ Probe API │ Alert API      │
└──────┬──────────────────────────┬──────────────────────────────┘
       │ asynq (Redis)             │ mTLS
┌──────▼──────────┐     ┌─────────▼──────────────────────────────┐
│  工作節點         │     │  執行層                                   │
│  cmd/worker     │     │  cmd/agent (每台 Nginx 一個 binary)        │
│  (asynq worker) │     │  cmd/probe (GFW 偵測探針節點)              │
└──────┬──────────┘     └────────────────────────────────────────┘
       │
┌──────▼──────────────────────────────────────────────────────────┐
│                    資料與儲存層                                     │
│  PostgreSQL 16  │  TimescaleDB  │  Redis 7  │  MinIO (S3)        │
│  業務資料         │  探針時序資料   │  任務佇列   │  Artifact 物件儲存  │
└─────────────────────────────────────────────────────────────────┘
```

### 2.2 主要資料流

```
域名資產 (Phase A+E)
    └→ DNS 記錄管理 (Phase B) → 同步到 Cloudflare/阿里雲等
    └→ 發布任務 (Phase 1-2) → Agent 拉取 Artifact → Nginx reload
    └→ 可用性探針 (Phase C) → 結果寫入 TimescaleDB → 告警引擎
    └→ GFW 探針 (Phase D) → 裁定引擎 → 置信度評分 → 自動 DNS 切換
```

---

## 3. Phase 依賴關係圖

```
Phase 1-2 ──────────────────────────────────────────────┐
(Release Platform)                                       │
      │                                                  │
      ▼                                                  │
Phase A ────────────────────────────────────────────┐   │
(Domain Asset Layer)                                │   │
  PA.1 Schema                                       │   │
  PA.2 Registrar + DNS Provider CRUD                │   │
  PA.3 Domain Asset Extension                       │   │
  PA.4 SSL Tracking                                 │   │
  PA.5 Fee Schedule                                 │   │
  PA.6 Tags + Bulk                                  │   │
  PA.7 Expiry Dashboard                             │   │
  PA.8 Import Queue                                 │   │
      │                                             │   │
      ├─────────────────────────────────────────────┤   │
      ▼                                             ▼   │
Phase B                                         Phase C  │
(DNS Operations)                            (Monitoring) │
  PB.1 DNS Record Model                       PC.1 Probe │
  PB.2 Provider Sync Engine                   PC.2 Alert │
  PB.3 Plan/Apply API                         PC.3 Status│
  PB.4 Safety Thresholds                      PC.4 Maint.│
  PB.5 DNS UI                                 PC.5 Uptime│
  PB.6 Zone RBAC                              PC.6 Notify│
  PB.7 Templates + Drift                           │    │
      │                                             │   │
      └─────────────────────┬───────────────────────┘   │
                            ▼                           │
                        Phase D                         │
                     (GFW Detection)                    │
                       PD.1 Probe Binary                │
                       PD.2 Detection Engine            │
                       PD.3 Comparison + Verdict        │
                       PD.4 Blocking Alert ◄────────────┘
                       PD.5 Auto-Failover
                       PD.6 GFW Dashboard
                            │
                            ▼
                        Phase E  (新增 — 從截圖分析補充)
                    (CDN Account + Domain Enrichment)
                       PE.1 CDN 帳號管理
                       PE.2 域名資產補全欄位
                       PE.3 域名列表強化 UI
```

### 關鍵依賴規則

| 後置 Phase | 必須先完成 | 說明 |
|-----------|-----------|------|
| Phase B | Phase A PA.1-PA.3 | DNS 操作需要域名資產資料模型 |
| Phase C | Phase A PA.1-PA.4 | 監控需要 SSL 資料；探針需要 domain FK |
| Phase D | Phase B + Phase C | 自動切換需要 DNS Plan/Apply；告警需要 Alert Engine |
| Phase E | Phase A PA.1-PA.2 | CDN 帳號管理是 Registrar/DNS Provider 的平行擴充 |

---

## 4. 完整進度總表

| Phase | 任務 | 狀態 | 完成日期 | 說明 |
|-------|------|------|----------|------|
| **Phase 1-2** | 發布平台 | ✅ 完成 | 2026-04-20前 | Release + Agent + Worker |
| **PA.1** | Schema + Store | ✅ 完成 | 2026-04-21 | 32張表 + TimescaleDB |
| **PA.2** | Registrar + DNS Provider CRUD | ✅ 完成 | 2026-04-21 | API + UI |
| **PA.3** | Domain 資產擴充 | ✅ 完成 | 2026-04-21 | expiry/cost/transfer 欄位 |
| **PA.4** | SSL 憑證追蹤 | ✅ 完成 | 2026-04-21 | cert_expires_at + 狀態計算 |
| **PA.5** | 費用管理 | ✅ 完成 | 2026-04-21 | 年費 + 幣別 + FeeSchedule |
| **PA.6** | 標籤 + 批次操作 | ✅ 完成 | 2026-04-21 | 多對多標籤 + 批次加標 |
| **PA.7** | 到期儀表板 + 通知 | ✅ 完成 | 2026-04-21 | ExpiryDashboard + 告警 |
| **PA.8** | 批次匯入佇列 | ✅ 完成 | 2026-04-21 | CSV 匯入 + ImportHistory |
| **PB.1** | DNS 記錄資料模型 | ✅ 完成 | 2026-04-22 | dns_records + 10 種類型 |
| **PB.2** | Provider 同步引擎 | ✅ 完成 | 2026-04-22 | Cloudflare HTTP + 阿里雲 |
| **PB.3** | Plan/Apply 工作流 API | ✅ 完成 | 2026-04-22 | diff 計算 + Correction |
| **PB.4** | 安全臨界值 | ✅ 完成 | 2026-04-22 | 33% 規則 + 強制 dry-run |
| **PB.5** | DNS 管理 UI | ✅ 完成 | 2026-04-22 | Plan/Apply Tab |
| **PB.6** | Zone 層級 RBAC | ✅ 完成 | 2026-04-22 | 域名級別權限控制 |
| **PB.7** | DNS 模板 + Drift 偵測 | ✅ 完成 | 2026-04-22 | Template + CheckDrift |
| **PC.1** | 探針引擎 L1/L2/L3 | ✅ 完成 | 2026-04-22 | DNS+TCP+HTTP + asynq |
| **PC.2** | 告警引擎 + 去重 | ✅ 完成 | 2026-04-22 | dedup + 嚴重度路由 |
| **PC.3** | 公開狀態頁 | ✅ 完成 | 2026-04-26 | slug URL + 事件列表 |
| **PC.4** | 維護窗口 | ✅ 完成 | 2026-04-26 | single/recurring/cron |
| **PC.5** | 可用性儀表板 | ✅ 完成 | 2026-04-26 | TimescaleDB 聚合 + 日歷 |
| **PC.6** | 通知中樞 | ✅ 完成 | 2026-04-25 | Telegram/Slack/Webhook |
| **PD.1** | GFW 探針節點二進位 | ✅ 完成 | 2026-04-23 | cmd/probe + 4層偵測 |
| **PD.2** | 多層偵測引擎 | ✅ 完成 | 2026-04-26 | bogon/injection + 儲存 |
| **PD.3** | 控制 vs 探針比對 | ✅ 完成 | 2026-04-26 | OONI 決策樹 + 置信度 |
| **PD.4** | 封鎖告警 + 儀表板 | ✅ 完成 | 2026-04-26 | BlockingAlertService + GFW Dashboard |
| **PD.5** | 自動切換 DNS | 🔲 未開始 | — | 依賴 PD.4 + PB.3 |
| **PD.6** | GFW 儀表板 + 恢復監控 | 🔲 未開始 | — | 依賴 PD.4 |
| **PE.1** | CDN 帳號管理 | ✅ 完成 | 2026-04-26 | cdn_providers + cdn_accounts + Vue CRUD |
| **PE.2** | 域名資產欄位補全 | ✅ 完成 | 2026-04-26 | cdn_account_id + origin_ips + UI |
| **PE.3** | 域名列表 UI 強化 | 🔲 未開始 | — | 顯示 CDN/解析帳號 |

**整體進度**：27 / 31 任務完成（87.1%）

---

## 5. Phase 1-2：發布平台（已完成）

### 5.1 目標

建立從「HTML + Nginx 模板」到「Agent 部署到指定伺服器」的完整 CI/CD 流水線。

### 5.2 已完成功能

- **Template 管理**：`templates` + `template_versions`（版本不可變）
- **Artifact 建置**：Go `text/template` 渲染 → MinIO 上傳 → SHA256 checksum + 簽名
- **Release 狀態機**：`pending→planning→ready→executing→succeeded/failed`
- **Agent 狀態機**：`registered→online↔offline/busy/idle/draining/upgrading/error`
- **Pull-based Agent 協議**：mTLS over HTTPS，Agent 主動 poll task
- **Rollback**：重新部署前一個 artifact（不重建）

### 5.3 核心業務規則（仍有效）

```
1. 狀態機單一寫入路徑
2. Artifact 不可變（signed_at 設定後禁止修改）
3. Agent 只做白名單操作
4. 生產環境發布需 approval_requests.status = 'granted'
5. Nginx reload 批次：30秒 或 50個域名，取先到者
```

---

## 6. Phase A：域名資產層

### 6.1 目標

將 `domains` 表從「薄的部署目標」變成「完整的資產管理層」，為後續所有 Phase 的基礎。

### 6.2 已完成任務說明

#### PA.1 — Schema + Models + Store Layer ✅

**新增表**（在 `000001_init.up.sql`）：
- `registrars` — 註冊商主表（名稱、API類型、能力）
- `registrar_accounts` — 註冊商帳號（加密憑證 JSONB）
- `dns_providers` — DNS 供應商主表
- `ssl_certificates` — SSL 憑證追蹤
- `domain_fee_schedules` — 域名費用排程
- `domain_tags` / `tags` — 標籤多對多
- `domain_import_jobs` / `domain_import_items` — 批次匯入

**domains 表新增欄位**：
```sql
registrar_account_id  BIGINT FK
dns_provider_id       BIGINT FK
expiry_date           DATE
expiry_status         VARCHAR(16)  -- 'active'|'expiring_90d'|'expiring_30d'|'expiring_7d'|'expired'|'grace'
auto_renew            BOOLEAN
transfer_lock         BOOLEAN
annual_cost           DECIMAL(10,2)
currency              VARCHAR(8)
notes                 TEXT
tld                   VARCHAR(32)  -- 自動從 fqdn 提取
```

#### PA.2 — Registrar + DNS Provider CRUD ✅

**API 端點**：
```
GET/POST     /api/v1/registrars
GET/PUT/DELETE /api/v1/registrars/:id
GET/POST     /api/v1/registrars/:id/accounts
GET/POST     /api/v1/dns-providers
GET/PUT/DELETE /api/v1/dns-providers/:id
```

**UI**：RegistrarList.vue / RegistrarDetail.vue / DNSProviderList.vue / DNSProviderDetail.vue

#### PA.3 — PA.8 ✅（詳見 PHASE_A_TASKLIST.md）

### 6.3 Phase A 已知缺口（→ Phase E 補充）

從競品系統截圖分析出以下 **未涵蓋**的欄位：

| 缺失欄位 | 現有系統中的位置 | 重要性 |
|---------|----------------|--------|
| `cdn_account_id` — CDN/加速商帳號 | 無 | 高 — 每個域名必備 |
| `origin_ips` — 源站 IP | 無 | 高 — 運維基礎資訊 |
| `purpose` — 域名用途 | 無 | 中 — 管理識別用 |
| `platform` — 所屬平台 | 無 | 中 — 比 project 更細 |
| CDN 供應商帳號統一管理 | 無 | 高 — 與 Registrar 同等級 |

→ 詳見 **Phase E** 補充計劃。

---

## 7. Phase B：DNS 操作層

### 7.1 目標

從「知道 DNS 在哪裡」（Phase A）升級到「宣告式管理 DNS 記錄，安全同步到供應商」。
靈感來源：DNSControl、OctoDNS。

### 7.2 核心設計決策

**宣告式 + Plan/Apply 模式**（類似 Terraform）：
```
1. 操作員在平台 DB 定義「期望狀態」（dns_records 表）
2. 點 "Plan" → 平台從 DNS 供應商 API 取得「當前狀態」
3. 計算 diff → 生成 Corrections（Add/Update/Delete）
4. 安全臨界值檢查（> 33% 記錄數變更 → 阻止）
5. 操作員確認 → "Apply" → 平台呼叫 DNS 供應商 API 執行
6. 結果寫回 + audit log
```

### 7.3 已完成任務說明

#### PB.1 — DNS 記錄資料模型 ✅

```sql
CREATE TABLE dns_records (
    id          BIGSERIAL PRIMARY KEY,
    domain_id   BIGINT NOT NULL REFERENCES domains(id),
    record_type VARCHAR(10) NOT NULL,  -- A/AAAA/CNAME/MX/TXT/NS/SOA/SRV/CAA/PTR
    name        VARCHAR(255) NOT NULL,
    content     TEXT NOT NULL,
    ttl         INT NOT NULL DEFAULT 300,
    priority    INT,                    -- MX/SRV
    source      VARCHAR(16) DEFAULT 'platform',  -- 'platform'|'provider'|'template'
    synced_at   TIMESTAMPTZ,
    ...
);
```

#### PB.2 — Provider 同步引擎 ✅

- `pkg/provider/dns/` — `Provider` 介面
- `pkg/provider/dns/cloudflare.go` — Cloudflare REST API（無 SDK）
- `pkg/provider/dns/aliyun.go` — 阿里雲 DNS API
- `CompareRecords()` — 純函數，返回 `[]Correction`

#### PB.3 — PB.7 ✅（詳見 PHASE_B_TASKLIST.md）

---

## 8. Phase C：監控與告警

### 8.1 目標

三層探針驗證域名存活 + 發布是否成功落地，告警引擎去重後推送到 Telegram/Slack/Webhook。

### 8.2 探針三層架構

```
L1 探針（每5分鐘）：DNS resolve + TCP :443 + HTTP 200
    → 覆蓋所有 active 域名
    → 結果存入 probe_results hypertable（TimescaleDB）

L2 探針（發布完成後觸發）：
    → 拉取每個域名，確認 <meta name="release-version"> = artifact_id

L3 探針（高優先域名，每1分鐘）：
    → 額外的關鍵字/meta tag 驗證
```

### 8.3 告警引擎設計

```
探針失敗 → StateTracker.RecordAndDetect()
         → 狀態變化（up→down）→ AlertEngine.Fire()
                             → 去重（相同 dedup_key + 1小時內只 1 條）
                             → 嚴重度路由（P1/P2/P3/INFO）
                             → NotificationDispatcher → Telegram/Slack/Webhook
```

### 8.4 已完成功能清單 ✅

- **PC.1** 探針引擎：L1/L2/L3 + asynq 任務 + StateTracker（狀態變化偵測）
- **PC.2** 告警引擎：dedup + 嚴重度路由 + 通知分派
- **PC.3** 公開狀態頁：slug URL、分組、事件/公告、密碼保護
- **PC.4** 維護窗口：single/recurring_weekly/recurring_monthly/cron
- **PC.5** 可用性儀表板：TimescaleDB 連續聚合（小時/日）+ 日歷熱圖
- **PC.6** 通知中樞：channel CRUD + notification_rules + dispatch history

---

## 9. Phase D：GFW 偵測與自動切換

### 9.1 目標

在中國大陸境內部署探針節點，定期偵測 GFW 封鎖，與境外控制節點比對，
產生有置信度評分的「裁定（Verdict）」，達到閾值自動切換 DNS。

### 9.2 架構設計

```
CN 探針節點（cmd/probe）
    ↓ 4層探測（DNS/TCP/TLS/HTTP）
    ↓ POST /probe/v1/measurements
控制平面（cmd/server）
    ↓ MeasurementService.StoreMeasurements()
    ↓ gfw_measurements (TimescaleDB, 180天保留)
                    ↓
            VerdictService.AnalyzeAndStore()
                    ↓
        Analyzer.Classify（OONI 決策樹）
                    ↓
         ConfidenceTracker（Redis，2h TTL）
                    ↓
         gfw_verdicts 表
                    ↓
        AlertEngine（PD.4）→ 自動切換（PD.5）
```

### 9.3 OONI 決策樹（已實作於 internal/gfw/analyzer.go）

```
1. DNS 層：
   - probe 返回 bogon IP 或 < 5ms（注入）→ blocking = "dns"
   - probe 與 control IP 集合不重疊且非 CDN 路由 → blocking = "dns"

2. TCP 層：
   - probe 所有 TCP 連線失敗，control 有成功 → blocking = "tcp_ip"

3. TLS 層：
   - probe TLS 握手 connection_reset 或 handshake_failure，control 成功
   → blocking = "tls_sni"
   （注意：cert_error 不是 SNI 封鎖信號，不觸發此判斷）

4. HTTP 層：
   - probe HTTP 連線失敗 → blocking = "http-failure"
   - probe HTTP 200 但頁面標題/body長度差異 > 閾值 → blocking = "http-diff"
   - 全部正常 → accessible = true
```

### 9.4 置信度評分（已實作於 internal/gfw/confidence.go）

```
count=1, nodes=1  → confidence = 0.30  （單次觀測）
count=2, nodes=1  → confidence = 0.50  （同節點重複確認）
count≥1, nodes=2  → confidence = 0.70  （多節點佐證）
count≥3, nodes≥2  → confidence = 0.90  （高置信度確認）
accessible = true → confidence 重置為 0.00
```

Redis key: `gfw:confidence:{domain_id}` TTL = 2小時

### 9.5 已完成任務

| 任務 | 狀態 | 核心檔案 |
|------|------|---------|
| PD.1 探針節點二進位 | ✅ | `cmd/probe/main.go`, `cmd/probe/checker/` |
| PD.2 多層偵測引擎 | ✅ | `cmd/probe/checker/checker.go`, `bogon.go` |
| PD.3 比對引擎 + 裁定 | ✅ | `internal/gfw/analyzer.go`, `confidence.go`, `verdict_service.go` |

### 9.6 待實作任務

#### PD.4 — 封鎖告警 + 儀表板（未開始）

**目標**：confidence ≥ 0.7 → 觸發告警；confidence ≥ 0.9 → P1 告警

**設計**：
```go
// internal/gfw/blocking_alert.go
type BlockingAlertService struct {
    verdictSvc *VerdictService
    alertSvc   *alert.Service        // 複用 Phase C 的告警引擎
    logger     *zap.Logger
}

// 在 VerdictService.AnalyzeAndStore() 完成後呼叫
func (s *BlockingAlertService) EvaluateAndAlert(ctx, verdict Verdict) error
    // confidence >= 0.9 → severity=P1, title="[GFW P1] 高置信度封鎖 example.com"
    // confidence >= 0.7 → severity=P2
    // blocking != "" && confidence < 0.7 → severity=P3 (informational)
    // accessible = true → resolve existing alert
```

**資料庫**：不需新表（複用 `alert_events`，`source='gfw'`）

**API**：`GET /api/v1/gfw/verdicts/blocked` — 已實作（PD.3 完成）

**UI**：GFW 封鎖儀表板頁面（`web/src/views/gfw/BlockingDashboard.vue`）
- 封鎖域名列表（按 confidence 排序）
- 每個域名的 4 層封鎖狀態展示
- 時間軸（最近 24 小時的 verdict 變化）

**測試重點**：
- `confidence >= 0.9` → P1 告警觸發
- `confidence < 0.7` → 不告警
- `accessible=true` 後 → 現有告警 resolved
- 相同域名 1 小時內不重複告警（去重邏輯）

---

#### PD.5 — 自動切換 DNS（未開始）

**目標**：偵測到 P1 封鎖且操作員已設定「failover 規則」→ 自動執行 DNS 切換

**設計**：
```sql
-- 新表：gfw_failover_rules
CREATE TABLE gfw_failover_rules (
    id              BIGSERIAL PRIMARY KEY,
    domain_id       BIGINT NOT NULL REFERENCES domains(id),
    trigger_confidence DECIMAL(3,2) NOT NULL DEFAULT 0.90,
    failover_dns_provider_id BIGINT REFERENCES dns_providers(id),
    failover_records JSONB,     -- 切換後要設定的 DNS 記錄
    cooldown_minutes INT NOT NULL DEFAULT 60,
    enabled         BOOLEAN NOT NULL DEFAULT false,  -- 必須手動啟用
    last_triggered_at TIMESTAMPTZ,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
```

```go
// internal/gfw/failover_service.go
type FailoverService struct {
    verdictSvc *VerdictService
    dnsSvc     *dns.PlanApplyService  // 複用 Phase B 的 Plan/Apply 引擎
    alertSvc   *alert.Service
    logger     *zap.Logger
}

func (s *FailoverService) EvaluateAndFailover(ctx, verdict Verdict) error
    // 1. 查詢 failover_rules（domain_id + enabled=true）
    // 2. verdict.Confidence >= rule.TriggerConfidence → 觸發
    // 3. cooldown 檢查（last_triggered_at + cooldown_minutes > now → 跳過）
    // 4. 建立 DNS Plan（切換到 failover_records）
    // 5. Auto-apply（不需人工確認）
    // 6. 記錄 audit_log + 觸發 P1 告警（含 "自動切換已執行" 訊息）
```

**安全閘門**：
- `failover_rules.enabled` 預設為 `false` — 必須操作員手動開啟
- 測試環境(`project.env='staging'`) 的域名不觸發自動切換
- 每次切換後強制 60 分鐘 cooldown（防止頻繁切換）
- 切換前先 dry-run DNS Plan，若超過安全臨界值（33%）→ 放棄並告警

---

#### PD.6 — GFW 儀表板 + 恢復監控（未開始）

**頁面設計**（`/gfw/dashboard`）：
```
┌─────────────────────────────────────────────────────┐
│  GFW 偵測總覽                                         │
│  探針節點狀態：● cn-beijing-01 在線  ● hk-01 在線     │
│  過去24小時：3個域名封鎖，1個已切換，2個恢復           │
├─────────────────────────────────────────────────────┤
│  封鎖域名列表                                         │
│  域名           封鎖層    置信度   持續時間  操作      │
│  blocked.com   DNS      90%      2h 30m   手動切換   │
│  blocked2.com  TLS/SNI  70%      45m      查看詳情   │
├─────────────────────────────────────────────────────┤
│  探針節點健康狀態                                     │
│  節點ID         區域       角色    最後心跳  今日檢測   │
│  cn-beijing-01  cn-north  probe  2分鐘前  1247次     │
└─────────────────────────────────────────────────────┘
```

---

## 10. Phase E：CDN 帳號管理與域名資產補全

> **背景**：分析現有域名管理系統截圖，發現我們缺少 CDN 帳號管理 +
> 源站 IP + 域名用途欄位。這是日常運維中最常查看的資訊。

### 10.1 PE.1 — CDN 帳號管理 ✅（完成 2026-04-26）

**目標**：將 CDN/加速商帳號納入統一的「雲帳號管理」體系，與 Registrar、DNS Provider 並列。

**資料庫設計**：
```sql
CREATE TABLE cdn_providers (
    id              BIGSERIAL PRIMARY KEY,
    uuid            UUID NOT NULL DEFAULT gen_random_uuid(),
    name            VARCHAR(128) NOT NULL,   -- "Cloudflare", "聚合", "網宿", "白山雲"
    provider_type   VARCHAR(64) NOT NULL,    -- "cloudflare"|"juhe"|"wangsu"|"baishan"|"tencent_cdn"|"huawei_cdn"
    description     TEXT,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT uq_cdn_providers_type_name UNIQUE (provider_type, name)
);

CREATE TABLE cdn_accounts (
    id              BIGSERIAL PRIMARY KEY,
    uuid            UUID NOT NULL DEFAULT gen_random_uuid(),
    cdn_provider_id BIGINT NOT NULL REFERENCES cdn_providers(id),
    account_name    VARCHAR(128) NOT NULL,   -- "直播2", "馬甲1"
    credentials     JSONB NOT NULL,          -- 加密儲存 {api_key, secret, token 等}
    notes           TEXT,
    enabled         BOOLEAN NOT NULL DEFAULT true,
    created_by      BIGINT REFERENCES users(id),
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
```

**預置 CDN 供應商**（seed data）：
```sql
INSERT INTO cdn_providers (name, provider_type) VALUES
    ('Cloudflare',  'cloudflare'),
    ('聚合',         'juhe'),
    ('網宿',         'wangsu'),
    ('白山雲',        'baishan'),
    ('騰訊雲 CDN',   'tencent_cdn'),
    ('華為雲 CDN',   'huawei_cdn'),
    ('阿里雲 CDN',   'aliyun_cdn'),
    ('Fastly',      'fastly');
```

**API 端點**：
```
GET/POST         /api/v1/cdn-providers
GET/PUT/DELETE   /api/v1/cdn-providers/:id
GET/POST         /api/v1/cdn-providers/:id/accounts
GET/PUT/DELETE   /api/v1/cdn-accounts/:id
```

**UI**：
- `web/src/views/cdn-providers/CDNProviderList.vue`
- `web/src/views/cdn-providers/CDNProviderDetail.vue`（帳號列表）
- 側邊欄新增「CDN 供應商」菜單項

---

### 10.2 PE.2 — 域名資產欄位補全（未開始）

**目標**：在 `domains` 表補充 4 個關鍵運維欄位。

**Migration**（新增到 `000001_init.up.sql` 或新建 `000003_domain_cdn.up.sql`）：
```sql
ALTER TABLE domains ADD COLUMN cdn_account_id BIGINT REFERENCES cdn_accounts(id);
ALTER TABLE domains ADD COLUMN origin_ips     TEXT[];   -- 源站 IP，可多個
ALTER TABLE domains ADD COLUMN purpose        VARCHAR(128);  -- 如「主域名跳轉」「業務域名」「備用域名」
ALTER TABLE domains ADD COLUMN platform       VARCHAR(128);  -- 如「直播平台A」「彩票系統」

CREATE INDEX idx_domains_cdn_account ON domains(cdn_account_id);
```

**RegisterDomainRequest 補充欄位**：
```go
type RegisterDomainRequest struct {
    // ... 現有欄位 ...
    CDNAccountID *int64   `json:"cdn_account_id"`
    OriginIPs    []string `json:"origin_ips"`
    Purpose      *string  `json:"purpose"`
    Platform     *string  `json:"platform"`
}
```

**DomainResponse 補充欄位**：
```go
type DomainResponse struct {
    // ... 現有欄位 ...
    CDNAccountID *int64   `json:"cdn_account_id,omitempty"`
    CDNAccount   *string  `json:"cdn_account_name,omitempty"`  // join 展示
    OriginIPs    []string `json:"origin_ips,omitempty"`
    Purpose      *string  `json:"purpose,omitempty"`
    Platform     *string  `json:"platform,omitempty"`
}
```

---

### 10.3 PE.3 — 域名列表 UI 強化（未開始）

**目標**：域名列表直接顯示 CDN 帳號、解析帳號、源站 IP，讓運維一眼看清每個域名的完整指向。

**對比：現有 vs 目標**

| 列 | 現有 | 目標 |
|----|------|------|
| 域名 | ✅ | ✅ |
| 狀態 | ✅ | ✅ |
| 到期日 | ✅ | ✅ |
| DNS Provider | ✅（ID） | ✅（名稱） |
| Registrar 帳號 | ❌ | ✅ 顯示 `godaddy: winter` |
| 解析商帳號 | ❌ | ✅ 顯示 `國際阿里雲: horse@...` |
| CDN 帳號 | ❌ | ✅ 顯示 `聚合: 直播2` |
| 源站 IP | ❌ | ✅ 顯示（hover 展開多個） |
| SSL 到期 | ❌（在詳情） | ✅ 顯示（顏色預警） |
| 用途 | ❌ | ✅ 小字顯示 |

**DomainList.vue 欄位更新**：
```typescript
// 新增欄
{ title: '解析帳號',  key: 'dns_account',    width: 160, render: ... },
{ title: 'CDN 帳號', key: 'cdn_account',     width: 140, render: ... },
{ title: '源站 IP',  key: 'origin_ips',      width: 150, render: ... },  // hover tooltip
{ title: 'SSL 到期', key: 'ssl_expires_at',  width: 110, render: ... },  // 顏色預警
{ title: '用途',     key: 'purpose',         width: 120, render: ... },
```

**篩選器新增**：
- CDN 帳號篩選
- 用途篩選（有用途 / 無用途）
- 源站 IP 搜尋

---

### 10.4 Phase E 驗收標準

```
1. 管理員進入「CDN 供應商」頁面 → 看到 Cloudflare/聚合/網宿 等預置供應商
2. 新增 CDN 帳號：選「聚合」→ 填入帳號名「直播2」+ API Token → 儲存
3. 編輯域名 → 選擇 CDN 帳號「聚合: 直播2」、填入源站 IP「18.166.12.150」、
   用途「主域名跳轉」→ 儲存
4. 域名列表顯示完整資訊：域名 / 狀態 / 解析帳號 / CDN帳號 / 源站IP / 到期日
5. 域名詳情頁「基本資訊」tab 顯示所有 5 個帳號層級資訊
```

---

## 11. 技術棧規格

### 11.1 後端（不可更換）

| 組件 | 技術 | 版本 | 說明 |
|------|------|------|------|
| 語言 | Go | 1.22+ | 所有後端服務 + Agent |
| API 框架 | Gin | latest | RESTful JSON |
| DB 存取 | sqlx | 1.4.0 | 原生 SQL，無 ORM |
| 任務佇列 | asynq | latest | Redis backed |
| 模板引擎 | text/template | stdlib | Artifact 渲染 |
| 配置 | Viper | latest | YAML + env |
| 日誌 | Zap | latest | 結構化 JSON |
| Auth | golang-jwt/v5 | latest | JWT Bearer |
| Redis | go-redis/v9 | v9.18.0 | 直接使用 |

### 11.2 前端（不可更換）

| 組件 | 技術 | 版本 |
|------|------|------|
| 框架 | Vue 3 | latest |
| UI 元件庫 | Naive UI | latest |
| 語言 | TypeScript | latest |
| 構建工具 | Vite | latest |
| 狀態管理 | Pinia | latest |
| Router | Vue Router | latest |
| HTTP Client | axios | latest |

### 11.3 基礎設施

| 組件 | 技術 | 用途 |
|------|------|------|
| DB | PostgreSQL 16 + TimescaleDB | 業務資料 + 時序資料 |
| 快取/佇列 | Redis 7 | asynq broker + 置信度追蹤 |
| 物件儲存 | MinIO (S3-compatible) | Artifact 儲存 |
| 反向代理 | Caddy | Auto HTTPS + 靜態資源 |

### 11.4 API 回應格式（統一）

```json
// 成功
{ "code": 0, "data": {...}, "message": "ok" }

// 錯誤
{ "code": 40001, "data": null, "message": "domain not found" }

// 分頁列表
{ "code": 0, "data": { "items": [...], "total": 1200, "cursor": "..." }, "message": "ok" }
```

### 11.5 HTTP 狀態碼規範

| 狀態碼 | 場景 |
|--------|------|
| 200 | GET/PUT/PATCH 成功 |
| 201 | POST 建立成功 |
| 202 | 接受非同步任務（release create, artifact build） |
| 204 | DELETE 成功 |
| 400 | 驗證錯誤 |
| 401 | 未認證 |
| 403 | 無權限（RBAC） |
| 404 | 資源不存在 |
| 409 | 衝突（重複 FQDN、狀態機非法轉換） |
| 500 | 伺服器錯誤（只 log，不暴露細節） |

---

## 12. 資料庫 Schema 演進

### 12.1 已存在的主要表（`000001_init.up.sql`）

```
核心業務（Phase 1-2）：
  users, roles, user_roles, projects, domains, lifecycle_history
  templates, template_versions, template_variables
  artifacts, releases, release_shards, release_shard_targets
  agents, host_groups, agent_host_group_assignments
  rollback_records, audit_logs

Phase A 補充：
  registrars, registrar_accounts
  dns_providers
  ssl_certificates
  domain_fee_schedules
  tags, domain_tags
  domain_import_jobs, domain_import_items

Phase B 補充：
  dns_records, dns_sync_plans, dns_sync_results

Phase C 補充：
  probe_policies, probe_tasks, probe_results (TimescaleDB hypertable)
  alert_events
  notification_channels, notification_rules, notification_history
  maintenance_windows, maintenance_window_targets
  status_pages, status_page_groups, status_page_monitors, status_page_incidents

Phase D 補充：
  gfw_probe_nodes, gfw_check_assignments
  gfw_measurements (TimescaleDB, 180天保留)
  gfw_bogon_ips
  gfw_verdicts
```

### 12.2 Phase E 新增表

```
cdn_providers      — CDN 供應商主表
cdn_accounts       — CDN 帳號（含加密 credentials）
```

### 12.3 Phase E 修改現有表

```
domains            — 新增 cdn_account_id, origin_ips, purpose, platform
```

### 12.4 Migration 規則

1. 每個 UP migration 必須有對應 DOWN migration
2. **預上線例外**：`000001_init.up.sql` 在 Phase 1 cutover 前可直接修改
3. Phase 1 cutover 後，每次 schema 變更必須建新的 `00000N_xxx.up.sql`
4. 新表必須包含：`id BIGSERIAL PK`, `uuid UUID DEFAULT gen_random_uuid()`, `created_at`, `updated_at`, `deleted_at`（soft delete）

---

## 13. 測試策略

### 13.1 測試分層

```
Unit Tests（同 package _test.go）
    ↓ 快速，無外部依賴
    ↓ 使用 stub/mock interface
    ↓ table-driven tests for state machines

Integration Tests（需要 DB/Redis）
    ↓ 對 Store 層做真實 DB 查詢（docker-compose 起 PG）
    ↓ 不 mock 資料庫（被線上故障教訓）

E2E Tests（手動 + Postman）
    ↓ 完整流程測試
    ↓ 跑 docker-compose full stack
```

### 13.2 強制測試覆蓋點

| 類型 | 必須測試 | 方式 |
|------|---------|------|
| 狀態機 | 所有合法轉換 + 非法轉換 | table-driven |
| 狀態機 race | `-race -count=50` | go test race |
| Store 層 | 不用 mock，跑真實 DB | testcontainers 或 docker-compose |
| 告警去重 | 1小時內同 dedup_key 只 1 條 | unit test |
| DNS Plan | 超過 33% 記錄變更 → 阻止 | unit test |
| GFW 決策樹 | 所有 blocking 類型 + CDN 誤報抑制 | unit test (已完成) |
| 置信度評分 | 0.3/0.5/0.7/0.9 四個閾值 | unit test (已完成) |

### 13.3 測試命名規範

```go
// 命名：Test{功能}_{場景}
func TestLifecycleService_Transition_ValidApprovedToProvisioned(t *testing.T)
func TestLifecycleService_Transition_InvalidRequestedToActive(t *testing.T)
func TestAnalyzer_Classify_DNSBogonIP(t *testing.T)
func TestConfidenceTracker_Record_ResetOnAccessible(t *testing.T)
```

### 13.4 執行測試

```bash
make test                              # 全部單元測試
go test ./internal/gfw/... -race -count=50  # GFW 狀態機 race 測試
go test ./... -coverprofile=cover.out  # 覆蓋率報告
go tool cover -html=cover.out          # 瀏覽器查看覆蓋率
```

---

## 14. 開發工作流程

### 14.1 開發環境啟動

```bash
make dev          # docker-compose（PG + Redis + MinIO）+ air 熱重載
make web          # Vite dev server (前端)
make migrate-up   # 執行 DB migration
make test         # 跑所有測試
make lint         # golangci-lint + eslint
make build        # 編譯所有二進位（server/worker/migrate/agent）
```

### 14.2 新功能開發流程

```
1. 閱讀本文件對應 Phase 的規格
2. 更新 DB Migration（migrations/000001_init.up.sql 或新檔）
3. 更新 Store 層（store/postgres/xxx.go）
4. 實作 Service 層（internal/xxx/service.go）
5. 撰寫 Service 單元測試（_test.go，stub interfaces）
6. 實作 Handler 層（api/handler/xxx.go）
7. 在 Router 新增路由（api/router/router.go）
8. 更新 cmd/server/main.go 的依賴注入
9. 新增前端 TypeScript types（web/src/types/xxx.ts）
10. 實作前端 API 封裝（web/src/api/xxx.ts）
11. 實作前端 Store（web/src/stores/xxx.ts）
12. 實作前端 View（web/src/views/xxx/XxxList.vue）
13. 在側邊欄 MainLayout.vue 新增菜單項
14. 在 router/index.ts 新增路由
15. 執行 go build ./... && go test ./... 確保不破壞既有功能
16. 更新對應的 PHASE_X_TASKLIST.md 任務狀態
```

### 14.3 Git 工作流程

```
main      — 永遠可部署，保護分支
feature/* — 功能分支，一個任務一個分支
fix/*     — Bug 修復分支
```

Commit 格式：
```
feat(PA.1): schema + store layer for domain asset management
fix(PB.3): plan/apply safety check off-by-one error
docs: update MASTER_ROADMAP with Phase E CDN account spec
```

---

## 15. 已知問題與技術債

### 15.1 現有技術債

| 問題 | 位置 | 優先級 | 說明 |
|------|------|--------|------|
| `internal/artifact/builder_test.go` 編譯失敗 | `*mockStorage` 缺少 `GetObjectContent` 方法 | 中 | 預存在問題，需修復 `mockStorage` 介面 |
| `cmd/scanner/` 目錄存在 | 歷史殘留 | 低 | 等 Phase D 完成後清理 |
| DNS Provider UI 只顯示 ID | `DomainList.vue` | 低 | PE.3 一併改成顯示名稱 |
| Registrar 帳號列表在域名列表不可見 | `DomainList.vue` | 中 | PE.3 補充 |

### 15.2 Phase E 後的架構改進建議

1. **credentials 加密**：`registrar_accounts.credentials` 和 `cdn_accounts.credentials` 目前是 JSONB 明文。建議在 Phase E 實作時加入 application-level 加密（AES-256-GCM，key 存環境變數）。

2. **cdn_accounts 與 dns_providers 統一**：長遠來看，Cloudflare 同時是 DNS Provider 也是 CDN Provider。考慮建立統一的 `cloud_providers` + `cloud_accounts` 表，用 `provider_type[]` 多角色。

3. **TimescaleDB 保留策略**：
   - `probe_results`：90 天
   - `gfw_measurements`：180 天
   - 確認 `add_retention_policy` 在 migration 中正確設定

---

## 附錄 A：域名完整資訊模型（目標狀態）

```
domain {
    // 基本識別
    fqdn, tld, uuid, project_id

    // 生命週期
    lifecycle_state: requested|approved|provisioned|active|disabled|retired

    // 資產資訊（Phase A）
    registrar_account_id → registrar_accounts.id → registrars.name
    dns_provider_id      → dns_providers.id → name
    expiry_date, expiry_status, auto_renew, transfer_lock
    annual_cost, currency

    // SSL（Phase A PA.4）
    ssl_certificates → cert_subject, cert_issuer, expires_at, ssl_status

    // CDN + 源站（Phase E）
    cdn_account_id → cdn_accounts.id → cdn_providers.name + account_name
    origin_ips     []string
    purpose        string  ("主域名跳轉" | "業務域名" | "備用域名" | ...)
    platform       string  ("直播平台A" | ...)

    // DNS 記錄（Phase B）
    dns_records → A/AAAA/CNAME/MX/TXT/NS records

    // 監控（Phase C）
    probe_results → uptime, response_time, status
    alert_events  → blocking/down alerts

    // GFW 狀態（Phase D）
    gfw_verdicts  → blocking, confidence, measured_at
    gfw_failover_rules → trigger conditions + failover DNS
}
```

---

*本文件由 Claude Code (claude-sonnet-4-6) 於 2026-04-26 生成並維護。*
*如有架構決策變更，請同步更新本文件及對應 Phase 的 TASKLIST.md。*
