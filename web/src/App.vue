<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { api } from './api'
import Login from './views/Login.vue'
import Providers from './views/Providers.vue'
import Models from './views/Models.vue'
import Keys from './views/Keys.vue'
import Settings from './views/Settings.vue'

type Tab = 'providers' | 'models' | 'keys' | 'settings'

const authed = ref(false)
const checking = ref(true)
const tab = ref<Tab>('providers')

const tabs: { id: Tab; label: string }[] = [
  { id: 'providers', label: '接入 (Providers)' },
  { id: 'models', label: '模型目录 (Models)' },
  { id: 'keys', label: '客户端密钥 (Keys)' },
  { id: 'settings', label: '全局配置 (Settings)' },
]

async function check() {
  try {
    await api.get('/console/api/session')
    authed.value = true
  } catch {
    authed.value = false
  } finally {
    checking.value = false
  }
}

async function logout() {
  try {
    await api.post('/console/api/logout')
  } finally {
    authed.value = false
  }
}

onMounted(check)
</script>

<template>
  <div v-if="checking" class="center muted">加载中…</div>
  <Login v-else-if="!authed" @ok="authed = true" />
  <div v-else class="layout">
    <aside class="sidebar">
      <div class="brand">ds-context</div>
      <nav>
        <button
          v-for="t in tabs"
          :key="t.id"
          class="nav-btn"
          :class="{ active: tab === t.id }"
          @click="tab = t.id"
        >
          {{ t.label }}
        </button>
      </nav>
      <button class="nav-btn logout" @click="logout">退出登录</button>
    </aside>
    <main class="main">
      <Providers v-if="tab === 'providers'" />
      <Models v-else-if="tab === 'models'" />
      <Keys v-else-if="tab === 'keys'" />
      <Settings v-else />
    </main>
  </div>
</template>

<style>
* {
  box-sizing: border-box;
}
body {
  margin: 0;
  font-family: -apple-system, 'PingFang SC', 'Microsoft YaHei', sans-serif;
  color: #1f2937;
  background: #f3f4f6;
}
.center {
  display: flex;
  align-items: center;
  justify-content: center;
  height: 100vh;
}
.muted {
  color: #6b7280;
}
.layout {
  display: flex;
  min-height: 100vh;
}
.sidebar {
  width: 220px;
  background: #111827;
  color: #e5e7eb;
  display: flex;
  flex-direction: column;
  padding: 16px 12px;
  gap: 6px;
}
.brand {
  font-weight: 700;
  font-size: 16px;
  padding: 8px 10px 16px;
  color: #fff;
}
.nav-btn {
  display: block;
  width: 100%;
  text-align: left;
  padding: 9px 10px;
  border: 0;
  border-radius: 6px;
  background: transparent;
  color: #d1d5db;
  font-size: 14px;
  cursor: pointer;
}
.nav-btn:hover {
  background: #1f2937;
}
.nav-btn.active {
  background: #2563eb;
  color: #fff;
}
.logout {
  margin-top: auto;
  color: #fca5a5;
}
.main {
  flex: 1;
  padding: 24px;
  max-width: 1100px;
}
h2 {
  margin: 0 0 16px;
  font-size: 18px;
}
.card {
  background: #fff;
  border: 1px solid #e5e7eb;
  border-radius: 8px;
  padding: 16px;
  margin-bottom: 16px;
}
table {
  width: 100%;
  border-collapse: collapse;
  font-size: 14px;
}
th,
td {
  text-align: left;
  padding: 8px 10px;
  border-bottom: 1px solid #e5e7eb;
  vertical-align: middle;
}
th {
  color: #6b7280;
  font-weight: 500;
}
input[type='text'],
input[type='password'],
select {
  padding: 7px 9px;
  border: 1px solid #d1d5db;
  border-radius: 6px;
  font-size: 14px;
  width: 100%;
}
.form-row {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(180px, 1fr));
  gap: 10px;
  margin-bottom: 10px;
}
label.field {
  display: block;
  font-size: 12px;
  color: #6b7280;
}
label.field input,
label.field select {
  margin-top: 4px;
}
button.primary {
  background: #2563eb;
  color: #fff;
  border: 0;
  border-radius: 6px;
  padding: 8px 14px;
  font-size: 14px;
  cursor: pointer;
}
button.ghost {
  background: transparent;
  border: 1px solid #d1d5db;
  border-radius: 6px;
  padding: 5px 10px;
  font-size: 13px;
  cursor: pointer;
  margin-right: 6px;
}
button.danger {
  color: #b91c1c;
  border-color: #fecaca;
}
.error {
  color: #b91c1c;
  font-size: 13px;
  margin-top: 8px;
}
.notice {
  background: #ecfdf5;
  border: 1px solid #a7f3d0;
  border-radius: 6px;
  padding: 10px 12px;
  font-size: 13px;
  margin-bottom: 12px;
  word-break: break-all;
}
.badge {
  display: inline-block;
  font-size: 12px;
  padding: 1px 8px;
  border-radius: 999px;
}
.badge.on {
  background: #dcfce7;
  color: #166534;
}
.badge.off {
  background: #f3f4f6;
  color: #6b7280;
}
</style>
