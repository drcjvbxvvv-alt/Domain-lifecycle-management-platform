<script setup lang="ts">
import { ref, computed } from 'vue'
import { useRouter, useRoute } from 'vue-router'
import {
  NForm, NFormItem, NInput, NButton,
  useMessage, type FormInst, type FormRules,
} from 'naive-ui'
import { useAuthStore } from '@/stores/auth'

const router  = useRouter()
const route   = useRoute()
const message = useMessage()
const auth    = useAuthStore()

// ── Form state ───────────────────────────────────────────────────────────────
const formRef = ref<FormInst | null>(null)
const loading = ref(false)
const showPwd = ref(false)

const model = ref({ email: '', password: '' })

const rules: FormRules = {
  email: [
    { required: true, message: '請輸入 Email',    trigger: 'blur' },
    { type: 'email',  message: 'Email 格式不正確', trigger: 'blur' },
  ],
  password: [
    { required: true, message: '請輸入密碼',    trigger: 'blur' },
    { min: 6,         message: '密碼至少 6 位', trigger: 'blur' },
  ],
}

// ── Error state ───────────────────────────────────────────────────────────────
const errorMsg = ref('')
const hasError = computed(() => errorMsg.value.length > 0)
function clearError() { errorMsg.value = '' }

// ── Submit ────────────────────────────────────────────────────────────────────
async function handleSubmit() {
  try { await formRef.value?.validate() } catch { return }

  loading.value  = true
  errorMsg.value = ''

  try {
    await auth.login(model.value.email, model.value.password)
    const redirect = (route.query.redirect as string) ?? '/'
    await router.push(redirect)
    message.success('登入成功')
  } catch (err: unknown) {
    const status = (err as { response?: { status?: number } })?.response?.status
    errorMsg.value = status === 401
      ? '帳號或密碼錯誤，請重新確認'
      : '登入失敗，請稍後再試'
  } finally {
    loading.value = false
  }
}
</script>

<template>
  <div class="login-root">

    <!-- Soft background shapes -->
    <div class="bg-blob bg-blob--a" />
    <div class="bg-blob bg-blob--b" />
    <div class="bg-blob bg-blob--c" />

    <!-- Card -->
    <div class="login-card">

      <!-- Brand + Heading (centered) -->
      <div class="brand">
        <div class="brand__icon">
          <img src="/logo.svg" width="40" height="40" alt="Domain Platform logo" />
        </div>
        <h1 class="brand__title">Domain Platform</h1>
        <p class="brand__subtitle">域名全生命週期管理平台</p>
        <div class="brand__divider" />
        <h2 class="brand__welcome">歡迎回來</h2>
        <p class="brand__desc">請登入您的帳號繼續操作</p>
      </div>

      <!-- Form -->
      <NForm
        ref="formRef"
        :model="model"
        :rules="rules"
        :show-label="false"
        size="large"
        @keydown.enter="handleSubmit"
      >
        <NFormItem path="email">
          <NInput
            v-model:value="model.email"
            placeholder="電子郵件"
            :input-props="{ type: 'email', autocomplete: 'email' }"
            :status="hasError ? 'error' : undefined"
            @input="clearError"
          >
            <template #prefix>
              <svg width="15" height="15" viewBox="0 0 24 24" fill="none" class="field-icon">
                <path d="M20 4H4c-1.1 0-2 .9-2 2v12c0 1.1.9 2 2 2h16c1.1 0 2-.9 2-2V6c0-1.1-.9-2-2-2z"
                      stroke="currentColor" stroke-width="1.8" stroke-linejoin="round"/>
                <path d="M22 6l-10 7L2 6"
                      stroke="currentColor" stroke-width="1.8" stroke-linecap="round"/>
              </svg>
            </template>
          </NInput>
        </NFormItem>

        <NFormItem path="password">
          <NInput
            v-model:value="model.password"
            :type="showPwd ? 'text' : 'password'"
            placeholder="密碼"
            :input-props="{ autocomplete: 'current-password' }"
            :status="hasError ? 'error' : undefined"
            @input="clearError"
          >
            <template #prefix>
              <svg width="15" height="15" viewBox="0 0 24 24" fill="none" class="field-icon">
                <rect x="3" y="11" width="18" height="11" rx="2"
                      stroke="currentColor" stroke-width="1.8"/>
                <path d="M7 11V7a5 5 0 0110 0v4"
                      stroke="currentColor" stroke-width="1.8" stroke-linecap="round"/>
              </svg>
            </template>
            <template #suffix>
              <span class="eye-toggle" @click="showPwd = !showPwd">
                <svg v-if="showPwd" width="15" height="15" viewBox="0 0 24 24" fill="none">
                  <path d="M1 12s4-8 11-8 11 8 11 8-4 8-11 8-11-8-11-8z"
                        stroke="currentColor" stroke-width="1.8"/>
                  <circle cx="12" cy="12" r="3" stroke="currentColor" stroke-width="1.8"/>
                </svg>
                <svg v-else width="15" height="15" viewBox="0 0 24 24" fill="none">
                  <path d="M17.94 17.94A10.07 10.07 0 0112 20c-7 0-11-8-11-8a18.45 18.45 0 015.06-5.94M9.9 4.24A9.12 9.12 0 0112 4c7 0 11 8 11 8a18.5 18.5 0 01-2.16 3.19m-6.72-1.07a3 3 0 11-4.24-4.24M1 1l22 22"
                        stroke="currentColor" stroke-width="1.8" stroke-linecap="round"/>
                </svg>
              </span>
            </template>
          </NInput>
        </NFormItem>
      </NForm>

      <!-- Error -->
      <Transition name="err">
        <div v-if="hasError" class="error-box">
          <svg width="14" height="14" viewBox="0 0 24 24" fill="none" style="flex-shrink:0">
            <circle cx="12" cy="12" r="10" stroke="currentColor" stroke-width="2"/>
            <path d="M12 8v4M12 16h.01" stroke="currentColor" stroke-width="2" stroke-linecap="round"/>
          </svg>
          {{ errorMsg }}
        </div>
      </Transition>

      <!-- Login button -->
      <NButton
        type="primary"
        size="large"
        block
        :loading="loading"
        class="submit-btn"
        @click="handleSubmit"
      >
        登入
      </NButton>

      <!-- Footer -->
      <p class="card-footer">
        Domain Lifecycle Management &nbsp;·&nbsp; v0.1.0
      </p>
    </div>

  </div>
