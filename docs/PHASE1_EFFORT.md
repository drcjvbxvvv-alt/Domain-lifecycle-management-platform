# PHASE1_EFFORT.md — Phase 1 開發週期粗估

> ## ⚠ 這是**粗估，不是承諾**
>
> 這份文件是一份 **planning tool**，不是對任何人的交付承諾。它的正確用法：
>
> 1. **自我校準警報系統** — 如果實際花的時間顯著超出區間上限，停下來問「為什麼？」，通常代表有隱藏問題要先解決。
> 2. **順序指引** — 依安全牌 → 中風險 → 高風險的順序建立動能。
> 3. **在 P1.3 完成後必須重新校準整份表格**。那時候你會知道「你 + Claude Code」一天能完成多少 effort，把 P1.1–P1.3 實際花時間除以估計值得到係數，乘到剩下任務。
>
> **不要**把這份文件給任何其他人當承諾。如果有人問 Phase 1 何時好，回答「**4.5–6 週**」並加上「**第一次蓋新架構**」就夠了。未來的你再讀到這份文件時，請記得寫它的人（2026-04-09 的 you）對實際 throughput 也是在猜的。

---

## 假設

這份估計**只在下列條件下成立**，任一條件改變就要重估：

| 項目 | 假設 |
|---|---|
| 人力 | **1 人全職**（solo developer）|
| 工作日 | **6–7 小時 / 天深度寫 code**（扣掉中斷 / 休息 / 切 context） |
| 工具 | **Claude Code session 混合使用**（Sonnet + Opus，依 `docs/CLAUDE_CODE_INSTRUCTIONS.md` §"Model Selection Policy"） |
| 技術熟悉度 | Go + Vue 3 + PostgreSQL + sqlx + asynq + MinIO + TimescaleDB 都**熟** |
| 範圍 | **Phase 1 scope 嚴格遵守 `docs/PHASE1_TASKLIST.md`**，不擴張到 P2+ 的 sharding / canary / probe / rollback / approval flow |
| 工作週 | 週一到週五，**週末休息**（日曆週計算方式） |
| Git workflow | 單人開發，**無 PR review 等待時間**；若有 review，每個任務 +0.5–1 天等待 |

---

## 每任務估計（工作天）

| # | 任務 | Owner | Lo | Hi | 風險 | 主要炸點 |
|---|---|---|---|---|---|---|
| P1.1 | Scaffold + bootstrap | Sonnet | 0.5 | 1.5 | 🟢 | MinIO / docker-compose 第一次起 |
| P1.2 | DB migrations（26+ 表）| Sonnet | 0.5 | 1.5 | 🟢 | TimescaleDB hypertable + compression policy |
| P1.3 | Auth + JWT + 5 角色 RBAC | Sonnet | 0.5 | 1.0 | 🟢 | 很熟的模式 |
| P1.4 | Project CRUD | Sonnet | 0.5 | 0.5 | 🟢 | 純樣板 |
| P1.5 | **Lifecycle 狀態機 + Transition + race test + CI gate** | **Opus** | 1.0 | 2.0 | 🟡 | race test 可能 flaky 要跑 `-count=50` 找穩定性 |
| P1.6 | Template + TemplateVersion CRUD | Sonnet | 0.5 | 1.0 | 🟢 | 含 `signed_at` immutability 強制在 store 層 |
| P1.7 | **Artifact build pipeline + MinIO + signer + reproducibility test** | **Opus** 合約 / Sonnet 實作 | 2.0 | 3.5 | 🔴 | Reproducibility test 會抓到沒想到的 nondeterminism — time / map order / UUID — 每抓一個要重跑 |
| P1.8 | **Release 狀態機 + Plan/Dispatch/Finalize** | **Opus** 狀態機 / Sonnet 其餘 | 2.0 | 3.5 | 🔴 | Plan → Dispatch → 等 agent → Finalize 的第一次 end-to-end wiring |
| P1.9 | **cmd/agent Pull Agent binary + safety gate + 整合測試** | **Opus** | 2.0 | 3.5 | 🔴 | 寫 fake control plane + fake MinIO 做 integration test，第一次 e2e 會有 1–2 個意外 |
| P1.10 | Agent mgmt 控制面 + mTLS middleware + 狀態機 | **Opus** 狀態機 / Sonnet 其餘 | 2.0 | 3.5 | 🔴 | **mTLS + 憑證生成 + CRL 是經典踩雷區**，就算熟 Go 也會花半天到一天 |
| P1.11 | asynq worker bootstrap | Sonnet | 0.5 | 0.5 | 🟢 | 樣板 |
| P1.12 | Frontend：登入 wire-up + 6 Pinia store + ~8 頁面 + types 對齊 | Sonnet | 3.0 | 5.0 | 🟡 | 頁面多但 pattern 一致；TypeScript 與 Go DTO byte-for-byte 對齊需要幾輪 |

**每任務加總**：**Lo = 15 天 / Hi = 27 天**

---

## 加上 integration 摩擦

上表**不含**下列固定成本，第一次蓋新架構時無法避免：

