# FRONTEND_GUIDE.md — 前端設計系統規範

> **Claude Code 必讀。** 每次新增或修改頁面前，先讀完本文件。
> 本文件的目標：讓所有頁面看起來像同一個人寫的。

---

## 核心原則

1. **不設計，只填空** — 視覺決策已全部預設好，開發者只負責組合元件
2. **不能自創** — 禁止在頁面中自定義顏色、間距、字體大小
3. **不能直接用 Naive UI** — 必須透過共用元件，不可在頁面直接用 `NDataTable`、`NTag`
4. **不能用 inline style** — 除非使用 CSS variable（`var(--xxx)`）或 `tokens.ts` 的值

---

## Token 使用規則

所有視覺值來自兩個地方，二選一：

```typescript
// 在 <script> 中使用 TypeScript 值
import { colors, spacing, fontSize } from '@/styles/tokens'
const activeColor = colors.status.active.color  // '#4ade80'

// 在 <style> 中使用 CSS 變數
.my-element { color: var(--status-active-color); }
```

**禁止直接寫 hex code：**
```html
<!-- ❌ 錯誤 -->
<span style="color: #4ade80">正常</span>

<!-- ✅ 正確 -->
<StatusTag status="active" />
```

---

## 頁面結構模板

### 列表頁（List Page）

每個列表頁必須使用以下結構，不得偏離：

```vue
<template>
  <div class="list-page">
    <!-- 1. 頁面標題列 -->
    <PageHeader title="域名列表" :subtitle="`共 ${total} 筆`">
      <template #actions>
        <NButton type="primary" @click="showCreate = true">新增域名</NButton>
      </template>
    </PageHeader>

    <!-- 2. 篩選列 -->
    <SearchBar>
      <NSelect v-model:value="filters.projectId" :options="projectOptions" placeholder="所有專案" />
      <NSelect v-model:value="filters.status"    :options="statusOptions"  placeholder="所有狀態" />
      <NInput  v-model:value="filters.keyword"   placeholder="搜尋域名..." clearable />
      <template #action>
        <NButton @click="resetFilters">重置</NButton>
      </template>
    </SearchBar>

    <!-- 3. 資料表格 -->
    <AppTable
      :columns="columns"
      :data="data"
      :loading="loading"
      :row-key="(row) => row.uuid"
      :page="page"
      :page-size="50"
      :item-count="total"
      @update:page="page = $event"
    />
  </div>
</template>

<style scoped>
.list-page {
  display: flex;
  flex-direction: column;
  height: 100%;
  overflow: hidden;
}
</style>
```

### 詳情頁（Detail Page）

```vue
<template>
  <div class="detail-page">
    <!-- 1. 標題（含返回按鈕） -->
    <PageHeader :title="domain?.domain ?? '載入中...'" :subtitle="domain?.status">
      <template #actions>
        <NButton @click="router.back()">返回</NButton>
        <NButton type="primary" @click="handleDeploy">部署</NButton>
      </template>
    </PageHeader>

    <!-- 2. 主體：左欄資訊 + 右欄 Tabs -->
    <div class="detail-page__body">
      <div class="detail-page__sidebar">
        <!-- 基本資訊卡片 -->
      </div>
      <div class="detail-page__main">
        <NTabs>
          <NTabPane name="subdomains" tab="子域名" />
          <NTabPane name="history"    tab="狀態歷史" />
          <NTabPane name="probe"      tab="探針狀態" />
        </NTabs>
      </div>
    </div>
  </div>
</template>

<style scoped>
.detail-page {
  display: flex;
  flex-direction: column;
  height: 100%;
  overflow: hidden;
}
.detail-page__body {
  display: flex;
  flex: 1;
  overflow: hidden;
  gap: 0;
}
.detail-page__sidebar {
  width: 320px;
  flex-shrink: 0;
  border-right: 1px solid var(--border);
  padding: var(--space-6);
  overflow-y: auto;
}
.detail-page__main {
  flex: 1;
  padding: var(--space-6);
  overflow-y: auto;
}
</style>
```

### Dashboard 頁

```vue
<template>
  <div class="dashboard">
    <PageHeader title="Dashboard" />

    <!-- 統計卡片列（永遠是 4 欄） -->
    <div class="dashboard__stats">
      <StatCard label="正常域名"  :value="stats.active"   color="#4ade80" />
      <StatCard label="封鎖告警"  :value="stats.blocked"  color="#ef4444" />
      <StatCard label="切換中"    :value="stats.switching" color="#c084fc" />
      <StatCard label="備用池剩餘" :value="stats.pool"     color="#38bdf8" />
    </div>

    <!-- 告警列表 -->
    <div class="dashboard__section">
      <h2 class="dashboard__section-title">最新告警</h2>
      <!-- AppTable -->
    </div>
  </div>
</template>

<style scoped>
.dashboard {
  display: flex;
  flex-direction: column;
  height: 100%;
  overflow-y: auto;
}
.dashboard__stats {
  display: grid;
  grid-template-columns: repeat(4, 1fr);
  gap: var(--space-4);
  padding: var(--space-6) var(--content-padding);
}
.dashboard__section {
  padding: 0 var(--content-padding) var(--space-6);
}
.dashboard__section-title {
  font-size: 14px;
  font-weight: 600;
  color: var(--text-secondary);
  text-transform: uppercase;
  letter-spacing: 0.5px;
  margin-bottom: var(--space-3);
}
</style>
```

