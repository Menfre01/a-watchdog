<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { api, type TraceDigest } from '../api'

const props = defineProps<{ sessionId: string }>()

const digest = ref<TraceDigest | null>(null)
const error = ref('')
const loading = ref(true)
const nameFilter = ref('')
const statusFilter = ref<'all' | 'ok' | 'err' | 'unseen'>('all')
const srcFilter = ref<'all' | 'local' | 'mcp'>('all')

async function load() {
  loading.value = true
  error.value = ''
  digest.value = null
  try {
    digest.value = await api.get<TraceDigest>(`/console/api/traces/${props.sessionId}/digest`)
  } catch (e) {
    error.value = e instanceof Error ? e.message : String(e)
  } finally {
    loading.value = false
  }
}

watch(() => props.sessionId, load, { immediate: true })

// rows 是会话级的筛选结果:数据已在内存,不需要额外的查询参数。
const rows = computed(() => {
  const all = digest.value?.tools ?? []
  const kw = nameFilter.value.trim().toLowerCase()
  return all.filter((tool) => {
    if (kw && !tool.name.toLowerCase().includes(kw)) return false
    if (statusFilter.value !== 'all' && tool[statusFilter.value] <= 0) return false
    if (srcFilter.value === 'mcp' && !tool.name.startsWith('mcp__')) return false
    if (srcFilter.value === 'local' && tool.name.startsWith('mcp__')) return false
    return true
  })
})

const totals = computed(() => {
  const sum = (key: 'calls' | 'ok' | 'err' | 'unseen') => rows.value.reduce((n, tool) => n + tool[key], 0)
  return { calls: sum('calls'), ok: sum('ok'), err: sum('err'), unseen: sum('unseen'), kinds: rows.value.length }
})

function sourceLabel(name: string): string {
  return name.startsWith('mcp__') ? 'MCP' : '内置'
}

// gapLabel 汇总该工具的近似间隔:口径是轮间间隔,含客户端执行与人的思考时间。
function gapLabel(gaps: number[]): string {
  if (!gaps.length) return '—'
  const min = Math.min(...gaps)
  const max = Math.max(...gaps)
  if (min === max) return fmtMs(min)
  return `${fmtMs(min)} ~ ${fmtMs(max)}`
}

function fmtMs(ms: number): string {
  if (ms >= 60000) return Math.floor(ms / 60000) + 'm' + Math.round((ms % 60000) / 1000) + 's'
  if (ms >= 1000) return (ms / 1000).toFixed(1) + 's'
  return ms + 'ms'
}

function successRate(tool: { calls: number; ok: number }): string {
  if (!tool.calls) return '—'
  return Math.round((tool.ok / tool.calls) * 100) + '%'
}
</script>