| 項目 | 加多少 |
|---|---|
| 第一次 e2e 串起來（P1.1 → P1.9 → P1.12）會發現缺件 | +1.5 ~ 3 天 |
| Go ↔ TypeScript DTO 對齊反覆校對 | +0.5 ~ 1 天 |
| docker-compose / MinIO 本地憑證 / mTLS 初次配置 debug | +0.5 ~ 1.5 天 |
| 4 個 CI gate（`check-lifecycle-writes` / `check-release-writes` / `check-agent-writes` / `check-agent-safety`）初次跑綠 | +0.5 ~ 1 天 |
| 寫完某任務發現 schema 少一欄，回頭改 migration + 所有 store | +0.5 ~ 1.5 天 |

**摩擦加總**：**Lo = +3 天 / Hi = +8 天**

---

## Phase 1 總計

|   | 工作天 | 5-day 週 | 日曆週（含週末） |
|---|---|---|---|
| **樂觀**（everything clicks）| **18 天** | 3.5 週 | **~4 週** |
| **中位**（一般情況）| **24 天** | 5 週 | **~5–6 週** |
| **悲觀**（幾個炸點同時中）| **35 天** | 7 週 | **~8 週** |

### 最可能落點

**4.5 – 6 週日曆時間**（中位情境）

---

## 風險集中的 3 個地方

### 1. P1.9 + P1.10：Agent 雙邊整合 🔴

**炸點**：mTLS 憑證 + agent register/heartbeat + 第一次 e2e 拉起 agent → control plane。這段是 Phase 1 最容易掉進兔子洞的地方。

**緩解策略**：
- P1.10 先只做 **mTLS middleware + register/heartbeat endpoint**（不碰 task dispatch）
- 讓 agent 能「**握手成功 + 回報心跳**」就停手
- 然後 P1.9 再往下做 task 流程
- 這樣出事時 blast radius 小，一次只驗證一件事

### 2. P1.7 Reproducibility Test 🔴

**炸點**：Reproducible build 是 CLAUDE.md Critical Rule #2 的硬性要求，但第一次實作時會一直抓到小問題：

- `time.Now()` 漏進 manifest 的 body
- `map` iteration order
- `uuid.New()` 在 rendered content 裡
- Sort order 不一致

**緩解策略**：
- **一開始就寫 `build twice, assert byte equal` 的測試**
- **先讓它失敗**，再逐個修
- 別寫完整個 builder 再加測試 — 會回頭改很多地方
- 強制自己：時間戳只能在 manifest，不能在 content；UUID 由呼叫端傳入不可由 builder 生成

### 3. P1.12 Frontend Scope Creep 🟡

**炸點**：8 個頁面每個都「差一點點就好看」可以讓你多花 3 天。

**緩解策略**：
- Phase 1 的前端是 **list view only, read-only**
- **沒有 create/edit 表單**（curl/postman 足夠）
- **沒有即時更新**（5 秒 polling 就好）
- **先把 P1.1–P1.11 都做完再做 P1.12**
- 時間剩多少就做多少，別反過來用前端進度推估後端該加速

---

## 建議工作順序（solo 優化）

依賴圖（`docs/PHASE1_TASKLIST.md` §"Dependency Graph"）允許多種順序。對單人全職最有利的排法：**先建動能 → 把 Opus bottleneck 放中間 → 前端收尾**。

```
Week 1  : P1.1 → P1.2 → P1.3 → P1.11             (骨架 + 動能 + Phase 1 跑起來)
Week 2  : P1.4 → P1.6 → P1.5 (Opus)              (CRUD + 第一個狀態機)
Week 3  : P1.7 (Opus 合約 / Sonnet 實作)          (artifact 管線)
Week 4  : P1.8 (Opus 狀態機 / Sonnet 其餘)        (release 狀態機)
Week 5  : P1.10 → P1.9 (Opus)                     (agent 雙邊 — P1.10 先)
Week 6  : P1.12                                    (前端收尾)
```

### 為什麼 P1.10 在 P1.9 前？

先在**控制面**把 agent register/heartbeat 端點做起來、發出一張測試憑證。
然後回頭寫 **agent binary**。這樣 agent 的第一次啟動就有東西可以打，不用自己同時維護兩端。**一次只除一個 bug**。

### 為什麼 P1.5 放在 Week 2 中間而不是最前？

P1.5 是 Opus 任務，需要你進入「認真想 race condition」的心智模式。放在 Week 2 中段，你已經透過 P1.1–P1.4 建立了 momentum 和環境信心，Opus session 跑起來會更順。**別一開始就打最硬的**。

### 為什麼 P1.11 跟 P1.1/P1.2/P1.3 放一起？

asynq worker bootstrap 是 0.5 天的樣板工作。在 Week 1 收尾時順手做掉，後面的任務就不用擔心 `cmd/worker` 是不是起得來。

---

## Phase 1 以外的後續粗估

**未經詳細拆解，僅提供量級概念**：

