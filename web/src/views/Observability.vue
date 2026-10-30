<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { api, type TraceSession } from '../api'
import { formatDateTime, formatSpan } from '../format'
import TracePanel from './TracePanel.vue'
import ToolsPanel from './ToolsPanel.vue'

const sessions = ref<TraceSession[]>([])
const loading = ref(true)
const error = ref('')
const query = ref('')
const selected = ref<TraceSession | null>(null)
const panel = ref<'trace' | 'tools'>('trace')

// load 拉会话列表。列表按 spec 不含轮次数:推断轮次的成本随会话数线性放大。
async function load() {
  loading.value = true
  error.value = ''
  try {
    const q = query.value.trim()
    const path = q ? `/console/api/traces?q=${encodeURIComponent(q)}` : '/console/api/traces'
    sessions.value = await api.get<TraceSession[]>(path)
  } catch (e) {
    error.value = e instanceof Error ? e.message : String(e)
  } finally {
    loading.value = false
  }
}

function sourceLabel(source: string): string {
  if (source === 'explicit') return '显式标识'
  if (source === 'anon') return '匿名'
  return source
}

function fmtK(n: number): string {
  if (n >= 1000) return (n / 1000).toFixed(n >= 10000 ? 0 : 1) + 'k'
  return String(n)
}

onMounted(load)
</script>

<template>
  <h2>可观测 (Observability)</h2>
  <div v-if="error" class="error">{{ error }}</div>

  <div v-if="!selected" class="card">
    <div class="filters">
      <input v-model="query" type="text" placeholder="按 session id 前缀过滤" @keyup.enter="load" />
      <button class="primary" @click="load">查询</button>
    </div>
    <p class="hint">
      跨会话入口。列表不含轮次数(推断轮次需逐会话比对,成本随会话数放大);轮次数在轨迹视图给出。
    </p>
    <table v-if="sessions.length">
      <thead>
        <tr>
          <th>session</th>
          <th>判定</th>
          <th>首见</th>
          <th>末见</th>
          <th>steps</th>
          <th>in / out / cache</th>
        </tr>
      </thead>
      <tbody>
        <tr v-for="s in sessions" :key="s.session_id" class="row-click" @click="selected = s">
          <td class="mono">{{ s.session_id }}</td>
          <td>
            <span class="tag">{{ sourceLabel(s.session_source) }}</span>
            <span class="muted small">{{ s.session_note }}</span>
          </td>
          <td class="small">{{ formatDateTime(s.first_seen) }}</td>
          <td class="small">{{ formatDateTime(s.last_seen) }}</td>
          <td>{{ s.steps }}</td>
          <td class="mono small">{{ fmtK(s.usage_in) }} / {{ fmtK(s.usage_out) }} / {{ fmtK(s.usage_cache) }}</td>
        </tr>
      </tbody>
    </table>
    <p v-else-if="loading" class="muted">加载中...</p>
    <p v-else class="muted">没有会话</p>
  </div>

  <template v-else>
    <div class="card">
      <div class="head-row">
        <div>
          <div class="mono strong">{{ selected.session_id }}</div>
          <div class="muted small">
            判定 <b>{{ sourceLabel(selected.session_source) }}</b> · {{ selected.session_note }} ·
            {{ formatDateTime(selected.first_seen) }} → {{ formatDateTime(selected.last_seen) }}
            <span v-if="formatSpan(selected.first_seen, selected.last_seen)">
              (跨度 {{ formatSpan(selected.first_seen, selected.last_seen) }})
            </span>
          </div>
        </div>
        <button class="ghost" @click="selected = null">← 返回列表</button>
      </div>
      <div class="tabs">
        <button :class="{ active: panel === 'trace' }" @click="panel = 'trace'">轨迹</button>
        <button :class="{ active: panel === 'tools' }" @click="panel = 'tools'">工具</button>
      </div>
    </div>
    <TracePanel v-if="panel === 'trace'" :session-id="selected.session_id" />
    <ToolsPanel v-else :session-id="selected.session_id" />
  </template>
</template>

<style scoped>
.filters {
  display: flex;
  gap: 10px;
  align-items: center;
  margin-bottom: 8px;
}
.filters input {
  max-width: 320px;
}
.hint {
  color: #6b7280;
  font-size: 12px;
  margin: 4px 0 14px;
}
.row-click {
  cursor: pointer;
}
.row-click:hover {
  background: #f9fafb;
}
.head-row {
  display: flex;
  justify-content: space-between;
  align-items: flex-start;
  gap: 12px;
}
.tabs {
  display: flex;
  gap: 6px;
  margin-top: 14px;
}
.tabs button {
  border: 1px solid #d1d5db;
  background: #fff;
  color: #6b7280;
  border-radius: 6px;
  padding: 6px 14px;
  font-size: 13px;
  cursor: pointer;
}
.tabs button.active {
  background: #eef2ff;
  border-color: #c7d2fe;
  color: #27357a;
  font-weight: 600;
}
.mono {
  font-family: ui-monospace, SFMono-Regular, Menlo, monospace;
}
.small {
  font-size: 12px;
}
.strong {
  font-weight: 600;
  margin-bottom: 2px;
}
.tag {
  display: inline-block;
  font-size: 12px;
  padding: 1px 8px;
  border-radius: 999px;
  background: #eef2ff;
  color: #27357a;
  margin-right: 6px;
}
</style>
