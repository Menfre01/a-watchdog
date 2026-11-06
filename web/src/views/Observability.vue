<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { api, type ClientKey, type TraceSession } from '../api'
import AppIcon from '../components/AppIcon.vue'
import AgentIcon from '../components/AgentIcon.vue'
import { formatDateTime, formatSpan } from '../format'
import { buildHash, listHash, parseHash, replaceHash, subscribe, type ObservabilityRoute, type Panel } from '../route'
import { splitSessionID } from '../session'
import MetricsIsland from './MetricsIsland.vue'
import TracePanel from './TracePanel.vue'
import ToolsPanel from './ToolsPanel.vue'

const sessions = ref<TraceSession[]>([])
const keys = ref<ClientKey[]>([])
const loading = ref(true)
const error = ref('')
const query = ref('')
const keyFilter = ref('')
const defaultRange = '24h'
const rangeFilter = ref(defaultRange)
const appliedRange = ref(defaultRange)
const appliedKey = ref('')
const appliedQuery = ref('')
const appliedFrom = ref('')
const appliedRevision = ref(0)
const notFound = ref(false)

const ranges = [
  { value: '1h', label: '1 小时', hours: 1 },
  { value: '24h', label: '24 小时', hours: 24 },
  { value: '7d', label: '7 天', hours: 168 },
  { value: 'all', label: '全部', hours: 0 },
]

// route 是唯一导航状态:selected 与 panel 都由它派生,组件不再自己记。
const route = ref<ObservabilityRoute | null>(parseHash(location.hash))
const session = ref<TraceSession | null>(null)
const selectedID = computed(() => route.value?.sessionID ?? null)
const panel = computed<Panel>(() => route.value?.panel ?? 'trace')

// 列表保活需要的两个锚点:进入前的滚动位置与触发行。
let listScroll = 0
let lastFocusID = ''
let queryTimer: number | null = null
let unsubscribe: (() => void) | null = null
const detailTitle = ref<HTMLElement | null>(null)

// fromFor 把筛选档位换成列表接口的起始时间;全部返回空串。
function fromFor(range: string): string {
  const item = ranges.find((r) => r.value === range)
  if (!item || !item.hours) return ''
  return new Date(Date.now() - item.hours * 3600_000).toISOString()
}

// applyFilters 只应用输入值;指标与列表由已应用值驱动。
function applyFilters(): void {
  if (queryTimer !== null) {
    clearTimeout(queryTimer)
    queryTimer = null
  }
  appliedRange.value = rangeFilter.value
  appliedKey.value = keyFilter.value
  appliedQuery.value = query.value.trim()
  appliedFrom.value = fromFor(rangeFilter.value)
  appliedRevision.value += 1
  void load()
}

// scheduleFilters 让 session 前缀在停止输入后自动应用;时间与 key 直接调用 applyFilters。
function scheduleFilters(): void {
  if (queryTimer !== null) clearTimeout(queryTimer)
  queryTimer = window.setTimeout(() => {
    queryTimer = null
    applyFilters()
  }, 300)
}

async function load(): Promise<void> {
  loading.value = true
  error.value = ''
  try {
    const params = new URLSearchParams()
    if (appliedQuery.value) params.set('q', appliedQuery.value)
    if (appliedKey.value) params.set('key_id', appliedKey.value)
    if (appliedFrom.value) params.set('from', appliedFrom.value)
    const suffix = params.toString()
    sessions.value = await api.get<TraceSession[]>(suffix ? `/console/api/traces?${suffix}` : '/console/api/traces')
  } catch (e) {
    error.value = e instanceof Error ? e.message : String(e)
  } finally {
    loading.value = false
  }
}