| Phase | 範圍 | 量級 |
|---|---|---|
| **Phase 2** | Sharding / Rollback / Dry-run / Diff / Per-host limit / Agent mgmt UI | ~3–4 週 |
| **Phase 3** | Gray release / Probe L1+L2+L3 / Alert engine / Agent canary upgrade | ~4–5 週 |
| **Phase 4** | Domain lifecycle approval / Nginx artifact / HA | ~3–4 週 |
| **Phase 5+** | GFW failover vertical（若啟動，獨立 ADR）| 未評估 |

**Phase 1–4 總量級**：約 **4–6 個月**（單人全職 + Claude Code 混合，相容於你的條件）。

**重要警告**：Phase 2+ 的估計**精準度低於 Phase 1**，因為：
- Phase 1 的 scope / schema / state machine 定義得很精準
- Phase 2+ 會基於 Phase 1 的實作狀態做設計決策，現在很多細節還不確定
- Phase 4 的 HA / 跨區域 / 合規性等項目**工作量與組織內部需求強相關**

**建議**：**Phase 2 的任務卡在 Phase 1 完成後再寫**，不要現在預排。每個 Phase 結束前一週開專門的 session 做下個 Phase 的 task breakdown，重新基於當時的實際進度估計。

---

## 校準指引

### 完成 P1.3 後（預計 Day 2 – Day 4 之間）

做下面這個計算：

```
實際耗時 / 原估計中位 = 校準係數

例如：
  原估計 P1.1 中位 = 1.0 天
  原估計 P1.2 中位 = 1.0 天
  原估計 P1.3 中位 = 0.75 天
  原估計三任務中位合計 = 2.75 天

  實際花了 4.5 天
  係數 = 4.5 / 2.75 = 1.64

接下來所有任務的估計都乘 1.64。
```

把新的總估寫回這份文件（更新「Phase 1 總計」區塊，並在底部加一行「**2026-04-XX 校準，係數 1.64**」）。

### 完成 P1.7 後（最大 🔴 風險任務）

再次校準。P1.7 是最容易爆炸的任務，它的實際耗時最能反映「整合摩擦」的實際大小。如果 P1.7 接近 Hi（3.5 天），剩下的 🔴 任務也把估計往 Hi 靠；如果在 Lo（2 天）附近，你可能已經過了最痛的階段。

### 每週五下午

花 15 分鐘看一下：
- 本週做完了什麼（對比原計畫）
- 哪個任務比預期難 / 簡單，為什麼
- 下週要重新排哪些

**不要試圖「補回進度」**。進度落後通常是訊號（scope 沒想清楚 / 架構有摩擦 / 環境有問題），不是要更努力。

---

## 範圍警告（Scope Creep 預警）

下列項目**一定會**在開發過程中誘惑你偏離 Phase 1 scope。**每一個都要抵抗**：

| 誘惑 | 真相 |
|---|---|
| 「P1.4 Project CRUD 好簡單，順便加上成員管理吧」| 成員管理是 Phase 4。多做 = 多 0.5–1 天。 |
| 「P1.8 Release 不分 shard 看起來很假，我加個 shard 算了」| Sharding 是 Phase 2 整整一張任務卡。開始做 = +2–3 天。 |
| 「P1.9 agent 順便加個 drain 吧」| Drain 是 Phase 2。+0.5–1 天。 |
| 「P1.12 這個 list view 沒有 create button 太醜了，加一下」| Create 表單每個要 0.5 天，8 個頁面 = 4 天。**這是 Phase 1 超期最大原因之一**。 |
| 「Probe 的 L2 verification 不難啊，我加一下」| Phase 3。需要 probe runner + timescale 寫入路徑。+3–5 天。 |
| 「mTLS 加個自動輪替吧」| Phase 3。憑證輪替測試光寫就 1 天。 |

**判斷規則**：想加東西時，去查 `docs/PHASE1_TASKLIST.md` §"What is OUT of Phase 1"。如果在那張表上 → **不做**，加一行 TODO 到 `docs/PHASE2_TASKLIST.md`（還沒存在，現在開始建）或 `docs/BACKLOG.md`，**繼續原任務**。

---

## 參考

- `docs/PHASE1_TASKLIST.md` — 12 張任務卡的完整定義（owner model / scope in/out / 依賴 / 驗收條件）
- `docs/CLAUDE_CODE_INSTRUCTIONS.md` §"Model Selection Policy" — Opus vs Sonnet 的選用標準
- `docs/ARCHITECTURE.md` §8 — Phase 1–4 headline scope
- `docs/adr/0003-pivot-to-generic-release-platform-2026-04.md` — 為何 Phase 1 長這個樣子的架構決策

---

## 更新記錄

| 日期 | 更新者 | 內容 |
|---|---|---|
| 2026-04-09 | Claude Opus 4.6 + ahern | 首次撰寫；基於「1 人全職 + Claude Code + 熟悉 stack」的假設給出 18/24/35 天區間 |
| _(待填)_ | _(待填)_ | P1.3 完成後首次校準 |
| _(待填)_ | _(待填)_ | P1.7 完成後二次校準 |
