<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { api, type TraceStepDetail, type TraceToolCallDetail, type TraceToolCallSummary } from '../api'
import MarkdownView from '../components/MarkdownView.vue'
import ToolIcon from '../components/ToolIcon.vue'
import { buildHash } from '../route'
import { formatFullTime, fmtK, fmtMs } from '../format'

const props = defineProps<{ sessionId: string }>()

const calls = ref<TraceToolCallSummary[]>([])
const error = ref('')
const loading = ref(true)
const nameFilter = ref('')
const statusFilter = ref<'all' | 'ok' | 'error' | 'unseen'>('all')
const srcFilter = ref<'all' | 'local' | 'mcp'>('all')
const sortKey = ref<'time' | 'duration'>('time')
const sortDir = ref<'desc' | 'asc'>('desc')
const visibleLimit = ref(50)
const openIDs = ref<string[]>([])
const callDetails = ref<Record<string, TraceToolCallDetail>>({})
const stepDetails = ref<Record<number, TraceStepDetail>>({})
const detailErrors = ref<Record<string, string>>({})
const loadingDetails = ref<string[]>([])
const openResponses = ref<string[]>([])
const openMessages = ref<string[]>([])
const openRequestSections = ref<string[]>([])
const openResponseSections = ref<string[]>([])

async function load(): Promise<void> {
  loading.value = true
  error.value = ''
  calls.value = []
  openIDs.value = []
  callDetails.value = {}
  stepDetails.value = {}
  detailErrors.value = {}
  openResponses.value = []
  openMessages.value = []
  openRequestSections.value = []
  openResponseSections.value = []
  try {
    calls.value = await api.get<TraceToolCallSummary[]>(
      `/console/api/traces/${encodeURIComponent(props.sessionId)}/tool-calls`,
    )
  } catch (e) {
    error.value = e instanceof Error ? e.message : String(e)
  } finally {
    loading.value = false
  }
}

watch(() => props.sessionId, load, { immediate: true })

// 统计与明细使用同一份合并调用数据。筛选只作用于下方调用明细。
const toolBreakdown = computed(() => {
  const acc = new Map<string, { name: string; calls: number; ok: number; err: number; unseen: number; durations: number[] }>()
  for (const call of calls.value) {
    const row = acc.get(call.tool_name) ?? { name: call.tool_name, calls: 0, ok: 0, err: 0, unseen: 0, durations: [] }
    row.calls += 1
    if (call.status === 'ok') row.ok += 1
    else if (call.status === 'error') row.err += 1
    else row.unseen += 1
    if (call.complete && call.duration_ms !== undefined) row.durations.push(call.duration_ms)
    acc.set(call.tool_name, row)
  }
  return [...acc.values()]
    .map((row) => {
      const total = row.durations.reduce((n, value) => n + value, 0)
      return { ...row, total, p95: row.durations.length ? p95Of(row.durations) : null }
    })
    .sort((a, b) => b.total - a.total || b.calls - a.calls || a.name.localeCompare(b.name))
})

const timedTools = computed(() => toolBreakdown.value.filter((tool) => tool.total > 0))
const soleTimedTool = computed(() => (timedTools.value.length === 1 ? timedTools.value[0] : null))
const toolTotal = computed(() => timedTools.value.reduce((n, tool) => n + tool.total, 0))
const chartColors = ['var(--chart-1)', 'var(--chart-2)', 'var(--chart-3)', 'var(--chart-4)', 'var(--chart-5)', 'var(--chart-6)']

function chartColor(index: number): string {
  return chartColors[index % chartColors.length] as string
}

function toolPct(tool: { total: number }): number {
  return toolTotal.value > 0 ? Math.round((tool.total / toolTotal.value) * 100) : 0
}

function compositionLabel(): string {
  return `工具耗时构成：${timedTools.value.map((tool) => `${tool.name} ${toolPct(tool)}%`).join('，')}；总 ${fmtMs(toolTotal.value)}`
}