// resolveSession 为当前地址找会话摘要:先查已加载的列表,再按标识前缀回查一次。
async function resolveSession(id: string): Promise<void> {
  notFound.value = false
  session.value = sessions.value.find((s) => s.session_id === id) ?? null
  if (session.value) return
  try {
    const raw = splitSessionID(id).raw
    const rows = await api.get<TraceSession[]>(`/console/api/traces?q=${encodeURIComponent(raw)}`)
    session.value = rows.find((s) => s.session_id === id) ?? null
  } catch {
    session.value = null
  }
  notFound.value = session.value === null
}

function focusRow(id: string): void {
  if (!id) return
  const target = [...document.querySelectorAll<HTMLAnchorElement>('a[data-session]')].find(
    (a) => a.dataset.session === id,
  )
  target?.focus()
  target?.scrollIntoView({ block: 'nearest' })
}

// rememberList 记录进入详情前的锚点;整行点击与链接点击都会经过这里。
function rememberList(id: string): void {
  listScroll = window.scrollY
  lastFocusID = id
}

function openSession(s: TraceSession): void {
  rememberList(s.session_id)
  location.hash = buildHash(s.session_id, 'trace')
}

function setPanel(next: Panel): void {
  if (!selectedID.value || panel.value === next) return
  replaceHash(buildHash(selectedID.value, next))
  route.value = parseHash(location.hash)
}

// agentClass 给四类 agent 各配一个标签色相;Other 用中性色。
function agentClass(agent: string): string {
  if (agent === 'claude-code') return 'agent-claude'
  if (agent === 'codex') return 'agent-codex'
  if (agent === 'dsh') return 'agent-dsh'
  return ''
}

function agentLabel(agent: string): string {
  if (agent === 'claude-code') return 'Claude Code'
  if (agent === 'codex') return 'Codex'
  if (agent === 'dsh') return 'DSH'
  return 'Other'
}

// shortID 取原始标识前 8 位:列表只做扫描,核对留给详情。
function shortID(id: string): string {
  const { raw } = splitSessionID(id)
  return raw.length > 8 ? raw.slice(0, 8) + '…' : raw
}

function rawID(id: string): string {
  return splitSessionID(id).raw
}

function keyName(id: string): string {
  const { keyID } = splitSessionID(id)
  return keys.value.find((k) => String(k.id) === keyID)?.name ?? ''
}

function fmtK(n: number): string {
  if (n >= 1000) return (n / 1000).toFixed(n >= 10000 ? 0 : 1) + 'k'
  return String(n)
}

watch(query, (value) => {
  if (value.trim() === appliedQuery.value) return
  scheduleFilters()
})

watch(selectedID, async (id, prev) => {
  if (id === prev) return
  if (id) {
    await resolveSession(id)
    await nextTick()
    detailTitle.value?.focus()
    return
  }
  await nextTick()
  window.scrollTo({ top: listScroll })
  focusRow(lastFocusID)
})

onMounted(async () => {
  unsubscribe = subscribe(() => {
    route.value = parseHash(location.hash)
  })
  try {
    keys.value = await api.get<ClientKey[]>('/console/api/client-keys')
  } catch {
    // 密钥列表取不到时只影响筛选选项,不影响会话列表。
  }
  appliedFrom.value = fromFor(rangeFilter.value)
  await load()
  // 深链首屏:地址里已带会话时,补齐摘要并聚焦标题。
  if (selectedID.value) {
    await resolveSession(selectedID.value)
    await nextTick()
    detailTitle.value?.focus()
  }
})

onBeforeUnmount(() => {
  if (queryTimer !== null) clearTimeout(queryTimer)
  unsubscribe?.()
})
</script>