</template>

<style scoped>
/* ── Root ─────────────────────────────────────────────────────────────────── */
.login-root {
  position: relative;
  display: flex;
  align-items: center;
  justify-content: center;
  min-height: 100vh;
  background-color: var(--bg-page);
  overflow: hidden;
}

/* ── Background blobs — soft, diffused colour washes ─────────────────────── */
.bg-blob {
  position: absolute;
  border-radius: 50%;
  pointer-events: none;
  filter: blur(72px);
  opacity: 0.55;
}
.bg-blob--a {
  width: 560px;
  height: 560px;
  top:  -200px;
  left: -160px;
  background: radial-gradient(circle, rgba(147, 197, 253, 0.55) 0%, transparent 70%);
}
.bg-blob--b {
  width: 480px;
  height: 480px;
  bottom: -160px;
  right:  -140px;
  background: radial-gradient(circle, rgba(196, 181, 253, 0.40) 0%, transparent 70%);
}
.bg-blob--c {
  width: 320px;
  height: 320px;
  top: 40%;
  left: 55%;
  background: radial-gradient(circle, rgba(167, 243, 208, 0.30) 0%, transparent 70%);
}

/* ── Card ─────────────────────────────────────────────────────────────────── */
.login-card {
  position: relative;
  z-index: 1;
  width: 400px;
  padding: 36px 40px 32px;
  background-color: var(--bg-card);
  border: 1px solid var(--border);
  border-radius: 16px;
  box-shadow:
    0 0 0 1px rgba(79, 126, 248, 0.06),
    0 4px 16px rgba(15, 23, 42, 0.08),
    0 16px 48px rgba(15, 23, 42, 0.08);
}

/* ── Brand + heading block (all centered) ─────────────────────────────────── */
.brand {
  display: flex;
  flex-direction: column;
  align-items: center;
  text-align: center;
  gap: 0;
  margin-bottom: 28px;
  padding-bottom: 28px;
  border-bottom: 1px solid var(--border-sub);
}

.brand__icon {
  display: flex;
  align-items: center;
  justify-content: center;
  width: 64px;
  height: 64px;
  background: linear-gradient(145deg, #eff6ff 0%, #dbeafe 50%, #e0f2fe 100%);
  border: 1px solid rgba(79, 126, 248, 0.18);
  border-radius: 18px;
  box-shadow: 0 2px 8px rgba(79, 126, 248, 0.12);
  margin-bottom: 14px;
}

.brand__title {
  font-size: 18px;
  font-weight: 700;
  color: var(--text-primary);
  letter-spacing: -0.3px;
  line-height: 1.2;
  margin-bottom: 4px;
}

.brand__subtitle {
  font-size: 12px;
  color: var(--text-muted);
  letter-spacing: 0.2px;
}

.brand__divider {
  width: 32px;
  height: 1px;
  background: var(--border);
  margin: 20px 0 18px;
}

.brand__welcome {
  font-size: 22px;
  font-weight: 700;
  color: var(--text-primary);
  letter-spacing: -0.4px;
  line-height: 1.2;
  margin-bottom: 6px;
}

.brand__desc {
  font-size: 14px;
  color: var(--text-muted);
}

/* ── Field icons ──────────────────────────────────────────────────────────── */
.field-icon  { color: var(--text-muted); }

.eye-toggle {
  display: flex;
  align-items: center;
  color: var(--text-muted);
  cursor: pointer;
  transition: color 0.15s;
}
.eye-toggle:hover { color: var(--text-secondary); }

/* ── Error box ────────────────────────────────────────────────────────────── */
.error-box {
  display: flex;
  align-items: center;
  gap: 7px;
  padding: 10px 12px;
  margin-bottom: 14px;
  background-color: rgba(220, 38, 38, 0.06);
  border: 1px solid rgba(220, 38, 38, 0.20);
  border-radius: 8px;
  font-size: 13px;
  color: #dc2626;
  line-height: 1.5;
}

/* ── Button ───────────────────────────────────────────────────────────────── */
.submit-btn {
  margin-top: 6px;
  height: 44px !important;
  font-size: 15px !important;
  font-weight: 600 !important;
  letter-spacing: 0.3px;
}

/* ── Footer ───────────────────────────────────────────────────────────────── */
.card-footer {
  margin-top: 20px;
  text-align: center;
  font-size: 12px;
  color: var(--text-muted);
  letter-spacing: 0.2px;
}

/* ── Error transition ─────────────────────────────────────────────────────── */
.err-enter-active, .err-leave-active { transition: all 0.2s ease; }
.err-enter-from,  .err-leave-to     { opacity: 0; transform: translateY(-4px); }
</style>
