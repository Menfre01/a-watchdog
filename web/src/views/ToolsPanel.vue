<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { api, type TraceDigest, type TraceToolCallDuration } from '../api'
import ToolIcon from '../components/ToolIcon.vue'

const props = defineProps<{ sessionId: string }>()

const digest = ref<TraceDigest | null>(null)
const durations = ref<TraceToolCallDuration[]>([])
const error = ref('')
const loading = ref(true)
const nameFilter = ref('')
const statusFilter = ref<'all' | 'ok' | 'err' | 'unseen'>('all')
const srcFilter = ref<'all' | 'local' | 'mcp'>('all')

async function load(): Promise<void> {
  loading.value = true
  error.value = ''
  digest.value = null
  try {
    ;[digest.value, durations.value] = await Promise.all([
      api.get<TraceDigest>(`/console/api/traces/${props.sessionId}/digest`),
      api.get<TraceToolCallDuration[]>(
        `/console/api/tool-events?session_id=${encodeURIComponent(props.sessionId)}`,
      ),
    ])
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

// durationStats 按工具名聚合耗时:只统计已配对的调用,近似值口径见工具事件文档。
const durationStats = computed(() => {
  const acc: Record<string, { n: number; total: number; max: number; approval: boolean; values: number[] }> = {}
  for (const call of durations.value) {
    if (!call.complete || call.duration_ms === undefined) continue
    const row = acc[call.tool_name] ?? { n: 0, total: 0, max: 0, approval: false, values: [] }
    row.n += 1
    row.total += call.duration_ms
    row.values.push(call.duration_ms)
    row.max = Math.max(row.max, call.duration_ms)
    row.approval = row.approval || call.approval_wait
    acc[call.tool_name] = row
  }
  return acc
})

// p95Of 取升序样本的第 95 百分位;样本少于 20 时不展示,避免误导。
function p95Of(values: number[]): number {
  const sorted = [...values].sort((a, b) => a - b)
  const index = Math.min(sorted.length - 1, Math.ceil(sorted.length * 0.95) - 1)
  return sorted[Math.max(0, index)]
}

// durationLabel 给出平均耗时;样本足够时并列 p95,否则标样本数。
function durationLabel(name: string): string {
  const row = durationStats.value[name]
  if (!row || row.n === 0) return '—'
  const avg = Math.round(row.total / row.n)
  if (row.n >= 20) return `${fmtMs(avg)} · p95 ${fmtMs(p95Of(row.values))}`
  return `${fmtMs(avg)} · ${row.n} 次`
}

function durationTitle(name: string): string {
  const row = durationStats.value[name]
  if (!row) return '没有已配对的工具事件'
  const base = `平均 ${fmtMs(Math.round(row.total / row.n))},p95 ${fmtMs(p95Of(row.values))},最长 ${fmtMs(row.max)}`
  return row.approval ? `${base};含审批等待` : base
}

function successRate(tool: { calls: number; ok: number }): string {
  if (!tool.calls) return '—'
  return Math.round((tool.ok / tool.calls) * 100) + '%'
}
</script>

<template>
  <p v-if="error" class="error" role="alert">{{ error }}</p>
  <p v-else-if="loading" class="muted" role="status" aria-live="polite">加载中…</p>
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
      <label class="field grow">
        按工具名过滤
        <input v-model="nameFilter" name="tool-filter" type="text" autocomplete="off" spellcheck="false" />
      </label>
      <div class="segmented" role="group" aria-label="按状态过滤">
        <button type="button" :aria-pressed="statusFilter === 'all'" @click="statusFilter = 'all'">全部状态</button>
        <button type="button" :aria-pressed="statusFilter === 'ok'" @click="statusFilter = 'ok'">成功</button>
        <button type="button" :aria-pressed="statusFilter === 'err'" @click="statusFilter = 'err'">失败</button>
        <button type="button" :aria-pressed="statusFilter === 'unseen'" @click="statusFilter = 'unseen'">未见</button>
      </div>
      <div class="segmented" role="group" aria-label="按来源过滤">
        <button type="button" :aria-pressed="srcFilter === 'all'" @click="srcFilter = 'all'">全部来源</button>
        <button type="button" :aria-pressed="srcFilter === 'local'" @click="srcFilter = 'local'">内置</button>
        <button type="button" :aria-pressed="srcFilter === 'mcp'" @click="srcFilter = 'mcp'">MCP</button>
      </div>
    </div>

    <div class="stats">
      <div class="stat"><span class="k">调用</span><span class="v num">{{ totals.calls }}</span></div>
      <div class="stat"><span class="k">成功</span><span class="v ok num">{{ totals.ok }}</span></div>
      <div class="stat"><span class="k">失败</span><span class="v danger num">{{ totals.err }}</span></div>
      <div class="stat"><span class="k">结果未见</span><span class="v muted num">{{ totals.unseen }}</span></div>
      <div class="stat"><span class="k">工具种类</span><span class="v num">{{ totals.kinds }}</span></div>
    </div>

    <div v-if="rows.length" class="table-scroll">
      <table class="table-wide">
        <thead>
          <tr>
            <th scope="col" class="type-col"><span class="sr-only">类型</span></th>
            <th scope="col">工具</th>
            <th scope="col">去向</th>
            <th scope="col">调用</th>
            <th scope="col">状态分布</th>
            <th scope="col">成功率</th>
            <th scope="col">耗时(近似)</th>
            <th scope="col">间隔(近似)</th>
            <th scope="col">发起轮次</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="tool in rows" :key="tool.name">
            <td class="type-col"><ToolIcon :name="tool.name" /></td>
            <td class="name truncate">{{ tool.name }}</td>
            <td><span class="tag">{{ sourceLabel(tool.name) }}</span></td>
            <td class="num">{{ tool.calls }}</td>
            <td>
              <span class="state ok" :title="`完成 ${tool.ok}`">{{ tool.ok }}</span>
              <span class="state danger" :title="`失败 ${tool.err}`">{{ tool.err }}</span>
              <span class="state unknown" :title="`未知 ${tool.unseen}`">{{ tool.unseen }}</span>
            </td>
            <td class="num">{{ successRate(tool) }}</td>
            <td class="num" :title="durationTitle(tool.name)">{{ durationLabel(tool.name) }}</td>
            <td class="num">{{ gapLabel(tool.gaps_ms) }}</td>
            <td class="num">{{ tool.turns.map((t) => 'T' + t).join(' · ') }}</td>
          </tr>
        </tbody>
      </table>
    </div>
    <p v-else class="muted">没有匹配的调用</p>
  </div>
</template>

<style scoped>
.title {
  font-weight: 600;
}

.hint {
  margin-top: 2px;
}

.filters {
  display: flex;
  gap: 10px;
  align-items: flex-end;
  flex-wrap: wrap;
  margin: 14px 0 4px;
}

.grow {
  max-width: 220px;
  flex: 1;
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
  font-size: var(--fs-11);
  color: var(--muted);
}

.stat .v {
  font-size: var(--fs-16);
  font-weight: 600;
}

.ok {
  color: var(--ok);
}

.danger {
  color: var(--danger);
}

.bar {
  display: inline-flex;
  height: 6px;
  width: 80px;
  border-radius: 3px;
  overflow: hidden;
  background: var(--surface-2);
  vertical-align: middle;
}

.bar i {
  display: block;
}

.b-ok {
  background: var(--ok);
}

.b-err {
  background: var(--danger);
}

.b-un {
  background: var(--muted);
}
</style>