const totals = computed(() => {
  let ok = 0
  let err = 0
  let unseen = 0
  for (const call of calls.value) {
    if (call.status === 'ok') ok += 1
    else if (call.status === 'error') err += 1
    else unseen += 1
  }
  return { calls: calls.value.length, ok, err, unseen }
})

const successPct = computed(() => (totals.value.calls ? Math.round((totals.value.ok / totals.value.calls) * 100) : null))

const statusSummary = computed(() => {
  if (!totals.value.calls) return '无工具调用'
  const parts: string[] = []
  if (totals.value.err === 0 && totals.value.unseen === 0) {
    parts.push('全部成功')
  } else {
    if (totals.value.ok > 0) parts.push(`${totals.value.ok} 成功`)
    if (totals.value.err > 0) parts.push(`${totals.value.err} 失败`)
    if (totals.value.unseen > 0) parts.push(`${totals.value.unseen} 未见`)
  }
  if (approvalCount.value > 0) parts.push(`${approvalCount.value} 含审批`)
  return parts.join(' · ')
})

const durationSummary = computed(() => {
  const values = calls.value
    .filter((call) => call.complete && call.duration_ms !== undefined)
    .map((call) => call.duration_ms as number)
  if (!values.length) return { n: 0, avg: 0, p95: 0 }
  const total = values.reduce((n, value) => n + value, 0)
  return { n: values.length, avg: Math.round(total / values.length), p95: p95Of(values) }
})

const approvalCount = computed(() => calls.value.filter((call) => call.approval_wait).length)

const filteredCalls = computed(() => {
  const kw = nameFilter.value.trim().toLowerCase()
  return calls.value.filter((call) => {
    if (kw && !call.tool_name.toLowerCase().includes(kw)) return false
    if (statusFilter.value !== 'all' && call.status !== statusFilter.value) return false
    if (srcFilter.value !== 'all' && call.source !== srcFilter.value) return false
    return true
  })
})

const sortedCalls = computed(() => {
  const out = [...filteredCalls.value]
  out.sort((a, b) => {
    let cmp = 0
    if (sortKey.value === 'duration') {
      const ad = a.complete && a.duration_ms !== undefined ? a.duration_ms : null
      const bd = b.complete && b.duration_ms !== undefined ? b.duration_ms : null
      if (ad === null && bd !== null) return 1
      if (ad !== null && bd === null) return -1
      if (ad !== null && bd !== null) cmp = ad - bd
    } else {
      if (!a.started_at && b.started_at) return 1
      if (a.started_at && !b.started_at) return -1
      cmp = a.started_at.localeCompare(b.started_at)
    }
    return sortDir.value === 'asc' ? cmp : -cmp
  })
  return out
})

const visibleCalls = computed(() => sortedCalls.value.slice(0, visibleLimit.value))
const hasMore = computed(() => sortedCalls.value.length > visibleLimit.value)

watch([nameFilter, statusFilter, srcFilter, sortKey, sortDir], () => {
  visibleLimit.value = 50
  openIDs.value = []
})

function p95Of(values: number[]): number {
  const sorted = [...values].sort((a, b) => a - b)
  const index = Math.min(sorted.length - 1, Math.ceil(sorted.length * 0.95) - 1)
  return sorted[Math.max(0, index)]
}

function sourceLabel(source: string): string {
  return source === 'mcp' || source === 'MCP' ? 'MCP' : '内置'
}

function statusLabel(status: string): string {
  if (status === 'ok') return '成功'
  if (status === 'error') return '失败'
  return '结果未见'
}

function statusClass(status: string): string {
  if (status === 'ok') return 'ok'
  if (status === 'error') return 'danger'
  return 'unknown'
}

function callDurationLabel(call: TraceToolCallSummary): string {
  if (!call.complete || call.duration_ms === undefined) return '—'
  return fmtMs(call.duration_ms)
}

function stepLabel(seq: number): string {
  return seq > 0 ? `#${seq}` : '—'
}

function callTime(call: TraceToolCallSummary): string {
  return call.started_at ? formatFullTime(call.started_at) : '—'
}

function isOpen(id: string): boolean {
  return openIDs.value.includes(id)
}

function detailLoading(id: string): boolean {
  return loadingDetails.value.includes(id)
}