---

## 元件使用規則

### StatusTag — 域名狀態

```vue
<!-- ✅ 正確：永遠用 StatusTag -->
<StatusTag status="active" />
<StatusTag :status="domain.status" />

<!-- ❌ 錯誤：自己做標籤 -->
<NTag type="success">正常</NTag>
<span style="color:green">active</span>
```

### SeverityTag — 告警嚴重度

```vue
<!-- ✅ 正確 -->
<SeverityTag severity="P1" />

<!-- ❌ 錯誤 -->
<NTag type="error">P1</NTag>
```

### AppTable — 資料表格

```vue
<!-- ✅ 正確：只能用 AppTable -->
<AppTable :columns="columns" :data="data" :loading="loading" />

<!-- ❌ 錯誤：直接使用 Naive UI -->
<NDataTable :columns="columns" :data="data" />
```

### ConfirmModal — 危險操作確認

```vue
<!-- ✅ 正確：所有刪除、部署、切換等不可逆操作必須用 ConfirmModal -->
<ConfirmModal
  v-model:show="showDelete"
  title="刪除域名"
  content="此操作無法還原，確定要刪除 example.com 嗎？"
  type="danger"
  :loading="deleting"
  @confirm="handleDelete"
/>

<!-- ❌ 錯誤：直接執行危險操作，或用 window.confirm -->
```

---

## 元件匯入方式

永遠從 `@/components` 統一匯入，不要直接 import 個別檔案：

```typescript
// ✅ 正確
import { StatusTag, PageHeader, AppTable, ConfirmModal } from '@/components'

// ❌ 錯誤
import StatusTag from '@/components/StatusTag.vue'
```

---

## 按鈕規範

| 用途 | 類型 | 範例 |
|------|------|------|
| 主要操作（新增、部署） | `type="primary"` | 新增域名 |
| 次要操作（重置、返回） | 預設（不加 type） | 返回 |
| 危險操作（刪除） | `type="error"` | 刪除 |
| 警告操作（暫停發布） | `type="warning"` | 暫停 |

頁面中操作按鈕最多 3 個，超過 3 個改用下拉選單（`NDropdown`）。

---

## 間距規範

只能使用以下間距值（來自 `--space-*` 變數）：

| 變數 | 值 | 使用場景 |
|------|----|---------|
| `--space-2` | 8px | 元素間小間距（icon + label） |
| `--space-3` | 12px | 篩選器間距 |
| `--space-4` | 16px | 卡片 padding、按鈕間距 |
| `--space-5` | 20px | 卡片 padding（較寬鬆） |
| `--space-6` | 24px | 區塊間距、`--content-padding` |
| `--space-8` | 32px | 大區塊間距 |

---

## 字體規範

| 場景 | 大小 | 粗細 |
|------|------|------|
| 頁面標題（`PageHeader`） | 18px | 600 |
| 卡片標題 | 15px | 600 |
| 表格欄位標頭 | 12px | 600（uppercase） |
| 表格內容 | 14px | 400 |
| 輔助說明文字 | 13px | 400 |
| 標籤、badge | 12px | 500 |
| 統計數字（`StatCard`） | 28px | 700 |

---

## 顏色使用規範

**永遠不要自己挑顏色。** 按照以下對應使用：

| 語意 | Token | 值 |
|------|-------|-----|
| 正常 / 成功 | `colors.status.active.color` | `#4ade80` |
| 警告 / 降級 | `colors.status.degraded.color` | `#fb923c` |
| 危險 / 封鎖 | `colors.status.blocked.color` | `#ef4444` |
| 進行中 | `colors.status.deploying.color` | `#fbbf24` |
| 切換中 | `colors.status.switching.color` | `#c084fc` |
| 主品牌色 | `colors.primary` | `#38bdf8` |
| 次要文字 | `colors.textSecondary` | `#94a3b8` |

---

## 禁止事項（Checklist）

在提交任何前端程式碼前，確認以下都不存在：

```
❌ 直接使用 NDataTable（必須用 AppTable）
❌ 直接使用 NTag 表示狀態（必須用 StatusTag）
❌ inline style 中有 hex color（#xxxxxx）
❌ inline style 中有 px 數值（必須用 var(--space-x)）
❌ 使用 any 型別
❌ API call 沒有 try/catch
❌ 刪除/部署操作沒有 ConfirmModal
❌ 頁面的 import 不是從 @/components 來的
```
