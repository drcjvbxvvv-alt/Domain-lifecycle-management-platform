# PHASE_E_TASKLIST.md — CDN Account Management & Domain Asset Enrichment (PE.1 ✅ PE.2 ✅)

> **Created 2026-04-26.** This document is the authoritative work order for
> Phase E (CDN Account Management & Domain Asset Enrichment).
>
> **Context**: Analysis of existing domain management system screenshots
> revealed missing CDN account management, origin IP, and domain purpose fields.
> Phase E fills those gaps at the same tier as Registrar and DNS Provider.
>
> **Pre-requisite**: Phase A PA.1–PA.2 complete (domain asset management, tags).
>
> **Audience**: Claude Code sessions. All tasks are standard-complexity CRUD;
> Sonnet is appropriate for all PE tasks.

---

## Phase E — Definition of Scope

Phase E adds **CDN account management** and **domain asset enrichment**:
unified cloud-account management for CDN/acceleration vendors (parallel to
Registrar / DNS Provider), plus extra metadata fields on domains that are
visible in daily operations but absent from the initial schema.

### What "Phase E done" looks like (acceptance demo)

```
1. Operator opens 資產管理 → CDN 供應商管理
2. Sees 8 pre-seeded providers: Cloudflare, 聚合, 網宿, 白山雲, 騰訊雲CDN,
   華為雲CDN, 阿里雲CDN, Fastly  (+ can add custom "其他")
3. Clicks into Cloudflare provider → creates account "主播 Cloudflare"
   with credentials { "api_key": "...", "zone_id": "..." }
4. Opens domain detail → assigns cdn_account_id = that account
5. Domain list now shows CDN column: "Cloudflare / 主播 Cloudflare"
6. Operator can see origin IP and domain purpose in domain detail
```

---

## Task Cards

---

### PE.1 — CDN Provider & Account CRUD ✅ (完成 2026-04-26)

**Goal**: `cdn_providers` + `cdn_accounts` tables with full API + Vue frontend,
parallel to the Registrar/DNS Provider pattern.

**Deliverables**:

| Layer | File | Status |
|-------|------|--------|
| Migration | `migrations/000001_init.up.sql` (cdn_providers, cdn_accounts tables + seed) | ✅ |
| Migration | `migrations/000001_init.down.sql` (DROP cdn_accounts, cdn_providers) | ✅ |
| Store | `store/postgres/cdn.go` (CDNStore + 12 methods + sentinel errors) | ✅ |
| Service | `internal/cdn/service.go` (Service + allowedProviderTypes + validation) | ✅ |
| Tests | `internal/cdn/service_test.go` (14 unit tests, nil-store pattern) | ✅ |
| Handler | `api/handler/cdn.go` (CDNHandler + 11 handlers) | ✅ |
| Router | `api/router/router.go` (/cdn-providers + /cdn-accounts route groups) | ✅ |
| Wire | `cmd/server/main.go` (cdnStore + cdnSvc + cdnHandler wiring) | ✅ |
| API Types | `web/src/api/cdn.ts` (CDN_PROVIDER_TYPES + cdnApi + TS interfaces) | ✅ |
| Store | `web/src/stores/cdn.ts` (useCDNStore Pinia store) | ✅ |
| View | `web/src/views/cdn-providers/CDNProviderList.vue` | ✅ |
| View | `web/src/views/cdn-providers/CDNProviderDetail.vue` | ✅ |
| Router | `web/src/router/index.ts` (/cdn-providers, /cdn-providers/:id routes) | ✅ |
| Nav | `web/src/views/layouts/MainLayout.vue` (CDN 供應商管理 nav item) | ✅ |

**Key design decisions**:
- `allowedProviderTypes` map in service layer (not DB enum) for easy extension
- `provider_type` + `name` unique together, not globally unique — allows "Cloudflare Prod" and "Cloudflare Dev"
- `credentials` stored as JSONB `'{}'::jsonb` default; service defaults nil → `{}`
- `parseParamID(c, param)` used (not `parseID`) to avoid collision with notification.go's `parseID(c)`
- Unit tests use nil-store pattern: only validation-error paths are tested; store calls would panic (not reached)

**Known gaps / deferred**:
- `credentials` stored as JSONB plaintext. PE.1 defers encryption; see MASTER_ROADMAP §15.2 for AES-256-GCM recommendation
- No role-based write protection on credentials (PE.2 or security pass)

---

### PE.2 — Domain Asset Field Enrichment ✅ (完成 2026-04-26)

**Goal**: Add `cdn_account_id`, `origin_ips`, and `domain_purpose` to the
`domains` table. Wire up in domain detail UI.

**Scope**:
- New migration: `ALTER TABLE domains ADD COLUMN cdn_account_id BIGINT REFERENCES cdn_accounts(id)`
- Add `origin_ips TEXT[]` and `domain_purpose VARCHAR(32)` columns
- Update `DomainDetail.vue` — show CDN account (read) + allow assignment
- Update `DomainList.vue` — add CDN column (vendor name + account name)
- Add `cdnApi.listAllAccounts()` to domain store for account picker

**Acceptance criteria**:
- Domain detail shows assigned CDN account (name + provider type)
- Domain detail allows changing CDN account from dropdown
- Domain list shows CDN vendor name in new column (empty = `-`)
- origin_ips stored as array, displayed as comma-separated in UI

**Estimated steps**: 5
1. Migration: add columns to domains
2. Store: update GetDomainByID, ListDomains queries
3. Service: update UpdateDomain input to include cdn_account_id + origin_ips + purpose
4. Handler: update DomainResponse DTO + update endpoint
5. Frontend: DomainDetail CDN assignment + DomainList CDN column

---

### PE.3 — Domain List UI Strengthening 🔲 (未開始)

**Goal**: Richer domain list — CDN column, Registrar column, purpose badge, origin IP tooltip.

**Scope**:
- `DomainList.vue`: add columns for CDN (from cdn_accounts join), Registrar, purpose
- Server-side: join `domains → cdn_accounts → cdn_providers` in list query
- Add purpose filter + CDN filter to list API query params
- Export CSV with new columns

**Dependencies**: PE.2 must complete first (schema + store changes).

**Acceptance criteria**:
- CDN column shows provider type tag + account name
- Registrar column shows registrar name
- Purpose column shows badge (e.g., 直播、備用、測試)
- Filter by CDN provider works
- CSV export includes all new columns

**Estimated steps**: 4
1. Store: update ListDomains to join cdn_accounts + cdn_providers + registrars
2. Handler: extend DomainListItem DTO
3. Frontend: DomainList.vue new columns + filters
4. Export: update CSV generation

---

## Phase E Progress

| Task | Description | Status | Date |
|------|-------------|--------|------|
| PE.1 | CDN 帳號管理 | ✅ 完成 | 2026-04-26 |
| PE.2 | 域名資產欄位補全 | ✅ 完成 | 2026-04-26 |
| PE.3 | 域名列表 UI 強化 | 🔲 未開始 | — |

**Phase E 整體進度**: 2 / 3 完成（66.7%）