function isResponseOpen(id: string): boolean {
  return openResponses.value.includes(id)
}

function isRequestSectionOpen(id: string): boolean {
  return openRequestSections.value.includes(id)
}

function isResponseSectionOpen(id: string): boolean {
  return openResponseSections.value.includes(id)
}

function toggleRequestSection(id: string): void {
  openRequestSections.value = openRequestSections.value.includes(id)
    ? openRequestSections.value.filter((v) => v !== id)
    : [...openRequestSections.value, id]
}

function toggleResponseSection(id: string): void {
  openResponseSections.value = openResponseSections.value.includes(id)
    ? openResponseSections.value.filter((v) => v !== id)
    : [...openResponseSections.value, id]
}

function toggleResponse(id: string): void {
  openResponses.value = openResponses.value.includes(id)
    ? openResponses.value.filter((v) => v !== id)
    : [...openResponses.value, id]
}

function messageKey(callId: string, index: number): string {
  return `${callId}:m${index}`
}

function isMessageOpen(callId: string, index: number): boolean {
  return openMessages.value.includes(messageKey(callId, index))
}

function toggleMessage(callId: string, index: number): void {
  const key = messageKey(callId, index)
  openMessages.value = openMessages.value.includes(key)
    ? openMessages.value.filter((v) => v !== key)
    : [...openMessages.value, key]
}


async function toggle(call: TraceToolCallSummary): Promise<void> {
  const id = call.tool_call_id
  if (!id) return
  if (isOpen(id)) {
    openIDs.value = openIDs.value.filter((v) => v !== id)
    return
  }
  openIDs.value = [...openIDs.value, id]
  if (callDetails.value[id] || detailLoading(id)) return
  loadingDetails.value = [...loadingDetails.value, id]
  try {
    const detail = await api.get<TraceToolCallDetail>(
      `/console/api/traces/${encodeURIComponent(props.sessionId)}/tool-calls/${encodeURIComponent(id)}`,
    )
    callDetails.value = { ...callDetails.value, [id]: detail }
    if (call.issued_seq > 0 && !stepDetails.value[call.issued_seq]) {
      const step = await api.get<TraceStepDetail>(
        `/console/api/traces/${encodeURIComponent(props.sessionId)}/${call.issued_seq}?full=1&ctx=6`,
      )
      stepDetails.value = { ...stepDetails.value, [call.issued_seq]: step }
    }
  } catch (e) {
    detailErrors.value = { ...detailErrors.value, [id]: e instanceof Error ? e.message : String(e) }
  } finally {
    loadingDetails.value = loadingDetails.value.filter((v) => v !== id)
  }
}

function showMore(): void {
  visibleLimit.value += 50
}

function pretty(value: unknown): string {
  if (value === undefined || value === null) return '—'
  if (typeof value === 'string') return value
  return JSON.stringify(value, null, 2)
}

type BlockLike = {
  type?: unknown
  text?: unknown
  thinking?: unknown
  name?: unknown
  input?: unknown
  content?: unknown
}

function blockText(block: BlockLike): string {
  if (typeof block.text === 'string' && block.text) return block.text
  if (typeof block.thinking === 'string' && block.thinking) return block.thinking
  if (block.type === 'tool_use') return `tool_use ${String(block.name ?? '')} ${pretty(block.input)}`
  if (block.type === 'tool_result') return `tool_result ${pretty(block.content)}`
  return JSON.stringify(block)
}

function blocksSummary(blocks: { type?: unknown }[]): string {
  const label: Record<string, string> = { thinking: 'think', text: 'text', tool_use: 'tool_use' }
  const counts = new Map<string, number>()
  for (const block of blocks) {
    const type = String(block.type ?? '?')
    const name = label[type] ?? type
    counts.set(name, (counts.get(name) ?? 0) + 1)
  }
  return [...counts.entries()].map(([name, n]) => `${name} ×${n}`).join(' · ')
}

function blockBody(block: BlockLike): string {
  if (typeof block.text === 'string' && block.text) return block.text
  if (typeof block.thinking === 'string' && block.thinking) return block.thinking
  return ''
}

