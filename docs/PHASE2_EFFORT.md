# PHASE2_EFFORT.md — Phase 2 開發週期粗估

> ## ⚠ 這是**粗估，不是承諾**
>
> 這份文件是一份 **planning tool**，用法與 `PHASE1_EFFORT.md` 相同：
>
> 1. **自我校準警報系統** — 實際花的時間顯著超出區間上限時停下來問「為什麼？」。
> 2. **順序指引** — 依安全牌 → 中風險 → 高風險的順序建立動能。
> 3. **注意 Claude Code 係數** — Phase 1 的實際數據顯示「Claude Code + 1 人」的 throughput 比傳統 solo 高出 10–20×。估計表的 Lo/Hi 已基於此係數校準，不是傳統人工開發的估法。

---

## ⚡ Phase 1 校準結果（2026-04-12）

Phase 1 的 12 張任務卡**全部在同一天完成**（2026-04-12），包含 Opus bottleneck 任務（P1.5 Lifecycle 狀態機、P1.7 Artifact build、P1.8 Release 狀態機、P1.9/P1.10 Agent binary + 控制面）。

**實際校準係數**：原估 15–27 天 → 實際 ~1 天。**係數 ≈ 0.05–0.07**。

Phase 1 以後的所有估計都基於這個係數：**「一張複雜任務 ≈ 半個 Claude Code session（1–4 小時）」**，Lo/Hi 分別代表順順跑完 vs 碰到整合摩擦或設計決策需要來回討論。

---

## 假設

| 項目 | 假設 |
|---|---|
| 人力 | **1 人（ahern）+ Claude Code**（Sonnet 主力，Opus 用於 Critical Rule bottleneck） |
| 工作單位 | **Session**（而非天），一個 session ≈ 1–4 小時；複雜任務可能跨 2 sessions |
| 依賴 | Phase 1 已全部完成（12 張任務卡，2026-04-12） |
| 範圍 | **Phase 2 scope 嚴格遵守 `docs/PHASE2_TASKLIST.md`**，不擴張到 Phase 3（probe / canary policy / alert engine） |
| Lo / Hi 單位 | **工作天**（≈ 半天 / 全天 session），不是傳統 man-days |

---

## 每任務估計

| # | 任務 | Owner | Lo | Hi | 風險 | 主要炸點 / 實際 |
|---|---|---|---|---|---|---|
| P2.1 | **Release dispatch pipeline：real task execution** | **Opus** | 0.5 | 1.5 | 🟢 ✅ | **Actual: ~0.3d (2026-04-12)** — domain_tasks + agent_tasks CRUD + plan/dispatch/finalize 升級、agent PullNextTask + ReportTask 接真實 DB、整個 release:plan→dispatch→finalize 鏈跑通 |
| P2.2 | **Multi-shard release splitting** | Sonnet | 0.3 | 1.0 | 🟢 ✅ | **Actual: ~0.2d (2026-04-12)** — PlanShards() (by_host_group)、GetAgentTaskStatsByShard、GetNextPendingShard、Plan/MarkReady/DispatchShard/Finalize 更新為 sequential per-shard chain |
| P2.3 | **Rollback execution** | **Opus** | 0.5 | 1.5 | 🟢 ✅ | **Actual: ~0.3d (2026-04-12)** — rollback.go + RollbackStore + GetLastSucceeded + ExecuteRollback + HandleRollback + agent handleRollback/restoreFromSnapshot + API endpoint + ConfirmModal |
| P2.4 | **Dry-run / Diff preview** | Sonnet | 0.3 | 1.0 | 🟢 ✅ | **Actual: ~0.2d (2026-04-12)** — dryrun.go + Storage interface (ListObjects/GetObjectContent) + MinIO impl + API endpoint + TypeScript types + frontend Dry Run tab |
| P2.5 | **Per-host concurrency control** | Sonnet | 0.3 | 1.0 | 🟡 | Redis-based per-agent 並發 slot；CLAUDE.md Critical Rule #7 (nginx reload batching 30s/50 domains)；主要炸點：batch flush timing 與 dispatch 路徑整合 |
| P2.6 | **Agent fleet management UI + Release 操作 UI** | Sonnet | 0.5 | 1.5 | 🟡 | 最大的 UI 任務：agent drain/enable/disable 操作面板、fleet 狀態頁、release detail 操作按鈕已大部分在 P2.3/P2.4 完成，剩餘以 fleet page 為主 |
| P2.7 | **DNS provider：Cloudflare 實作** | Sonnet | 0.3 | 1.0 | 🟡 | cloudflare-go client + Provider interface impl + registry + lifecycle module hook；炸點：Cloudflare API rate limiting + zone/record 型別對齊 |
| P2.8 | **E2E integration test + documentation** | Sonnet | 0.5 | 1.5 | 🟡 | docker-compose full-stack test (server + worker + agent binary + mock Nginx) + PHASE2_TASKLIST 所有 Acceptance 條件驗收；炸點：agent binary 跑在 container 內的 mTLS 路徑 |

**每任務加總**：**Lo = 3.2 天 / Hi = 10 天**

---

## 加上 integration 摩擦

| 項目 | 加多少 |
|---|---|
| P2.5 → P2.6 dispatch 路徑整合（並發 limit 與 DispatchShard 互動）| +0.2 ~ 0.5 天 |
| Cloudflare API sandbox / test token 設置 | +0.2 ~ 0.5 天 |
| E2E agent binary 在 container 內的 mTLS 初次設置 | +0.2 ~ 0.8 天 |
| Go ↔ TypeScript DTO 對齊（P2.6 新 API 欄位）| +0.1 ~ 0.3 天 |