<template>
  <h1>可观测</h1>
  <div v-if="!selectedID" class="filters">
    <label class="field time-filter">
      时间范围
      <select v-model="rangeFilter" name="trace-range" @change="applyFilters">
        <option v-for="r in ranges" :key="r.value" :value="r.value">{{ r.label }}</option>
      </select>
    </label>
    <label class="field key-filter">
      密钥
      <select v-model="keyFilter" name="trace-key" @change="applyFilters">
        <option value="">全部密钥</option>
        <option v-for="k in keys" :key="k.id" :value="String(k.id)">{{ k.name }}</option>
      </select>
    </label>
    <label class="field grow">
      session 前缀
      <input
        v-model="query"
        name="trace-query"
        type="text"
        autocomplete="off"
        spellcheck="false"
        placeholder="输入后自动筛选"
        @keyup.enter="applyFilters"
      />
    </label>
  </div>
  <MetricsIsland
    v-if="!selectedID"
    class="metrics-island"
    :range="appliedRange"
    :key-id="appliedKey"
    :session-prefix="appliedQuery"
    :revision="appliedRevision"
  />
  <p v-if="error" class="error" role="alert">{{ error }}</p>

  <!-- 列表保活:隐藏时用 inert 断掉键盘与读屏访问 -->
  <div v-show="!selectedID" :inert="selectedID !== null">
    <p class="hint">
      跨会话入口。列表不含轮次数(推断轮次需逐会话比对,成本随会话数放大)。轮次数在轨迹视图给出。
    </p>
    <div v-if="sessions.length" class="card no-pad">
      <div class="table-scroll">
        <table class="table-wide">
          <thead>
            <tr>
              <th scope="col" class="session-head">session</th>
              <th scope="col">Agent</th>
              <th scope="col" class="num-head">首见</th>
              <th scope="col" class="num-head">末见</th>
              <th scope="col" class="num-head">steps</th>
              <th scope="col">in / out / cache</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="s in sessions" :key="s.session_id" class="row-click" @click="openSession(s)">
              <td class="mono session-cell" :title="s.session_id">
                <a
                  class="session-link"
                  :href="buildHash(s.session_id, 'trace')"
                  :data-session="s.session_id"
                  @click="rememberList(s.session_id)"
                >
                  {{ shortID(s.session_id) }}
                </a>
              </td>
              <td>
                <span class="agent-label" :class="agentClass(s.agent)">
                  <AgentIcon :agent="s.agent" />{{ agentLabel(s.agent) }}
                </span>
              </td>
              <td class="small num">{{ formatDateTime(s.first_seen) }}</td>
              <td class="small num">{{ formatDateTime(s.last_seen) }}</td>
              <td class="num">{{ s.steps }}</td>
              <td class="mono small">{{ fmtK(s.usage_in) }} / {{ fmtK(s.usage_out) }} / {{ fmtK(s.usage_cache) }}</td>
            </tr>
          </tbody>
        </table>
      </div>
    </div>
    <p v-else-if="loading" class="muted" role="status" aria-live="polite">加载中…</p>
    <p v-else class="muted">没有会话。先在客户端发起一次请求。</p>
  </div>

  <div v-if="selectedID">
    <p v-if="notFound" class="error" role="alert">
      会话不存在:<span class="mono">{{ selectedID }}</span>。<a :href="listHash">返回列表</a>
    </p>
    <template v-else-if="session">
      <nav class="crumbs" aria-label="位置">
        <a class="crumb-back" :href="listHash">
          <AppIcon name="chevron" />可观测
        </a>
        <span class="crumb-sep" aria-hidden="true">/</span>
        <span ref="detailTitle" class="crumb-current mono" tabindex="-1" :title="session.session_id">
          {{ shortID(session.session_id) }}
        </span>
      </nav>

      <div class="card">
        <div class="head-row">
          <div class="head-main">
            <div class="mono strong" :title="session.session_id">
              {{ rawID(session.session_id) }}
              <span class="agent-label" :class="agentClass(session.agent)">
                <AgentIcon :agent="session.agent" />{{ agentLabel(session.agent) }}
              </span>
              <template v-if="keyName(session.session_id)">
                <span class="meta-sep" aria-hidden="true">·</span>
                <span class="key-label"><AppIcon name="key" :size="13" />{{ keyName(session.session_id) }}</span>
              </template>
            </div>
            <div class="muted small num">
              {{ formatDateTime(session.first_seen) }} → {{ formatDateTime(session.last_seen) }}
              <span v-if="formatSpan(session.first_seen, session.last_seen)">
                (跨度 {{ formatSpan(session.first_seen, session.last_seen) }})
              </span>
            </div>
          </div>
          <div class="segmented" aria-label="会话详情视图">
            <button type="button" :aria-pressed="panel === 'trace'" @click="setPanel('trace')">轨迹</button>
            <button type="button" :aria-pressed="panel === 'tools'" @click="setPanel('tools')">工具</button>
          </div>
        </div>
      </div>
      <TracePanel v-if="panel === 'trace'" :session-id="session.session_id" />
      <ToolsPanel v-else :session-id="session.session_id" />
    </template>
    <p v-else class="muted" role="status" aria-live="polite">加载中…</p>
  </div>