function traceLink(seq: number): string {
  const base = buildHash(props.sessionId, 'trace')
  return seq > 0 ? `${base}&step=${seq}` : base
}
</script>

<template>
  <p v-if="error" class="error" role="alert">{{ error }}</p>
  <p v-else-if="loading" class="muted" role="status" aria-live="polite">加载中…</p>
  <template v-else>
    <div class="card">
      <div class="stats">
        <div class="stat"><span class="k">调用</span><span class="v num">{{ totals.calls }}</span></div>
        <div class="stat"><span class="k">成功率</span><span class="v num">{{ successPct === null ? '—' : successPct + '%' }}</span></div>
        <div class="stat"><span class="k">p95 耗时</span><span class="v num">{{ durationSummary.n ? fmtMs(durationSummary.p95) : '—' }}</span></div>
      </div>
      <p class="status-summary">{{ statusSummary }}</p>

      <div v-if="soleTimedTool" class="tool-single">
        <span class="tool-id"><ToolIcon :name="soleTimedTool.name" /><span>{{ soleTimedTool.name }}</span></span>
        <span class="tool-figures">
          {{ soleTimedTool.calls }} 次 · 总 {{ fmtMs(soleTimedTool.total) }} · p95 {{ soleTimedTool.p95 === null ? '—' : fmtMs(soleTimedTool.p95) }} · {{ durationSummary.n }}/{{ totals.calls }} 次有耗时
        </span>
      </div>
      <div v-else-if="timedTools.length > 1" class="tool-composition">
        <div class="composition-head">
          <span>工具耗时构成</span>
          <span>{{ durationSummary.n }}/{{ totals.calls }} 次有耗时 · 总 {{ fmtMs(toolTotal) }}</span>
        </div>
        <div class="composition-track" role="img" :aria-label="compositionLabel()">
          <span
            v-for="(tool, i) in timedTools"
            :key="tool.name"
            class="composition-segment"
            :style="{ width: toolPct(tool) + '%', background: chartColor(i) }"
            :title="`${tool.name} ${toolPct(tool)}% · 总 ${fmtMs(tool.total)}`"
          ></span>
        </div>
        <div class="composition-legend">
          <span v-for="(tool, i) in timedTools" :key="tool.name" class="legend-item">
            <i :style="{ background: chartColor(i) }"></i>
            <span>{{ tool.name }} {{ toolPct(tool) }}% · {{ tool.calls }} 次 · p95 {{ tool.p95 === null ? '—' : fmtMs(tool.p95) }}</span>
          </span>
        </div>
      </div>
      <p v-else class="muted">没有已配对耗时数据。</p>

    </div>

    <div class="card">
      <div class="head">
        <div>
          <div class="title">工具调用明细</div>
          <div class="hint">一次调用一行。展开查看发起 step 的请求与响应，以及工具入参与结果。</div>
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
          <button type="button" :aria-pressed="statusFilter === 'error'" @click="statusFilter = 'error'">失败</button>
          <button type="button" :aria-pressed="statusFilter === 'unseen'" @click="statusFilter = 'unseen'">未见</button>
        </div>
        <div class="segmented" role="group" aria-label="按来源过滤">
          <button type="button" :aria-pressed="srcFilter === 'all'" @click="srcFilter = 'all'">全部来源</button>
          <button type="button" :aria-pressed="srcFilter === 'local'" @click="srcFilter = 'local'">内置</button>
          <button type="button" :aria-pressed="srcFilter === 'mcp'" @click="srcFilter = 'mcp'">MCP</button>
        </div>
      </div>

      <div class="call-toolbar">
        <span class="hint">筛选结果 {{ filteredCalls.length }} / 全会话 {{ calls.length }}</span>
        <div class="segmented" role="group" aria-label="调用排序">
          <button type="button" :aria-pressed="sortKey === 'time'" @click="sortKey = 'time'">按时间</button>
          <button type="button" :aria-pressed="sortKey === 'duration'" @click="sortKey = 'duration'">按耗时</button>
          <button type="button" :aria-pressed="sortDir === 'desc'" @click="sortDir = sortDir === 'desc' ? 'asc' : 'desc'">
            {{ sortDir === 'desc' ? '降序' : '升序' }}
          </button>
        </div>
      </div>

      <div v-if="visibleCalls.length" class="table-scroll">
        <table class="table-wide call-table">
          <colgroup>
            <col class="col-expand" />
            <col class="col-tool" />
            <col />
            <col class="col-state" />
            <col class="col-source" />
            <col class="col-step" />
            <col class="col-step" />
            <col class="col-duration" />
            <col class="col-time" />
          </colgroup>
          <thead>
            <tr>
              <th scope="col" class="expand-col"><span class="sr-only">展开</span></th>
              <th scope="col">工具</th>
              <th scope="col">入参摘要</th>
              <th scope="col">状态</th>
              <th scope="col">来源</th>
              <th scope="col" class="num-head">发起 step</th>
              <th scope="col" class="num-head">结果 step</th>
              <th scope="col" class="num-head">耗时</th>
              <th scope="col" class="num-head">时间</th>
            </tr>
          </thead>
          <tbody>
            <template v-for="call in visibleCalls" :key="call.tool_call_id">
              <tr class="call-row" @click="toggle(call)">
                <td class="expand-col">
                  <button
                    class="expand-btn"
                    type="button"
                    :aria-expanded="isOpen(call.tool_call_id)"
                    :aria-label="isOpen(call.tool_call_id) ? '收起调用上下文' : '展开调用上下文'"
                    @click.stop="toggle(call)"
                  >
                    {{ isOpen(call.tool_call_id) ? '▾' : '▸' }}
                  </button>
                </td>
                <td><span class="tool-name"><ToolIcon :name="call.tool_name" />{{ call.tool_name }}</span></td>
                <td class="input-cell"><span class="call-input" :title="call.input_preview">{{ call.input_preview }}</span></td>
                <td><span class="state" :class="statusClass(call.status)">{{ statusLabel(call.status) }}</span></td>
                <td><span class="tag">{{ sourceLabel(call.source) }}</span></td>
                <td class="num">{{ stepLabel(call.issued_seq) }}</td>
                <td class="num">{{ stepLabel(call.result_seq) }}</td>
                <td class="num">{{ callDurationLabel(call) }}</td>
                <td class="call-time">{{ callTime(call) }}</td>
              </tr>
              <tr v-if="isOpen(call.tool_call_id)" :key="call.tool_call_id + '-detail'">
                <td colspan="9" class="call-detail">
                  <p v-if="detailLoading(call.tool_call_id)" class="muted">加载中…</p>
                  <p v-else-if="detailErrors[call.tool_call_id]" class="error">{{ detailErrors[call.tool_call_id] }}</p>
                  <template v-else-if="callDetails[call.tool_call_id]">
                    <div class="context-meta">
                      <span>发起 step {{ stepLabel(call.issued_seq) }}</span>
                      <span>结果 step {{ stepLabel(call.result_seq) }}</span>
                      <span v-if="call.approval_wait" class="muted">含审批等待</span>
                      <a :href="traceLink(call.issued_seq)">在轨迹中查看</a>
                    </div>
                    <p v-if="call.issued_seq <= 0" class="muted">上下文不可用：没有对应轨迹 step。</p>
                    <div v-else-if="stepDetails[call.issued_seq]" class="trace-context">
                      <section class="region">
                        <button
                          type="button"
                          class="region-toggle"
                          :aria-expanded="isRequestSectionOpen(call.tool_call_id)"
                          @click="toggleRequestSection(call.tool_call_id)"
                        >
                          <span class="caret">{{ isRequestSectionOpen(call.tool_call_id) ? '▾' : '▸' }}</span>
                          <span class="region-title-text">请求</span>
                          <span class="region-meta">{{ stepDetails[call.issued_seq].messages.length }} 条消息</span>
                        </button>
                        <div v-if="isRequestSectionOpen(call.tool_call_id)" class="outer-body">
                          <p v-if="!stepDetails[call.issued_seq].messages.length" class="muted small">（无请求消息）</p>
                          <div v-for="(msg, mi) in stepDetails[call.issued_seq].messages" :key="'m' + mi" class="msg">
                            <button
                              type="button"
                              class="msg-head"
                              :aria-expanded="isMessageOpen(call.tool_call_id, mi)"
                              @click="toggleMessage(call.tool_call_id, mi)"
                            >
                              <span class="caret">{{ isMessageOpen(call.tool_call_id, mi) ? '▾' : '▸' }}</span>
                              <span class="idx">#{{ mi + 1 }}</span>
                              <span class="role" :class="msg.role">{{ msg.role }}</span>
                              <span class="blk">{{ blocksSummary(msg.blocks) }}</span>
                            </button>
                            <div v-if="isMessageOpen(call.tool_call_id, mi)" class="msg-body">
                              <div
                                v-for="(block, bi) in msg.blocks"
                                :key="'b' + bi"
                                class="blk-item"
                                :class="'kind-' + String(block.type)"
                              >
                                <div class="blk-tag">{{ block.type }}</div>
                                <pre v-if="block.type === 'tool_use' || block.type === 'tool_result'" class="raw">{{ blockText(block) }}</pre>
                                <MarkdownView v-else-if="blockText(block)" :text="blockText(block)" />
                                <p v-else class="muted small">（空块）</p>
                              </div>
                            </div>
                          </div>
                        </div>
                      </section>

                      <section class="region">
                        <button
                          type="button"
                          class="region-toggle"
                          :aria-expanded="isResponseSectionOpen(call.tool_call_id)"
                          @click="toggleResponseSection(call.tool_call_id)"
                        >
                          <span class="caret">{{ isResponseSectionOpen(call.tool_call_id) ? '▾' : '▸' }}</span>
                          <span class="region-title-text">响应</span>
                          <span class="region-meta">{{ stepDetails[call.issued_seq].response?.blocks?.length ?? 0 }} 块</span>
                        </button>
                        <div v-if="isResponseSectionOpen(call.tool_call_id)" class="outer-body">
                          <p v-if="!stepDetails[call.issued_seq].response?.recorded" class="hint">响应未记录。</p>
                          <div v-else class="msg">
                            <button
                              type="button"
                              class="msg-head"
                              :aria-expanded="isResponseOpen(call.tool_call_id)"
                              @click="toggleResponse(call.tool_call_id)"
                            >
                              <span class="caret">{{ isResponseOpen(call.tool_call_id) ? '▾' : '▸' }}</span>
                              <span class="role assistant">assistant</span>
                              <span class="blk">{{ blocksSummary(stepDetails[call.issued_seq].response?.blocks ?? []) }}</span>
                              <span class="spacer"></span>
                              <span class="num">{{ stepDetails[call.issued_seq].response?.stop_reason || 'stop —' }}</span>
                              <span class="num w-size">in {{ fmtK(stepDetails[call.issued_seq].response?.usage_in ?? 0) }} / out {{ fmtK(stepDetails[call.issued_seq].response?.usage_out ?? 0) }}</span>
                            </button>
                            <div v-if="isResponseOpen(call.tool_call_id)" class="msg-body">
                              <div
                                v-for="(block, bi) in stepDetails[call.issued_seq].response?.blocks ?? []"
                                :key="'r' + bi"
                                class="blk-item"
                                :class="'kind-' + String(block.type)"
                              >
                                <div class="blk-tag">{{ block.type }}</div>
                                <pre v-if="block.type === 'tool_use'" class="raw">{{ block.name }} {{ pretty(block.input) }}</pre>
                                <MarkdownView v-else-if="blockBody(block)" :text="blockBody(block)" />
                                <p v-else class="muted small">（空块）</p>
                              </div>
                              <p v-if="!stepDetails[call.issued_seq].response?.blocks?.length" class="muted small">（无内容）</p>
                            </div>
                          </div>
                        </div>
                      </section>
                    </div>
                    <div class="payload-stack">
                      <section class="region">
                        <h4 class="region-title">工具入参</h4>
                        <pre class="json">{{ pretty(callDetails[call.tool_call_id].input) }}</pre>
                      </section>
                      <section class="region">
                        <h4 class="region-title">工具结果</h4>
                        <pre class="json">{{ pretty(callDetails[call.tool_call_id].result) }}</pre>
                      </section>
                    </div>
                  </template>
                </td>
              </tr>
            </template>
          </tbody>
        </table>
      </div>
      <p v-else class="muted">没有匹配的调用</p>

      <button v-if="hasMore" class="ghost more" type="button" @click="showMore">
        显示更多（{{ visibleCalls.length }} / {{ sortedCalls.length }}）
      </button>
    </div>
  </template>
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
  margin: 14px 0 8px;
}