**摩擦加總**：**Lo = +0.7 天 / Hi = +2.1 天**

---

## Phase 2 總計

|   | 工作天 | Session 數（估）| 日曆估算 |
|---|---|---|---|
| **樂觀**（everything clicks）| **~4 天** | ~6–8 sessions | **~1 週** |
| **中位**（一般情況）| **~6 天** | ~10–12 sessions | **~1.5 週** |
| **悲觀**（P2.5/P2.8 炸點都中）| **~12 天** | ~18–22 sessions | **~2.5 週** |

### 最可能落點

**1 – 1.5 週日曆時間**（中位情境，P2.1–P2.4 已完成）

---

## 已完成進度（截至 2026-04-12）

| 任務 | 完成日 | 耗時 |
|---|---|---|
| P2.1 Release dispatch pipeline | 2026-04-12 | ~0.3 天 |
| P2.2 Multi-shard release splitting | 2026-04-12 | ~0.2 天 |
| P2.3 Rollback execution | 2026-04-12 | ~0.3 天 |
| P2.4 Dry-run / Diff preview | 2026-04-12 | ~0.2 天 |

**已消耗**：~1 天。**剩餘**：P2.5 → P2.8。

---

## 剩餘任務依賴圖

```
P2.5 (Per-host concurrency) — 依賴 P2.2 ✅
  └─▶ P2.6 (Fleet mgmt UI)  — 依賴 P2.5, P2.3 ✅, P2.4 ✅
        └─▶ P2.8 (E2E tests)

P2.7 (DNS provider)         — 獨立，隨時可做
```

### 建議順序

```
Session 1 : P2.5 Per-host concurrency (P2.5 本身就是 P2.6 的先決條件)
Session 2 : P2.7 Cloudflare DNS (獨立任務，插縫做)
Session 3 : P2.6 Agent fleet mgmt UI
Session 4 : P2.8 E2E integration tests + doc cleanup
```

---

## 風險集中的地方

### 1. P2.5 Redis-based 並發 slot 與 DispatchShard 整合 🟡

**炸點**：CLAUDE.md Critical Rule #7 要求「同主機多個 conf 變更 → 緩衝 30s 或 50 domains，再發單次 `nginx -s reload`」。這個 batching 邏輯需要在 worker 端實作，而不是 agent 端。

**緩解策略**：
- 先用 Redis INCR + TTL 做 per-agent slot 計數（最簡單的正確實作）
- Batch flush 用 scheduled asynq task（`asynq.ProcessIn(30s)`）而不是 goroutine sleep
- Emergency rollback 必須 bypass buffer — 在 `TypeReleaseRollback` handler 加 `flush_immediately` flag

### 2. P2.8 agent binary 在 container 的 mTLS 路徑 🟡

**炸點**：E2E 測試需要 agent binary 跑在 container 裡，連到 server 時需要有效的 mTLS 憑證。第一次設置容易在憑證路徑 / CN 匹配 / SAN 上卡住。

**緩解策略**：
- 用 `deploy/docker-compose.yml` 的 dev 模式先讓 server + worker + Redis + MinIO 跑起來
- Agent 用 `--mtls-skip-verify` dev flag（已在 P1.9 設計為 flag）做第一次 e2e 打通
- 第二輪再補上真實 mTLS 憑證路徑的測試

### 3. P2.7 Cloudflare API 憑證管理 🟡

**炸點**：Cloudflare provider 需要 API token，但 test 環境通常沒有真實 zone。

**緩解策略**：
- 實作 `dns.MockProvider`（in-memory record store）供測試使用
- Cloudflare provider 用 `CLOUDFLARE_API_TOKEN` env var，dev 環境設 `dns_provider: mock`
- 真實 Cloudflare 路徑留 integration test（加 `//go:build integration` build tag，不跑在 CI）

---

## 範圍警告（Scope Creep 預警）

| 誘惑 | 真相 |
|---|---|
| 「P2.5 已經有 per-agent limit，順便加 global queue limit 吧」| Global queue 是 Phase 3 canary policy 的一部分。+0.5–1 天。 |
| 「P2.6 Agent fleet UI 順便加 upgrade dispatch 按鈕」| Agent canary upgrade 是 Phase 3。+0.5 天。 |
| 「P2.7 DNS 搞完順便加 CDN provider abstraction」| CDN 在 ADR-0003 明確排除。不做。 |
| 「P2.8 E2E 順便加 probe L1 smoke test」| Probe 是 Phase 3 整張任務。+1–2 天。 |
| 「P2.4 的 diff 要加 side-by-side 視覺效果」| Phase 3+ 的 rich diff viewer。目前 unified diff `<pre>` 已足夠。 |

---

## 參考

- `docs/PHASE2_TASKLIST.md` — 8 張任務卡的完整定義（owner model / scope in/out / 依賴 / 驗收條件）
- `docs/PHASE1_EFFORT.md` — Phase 1 實際數據與 Claude Code 校準係數
- `docs/CLAUDE_CODE_INSTRUCTIONS.md` §"Model Selection Policy" — Opus vs Sonnet 的選用標準
- `docs/ARCHITECTURE.md` §8 — Phase 1–4 headline scope

---

## 更新記錄

| 日期 | 更新者 | 內容 |
|---|---|---|
| 2026-04-12 | Claude Sonnet 4.6 + ahern | 首次撰寫；基於 Phase 1 實際校準係數（~0.05–0.07×）；P2.1–P2.4 已完成紀錄補入 |
| _(待填)_ | _(待填)_ | P2.5 完成後更新實際耗時 |
| _(待填)_ | _(待填)_ | P2.8 完成後填入 Phase 2 總實際時間 |