</template>

<style scoped>
.metrics-island {
  margin-bottom: var(--sp-4);
}

.filters {
  display: flex;
  gap: 10px;
  align-items: flex-end;
  margin-bottom: var(--sp-4);
}

.grow {
  max-width: 320px;
  flex: 1;
}

.time-filter {
  max-width: 160px;
}

.key-filter {
  max-width: 200px;
}

.hint {
  margin: 0 0 14px;
}

.card.no-pad {
  padding: 0;
}

.row-click {
  cursor: pointer;
}

.row-click:hover {
  background: var(--surface-2);
}

/* 第一列表头与 ID 共用等宽字体与字号,避免字形差异看起来错位 */
.session-head,
.session-cell {
  font-family: var(--mono);
  font-size: var(--fs-13);
}

/* Agent 标签降为元数据:图标负责识别,文字保持中性 */
.agent-label {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  font-family: var(--sans);
  font-size: var(--fs-12);
  font-weight: 400;
  line-height: 1;
  color: var(--muted);
  vertical-align: middle;
  white-space: nowrap;
}

.agent-label.agent-claude {
  --agent-color: var(--agent-claude);
}

.agent-label.agent-codex {
  --agent-color: var(--agent-codex);
}

.agent-label.agent-dsh {
  --agent-color: var(--agent-dsh);
}

.agent-label :deep(.agent-icon) {
  color: var(--agent-color, var(--muted));
}

.meta-sep {
  display: inline-block;
  margin: 0 2px;
  line-height: 1;
  color: var(--muted);
  vertical-align: middle;
}

.key-label {
  display: inline-flex;
  align-items: center;
  gap: 4px;
  font-family: var(--sans);
  font-size: var(--fs-12);
  font-weight: 400;
  line-height: 1;
  color: var(--muted);
  vertical-align: middle;
  white-space: nowrap;
}

.session-link {
  color: var(--accent-strong);
  text-decoration: none;
}

.session-link:hover {
  text-decoration: underline;
}

/* 面包屑兼作返回入口:图标加文字,比纯文字按钮更容易被找到 */
.crumbs {
  display: flex;
  align-items: center;
  gap: var(--sp-2);
  margin-bottom: var(--sp-3);
  font-size: var(--fs-13);
}

.crumb-back {
  display: inline-flex;
  align-items: center;
  gap: 2px;
  color: var(--accent-strong);
  text-decoration: none;
  padding: 2px 6px 2px 2px;
  border-radius: var(--r-sm);
}

.crumb-back:hover {
  background: var(--accent-soft);
  text-decoration: underline;
}

.crumb-sep {
  color: var(--muted);
}

.crumb-current {
  color: var(--text);
}

.head-row {
  display: flex;
  justify-content: space-between;
  align-items: flex-start;
  gap: var(--sp-3);
}

.head-main {
  min-width: 0;
}

.segmented {
  flex: 0 0 auto;
}
</style>