.grow {
  max-width: 220px;
  flex: 1;
}

.stats {
  display: flex;
  gap: 22px;
  margin: 0 0 10px;
}

.stats:last-of-type {
  margin-bottom: 8px;
}

.stat {
  display: flex;
  flex-direction: column;
  gap: 2px;
}

.stat .k {
  font-size: var(--fs-11);
  color: var(--muted);
}

.stat .v {
  font-size: var(--fs-16);
  font-weight: 600;
}

.status-summary {
  margin: 0 0 10px;
  color: var(--muted);
  font-size: var(--fs-12);
}

.tool-single {
  display: flex;
  flex-wrap: wrap;
  align-items: baseline;
  justify-content: space-between;
  gap: 4px 12px;
  font-size: var(--fs-12);
}

.tool-single .tool-figures {
  white-space: normal;
}

.tool-composition {
  display: grid;
  gap: 8px;
  margin-top: 4px;
}

.composition-head {
  display: flex;
  justify-content: space-between;
  gap: 12px;
  color: var(--muted);
  font-size: var(--fs-12);
}

.composition-track {
  display: flex;
  width: 100%;
  height: 10px;
  overflow: hidden;
  border-radius: 999px;
  background: var(--surface-2);
}

.composition-segment {
  display: block;
  height: 100%;
  min-width: 2px;
  border-right: 1px solid var(--surface);
}

