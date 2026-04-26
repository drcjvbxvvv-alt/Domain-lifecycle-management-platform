# DNS & CDN Infrastructure Management Roadmap

> **版本**: v1.0 — 2026-04-26
> **作者**: 根據產品需求與現有架構設計
> **適用範圍**: 本文件涵蓋 Phase A → D 的完整 DNS 記錄管理 + CDN 深度整合功能。
> **前置條件**: PF.1（Registrar Sync）已完成，域名已可從 GoDaddy/Namecheap/Aliyun 同步。

---

## 目錄

1. [背景與目標](#1-背景與目標)
2. [整體架構](#2-整體架構)
3. [依賴關係圖](#3-依賴關係圖)
4. [Phase A — DNS Provider 能力建設](#phase-a--dns-provider-能力建設)
5. [Phase B — 域名與 DNS 整合](#phase-b--域名與-dns-整合)
6. [Phase C — CDN 深度整合](#phase-c--cdn-深度整合)
7. [Phase D — 端對端驗證與監控](#phase-d--端對端驗證與監控)
8. [資料模型總表](#8-資料模型總表)
9. [API 端點總表](#9-api-端點總表)
10. [測試策略](#10-測試策略)
11. [風險與決策記錄](#11-風險與決策記錄)

---

## 1. 背景與目標

### 使用情境

```
[GoDaddy 購買域名]
        │ nameservers 指向
        ▼
[DNS 供應商帳號]          ← Phase A/B：本平台管理所有 DNS 記錄
(阿里雲 DNS / Cloudflare / 騰訊 DNSPod / 華為雲 DNS)
        │ CNAME / A 記錄指向
        ▼
[CDN 供應商]              ← Phase C：本平台管理 CDN 完整配置
(阿里雲 CDN / 騰訊雲 CDN / Cloudflare / 華為雲 CDN)
        │ 回源
        ▼
[Origin / Nginx]          ← 現有 Pull Agent 部署（不變）
```

### 目標

| 目標 | 說明 |
|---|---|
| **全面 DNS 管理** | 支援 Cloudflare / 阿里雲 / 騰訊 DNSPod / 華為雲，管理所有記錄類型 |
| **CDN 配置對等** | 在本平台操作 CDN 的功能與雲廠商控制台一致（緩存規則、訪問控制、HTTPS、性能優化等） |
| **鏈路可視化** | GoDaddy → NS → DNS → CDN → Origin 每一跳的狀態即時可見 |
| **變更審計** | 所有 DNS / CDN 配置變更記入 audit log |

---

## 2. 整體架構

### 後端分層

```
api/handler/
  dns_record.go        # DNS 記錄 CRUD handler
  cdn_domain.go        # CDN 域名管理 handler
  cdn_config.go        # CDN 配置管理 handler (cache/acl/perf/https)
  cdn_content.go       # CDN 內容管理 (purge/prefetch)

internal/
  dnsrecord/           # DNS 記錄業務邏輯
  cdndomain/           # CDN 域名生命週期
  cdnconfig/           # CDN 配置管理（統一介面 → 各供應商翻譯）

pkg/provider/
  dns/
    cloudflare.go      ← 已有 (需補 Record CRUD)
    aliyun.go          ← 新增
    tencentcloud.go    ← 新增
    huaweicloud.go     ← 新增
  cdn/
    provider.go        ← 新增 (介面定義)
    aliyun.go          ← 新增
    tencentcloud.go    ← 新增
    cloudflare.go      ← 新增
    huaweicloud.go     ← 新增

store/postgres/
  dns_record.go        # 新增
  cdn_domain.go        # 新增
  cdn_config.go        # 新增
```

### 前端分層

```
web/src/views/
  dns-providers/
    DNSProviderDetail.vue   ← 擴充：加入記錄管理 Tab
  domains/
    DomainDetail.vue        ← 擴充：加入 DNS 記錄 Tab + 鏈路狀態 Tab
  cdn-providers/
    CDNProviderDetail.vue   ← 擴充：加入域名列表
    CDNDomainDetail.vue     ← 新增：CDN 域名完整配置頁
```

---

## 3. 依賴關係圖

```
Phase A（DNS Provider 能力建設）
├── A.1  DNS Provider 介面擴充（Record CRUD 方法）  ✅ (完成)
├── A.2  Cloudflare DNS Record CRUD        [depends: A.1]  ✅ (完成)
├── A.3  阿里雲 DNS (ALIDNS) Record CRUD   [depends: A.1]  ✅ (完成)
├── A.4  騰訊雲 DNSPod Record CRUD         [depends: A.1]  ✅ (完成)
├── A.5  華為雲 DNS Record CRUD            [depends: A.1]  ✅ (完成)
└── A.6  DNS 記錄管理 UI                   [depends: A.1 + 至少一個 A.2-5]

Phase B（域名與 DNS 整合）        [depends: A 完成]
├── B.1  域名與 DNS 供應商帳號綁定
├── B.2  NS 委派驗證（async probe）        [depends: B.1]
├── B.3  域名 DNS 記錄同步（pull）         [depends: B.1 + A.2-5]
└── B.4  DNS 範本套用                      [depends: B.3]

Phase C（CDN 深度整合）           [depends: B 完成]
├── C.1  CDN Provider 介面定義
├── C.2  CDN 域名生命週期                  [depends: C.1]
│   ├── 新增加速域名
│   └── 删除加速域名
├── C.3  緩存配置                          [depends: C.2]
│   ├── 緩存規則（URL/後綴/目錄）
│   ├── 緩存 TTL
│   └── 狀態碼緩存
├── C.4  訪問控制                          [depends: C.2]
│   ├── Referer 黑/白名單
│   ├── IP 黑/白名單
│   ├── URL 鑑權（Token Auth）
│   └── 地區限制
├── C.5  性能優化                          [depends: C.2]
│   ├── 智能壓縮（Gzip/Brotli）
│   ├── HTTP/2 + HTTP/3
│   ├── 參數過濾
│   └── 頁面優化
├── C.6  HTTPS 與安全                      [depends: C.2]
│   ├── SSL 憑證配置
│   ├── 強制 HTTPS
│   ├── HSTS
│   └── TLS 版本控制
├── C.7  內容管理                          [depends: C.2]
│   ├── URL 刷新（Purge）
│   ├── 目錄刷新
│   └── URL 預熱（Prefetch）
└── C.8  回源配置                          [depends: C.2]
    ├── 回源協議
    ├── 回源 Host
    └── 回源 Header

Phase D（端對端驗證）             [depends: B + C]
├── D.1  NS 解析 Probe
├── D.2  DNS 解析 Probe
├── D.3  CDN 健康 Probe
└── D.4  全鏈路狀態 Dashboard
```

---

## Phase A — DNS Provider 能力建設

### A.1 DNS Provider 介面擴充 ✅ (完成)

**目標**：在現有 `pkg/provider/dns/provider.go` 的 `Provider` 介面中加入完整的記錄 CRUD 方法。

#### 設計

```go
// pkg/provider/dns/provider.go（擴充）

type Provider interface {
    // 現有方法（不變）
    Name() string
    CreateRecord(ctx context.Context, zone string, rec Record) (*Record, error)
    DeleteRecord(ctx context.Context, zone string, recordID string) error
    ListRecords(ctx context.Context, zone string, filter RecordFilter) ([]Record, error)
    UpdateRecord(ctx context.Context, zone string, recordID string, rec Record) (*Record, error)

    // 新增：取得供應商應配置的 NS 記錄（用於引導使用者在 Registrar 設定）
    GetNameservers(ctx context.Context, zone string) ([]string, error)

    // 新增：批次操作（減少 API 呼叫次數）
    BatchCreateRecords(ctx context.Context, zone string, recs []Record) ([]Record, error)
    BatchDeleteRecords(ctx context.Context, zone string, recordIDs []string) error
}

// 記錄類型擴充
type RecordType string
const (
    RecordTypeA     RecordType = "A"
    RecordTypeAAAA  RecordType = "AAAA"
    RecordTypeCNAME RecordType = "CNAME"
    RecordTypeTXT   RecordType = "TXT"
    RecordTypeMX    RecordType = "MX"
    RecordTypeNS    RecordType = "NS"
    RecordTypeSRV   RecordType = "SRV"
    RecordTypeCAA   RecordType = "CAA"
    RecordTypePTR   RecordType = "PTR"
)

type Record struct {
    ID       string
    Type     RecordType
    Name     string     // @ 代表根域名
    Content  string
    TTL      int
    Priority int        // MX / SRV 用
    Proxied  bool       // Cloudflare orange-cloud 用
    Extra    map[string]string // 供應商特有欄位
}

type RecordFilter struct {
    Type RecordType
    Name string
}
```

#### 實作任務

| 任務 | 檔案 | 說明 |
|---|---|---|
| 擴充介面 | `pkg/provider/dns/provider.go` | 加入 GetNameservers, Batch 方法 |
| 擴充 Record 結構 | `pkg/provider/dns/provider.go` | 加入 Priority, Proxied, Extra |
| 更新現有 Cloudflare 實作 | `pkg/provider/dns/cloudflare.go` | 實作新方法 |

#### 測試

- 介面 contract test（確保所有實作都覆蓋所有方法）
- `pkg/provider/dns/contract_test.go` — table-driven，注入 mock provider

---

### A.2 Cloudflare DNS Record CRUD ✅ (完成)

**API**: Cloudflare API v4 — `GET/POST/PUT/DELETE /zones/{zone_id}/dns_records`

**認證**: `Authorization: Bearer {api_token}` 或 `X-Auth-Email` + `X-Auth-Key`

#### 設計要點

```go
// pkg/provider/dns/cloudflare.go

// Cloudflare 記錄 API 回應
type cfDNSRecord struct {
    ID       string `json:"id"`
    Type     string `json:"type"`
    Name     string `json:"name"`
    Content  string `json:"content"`
    TTL      int    `json:"ttl"`      // 1 = auto
    Priority *int   `json:"priority"` // MX only
    Proxied  bool   `json:"proxied"`
}

// 注意：Cloudflare 的 zone 參數是 zone_id（非域名），
// 需要先 GET /zones?name={domain} 取得 zone_id
// 應快取 zone_id（TTL: 1h）避免重複查詢
```

**特殊處理**：
- `TTL=1` 代表「自動」，回傳時可能是 300 或其他值，比較時需特殊處理
- `Proxied=true` 時 Cloudflare 不回傳真實 IP，DNS 解析指向 Cloudflare anycast IP
- CAA / SRV 記錄的 Content 格式與標準不同，需要各自的序列化

#### 實作任務

| 任務 | 說明 |
|---|---|
| `ListRecords` | GET /zones/{id}/dns_records，支援 type/name filter |
| `CreateRecord` | POST，處理各類型序列化 |
| `UpdateRecord` | PUT（全量更新），非 PATCH |
| `DeleteRecord` | DELETE |
| `BatchCreateRecords` | 循環呼叫 CreateRecord（Cloudflare 無批次 API）|
| `GetNameservers` | 從 zone 詳情取得 name_servers 欄位 |
| zone_id 快取 | sync.Map + 1h TTL |

#### 測試

```
TestCloudflare_ListRecords_AllTypes     // A, AAAA, CNAME, TXT, MX, NS, SRV, CAA
TestCloudflare_CreateRecord_MX          // priority 欄位正確
TestCloudflare_UpdateRecord             // 全量更新語意
TestCloudflare_DeleteRecord
TestCloudflare_GetNameservers
TestCloudflare_ZoneIDCache              // 第二次呼叫不發 HTTP 請求
TestCloudflare_RateLimit                // 429 → ErrRateLimitExceeded
TestCloudflare_ZoneNotFound            // 404 → ErrZoneNotFound (新 sentinel)
```

---

### A.3 阿里雲 DNS (ALIDNS) Record CRUD ✅ (完成)

**API**: `https://alidns.aliyuncs.com` — 與阿里雲 registrar API 相同的 HMAC-SHA1 簽名機制，可共用 `aliyunEncode` / `aliyunSign` helper。

#### 主要 Actions

| Action | 用途 |
|---|---|
| `DescribeDomainRecords` | 列出記錄（支援 RRKeyWord, TypeKeyWord filter） |
| `AddDomainRecord` | 新增記錄 |
| `UpdateDomainRecord` | 更新記錄 |
| `DeleteDomainRecord` | 刪除單筆 |
| `DeleteDomainRecords` | 批次刪除（by Type） |
| `DescribeDNSSLBSubDomains` | 取得 NS（from zone info） |

#### 阿里雲記錄類型特殊說明

| 類型 | 備注 |
|---|---|
| MX | Priority 欄位獨立（Priority 參數，非 Content 的一部分） |
| TXT | Content 需要加雙引號 |
| SRV | Format: `priority weight port target`（空格分隔） |
| 隱性跳轉 / 顯性跳轉 | 阿里雲特有，映射為 TYPE=REDIRECT，存入 Extra |

#### 設計要點

```go
// pkg/provider/dns/aliyun.go

// 與阿里雲 registrar provider 共用 signing helper
// 建議抽取到 pkg/provider/aliyunauth/signer.go 供兩個 package import
type AliyunSigner struct {
    AccessKeyID     string
    AccessKeySecret string
}
func (s *AliyunSigner) SignedURL(baseURL, action string, params map[string]string) string
```

#### 實作任務

| 任務 | 說明 |
|---|---|
| 抽取 aliyun signing helper | `pkg/provider/aliyunauth/signer.go`，供 dns/aliyun + registrar/aliyun 共用 |
| 實作 `ListRecords` | 分頁（PageNumber + PageSize），最大 500 |
| 實作 `CreateRecord` | 注意 MX priority 欄位 |
| 實作 `UpdateRecord` | 用 RecordId |
| 實作 `DeleteRecord` | 用 RecordId |
| 實作 `BatchDeleteRecords` | DeleteDomainRecords（by Type）|
| `GetNameservers` | DescribeDomainInfo → DnsServers |

#### 測試

```
TestAliyunDNS_ListRecords
TestAliyunDNS_CreateRecord_MX_Priority
TestAliyunDNS_CreateRecord_TXT_Quotes
TestAliyunDNS_DeleteRecord
TestAliyunDNS_BatchDelete
TestAliyunDNS_Pagination
TestAliyunDNS_InvalidCredentials → ErrUnauthorized
```

---

### A.4 騰訊雲 DNSPod Record CRUD ✅ (完成)

**API**: `https://dnspod.tencentcloudapi.com` — 騰訊雲 API 3.0，使用 TC3-HMAC-SHA256 簽名。

#### 簽名機制（TC3）

與阿里雲不同，騰訊雲使用更複雜的 TC3 簽名：

```
StringToSign = "TC3-HMAC-SHA256\n" +
               timestamp + "\n" +
               date + "/dnspod/tc3_request\n" +
               hex(sha256(canonicalRequest))

SigningKey = HMAC-SHA256(HMAC-SHA256(HMAC-SHA256("TC3"+secretKey, date), "dnspod"), "tc3_request")
Signature  = hex(HMAC-SHA256(SigningKey, StringToSign))
```

建議：`pkg/provider/tencentauth/signer.go`（類似 aliyunauth）

#### 主要 Actions（JSON POST body）

| Action | 用途 |
|---|---|
| `DescribeRecordList` | 列出記錄 |
| `CreateRecord` | 新增記錄 |
| `ModifyRecord` | 更新記錄 |
| `DeleteRecord` | 刪除記錄 |
| `DescribeDomain` | 取得域名 NS |

#### 設計要點

```go
// 騰訊雲 API 請求 body 範例
type TencentDNSRequest struct {
    Domain     string `json:"Domain"`
    Subdomain  string `json:"Subdomain"`   // @ 代表根域名
    RecordType string `json:"RecordType"`
    RecordLine string `json:"RecordLine"`  // "默认"（必填）
    Value      string `json:"Value"`
    MX         *int   `json:"MX,omitempty"`
    TTL        *int   `json:"TTL,omitempty"`
}
// 注意：RecordLine="默认" 是必填欄位（DNSPod 的線路概念）
```

#### 特殊處理

- 騰訊 DNSPod 有「線路（RecordLine）」概念（電信、聯通、移動等），預設填 `"默认"`
- RecordId 是 uint64，注意 JSON 序列化
- 免費版限速：每秒 20 次

---

### A.5 華為雲 DNS Record CRUD ✅ (完成)

**API**: `https://dns.myhuaweicloud.com/v2` — RESTful JSON API，使用 AK/SK 簽名（HMAC-SHA256，與騰訊 TC3 類似）。

#### 主要端點

| 方法 | 路徑 | 用途 |
|---|---|---|
| GET | `/zones/{zone_id}/recordsets` | 列出記錄 |
| POST | `/zones/{zone_id}/recordsets` | 新增記錄 |
| PUT | `/zones/{zone_id}/recordsets/{id}` | 更新記錄 |
| DELETE | `/zones/{zone_id}/recordsets/{id}` | 刪除記錄 |
| GET | `/zones?name={domain}` | 取得 zone_id |

#### 特殊處理

- 華為雲 DNS 記錄是陣列 format：`"records": ["1.2.3.4", "5.6.7.8"]`（同一名稱 + 類型多個值合一筆）
- 需要拆分為本平台的單筆 Record 格式
- 建議：`pkg/provider/huaweiauth/signer.go`

---

### A.6 DNS 記錄管理 UI

**位置**: 域名詳情頁（`DomainDetail.vue`）新增「DNS 記錄」Tab + DNS 供應商詳情頁（`DNSProviderDetail.vue`）新增「管理域名記錄」入口。

#### 域名 DNS 記錄 Tab 功能

```
┌─────────────────────────────────────────────────────────┐
│  DNS 記錄  [供應商: 阿里雲 DNS / 帳號: prod-account]   │
│  [從供應商同步]  [新增記錄 ▼]                            │
├──────┬───────────┬──────────────────────┬───────┬──────┤
│ 類型 │ 名稱      │ 內容                 │  TTL  │ 操作 │
├──────┼───────────┼──────────────────────┼───────┼──────┤
│  A   │ @         │ 104.18.x.x           │  300  │ 編輯 刪除 │
│ CNAME│ www       │ example.cdn.com      │  300  │ 編輯 刪除 │
│  MX  │ @         │ mail.example.com     │ 3600  │ 編輯 刪除 │
│  TXT │ @         │ v=spf1 include:...   │  300  │ 編輯 刪除 │
└──────┴───────────┴──────────────────────┴───────┴──────┘
```

#### 新增/編輯記錄 Modal

根據 RecordType 動態顯示欄位：
- **A / AAAA**: Name + IP + TTL
- **CNAME**: Name + 目標 + TTL
- **MX**: Name + 郵件伺服器 + Priority + TTL
- **TXT**: Name + 文字內容（自動加引號）+ TTL
- **SRV**: Service + Proto + Name + Priority + Weight + Port + Target + TTL
- **CAA**: Name + Flags + Tag + Value + TTL

---

## Phase B — 域名與 DNS 整合

### B.1 域名與 DNS 供應商帳號綁定

#### 資料模型

```sql
-- migrations/000XXX_domain_dns_binding.up.sql

ALTER TABLE domains
  ADD COLUMN dns_provider_account_id BIGINT REFERENCES dns_provider_accounts(id),
  ADD COLUMN ns_delegation_status    VARCHAR(30) NOT NULL DEFAULT 'unset',
  -- unset | pending | verified | mismatch
  ADD COLUMN ns_verified_at          TIMESTAMPTZ,
  ADD COLUMN ns_last_checked_at      TIMESTAMPTZ,
  ADD COLUMN ns_actual               TEXT[];   -- 最後查到的實際 NS

COMMENT ON COLUMN domains.ns_delegation_status IS
  'unset: 未設定 DNS 供應商
   pending: 已設定，等待 NS 傳播
   verified: NS 已指向正確的 DNS 供應商
   mismatch: NS 指向錯誤供應商或未傳播';
```

#### API

```
PUT /api/v1/domains/:id/dns-binding
Body: { "dns_provider_account_id": 5 }

GET /api/v1/domains/:id/dns-binding
Response: {
  "dns_provider_account_id": 5,
  "ns_delegation_status": "pending",
  "expected_nameservers": ["ns1.alidns.com", "ns2.alidns.com"],
  "actual_nameservers": ["ns1.godaddy.com"],
  "ns_verified_at": null
}
```

#### 前端：域名詳情頁「NS 設定」區塊

```
┌─────────────────────────────────────────────────────────┐
│  Nameserver 設定                                         │
│  DNS 供應商：[阿里雲 DNS - prod ▼]  [綁定]              │
│                                                          │
│  ⚠️  NS 尚未生效（狀態：pending）                        │
│                                                          │
│  請在 GoDaddy 將 NS 記錄改為：                           │
│  ┌──────────────────────────────────┐                   │
│  │  ns1.alidns.com                  │ [複製]            │
│  │  ns2.alidns.com                  │ [複製]            │
│  └──────────────────────────────────┘                   │
│                                           [手動觸發驗證] │
└─────────────────────────────────────────────────────────┘
```

---

### B.2 NS 委派驗證

#### 設計

```go
// internal/dnsrecord/ns_checker.go

// 使用 Go 標準庫 net.LookupNS，不依賴外部 DNS 服務
// 直接查詢 root DNS（不使用系統 resolver，避免快取影響）
func CheckNSDelegation(ctx context.Context, domain string, expectedNS []string) (NSCheckResult, error)

type NSCheckResult struct {
    Status   string   // verified | mismatch | error
    Actual   []string // 實際查到的 NS
    Expected []string
    CheckedAt time.Time
}
```

#### asynq Task

```go
// internal/tasks/types.go（新增）
TypeNSCheck = "domain:ns_check"

// Payload
type NSCheckPayload struct {
    DomainID int64
}
```

**排程**：每小時對所有 `ns_delegation_status IN ('pending', 'mismatch')` 的域名執行。

#### 告警整合

`mismatch` 且超過 24 小時 → 觸發 `TypeNotifySend`，嚴重度 `warning`。

---

### B.3 域名 DNS 記錄 CRUD

#### 資料模型

```sql
-- migrations/000XXX_domain_dns_records.up.sql

CREATE TABLE domain_dns_records (
  id                     BIGSERIAL PRIMARY KEY,
  uuid                   UUID NOT NULL DEFAULT gen_random_uuid(),
  domain_id              BIGINT NOT NULL REFERENCES domains(id),
  dns_provider_account_id BIGINT REFERENCES dns_provider_accounts(id),
  provider_record_id     VARCHAR(255),   -- DNS 供應商側的記錄 ID
  record_type            VARCHAR(10)  NOT NULL,
  name                   VARCHAR(255) NOT NULL,  -- @ / www / mail ...
  content                TEXT         NOT NULL,
  ttl                    INT          NOT NULL DEFAULT 300,
  priority               INT,                     -- MX / SRV
  proxied                BOOLEAN      NOT NULL DEFAULT FALSE, -- Cloudflare
  extra                  JSONB        NOT NULL DEFAULT '{}',  -- 供應商特有欄位
  synced_at              TIMESTAMPTZ,             -- 最後從供應商同步時間
  created_at             TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
  updated_at             TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
  deleted_at             TIMESTAMPTZ,
  UNIQUE (domain_id, record_type, name, content) -- 防止重複（soft-delete 時需忽略）
);

CREATE INDEX idx_dns_records_domain ON domain_dns_records(domain_id) WHERE deleted_at IS NULL;
```

#### API

```
# 同步（從 DNS 供應商拉取記錄，覆蓋本地）
POST /api/v1/domains/:id/dns-records/sync

# CRUD
GET    /api/v1/domains/:id/dns-records
POST   /api/v1/domains/:id/dns-records
PUT    /api/v1/domains/:id/dns-records/:record_id
DELETE /api/v1/domains/:id/dns-records/:record_id

# 批次
POST /api/v1/domains/:id/dns-records/batch-delete
Body: { "record_ids": [1, 2, 3] }
```

#### 業務規則

- 新增/更新/刪除操作先呼叫 DNS Provider API，成功後再寫本地 DB
- 若 DNS Provider API 失敗，本地 DB 不變（保持一致性）
- `sync` 操作：拉取遠端記錄，以 `provider_record_id` 為 key 做 upsert，刪除本地有但遠端沒有的記錄

---

### B.4 DNS 範本套用

```
POST /api/v1/domains/:id/dns-records/apply-template
Body: {
  "dns_template_id": 3,
  "variables": { "ip": "1.2.3.4", "mail": "mail.example.com" }
}
```

- 呼叫現有 `dns_templates` 表，渲染後批次建立記錄
- 套用前可預覽（`dry_run: true`）

---

## Phase C — CDN 深度整合

### C.1 CDN Provider 介面定義

#### 核心介面

```go
// pkg/provider/cdn/provider.go

type Provider interface {
    Name() string

    // ── 域名生命週期 ──────────────────────────────────────────────
    // 在 CDN 平台建立一個加速域名
    AddDomain(ctx context.Context, req AddDomainRequest) (*CDNDomain, error)
    // 從 CDN 平台刪除一個加速域名
    RemoveDomain(ctx context.Context, domain string) error
    // 取得 CDN 域名詳情（狀態、CNAME 等）
    GetDomain(ctx context.Context, domain string) (*CDNDomain, error)
    // 列出帳號下所有 CDN 域名
    ListDomains(ctx context.Context) ([]CDNDomain, error)

    // ── 配置管理（每個類別獨立）──────────────────────────────────
    GetCacheConfig(ctx context.Context, domain string) (*CacheConfig, error)
    SetCacheConfig(ctx context.Context, domain string, cfg CacheConfig) error

    GetOriginConfig(ctx context.Context, domain string) (*OriginConfig, error)
    SetOriginConfig(ctx context.Context, domain string, cfg OriginConfig) error

    GetAccessControl(ctx context.Context, domain string) (*AccessControl, error)
    SetAccessControl(ctx context.Context, domain string, ac AccessControl) error

    GetHTTPSConfig(ctx context.Context, domain string) (*HTTPSConfig, error)
    SetHTTPSConfig(ctx context.Context, domain string, cfg HTTPSConfig) error

    GetPerformanceConfig(ctx context.Context, domain string) (*PerformanceConfig, error)
    SetPerformanceConfig(ctx context.Context, domain string, cfg PerformanceConfig) error

    // ── 內容管理 ──────────────────────────────────────────────────
    PurgeURLs(ctx context.Context, urls []string) (*PurgeTask, error)
    PurgeDirectory(ctx context.Context, dir string) (*PurgeTask, error)
    PrefetchURLs(ctx context.Context, urls []string) (*PrefetchTask, error)
    GetTaskStatus(ctx context.Context, taskID string) (*TaskStatus, error)

    // ── 統計 ──────────────────────────────────────────────────────
    GetBandwidthStats(ctx context.Context, domain string, req StatsRequest) ([]BandwidthPoint, error)
    GetTrafficStats(ctx context.Context, domain string, req StatsRequest) ([]TrafficPoint, error)
    GetHitRateStats(ctx context.Context, domain string, req StatsRequest) ([]HitRatePoint, error)
}
```

#### 共用型別定義

```go
// CDNDomain — CDN 平台上的域名記錄
type CDNDomain struct {
    Domain      string     // 加速域名
    CNAME       string     // CDN 分配的 CNAME（需在 DNS 配置）
    Status      string     // online | offline | configuring | checking
    BusinessType string    // web | download | media
    CreatedAt   *time.Time
}

// ── 回源配置 ──────────────────────────────────────────────────────────────────
type OriginConfig struct {
    Origins         []Origin
    OriginProtocol  string // http | https | follow（跟隨請求）
    OriginHost      string // 回源 Host（空表示使用加速域名）
    OriginHeaders   []HTTPHeader
    Follow302       bool   // 回源時是否跟隨 302
    OriginTimeout   int    // 秒
}

type Origin struct {
    Address  string  // IP 或域名
    Port     int
    Weight   int     // 負載均衡權重（1-100）
    Type     string  // primary | backup
}

// ── 緩存配置 ──────────────────────────────────────────────────────────────────
type CacheConfig struct {
    Rules        []CacheRule
    IgnoreQuery  bool          // 忽略 URL 參數緩存
    IgnoreCase   bool          // 忽略大小寫
    StatusCode   []StatusCodeCache
}

type CacheRule struct {
    RuleType  string  // all | suffix | directory | url | regex
    Pattern   string  // *.jpg | /static/ | /api/* | ...
    TTL       int     // 秒；0 = 不緩存；-1 = 跟隨源站
    Priority  int
}

type StatusCodeCache struct {
    Code int
    TTL  int
}

// ── 訪問控制 ──────────────────────────────────────────────────────────────────
type AccessControl struct {
    Referer     *RefererControl
    IP          *IPControl
    URLAuth     *URLAuth
    GeoBlock    *GeoBlock
    RateLimit   *RateLimit
    UserAgent   *UserAgentControl
}

type RefererControl struct {
    Type        string   // whitelist | blacklist
    Domains     []string
    AllowEmpty  bool     // 是否允許空 Referer
}

type IPControl struct {
    Type string   // whitelist | blacklist
    IPs  []string // 支援 CIDR（e.g. 192.168.0.0/24）
}

type URLAuth struct {
    Enabled   bool
    Type      string // TypeA | TypeB | TypeC（各供應商命名不同，統一）
    Key       string
    ExpireSeconds int
}

type GeoBlock struct {
    Type      string   // whitelist | blacklist
    Regions   []string // ISO 3166-1 alpha-2 國家碼 + 中國省份代碼
}

type RateLimit struct {
    Enabled    bool
    Threshold  int    // 每秒請求數
    BurstSize  int
}

// ── HTTPS 配置 ────────────────────────────────────────────────────────────────
type HTTPSConfig struct {
    Enabled         bool
    CertID          string // 憑證 ID（供應商側管理）
    ForceHTTPS      bool
    HTTP2           bool
    HTTP3           bool   // QUIC
    HSTS            *HSTSConfig
    OCSPStapling    bool
    TLSVersions     []string // TLSv1.2 | TLSv1.3
}

type HSTSConfig struct {
    Enabled           bool
    MaxAge            int  // 秒
    IncludeSubdomains bool
    Preload           bool
}

// ── 性能優化 ──────────────────────────────────────────────────────────────────
type PerformanceConfig struct {
    Gzip            bool
    Brotli          bool
    FilterParams    []string   // 過濾的 URL 參數（不影響緩存 key）
    PageOptimize    bool       // HTML/CSS/JS 壓縮合併（部分供應商支援）
    RangeOrigin     bool       // Range 回源（斷點續傳）
    VideoSeek       bool       // 拖拽播放（flv/mp4 時間軸）
}

// ── 統計 ──────────────────────────────────────────────────────────────────────
type StatsRequest struct {
    StartTime time.Time
    EndTime   time.Time
    Interval  string // 5min | 1hour | 1day
}
type BandwidthPoint struct { Time time.Time; Bps int64 }
type TrafficPoint   struct { Time time.Time; Bytes int64 }
type HitRatePoint   struct { Time time.Time; Rate float64 }
```

---

### C.2 資料模型

```sql
-- migrations/000XXX_cdn_domain_management.up.sql

-- CDN 域名綁定（域名 ↔ CDN 供應商帳號）
CREATE TABLE domain_cdn_bindings (
  id                BIGSERIAL PRIMARY KEY,
  uuid              UUID NOT NULL DEFAULT gen_random_uuid(),
  domain_id         BIGINT NOT NULL REFERENCES domains(id),
  cdn_account_id    BIGINT NOT NULL REFERENCES cdn_accounts(id),
  cdn_cname         VARCHAR(500),       -- CDN 分配的 CNAME
  business_type     VARCHAR(30) NOT NULL DEFAULT 'web', -- web | download | media
  status            VARCHAR(30) NOT NULL DEFAULT 'offline',
  created_at        TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at        TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  deleted_at        TIMESTAMPTZ,
  UNIQUE (domain_id, cdn_account_id)
);

-- CDN 配置快照（每個類別一筆，JSONB 存原始配置）
CREATE TABLE cdn_domain_configs (
  id               BIGSERIAL PRIMARY KEY,
  binding_id       BIGINT NOT NULL REFERENCES domain_cdn_bindings(id),
  config_type      VARCHAR(30) NOT NULL,
  -- cache | origin | access_control | https | performance
  config           JSONB NOT NULL DEFAULT '{}',
  synced_at        TIMESTAMPTZ,  -- 最後從 CDN 供應商同步的時間
  updated_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  UNIQUE (binding_id, config_type)
);

-- CDN 內容操作任務（purge / prefetch）
CREATE TABLE cdn_content_tasks (
  id               BIGSERIAL PRIMARY KEY,
  uuid             UUID NOT NULL DEFAULT gen_random_uuid(),
  binding_id       BIGINT NOT NULL REFERENCES domain_cdn_bindings(id),
  task_type        VARCHAR(20) NOT NULL,  -- purge_url | purge_dir | prefetch
  provider_task_id VARCHAR(255),          -- 供應商回傳的 task ID
  status           VARCHAR(20) NOT NULL DEFAULT 'pending',
  -- pending | processing | done | failed
  targets          TEXT[],                -- URLs or directories
  created_by       BIGINT REFERENCES users(id),
  created_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  completed_at     TIMESTAMPTZ
);

-- CDN 統計緩存（TimescaleDB）
CREATE TABLE cdn_stats (
  time           TIMESTAMPTZ NOT NULL,
  binding_id     BIGINT NOT NULL,
  stat_type      VARCHAR(20) NOT NULL,   -- bandwidth | traffic | hit_rate
  value          DOUBLE PRECISION NOT NULL
);
SELECT create_hypertable('cdn_stats', 'time');
```

---

### C.3 各供應商 CDN 實作

#### 阿里雲 CDN

**API**: `https://cdn.aliyuncs.com` — 同 HMAC-SHA1 簽名

主要 Actions：

| Action | 對應 Provider 方法 |
|---|---|
| `AddCdnDomain` | AddDomain |
| `DeleteCdnDomain` | RemoveDomain |
| `DescribeCdnDomainDetail` | GetDomain |
| `DescribeUserDomains` | ListDomains |
| `SetCacheExpiredConfig` | SetCacheConfig（緩存規則） |
| `SetFileCacheExpiredConfig` | SetCacheConfig（文件後綴緩存） |
| `SetDirCacheExpiredConfig` | SetCacheConfig（目錄緩存） |
| `SetSourceHostConfig` | SetOriginConfig（回源 Host）|
| `SetRefererConfig` | SetAccessControl（Referer）|
| `SetIpBlackListConfig` | SetAccessControl（IP 黑名單）|
| `SetCCConfig` | SetAccessControl（CC 防護） |
| `SetReqAuthConfig` | SetAccessControl（URL 鑑權）|
| `SetForceRedirectConfig` | SetHTTPSConfig（強制 HTTPS）|
| `SetDomainServerCertificate` | SetHTTPSConfig（憑證）|
| `SetHttpHeaderConfig` | 回源 Header |
| `RefreshObjectCaches` | PurgeURLs / PurgeDirectory |
| `PushObjectCache` | PrefetchURLs |
| `DescribeRefreshTaskById` | GetTaskStatus |
| `DescribeDomainBpsData` | GetBandwidthStats |
| `DescribeDomainTrafficData` | GetTrafficStats |
| `DescribeDomainHitRateData` | GetHitRateStats |

#### 騰訊雲 CDN

**API**: `https://cdn.tencentcloudapi.com` — TC3-HMAC-SHA256 簽名

主要 Actions（JSON POST）：

| Action | 對應 Provider 方法 |
|---|---|
| `AddCdnDomain` | AddDomain |
| `StopCdnDomain` + `DeleteCdnDomain` | RemoveDomain |
| `DescribeDomainsConfig` | GetDomain / GetCacheConfig / GetOriginConfig / ... |
| `UpdateDomainConfig` | SetCacheConfig / SetOriginConfig / SetAccessControl / ... |
| `PurgeUrlsCache` | PurgeURLs |
| `PurgePathCache` | PurgeDirectory |
| `PushUrlsCache` | PrefetchURLs |
| `DescribePurgeTasks` | GetTaskStatus |
| `DescribeBillingData` | GetBandwidthStats / GetTrafficStats |

**注意**：騰訊 `UpdateDomainConfig` 是全量更新，需先 GetDomain 取得現有配置再合併。

#### 華為雲 CDN

**API**: `https://cdn.myhuaweicloud.com/v1.0` — AK/SK 簽名

主要端點：

| 方法 | 路徑 | 對應方法 |
|---|---|---|
| POST | `/cdn/domains` | AddDomain |
| DELETE | `/cdn/domains/{id}` | RemoveDomain |
| GET | `/cdn/domains/{id}/detail` | GetDomain |
| PUT | `/cdn/domains/{id}/cache` | SetCacheConfig |
| PUT | `/cdn/domains/{id}/origin` | SetOriginConfig |
| PUT | `/cdn/domains/{id}/referer` | SetAccessControl（Referer）|
| PUT | `/cdn/domains/{id}/ip-acl` | SetAccessControl（IP）|
| PUT | `/cdn/domains/{id}/https` | SetHTTPSConfig |
| POST | `/cdn/content/refresh-tasks` | PurgeURLs / PurgeDirectory |
| POST | `/cdn/content/preheating-tasks` | PrefetchURLs |

#### Cloudflare CDN

**API**: Cloudflare API v4 — Bearer Token

Cloudflare 的 CDN 與 DNS 整合在一起（orange-cloud），主要操作：

| 操作 | API |
|---|---|
| 啟用 CDN proxy | DNS 記錄 `proxied=true` |
| 緩存規則 | `POST /zones/{id}/cache/rules` |
| 頁面規則 | `POST /zones/{id}/pagerules` |
| Purge | `POST /zones/{id}/purge_cache` |
| HTTPS 設定 | `PATCH /zones/{id}/settings` |
| Rate Limiting | `POST /zones/{id}/rate_limits` |

---

### C.4 API 設計

```
# CDN 域名生命週期
POST   /api/v1/domains/:id/cdn-binding          # 新增 CDN 綁定
DELETE /api/v1/domains/:id/cdn-binding          # 移除 CDN 綁定
GET    /api/v1/domains/:id/cdn-binding          # 取得綁定狀態
POST   /api/v1/domains/:id/cdn-binding/sync     # 從 CDN 同步狀態

# CDN 配置（每個類別獨立端點，方便細粒度更新）
GET  /api/v1/cdn-domains/:binding_id/config/cache
PUT  /api/v1/cdn-domains/:binding_id/config/cache

GET  /api/v1/cdn-domains/:binding_id/config/origin
PUT  /api/v1/cdn-domains/:binding_id/config/origin

GET  /api/v1/cdn-domains/:binding_id/config/access-control
PUT  /api/v1/cdn-domains/:binding_id/config/access-control

GET  /api/v1/cdn-domains/:binding_id/config/https
PUT  /api/v1/cdn-domains/:binding_id/config/https

GET  /api/v1/cdn-domains/:binding_id/config/performance
PUT  /api/v1/cdn-domains/:binding_id/config/performance

# 內容管理
POST /api/v1/cdn-domains/:binding_id/purge
     Body: { "type": "url", "targets": ["https://..."] }
     Body: { "type": "directory", "targets": ["/static/"] }

POST /api/v1/cdn-domains/:binding_id/prefetch
     Body: { "urls": ["https://..."] }

GET  /api/v1/cdn-domains/:binding_id/tasks/:task_id

# 統計
GET  /api/v1/cdn-domains/:binding_id/stats/bandwidth?start=...&end=...&interval=5min
GET  /api/v1/cdn-domains/:binding_id/stats/traffic?...
GET  /api/v1/cdn-domains/:binding_id/stats/hit-rate?...
```

---

### C.5 CDN 配置頁面 UI

**路由**: `/cdn-domains/:binding_id`（新頁面 `CDNDomainDetail.vue`）

**Tab 結構**：

```
[概覽] [回源配置] [緩存規則] [訪問控制] [HTTPS] [性能優化] [內容管理] [統計]
```

#### 緩存規則 Tab

```
┌─────────────────────────────────────────────────────────────┐
│ 緩存規則                                          [新增規則] │
├────┬──────────┬────────────────────────┬──────┬────────────┤
│ 優先│ 規則類型  │ 模式                   │ TTL  │ 操作       │
├────┼──────────┼────────────────────────┼──────┼────────────┤
│  1  │ 文件後綴  │ .jpg .png .gif .webp  │ 30天 │ 編輯 刪除  │
│  2  │ 文件後綴  │ .css .js              │ 7天  │ 編輯 刪除  │
│  3  │ 目錄     │ /api/                 │ 不緩存│ 編輯 刪除  │
│  4  │ 全部     │ *                     │ 1天  │ 編輯 刪除  │
└────┴──────────┴────────────────────────┴──────┴────────────┘

□ 忽略 URL 參數緩存    □ 忽略大小寫
狀態碼緩存：404 = 5秒  [編輯]
```

#### 訪問控制 Tab

```
┌─── Referer 防盜鏈 ──────────────────────────────────────────┐
│  ● 白名單  ○ 黑名單  ○ 停用                                  │
│  域名列表：example.com ×  *.partner.com ×  [新增]           │
│  □ 允許空 Referer（直接訪問）                                │
└─────────────────────────────────────────────────────────────┘

┌─── IP 訪問控制 ─────────────────────────────────────────────┐
│  ● 黑名單  ○ 白名單  ○ 停用                                  │
│  IP 列表：1.2.3.4 ×  10.0.0.0/8 ×  [新增]                  │
└─────────────────────────────────────────────────────────────┘

┌─── URL 鑑權 ────────────────────────────────────────────────┐
│  ☑ 啟用 URL 鑑權  鑑權類型：[Type A ▼]                      │
│  主密鑰：[********************]  [顯示]  [重新產生]          │
│  有效期：3600 秒                                             │
└─────────────────────────────────────────────────────────────┘

┌─── 地區限制 ────────────────────────────────────────────────┐
│  ○ 白名單  ○ 黑名單  ● 停用                                  │
└─────────────────────────────────────────────────────────────┘
```

#### 內容管理 Tab

```
┌─── URL 刷新 ────────────────────────────────────────────────┐
│  每行一個 URL：                                               │
│  ┌──────────────────────────────────────────────────────┐   │
│  │ https://example.com/static/app.js                    │   │
│  │ https://example.com/images/banner.png                │   │
│  └──────────────────────────────────────────────────────┘   │
│  [提交刷新]                                                  │
└─────────────────────────────────────────────────────────────┘

┌─── 最近操作 ───────────────────────────────────────────────┐
│  2026-04-26 14:30  URL刷新  3個URL  ✅ 完成               │
│  2026-04-26 09:00  目錄刷新  /static/  🔄 處理中           │
└─────────────────────────────────────────────────────────────┘
```

---

## Phase D — 端對端驗證與監控

### D.1 NS 解析 Probe

- **觸發**：每小時 asynq 定時任務 + 域名 NS 綁定變更後立即觸發
- **實作**：`net.Resolver` with custom DNS（`8.8.8.8:53` + `1.1.1.1:53` 雙查詢取交集）
- **儲存**：`probe_results`（TimescaleDB，現有表）

### D.2 DNS 解析 Probe

- 對域名做 A / CNAME 查詢，確認解析到的 IP 與 CDN anycast 範圍一致
- CDN CNAME 解析驗證：`domain.com` → 解析到 CDN 分配的 CNAME → 再解析到 IP

### D.3 CDN 健康 Probe

- HTTP HEAD 請求到域名，檢查回應的 CDN 特徵 Header（`cf-ray` / `x-cache` / `x-cdn`）
- 確認 CDN 正在代理（而非直連 origin）

### D.4 全鏈路狀態 Dashboard

**域名詳情頁「鏈路狀態」面板**（新增 Tab）：

```
┌─── 域名鏈路狀態：example.com ──────────────────────────────┐
│                                                              │
│  [GoDaddy]  ─────────────────────  ✅ 有效                  │
│    域名到期：2027-01-15                                       │
│                                                              │
│  [NS 委派]  ─────────────────────  ✅ 已驗證（2小時前）      │
│    ns1.alidns.com / ns2.alidns.com                          │
│                                                              │
│  [DNS 解析]  ────────────────────  ✅ 正常                   │
│    example.com → xxx.cdn.com（CNAME）                        │
│                                                              │
│  [CDN]  ─────────────────────────  ✅ 在線                   │
│    阿里雲 CDN，命中率 94.2%                                   │
│                                                              │
│  [Origin]  ──────────────────────  ✅ 正常（HTTP 200）       │
│    回源延遲 45ms                                             │
│                                                   [重新檢查] │
└─────────────────────────────────────────────────────────────┘
```

---

## 8. 資料模型總表

| 表名 | Phase | 說明 |
|---|---|---|
| `domain_dns_records` | B.3 | 域名 DNS 記錄本地快照 |
| `domains.dns_provider_account_id` | B.1 | 域名 → DNS 供應商帳號外鍵 |
| `domains.ns_delegation_status` | B.1/B.2 | NS 委派驗證狀態 |
| `domain_cdn_bindings` | C.2 | 域名 → CDN 供應商帳號綁定 |
| `cdn_domain_configs` | C.3-6 | CDN 配置快照（JSONB） |
| `cdn_content_tasks` | C.7 | Purge / Prefetch 任務記錄 |
| `cdn_stats` | C.8 | CDN 統計（TimescaleDB） |

---

## 9. API 端點總表

| 類別 | 端點數 | Phase |
|---|---|---|
| DNS 記錄 CRUD | 6 | B.3 |
| DNS 綁定 + NS 驗證 | 3 | B.1/B.2 |
| CDN 綁定 | 4 | C.2 |
| CDN 配置（5 類別 × 2） | 10 | C.3-6 |
| CDN 內容管理 | 3 | C.7 |
| CDN 統計 | 3 | C.8 |
| **合計** | **29** | |

---

## 10. 測試策略

### 單元測試

| 層級 | 策略 |
|---|---|
| `pkg/provider/dns/*` | `httptest.NewServer` mock，每個 provider 覆蓋所有記錄類型 |
| `pkg/provider/cdn/*` | 同上，每個配置類別至少一個正向 + 一個錯誤案例 |
| `internal/dnsrecord` | mock Provider 介面，測試業務規則（同步邏輯、錯誤處理）|
| `internal/cdnconfig` | mock Provider 介面，測試配置合併邏輯 |

### 整合測試

| 測試 | 說明 |
|---|---|
| NS Check 端對端 | 對實際域名查詢 NS，驗證狀態機轉換 |
| DNS Record Sync | mock provider 回傳一批記錄，驗證 upsert 邏輯正確 |
| CDN Config 序列化 | 驗證 Go struct → JSONB → Go struct 的往返序列化不丟失欄位 |

### 供應商 Mock 原則

- 所有 provider 測試使用 `httptest.NewServer`，**不發真實 API 請求**
- 每個 provider 建立 `fixtures/` 目錄存放真實 API 回應的 JSON/XML 快照
- CI 全程不需要真實憑證

---

## 11. 風險與決策記錄

| 編號 | 決策 | 理由 |
|---|---|---|
| D-01 | CDN 配置以 JSONB 快照存儲 | 各供應商配置結構差異大，typed struct 存 Go 側，DB 存原始格式以供 audit |
| D-02 | 騰訊 CDN `UpdateDomainConfig` 採「讀-改-寫」 | 騰訊 API 為全量更新，需先拉取現有配置再合併，避免覆蓋其他欄位 |
| D-03 | Cloudflare CDN = DNS proxied 模式 | Cloudflare 不分 CDN 與 DNS，啟用加速 = DNS 記錄設 `proxied=true` |
| D-04 | aliyunauth / tencentauth 抽為獨立 package | DNS + CDN + Registrar 三個 package 共用簽名邏輯，避免複製 |
| D-05 | CDN 統計寫入 TimescaleDB | 高頻時間序列資料，與現有 probe_results 共用 hypertable 策略 |
| D-06 | NS Check 使用 `8.8.8.8` 而非系統 resolver | 避免服務器 DNS 快取影響驗證結果 |