<template>
  <div v-if="error" class="error">{{ error }}</div>
  <p v-else-if="loading" class="muted">加载中...</p>
  <div v-else-if="digest" class="card">
    <div class="head">
      <div>
        <div class="title">工具调用 · 本会话</div>
        <div class="hint">
          会话级聚合;间隔为轮间近似(含客户端执行与人的思考时间),「结果未见」的调用不计入间隔。
        </div>
      </div>
    </div>

    <div class="filters">
      <input v-model="nameFilter" type="text" placeholder="按工具名过滤" />
      <div class="seg">
        <button :class="{ on: statusFilter === 'all' }" @click="statusFilter = 'all'">全部状态</button>
        <button :class="{ on: statusFilter === 'ok' }" @click="statusFilter = 'ok'">成功</button>
        <button :class="{ on: statusFilter === 'err' }" @click="statusFilter = 'err'">失败</button>
        <button :class="{ on: statusFilter === 'unseen' }" @click="statusFilter = 'unseen'">未见</button>
      </div>
      <div class="seg">
        <button :class="{ on: srcFilter === 'all' }" @click="srcFilter = 'all'">全部来源</button>
        <button :class="{ on: srcFilter === 'local' }" @click="srcFilter = 'local'">内置</button>
        <button :class="{ on: srcFilter === 'mcp' }" @click="srcFilter = 'mcp'">MCP</button>
      </div>
    </div>

    <div class="stats">
      <div class="stat"><span class="k">调用</span><span class="v">{{ totals.calls }}</span></div>
      <div class="stat"><span class="k">成功</span><span class="v ok">{{ totals.ok }}</span></div>
      <div class="stat"><span class="k">失败</span><span class="v danger">{{ totals.err }}</span></div>
      <div class="stat"><span class="k">结果未见</span><span class="v muted">{{ totals.unseen }}</span></div>
      <div class="stat"><span class="k">工具种类</span><span class="v">{{ totals.kinds }}</span></div>
    </div>

    <table v-if="rows.length">
      <thead>
        <tr>
          <th>工具</th>
          <th>去向</th>
          <th>调用</th>
          <th>状态分布</th>
          <th>成功率</th>
          <th>间隔(近似)</th>
          <th>发起轮次</th>
        </tr>
      </thead>
      <tbody>
        <tr v-for="tool in rows" :key="tool.name">
          <td class="mono">{{ tool.name }}</td>
          <td><span class="tag">{{ sourceLabel(tool.name) }}</span></td>
          <td>{{ tool.calls }}</td>
          <td>
            <span class="bar" :title="`成功 ${tool.ok} · 失败 ${tool.err} · 未见 ${tool.unseen}`">
              <i class="b-ok" :style="{ width: (tool.ok / tool.calls) * 100 + '%' }"></i>
              <i class="b-err" :style="{ width: (tool.err / tool.calls) * 100 + '%' }"></i>
              <i class="b-un" :style="{ width: (tool.unseen / tool.calls) * 100 + '%' }"></i>
            </span>
          </td>
          <td>{{ successRate(tool) }}</td>
          <td>{{ gapLabel(tool.gaps_ms) }}</td>
          <td>{{ tool.turns.map((t) => 'T' + t).join(' · ') }}</td>
        </tr>
      </tbody>
    </table>
    <p v-else class="muted">没有匹配的调用</p>
  </div>
</template>

<style scoped>
.card {
  background: #fff;
  border: 1px solid #e5e7eb;
  border-radius: 8px;
  padding: 14px 16px;
}
.title {
  font-weight: 600;
}
.hint {
  font-size: 12px;
  color: #6b7280;
  margin-top: 2px;
}
.filters {
  display: flex;
  gap: 10px;
  align-items: center;
  flex-wrap: wrap;
  margin: 14px 0 4px;
}
.filters input {
  max-width: 220px;
}
.seg {
  display: inline-flex;
  border: 1px solid #d1d5db;
  border-radius: 6px;
  overflow: hidden;
}
.seg button {
  border: 0;
  border-left: 1px solid #d1d5db;
  background: #fff;
  color: #6b7280;
  font-size: 12px;
  padding: 5px 11px;
  cursor: pointer;
}
.seg button:first-child {
  border-left: 0;
}
.seg button.on {
  background: #eef2ff;
  color: #27357a;
  font-weight: 600;
}
.stats {
  display: flex;
  gap: 22px;
  margin: 12px 0;
}
.stat {
  display: flex;
  flex-direction: column;
}
.stat .k {
  font-size: 11px;
  color: #6b7280;
  text-transform: uppercase;
}
.stat .v {
  font-size: 16px;
  font-weight: 600;
}
.ok {
  color: #127a4b;
}
.danger {
  color: #b42318;
}
.muted {
  color: #6b7280;
}
.mono {
  font-family: ui-monospace, SFMono-Regular, Menlo, monospace;
}
.tag {
  display: inline-block;
  font-size: 11px;
  padding: 0 7px;
  border-radius: 999px;
  border: 1px solid #e5e7eb;
  color: #6b7280;
}
.bar {
  display: inline-flex;
  height: 6px;
  width: 80px;
  border-radius: 3px;
  overflow: hidden;
  background: #eef1f5;
  vertical-align: middle;
}
.bar i {
  display: block;
}
.b-ok {
  background: #34a06a;
}
.b-err {
  background: #d4574a;
}
.b-un {
  background: #b8c0cc;
}
</style>