.composition-segment:last-child {
  border-right: 0;
}

.composition-legend {
  display: flex;
  flex-wrap: wrap;
  gap: 6px 16px;
  color: var(--muted);
  font-size: var(--fs-12);
}

.legend-item {
  display: inline-flex;
  align-items: center;
  gap: 6px;
}

.legend-item i {
  width: 7px;
  height: 7px;
  border-radius: 50%;
  flex: none;
}

.call-toolbar {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  margin: 4px 0 10px;
}

.call-toolbar .hint {
  margin: 0;
}

.call-row {
  cursor: pointer;
}

.call-row:hover {
  background: var(--surface-2);
}

.expand-col {
  width: 28px;
  padding-right: 0;
}

.expand-btn {
  width: 22px;
  height: 22px;
  padding: 0;
  border: 0;
  background: transparent;
  color: var(--muted);
  cursor: pointer;
}

.call-time {
  white-space: nowrap;
  color: var(--muted);
  font-size: var(--fs-12);
  text-align: right;
}

.tool-name {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  white-space: nowrap;
}

.input-cell {
  max-width: 320px;
}

.call-table {
  table-layout: fixed;
}

.call-table .tag {
  white-space: nowrap;
}

.call-table .col-expand {
  width: 34px;
}

.call-table .col-tool {
  width: 110px;
}

.call-table .col-state {
  width: 64px;
}

.call-table .col-source {
  width: 58px;
}

.call-table .col-step {
  width: 84px;
}

.call-table th {
  white-space: nowrap;
}

.call-table .col-duration {
  width: 58px;
}

.call-table .col-time {
  width: 158px;
}

.call-input {
  display: block;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  color: var(--muted);
  font-size: var(--fs-12);
}

.call-detail {
  background: var(--surface);
  padding: 12px;
}

.context-meta {
  display: flex;
  flex-wrap: wrap;
  align-items: baseline;
  gap: 12px;
  color: var(--muted);
  font-size: var(--fs-12);
}

.context-meta a {
  color: var(--accent-strong);
}

.trace-context {
  margin-top: 4px;
}

.region {
  display: block;
  padding: 8px 0 10px;
  border-top: 1px dashed var(--line);
}

.region-title {
  margin: 0 0 8px;
  font-size: var(--fs-12);
  letter-spacing: 0.08em;
  color: var(--muted);
}

.region-toggle {
  display: flex;
  align-items: baseline;
  gap: 6px;
  width: 100%;
  padding: 0;
  border: 0;
  background: transparent;
  color: var(--muted);
  font: inherit;
  text-align: left;
  cursor: pointer;
}

.region-title-text {
  font-size: var(--fs-12);
  letter-spacing: 0.08em;
}

.region-meta {
  color: var(--muted);
  font-size: var(--fs-11);
}

.outer-body {
  margin-top: 6px;
}

.msg {
  background: var(--surface);
  border: 1px solid var(--surface-2);
  border-radius: var(--r-md);
  margin-bottom: 4px;
  overflow: hidden;
}

.msg-head {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 4px 10px;
  width: 100%;
  padding: 5px 8px;
  border: 0;
  background: none;
  color: inherit;
  font: inherit;
  text-align: left;
  cursor: pointer;
}

.msg-head:hover {
  background: var(--surface-2);
}

.msg-head .blk {
  flex: 1 1 200px;
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.msg .num {
  color: var(--muted);
  font-family: var(--mono);
  font-size: var(--fs-11);
  font-variant-numeric: tabular-nums;
}

.msg .spacer {
  flex: 1;
}

.msg .w-size {
  white-space: nowrap;
}

.idx {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 22px;
  height: 22px;
  border-radius: var(--r-md);
  background: var(--surface-2);
  font-size: 12px;
  font-weight: 600;
}

.msg .idx {
  min-width: 34px;
  color: var(--muted);
  font-family: var(--mono);
  font-size: var(--fs-11);
  font-variant-numeric: tabular-nums;
}

.msg .role {
  min-width: 74px;
  color: var(--muted);
  font-family: var(--mono);
  font-size: 12px;
}

.msg .role.user {
  color: var(--accent);
}

.msg .role.assistant {
  color: var(--ok);
}

.msg .role.system {
  color: var(--infer);
}

.msg .role.tool {
  color: var(--tool);
}

.msg .blk {
  color: var(--muted);
  font-family: var(--mono);
  font-size: var(--fs-11);
}

.msg-body {
  padding: 6px 10px 10px 12px;
  border-top: 1px solid var(--surface-2);
  background: var(--surface-2);
}

.blk-item + .blk-item {
  margin-top: 10px;
  padding-top: 10px;
  border-top: 1px dashed var(--line);
}

.blk-tag {
  margin-bottom: 3px;
  color: var(--muted);
  font-family: var(--mono);
  font-size: 10px;
  letter-spacing: 0.03em;
}

.blk-item.kind-thinking .blk-tag,
.blk-item.kind-redacted_thinking .blk-tag {
  color: var(--infer);
}

.blk-item.kind-tool_use .blk-tag {
  color: var(--accent);
}

.blk-item.kind-tool_result .blk-tag {
  color: var(--ok);
}

.msg-body .raw {
  max-height: 320px;
  margin: 6px 0;
  padding: 8px 10px;
  overflow: auto;
  border: 1px solid var(--line);
  border-radius: var(--r-md);
  background: var(--surface-2);
  color: var(--text);
  font-family: var(--mono);
  font-size: 12px;
  line-height: 1.55;
  white-space: pre-wrap;
  word-break: break-word;
}

.msg-body :deep(p) {
  margin: 0;
}

.payload-stack {
  display: grid;
  gap: 12px;
  margin-top: 12px;
}

pre.json {
  max-height: 240px;
  margin: 0;
  padding: 8px;
  overflow: auto;
  border: 1px solid var(--line);
  border-radius: var(--r-sm);
  background: var(--code-bg);
  color: var(--text);
  font-family: var(--mono);
  font-size: var(--fs-11);
  line-height: 1.5;
  white-space: pre-wrap;
  overflow-wrap: anywhere;
}

.more {
  margin-top: 10px;
}

</style>
