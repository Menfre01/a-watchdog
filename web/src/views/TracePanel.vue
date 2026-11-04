<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import {
  api,
  type TraceDiffHunk,
  type TraceDiffRow,
  type TraceDigest,
  type TraceMessage,
  type TraceSkeleton,
  type TraceStep,
  type TraceToolCallDuration,
  type TraceStepDetail,
  type TraceTurn,
} from '../api'
import { fmtK, fmtMs, formatFullTime } from '../format'
import MarkdownView from '../components/MarkdownView.vue'
import {
  AXIS_NOTE,
  CHAIN_NOTE,
  HARNESS_NOTE,
  MIXED_PROTOCOL_NOTE,
  START_NOTE,
  TURN_CHAIN_NOTE,
  USAGE_NOTE,
} from '../trailLegend'

const props = defineProps<{ sessionId: string }>()

const skeleton = ref<TraceSkeleton | null>(null)
const digest = ref<TraceDigest | null>(null)
const error = ref('')
const loading = ref(true)
const openTurns = ref<number[]>([])
const openSteps = ref<number[]>([])
// 阅读导航:正序/倒序、当前轮次、浮动按钮与迷你地图。
const orderDesc = ref(localStorage.getItem('awatchdog.trace.order') === 'desc')
const currentTurn = ref(0)
const showQuick = ref(false)
const jumpValue = ref('')
const turnEls = new Map<number, HTMLElement>()

const orderedTurns = computed(() => {
  const turns = skeleton.value?.turns ?? []
  return orderDesc.value ? [...turns].reverse() : turns
})

const openAny = computed(() => openTurns.value.length > 0 || openSteps.value.length > 0)

const turnPosition = computed(() => {
  const pos = orderedTurns.value.findIndex((turn) => turn.index === currentTurn.value)
  return pos < 0 ? 0 : pos + 1
})

function setTurnEl(index: number, el: unknown): void {
  if (el instanceof HTMLElement) turnEls.set(index, el)
  else turnEls.delete(index)
}

function toggleOrder(): void {
  orderDesc.value = !orderDesc.value
  localStorage.setItem('awatchdog.trace.order', orderDesc.value ? 'desc' : 'asc')
}

function focusTurn(index: number): void {
  if (!openTurns.value.includes(index)) openTurns.value = [...openTurns.value, index]
  void nextTick(() => {
    const el = turnEls.get(index)
    if (!el) return
    const top = el.getBoundingClientRect().top + window.scrollY - 104
    window.scrollTo({ top, behavior: 'smooth' })
  })
}

function stepTurn(delta: number): void {
  const list = orderedTurns.value
  if (!list.length) return
  const pos = list.findIndex((turn) => turn.index === currentTurn.value)
  const next = Math.min(list.length - 1, Math.max(0, (pos < 0 ? 0 : pos) + delta))
  focusTurn(list[next].index)
}

function jumpTurn(): void {
  const n = Number.parseInt(jumpValue.value, 10)
  if (!Number.isFinite(n) || n < 1 || n > orderedTurns.value.length) return
  focusTurn(orderedTurns.value[n - 1].index)
  jumpValue.value = ''
}

function collapseAll(): void {
  openTurns.value = []
  openSteps.value = []
  openMsgs.value = {}
  respOpen.value = []
  toolOpen.value = []
  window.scrollTo({ top: 0, behavior: 'smooth' })
}

function scrollTop(): void {
  window.scrollTo({ top: 0, behavior: 'smooth' })
}

function onScroll(): void {
  showQuick.value = window.scrollY > 280
  let visible = currentTurn.value
  for (const [index, el] of turnEls) {
    if (el.getBoundingClientRect().top <= 140) visible = index
  }
  currentTurn.value = visible
}

onMounted(() => {
  window.addEventListener('scroll', onScroll, { passive: true })
  onScroll()
})

onBeforeUnmount(() => window.removeEventListener('scroll', onScroll))

// autoOpened 记录已自动展开过失败步的轮次:「首次展开」只做一次,
// 之后不再改动读者的折叠选择。它不随数据重载清空。
const autoOpened = ref<number[]>([])
const details = ref<Record<number, TraceStepDetail>>({})
// respOpen 与 toolOpen 记录展开状态:与 messages 用同一种折叠行。
const respOpen = ref<number[]>([])
const toolOpen = ref<string[]>([])

function toggleResp(seq: number): void {
  respOpen.value = respOpen.value.includes(seq)
    ? respOpen.value.filter((v) => v !== seq)
    : [...respOpen.value, seq]
}

function isRespOpen(seq: number): boolean {
  return respOpen.value.includes(seq)
}

function toolKey(seq: number, index: number): string {
  return `${seq}-${index}`
}

function toggleTool(key: string): void {
  toolOpen.value = toolOpen.value.includes(key)
    ? toolOpen.value.filter((v) => v !== key)
    : [...toolOpen.value, key]
}

function isToolOpen(key: string): boolean {
  return toolOpen.value.includes(key)
}

// respBlocks 返回响应的有序内容块;旧数据没有块时为空。
function respBlocks(seq: number) {
  const response = respOf(seq)
  if (!response) return []
  if (response.blocks && response.blocks.length > 0) return response.blocks
  // 旧数据没有块:按文本、思考、工具请求拼出等价次序。
  const blocks: { type: string; text?: string; thinking?: string; name?: string }[] = []
  if (response.thinking) blocks.push({ type: 'thinking', thinking: response.thinking })
  if (response.text) blocks.push({ type: 'text', text: response.text })
  for (const name of response.tool_uses ?? []) blocks.push({ type: 'tool_use', name })
  return blocks
}

// respSummary 与 messages 一致:按块类型计数。
function respSummary(seq: number): string {
  const blocks = respBlocks(seq)
  if (blocks.length === 0) {
    const text = respText(seq).replace(/\s+/g, ' ').trim()
    return text ? oneLine(text, 60) : '(无内容)'
  }
  return blocksSummary(blocks)
}

// resppEmpty 判断响应是否没有可展示的块。
function resspEmpty(seq: number): boolean {
  return respBlocks(seq).length === 0
}

// blockBody 给块取可直接展示的文本。
function blockBody(block: { type: string; text?: string; thinking?: string; input?: unknown }): string {
  if (block.type === 'text') return block.text ?? ''
  if (block.type === 'thinking') return block.thinking ?? ''
  return structuredText(block.input) || fmtValue(block.input)
}
const toolCalls = ref<TraceToolCallDuration[]>([])
const detailErrors = ref<Record<number, string>>({})
const fullMode = ref<Record<number, boolean>>({})
// diffView 是差异块的呈现方式:单栏(unified)/ 双栏(split),全局共享一次切换。
const diffView = ref<'unified' | 'split'>('unified')
// msgView 是「消息区」的呈现选择:差异 / 新增消息。
// 差异块的 `+` 侧就是新增消息本身,两者同时铺开会重复显示同一批行号。
const msgView = ref<'diff' | 'added'>('diff')

// effectiveMsgView 决定该步消息区显示什么。
// 差异块在详情里(detail.diff),不在骨架的 step 上——判据要看详情。
function effectiveMsgView(seq: number): 'diff' | 'added' {
  if (msgView.value !== 'diff') return 'added'
  return details.value[seq]?.diff?.length ? 'diff' : 'added'
}
// ctxLines 是当前请求的上下文行数:默认 3(与 git 一致),展开时递增。
const ctxLines = ref(3)
// openMsgs 记录被手动折叠/展开的消息(key 为 `${seq}:${index}`);
// 未记录时按体积取默认值——短消息直接铺开,长内容收起。
const openMsgs = ref<Record<string, boolean>>({})
// turnTab 是轮次内部的 tab 选择,全局共享:0 表示主路,大于 0 表示某条旁路链。
// 一次切换即可横向对比各轮,同时保留"哪些旁路属于这一轮"的对应关系。
const turnTab = ref(0)

// resetViewState 只在切换会话时清空视图状态:数据重载不清空展开集合,
// 也不改滚动位置,否则读者正在看的内容会被打断(admin-console 规范「阅读连续性」)。
function resetViewState() {
  skeleton.value = null
  digest.value = null
  details.value = {}
  detailErrors.value = {}
  openTurns.value = []
  openSteps.value = []
  autoOpened.value = []
}

// load 先取骨架(指纹级,含判定理由),再取 digest(标题与工具聚合,需解析各步新增行)。
// 骨架先渲染,digest 到达后填充——对应 spec 的渐进填充。
async function load() {
  loading.value = true
  error.value = ''
  try {
    ;[skeleton.value, digest.value, toolCalls.value] = await Promise.all([
      api.get<TraceSkeleton>(`/console/api/traces/${props.sessionId}`),
      api.get<TraceDigest>(`/console/api/traces/${props.sessionId}/digest`),
      api.get<TraceToolCallDuration[]>(`/console/api/tool-events?session_id=${encodeURIComponent(props.sessionId)}`),
    ])
  } catch (e) {
    error.value = e instanceof Error ? e.message : String(e)
  } finally {
    loading.value = false
  }
}

watch(
  () => props.sessionId,
  () => {
    resetViewState()
    void load()
  },
  { immediate: true },
)

const turnMeta = computed(() => {
  const out: Record<number, { title: string; calls: number }> = {}
  for (const row of digest.value?.by_turn ?? []) {
    out[row.turn] = { title: row.title, calls: row.calls }
  }
  return out
})

// mainStepsOf / sideStepsOf 拆出轮次里的两类步。
function mainStepsOf(turn: TraceTurn): TraceStep[] {
  return turn.steps.filter((step) => !step.side)
}

function sideStepsOf(turn: TraceTurn): TraceStep[] {
  return turn.steps.filter((step) => step.side)
}

// sideChainsOf 列出该轮出现的旁路链编号(升序)。每条链在轮次内单独占一个 tab:
// 实测一个会话里可能有「生成标题」与「安全监控」两条互不相干的旁路链。
function sideChainsOf(turn: TraceTurn): number[] {
  const seen = new Set(sideStepsOf(turn).map((step) => step.side_chain ?? 0))
  return [...seen].sort((a, b) => a - b)
}

// chainStepCount 数该轮内某条旁路链的步数。
function chainStepCount(turn: TraceTurn, chain: number): number {
  return sideStepsOf(turn).filter((step) => (step.side_chain ?? 0) === chain).length
}

// effectiveTab 返回该轮实际生效的 tab:选中的旁路链若不在本轮,退回主路,
// 避免"切到某条链后翻到别的轮次看到空列表"。
function effectiveTab(turn: TraceTurn): number {
  if (turnTab.value === 0) return 0
  return sideChainsOf(turn).includes(turnTab.value) ? turnTab.value : 0
}

function displayStepsOf(turn: TraceTurn): TraceStep[] {
  const tab = effectiveTab(turn)
  if (tab === 0) return mainStepsOf(turn)
  return sideStepsOf(turn).filter((step) => (step.side_chain ?? 0) === tab)
}

function titleFor(index: number): string {
  return turnMeta.value[index]?.title ?? ''
}

function callsFor(index: number): string {
  const calls = turnMeta.value[index]?.calls
  return calls === undefined ? '…' : String(calls)
}

function toggleTurn(index: number) {
  const opening = !openTurns.value.includes(index)
  openTurns.value = opening ? [...openTurns.value, index] : openTurns.value.filter((i) => i !== index)
  if (!opening || autoOpened.value.includes(index)) return
  // 首次展开:直接打开含失败的步,失败不藏在折叠行后面。
  autoOpened.value = [...autoOpened.value, index]
  const turn = skeleton.value?.turns.find((t) => t.index === index)
  if (!turn) return
  for (const step of failedStepsOf(turn)) {
    if (!openSteps.value.includes(step.seq)) void toggleStep(step.seq)
  }
}

// isFailedStep 是失败的唯一判据:只取记录侧字段(状态码 ≥400 或 error_kind 非空)。
// 失败清单、时间轴条的颜色都走它,避免两处判据漂移。
// REGRESSION:曾用 statusClass(≥500 才算危险)给条上色,导致 429 在失败清单里是失败、
// 在轴上却是正常色——与形态基准 docs/trail-view.html 不一致。无法单测:前端没有测试运行器,
// 由原型冒烟的形态断言约束。
function isFailedStep(step: TraceStep): boolean {
  return step.status_code >= 400 || !!step.error_kind
}

// failedStepsOf 列出本轮的失败步。
function failedStepsOf(turn: TraceTurn): TraceStep[] {
  return turn.steps.filter(isFailedStep)
}

// failureText 给出失败原因:网关侧归因优先,否则给状态码。
function failureText(step: TraceStep): string {
  return step.error_kind ? `网关侧 ${step.error_kind}` : `状态码 ${step.status_code}`
}

// jumpTo 跳到某一步:必要时打开轮次、切到该步所在的链,再滚动到它并居中。
async function jumpTo(seq: number) {
  const turn = skeleton.value?.turns.find((t) => t.steps.some((s) => s.seq === seq))
  const step = turn?.steps.find((s) => s.seq === seq)
  if (!turn || !step) return
  if (!openTurns.value.includes(turn.index)) openTurns.value = [...openTurns.value, turn.index]
  turnTab.value = step.side ? (step.side_chain ?? 0) : 0
  if (!openSteps.value.includes(seq)) await toggleStep(seq)
  await nextTick()
  document.getElementById(stepDomId(seq))?.scrollIntoView({ block: 'center' })
}

// stepDomId 是步容器的 DOM id:失败清单与叙述段共用。
function stepDomId(seq: number): string {
  return `step-${seq}`
}

// jumpToPart 处理叙述段的点击:只有带 seq 的段可跳转。
function jumpToPart(part: { seq?: number }) {
  if (part.seq !== undefined) void jumpTo(part.seq)
}

// harnessLabel 给出该步之后的 harness 侧耗时;链末步没有后继,显示「—」。
function harnessLabel(step: TraceStep): string {
  return step.harness_ms === undefined ? '—' : fmtMs(step.harness_ms)
}

// harnessClipped 判断该段是否越过轴末端:同链下一步落在下一轮时会越界。
function harnessClipped(step: TraceStep, turn: TraceTurn): boolean {
  const total = turn.duration_ms ?? 0
  if (!total || step.at_ms === undefined || step.harness_ms === undefined) return false
  return step.at_ms + step.latency_ms + step.harness_ms > total
}

// harnessTitle 给出斜纹段的说明:口径常驻;越界时补一句「延续到下一轮」。
function harnessTitle(step: TraceStep, turn: TraceTurn): string {
  return harnessClipped(step, turn) ? `${HARNESS_NOTE}本段止于轴末端,该值延续到下一轮。` : HARNESS_NOTE
}

// harnessStyle 把 harness 侧耗时换算成斜纹段的位置与长度。
// 段从本步条的右端开始;越过轴末端时裁到末端,数值仍由文字给出。
// 值为 0(推算结果为负时按 0)或缺席时不画段。
function harnessStyle(step: TraceStep, turn: TraceTurn) {
  const total = turn.duration_ms ?? 0
  if (!total || step.at_ms === undefined || !step.harness_ms) return { display: 'none' }
  const left = Math.max(0, Math.min(100, ((step.at_ms + step.latency_ms) / total) * 100))
  const width = Math.min((step.harness_ms / total) * 100, 100 - left)
  if (width <= 0) return { display: 'none' }
  return { left: `${left.toFixed(2)}%`, width: `${width.toFixed(2)}%` }
}

// stepsHaveAxis 判断该轮能否画时间轴:服务端给出了偏移,且总时长大于 0。
function stepsHaveAxis(turn: TraceTurn): boolean {
  return (turn.duration_ms ?? 0) > 0 && turn.steps.some((step) => step.at_ms !== undefined)
}

// durationLabel 给出轮次总时长;不可用时显示「—」,不显示 0。
function durationLabel(turn: TraceTurn): string {
  const total = turn.duration_ms ?? 0
  return total > 0 ? fmtMs(total) : '—'
}

// barStyle 把偏移与耗时换算成条的位置与长度。亚秒步保底 0.6%,
// 否则它在轴上不可见;条宽只作位置指示,数值一律由文字给出。
function barStyle(step: TraceStep, turn: TraceTurn) {
  const total = turn.duration_ms ?? 0
  if (total <= 0 || step.at_ms === undefined) return { display: 'none' }
  const left = Math.max(0, Math.min(100, (step.at_ms / total) * 100))
  const width = Math.max(0.6, (step.latency_ms / total) * 100)
  return { left: `${left.toFixed(2)}%`, width: `${Math.min(width, 100 - left).toFixed(2)}%` }
}

// hotspots 标出每轮最慢的步与最多 token 的步。步之间没有差异时不标注:
// 全部相同时标注没有信息量。
const hotspots = computed<Record<number, string[]>>(() => {
  const out: Record<number, string[]> = {}
  for (const turn of skeleton.value?.turns ?? []) {
    const steps = turn.steps
    if (steps.length < 2) continue
    const latencies = steps.map((s) => s.latency_ms)
    if (Math.max(...latencies) > Math.min(...latencies)) {
      const slow = steps.reduce((a, b) => (b.latency_ms > a.latency_ms ? b : a))
      addHot(out, slow.seq, '最慢')
    }
    const tokens = steps.map(tokenSum)
    if (Math.max(...tokens) > Math.min(...tokens)) {
      const heavy = steps.reduce((a, b) => (tokenSum(b) > tokenSum(a) ? b : a))
      addHot(out, heavy.seq, '最多 token')
    }
  }
  return out
})

function tokenSum(step: TraceStep): number {
  return (step.usage_in ?? 0) + (step.usage_out ?? 0)
}

function addHot(out: Record<number, string[]>, seq: number, label: string) {
  out[seq] = [...(out[seq] ?? []), label]
}

function hotLabels(seq: number): string[] {
  return hotspots.value[seq] ?? []
}

// respOf 返回该步的响应记录;未记录时为空。
function respOf(seq: number) {
  return details.value[seq]?.response
}
function respRecorded(seq: number): boolean {
  return respOf(seq)?.recorded === true
}
function respText(seq: number): string {
  return respOf(seq)?.text ?? ''
}
function respThinking(seq: number): string {
  return respOf(seq)?.thinking ?? ''
}
function respTools(seq: number): string[] {
  return respOf(seq)?.tool_uses ?? []
}
function respStop(seq: number): string {
  return respOf(seq)?.stop_reason ?? ''
}
function respIn(seq: number): number {
  return respOf(seq)?.usage_in ?? 0
}
function respOut(seq: number): number {
  return respOf(seq)?.usage_out ?? 0
}
function respTruncated(seq: number): boolean {
  return respOf(seq)?.truncated === true
}
function respIncomplete(seq: number): boolean {
  return respOf(seq)?.complete === false
}

// reportedOf 取该工具的客户端上报记录;没有事件时为空。
function reportedOf(tool: { tool_call_id?: string }) {
  if (!tool.tool_call_id) return undefined
  return toolCalls.value.find((c) => c.tool_call_id === tool.tool_call_id)
}

// toolStatusLabel 区分完成与失败:完成不等于命令退出码为 0。
function toolStatusLabel(status: string): string {
  if (status === 'ok') return '完成'
  if (status === 'error') return '失败'
  return '未知'
}

// toolInputOf 优先用上报入参,缺失时用轨迹复原值。
function toolInputOf(tool: { tool_call_id?: string; input: unknown }): unknown {
  const reported = reportedOf(tool)
  return reported?.input !== undefined ? reported.input : tool.input
}

// toolResultOf 优先用上报结果,缺失时用轨迹复原值。
function toolResultOf(tool: { tool_call_id?: string; result: unknown }): unknown {
  const reported = reportedOf(tool)
  return reported?.result !== undefined ? reported.result : tool.result
}

// toolArgs 从入参里取最能说明动作的字段,截断成一行。没有可读字段时退回第一个键。
function toolArgs(tool: { tool_call_id?: string; input: unknown }): string {
  const input = toolInputOf(tool)
  if (typeof input === 'string') return oneLine(input, 72)
  if (!input || typeof input !== 'object') return '—'
  const obj = input as Record<string, unknown>
  for (const key of ['command', 'file_path', 'path', 'pattern', 'query', 'url', 'description', 'prompt']) {
    const value = obj[key]
    if (typeof value === 'string' && value.trim()) return oneLine(value, 72)
  }
  const first = Object.keys(obj)[0]
  if (!first) return '—'
  const value = obj[first]
  return typeof value === 'string' ? oneLine(value, 72) : `${first}: ${oneLine(JSON.stringify(value), 60)}`
}

// oneLine 折叠空白并截断,保证条目只占一行。
function oneLine(text: string, limit: number): string {
  const flat = text.replace(/\s+/g, ' ').trim()
  return flat.length > limit ? flat.slice(0, limit - 1) + '…' : flat
}

// toolCost 用工具事件给出实测耗时;没有事件时标为未知。
function toolCost(tool: { tool_call_id?: string }): string {
  const call = toolCalls.value.find((c) => c.tool_call_id === tool.tool_call_id)
  if (!call || !call.complete || call.duration_ms === undefined) return '—'
  return fmtMs(call.duration_ms)
}

// storyOf 给出该轮的一行叙述:骨架段 + 工具段。
// 工具调用数属渐进填充层:未取到时省略该段,不显示 0。
function storyOf(turn: TraceTurn): { text: string; seq?: number }[] {
  const parts: { text: string; seq?: number }[] = [...(turn.story ?? [])]
  const calls = turnMeta.value[turn.index]?.calls
  if (typeof calls === 'number' && calls > 0) {
    parts.splice(Math.min(1, parts.length) + 1, 0, { text: `${calls} 次工具` })
  }
  return parts
}

// parseCostOf 给出该轮工具数所需的解析代价(行数),替代「需解析」这类无代价说法。
function parseCostOf(turn: TraceTurn): number {
  return turn.steps.reduce((n, step) => n + (step.delta?.added ?? 0), 0)
}

// chainsLabel 给出链汇总:主链恒为 1 条,其余为旁路链。
// 退化(缺 system 指纹)时显示「—」:退化的「1 条链」不是事实。
const chainsLabel = computed(() => {
  const chains = skeleton.value?.chains
  if (!chains || chains.degraded || chains.main === undefined || chains.side === undefined) return '—'
  return `${chains.main} + ${chains.side}`
})

// composition 只保留两段合计:模型实测与工具事件边界。
// 输入与空闲仍由客户端采集,但不进构成;超过 5 秒的空档改为数据完整性提示。
const composition = computed(() => {
  const steps = (skeleton.value?.turns ?? []).flatMap((turn) => turn.steps)
  const model = steps.reduce((n, step) => n + step.latency_ms, 0)
  // 工具按步取区间并集:并行调用不重复计时,与时间轴上的色块一致。
  const tool = steps.reduce((n, step) => n + (step.tool_ms ?? 0), 0)

  // 完整性只看工具事件覆盖:轨迹里发起了 N 次调用,就该有 N 对事件。
  const calls = digest.value?.totals.calls ?? 0
  const toolMissing = Math.max(0, calls - toolCalls.value.length)

  const total = model + tool
  return {
    model,
    tool,
    total,
    toolShare: total > 0 ? (tool / total) * 100 : 0,
    modelCount: steps.length,
    toolCount: toolCalls.value.length,
    toolMissing,
    show: total > 0,
  }
})




// toolStyle 把该步发起的工具耗时画在模型条之后:左端接模型条右端。
function toolStyle(step: TraceStep, turn: TraceTurn) {
  const total = turn.duration_ms ?? 0
  if (!total || !step.tool_ms || step.at_ms === undefined) return { display: 'none' }
  const left = Math.max(0, Math.min(100, ((step.at_ms + step.latency_ms) / total) * 100))
  // 工具段不得超过 harness 窗口:harness 的终点就是同链下一步的起点。
  const cap = step.harness_ms !== undefined ? Math.min(step.tool_ms, step.harness_ms) : step.tool_ms
  const width = Math.min((cap / total) * 100, 100 - left)
  if (width <= 0) return { display: 'none' }
  return { left: `${left.toFixed(2)}%`, width: `${width.toFixed(2)}%` }
}

// hasSide 决定是否渲染拆分行:总计已含旁路,没有旁路就不拆。
const hasSide = computed(() => (skeleton.value?.side?.steps ?? 0) > 0)

// protocols 列出该会话用到的协议(去重,保持出现顺序)。
// 用量列的口径随协议不同,故会话层必须先给出协议,读者才知道该用哪种加法。
const protocols = computed<string[]>(() => {
  const seen: string[] = []
  for (const turn of skeleton.value?.turns ?? []) {
    for (const step of turn.steps) {
      if (step.protocol && !seen.includes(step.protocol)) seen.push(step.protocol)
    }
  }
  return seen
})

// protocolsLabel 是会话头的协议标记:单一协议直接列名,多协议并列。
const protocolsLabel = computed(() => protocols.value.join(' + '))


// toggleDiffView 在单栏(unified)与双栏(split)之间切换:全局共享一次切换。
function toggleDiffView() {
  diffView.value = diffView.value === 'unified' ? 'split' : 'unified'
}

// expandContext 调整上下文行数并重新拉取详情。
// delta=0 表示收起(回到 git 默认的 3 行);正数在当前基础上多展开。
function expandContext(seq: number, delta: number) {
  ctxLines.value = delta === 0 ? 3 : Math.min(ctxLines.value + delta, 200)
  void fetchDetail(seq)
}

// toggleStep 首次展开时按需加载该步详情(懒加载)。
async function toggleStep(seq: number) {
  if (openSteps.value.includes(seq)) {
    openSteps.value = openSteps.value.filter((s) => s !== seq)
    return
  }
  openSteps.value = [...openSteps.value, seq]
  if (!details.value[seq]) await fetchDetail(seq)
}

// switchFull 在「本轮新增」与「全量 messages」之间切换。
async function switchFull(seq: number) {
  fullMode.value = { ...fullMode.value, [seq]: !fullMode.value[seq] }
  await fetchDetail(seq)
}

async function fetchDetail(seq: number) {
  try {
    // ctx 控制差异块的上下文行数:默认 3,展开时递增。
    const params = new URLSearchParams({ ctx: String(ctxLines.value) })
    if (fullMode.value[seq]) params.set('full', '1')
    const detail = await api.get<TraceStepDetail>(
      `/console/api/traces/${props.sessionId}/${seq}?${params.toString()}`,
    )
    details.value = { ...details.value, [seq]: detail }
    const rest = { ...detailErrors.value }
    delete rest[seq]
    detailErrors.value = rest
  } catch (e) {
    detailErrors.value = { ...detailErrors.value, [seq]: e instanceof Error ? e.message : String(e) }
  }
}

function turnUsage(turn: TraceTurn, key: 'in' | 'out' | 'cache'): number {
  return turn.steps.reduce((sum, s) => sum + (key === 'in' ? s.usage_in : key === 'out' ? s.usage_out : s.usage_cache ?? 0)!, 0)
}

function statusClass(code: number): string {
  if (code >= 500) return 'danger'
  if (code >= 400) return 'warn'
  return 'ok'
}

// kindLabel 给分类徽标取文案:旁路步与主路共用同一套,链的区分由 tab 承担。
// new 只有一种语义:一条链的起点。主链首步可能晚于会话首步,但仍是本链起点。
function kindLabel(step: TraceStep): string {
  if (step.cleared) return '上下文清零'
  if (step.compress) return `压缩 ${step.compress.before_lines}→${step.compress.after_lines} 行`
  // 末行更新按字节数给方向:变长是刷新末尾注入内容,变短多为旁路转录被重建。
  // 两者都不是错误,文案与配色取中性。
  if (step.tail_update) {
    const before = step.tail_before
    const after = step.tail_after
    // 行长度缺失时不报方向,退回中性文案,避免把未知说成「改写」。
    if (before === undefined || after === undefined) return '末行更新'
    if (after > before) return `末行扩展 +${after - before}B`
    if (after < before) return `末行收缩 −${before - after}B`
    return '末行改写'
  }
  // 分叉带行数:一次可能改多行,徽标给出规模。
  if (step.diff_lines) return `分叉 ${step.diff_lines} 行`
  // session 中途出现 new 时,该步必然属于另一条链,故同样称「首步」。
  if (step.delta.kind === 'new') return '首步'
  // retry 的判据是「内容与上一步完全相同」,不必然是失败后重发,故文案取中性的「重复」。
  const labels: Record<string, string> = { new: '首步', added: '追加', retry: '重复', fork: '分叉', rewrite: '改写', chain: '无重合' }
  return labels[step.delta.kind] ?? step.delta.kind
}

function kindClass(step: TraceStep): string {
  if (step.cleared || step.compress) return 'compress'
  return step.seq === 1 ? 'new' : step.delta.kind
}

function fmtValue(v: unknown): string {
  if (typeof v === 'string') return v
  return JSON.stringify(v, null, 2)
}

// structuredText 把对象或 JSON 字符串整理成缩进文本;不是结构化内容时返回空。
function structuredText(v: unknown): string {
  if (v && typeof v === 'object') return JSON.stringify(v, null, 2)
  if (typeof v === 'string') {
    const s = v.trim()
    const looksJSON =
      (s.startsWith('{') && s.endsWith('}')) || (s.startsWith('[') && s.endsWith(']'))
    if (!looksJSON) return ''
    try {
      return JSON.stringify(JSON.parse(s), null, 2)
    } catch {
      return ''
    }
  }
  return ''
}

function fmtBytes(n: number | undefined): string {
  if (n === undefined) return '—'
  return n >= 1024 ? (n / 1024).toFixed(1) + 'KB' : n + 'B'
}

// 消息折叠阈值:不超过它的消息默认铺开,超过则收起(长输出与大文件内容不占地方)。
const msgAutoOpenChars = 300

// msgSize 估算消息的体积:文本按字符数,其余按 JSON 长度,用于默认折叠判定与体积标签。
function msgSize(msg: TraceMessage): number {
  let n = 0
  for (const block of msg.blocks) {
    const text = typeof block.text === 'string' ? block.text : ''
    n += text.length || JSON.stringify(block).length
  }
  return n
}

// isMsgOpen 取消息的展开状态:用户手动切换过则以手动值为准,否则按体积取默认。
function isMsgOpen(seq: number, index: number): boolean {
  const key = `${seq}:${index}`
  const manual = openMsgs.value[key]
  if (manual !== undefined) return manual
  const msg = details.value[seq]?.messages[index]
  return msg ? msgSize(msg) <= msgAutoOpenChars : false
}

// toggleMsg 切换消息的展开状态。
function toggleMsg(seq: number, index: number) {
  openMsgs.value = { ...openMsgs.value, [`${seq}:${index}`]: !isMsgOpen(seq, index) }
}

// blockSummary 概览该消息包含哪些块类型(折叠状态下也能扫读)。
// 同类型合并计数:客户端常把一段内容切成多个块(如为挂 cache_control 断点),
// 「text, text, text」既啰嗦又看不出差别。
// blocksSummary 统一消息与响应的摘要:一律按块类型计数,单块也写 ×1。
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

// sizeLabel 把体积转成可读标签。
function sizeLabel(msg: TraceMessage): string {
  const n = msgSize(msg)
  return n >= 1024 ? (n / 1024).toFixed(1) + 'KB' : n + 'B'
}

// toolsFor 返回该步的工具标签(如 `Read ×2 Bash`)。与轮次行的工具数同源,
// 都来自 digest:未到达时显示占位,到达后无调用的步显示空。
function toolsFor(seq: number): string {
  const rows = digest.value?.by_step
  if (!rows) return '…'
  const row = rows.find((r) => r.seq === seq)
  if (!row || !row.calls) return ''
  return Object.entries(row.by_name)
    .map(([name, n]) => (n > 1 ? `${name} ×${n}` : name))
    .join(' ')
}

// blockText 返回块里可渲染为 markdown 的文本;工具调用与未知块返回空,
// 由调用方退回 JSON 展示。
function blockText(block: Record<string, unknown>): string {
  if (typeof block.text === 'string') return block.text
  if (typeof block.thinking === 'string') return block.thinking
  const content = block.content
  if (typeof content === 'string') return content
  if (Array.isArray(content)) {
    return content
      .map((part) => {
        if (part && typeof part === 'object' && typeof (part as Record<string, unknown>).text === 'string') {
          return String((part as Record<string, unknown>).text)
        }
        return JSON.stringify(part)
      })
      .join('\n\n')
  }
  return ''
}

// msgText 把消息压成单行摘要:差异行、压缩对照这类紧凑位置使用。
// 认识所有块类型——只用 text 块会让纯工具消息显示成空白。
function msgText(msg?: TraceMessage): string {
  if (!msg) return ''
  return msg.blocks
    .map((block) => {
      const kind = String(block.type ?? '?')
      if (kind === 'tool_use') return `tool_use → ${block.name}`
      if (kind === 'tool_result') {
        const c = block.content
        return `tool_result → ${typeof c === 'string' ? c : JSON.stringify(c)}`
      }
      return blockText(block) || kind
    })
    .join(' | ')
}

// msgOrdinal 返回消息在**完整上下文中的序号**(自 1 起),不是当前列表内的序号。
// 增量模式下列表只含新增消息,故要加上继承行数——这样序号才能与「分叉 · 第 N 行」
// 之类的信息对照上。
function msgOrdinal(seq: number, index: number): number {
  const base = fullMode.value[seq] ? 0 : (details.value[seq]?.inherited ?? 0)
  return base + index + 1
}

// hunkKindLabel 给差异块类型取中文。
function hunkKindLabel(kind: string): string {
  if (kind === 'delete') return '删除'
  if (kind === 'insert') return '新增'
  return '改动'
}

// ---- 行内字符级高亮 ----

// InlineParts 是一条消息文本按变更切成的三段:相同前缀 / 变更中段 / 相同后缀。
// 长 JSON 行改动一个字符时,整行标红看不出改在哪;切出中段即可只高亮它。
interface InlineParts {
  pre: string
  mid: string
  suf: string
}

// 行内高亮的最小收益:变更中段短于整行的这个比例时才切分。
// 否则(改动很大)整行标色反而更清晰,切碎成三段只是噪音。
const inlineMaxChangedRatio = 0.6
// 参与行内比对的最大文本长度:超长文本的 O(n) 扫描收益不大,
// 且切碎后渲染成大量节点反而更慢。
const inlineMaxTextLen = 20000

// inlineParts 用公共前/后缀裁剪算出变更中段;不需要切分时返回 null。
function inlineParts(prevText: string, curText: string): { prev: InlineParts; cur: InlineParts } | null {
  if (!prevText || !curText) return null
  if (prevText.length > inlineMaxTextLen || curText.length > inlineMaxTextLen) return null
  let head = 0
  const maxHead = Math.min(prevText.length, curText.length)
  while (head < maxHead && prevText[head] === curText[head]) head++
  let tail = 0
  const maxTail = Math.min(prevText.length, curText.length) - head
  while (tail < maxTail && prevText[prevText.length - 1 - tail] === curText[curText.length - 1 - tail]) tail++

  const prevMid = prevText.slice(head, prevText.length - tail)
  const curMid = curText.slice(head, curText.length - tail)
  // 中段占比过大时放弃切分:三段都比中段短,切了反而更难读。
  const total = Math.max(prevText.length, curText.length)
  if (Math.max(prevMid.length, curMid.length) > total * inlineMaxChangedRatio) return null
  if (!prevMid && !curMid) return null
  return {
    prev: { pre: prevText.slice(0, head), mid: prevMid, suf: tail > 0 ? prevText.slice(prevText.length - tail) : '' },
    cur: { pre: curText.slice(0, head), mid: curMid, suf: tail > 0 ? curText.slice(curText.length - tail) : '' },
  }
}

// pairsWithInline 把改动块的两侧逐行配对,并算出每对的行内高亮。
// 两侧行数不等时,多出的行没有配对对象,单独成行、不切分。
interface DiffPair {
  prev?: TraceDiffRow
  cur?: TraceDiffRow
  inline?: { prev: InlineParts; cur: InlineParts } | null
}

function pairRows(hunk: TraceDiffHunk): DiffPair[] {
  const prevRows = hunk.prev ?? []
  const curRows = hunk.cur ?? []
  const out: DiffPair[] = []
  const n = Math.max(prevRows.length, curRows.length)
  for (let i = 0; i < n; i++) {
    const p = prevRows[i]
    const c = curRows[i]
    out.push({
      prev: p,
      cur: c,
      // 只有「改动」且两侧都有内容时才做行内高亮(删除/新增没有对照对象)。
      // 比对用**显示摘要**(msgText),不是 text 块:纯工具消息的差异全在
      // tool_result 内容里,只比 text 块会取到空串,两行就显示成一模一样。
      inline: hunk.kind === 'replace' && p && c ? inlineParts(msgText(p.message), msgText(c.message)) : null,
    })
  }
  return out
}

// htmlEscape 转义文本:这些内容经 v-html 注入,而来源是不可信的(模型输出、
// 客户端注入的标记),必须自行转义——与 MarkdownView 里 DOMPurify 的职责相同。
function htmlEscape(s: string): string {
  return s.replace(/[&<>"]/g, (c) => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;' })[c] as string)
}

// truncateForDiff 截断过长的行内文本:差异行可能上万字符,整段渲染既慢又难读,
// 两侧各留首尾一段即可看出改动位置(中段被省略的部分标注长度)。
const inlineHeadKeep = 600
const inlineTailKeep = 300

function truncateForDiff(s: string): { text: string; omitted: number } {
  if (s.length <= inlineHeadKeep + inlineTailKeep + 40) return { text: s, omitted: 0 }
  const omitted = s.length - inlineHeadKeep - inlineTailKeep
  return { text: s.slice(0, inlineHeadKeep) + s.slice(s.length - inlineTailKeep), omitted }
}

// inlineHTML 渲染一行的正文:有行内切分时只高亮变更中段,否则整行呈现。
// 文本一律先转义再拼标签,避免 v-html 注入。
//
// 切分与显示都基于 msgText(认识所有块类型):只读 text 块会让纯工具消息
// 渲染成空白(实测表现为「(无内容)」),两行也会因取到空串而显示成一模一样。
function inlineHTML(parts: InlineParts | null | undefined, row: TraceDiffRow): string {
  const summary = msgText(row.message) || '(无内容)'
  if (!parts) {
    const { text, omitted } = truncateForDiff(summary)
    return htmlEscape(text) + (omitted ? `<span class="omitted">… 省略 ${omitted} 字符 …</span>` : '')
  }
  // 有行内切分:三段各自截断后拼装,中段加高亮标记。
  const head = truncateForDiff(parts.pre)
  const mid = truncateForDiff(parts.mid)
  const tail = truncateForDiff(parts.suf)
  const omitted = head.omitted + mid.omitted + tail.omitted
  return (
    htmlEscape(head.text) +
    `<mark class="chg">${htmlEscape(mid.text)}</mark>` +
    htmlEscape(tail.text) +
    (omitted ? `<span class="omitted">… 省略 ${omitted} 字符 …</span>` : '')
  )
}

</script>

<template>
  <div v-if="error" class="error" role="alert">{{ error }}</div>
  <p v-else-if="loading" class="muted" role="status" aria-live="polite">加载中…</p>
  <template v-else-if="skeleton">
    <div class="card summary">
      <div class="summary-row">
        <div class="stat"><span class="k">turns</span><span class="v">{{ skeleton.turns.length }}</span></div>
        <div class="stat"><span class="k">总计 steps</span><span class="v">{{ skeleton.steps }}</span></div>
        <div class="stat"><span class="k">总计 in</span><span class="v">{{ fmtK(skeleton.usage_in) }}</span></div>
        <div class="stat"><span class="k">总计 out</span><span class="v">{{ fmtK(skeleton.usage_out) }}</span></div>
        <div class="stat"><span class="k">总计 cache</span><span class="v">{{ fmtK(skeleton.usage_cache) }}</span></div>
        <div class="stat">
          <span class="k">链</span>
          <span class="v infer" :title="CHAIN_NOTE">{{ chainsLabel }}</span>
        </div>
        <div v-if="skeleton.chains?.degraded" class="stat">
          <span class="k">链口径</span>
          <span class="v note">{{ skeleton.chains.note }}</span>
        </div>
        <div v-if="protocols.length" class="stat" :title="USAGE_NOTE">
          <span class="k">协议</span>
          <span class="v proto">{{ protocolsLabel }}</span>
        </div>
      </div>

      <div v-if="composition.show" class="summary-row composition">
        <span class="comp-label">机器时间</span>
        <span class="comp-legend">
          <span><i class="sw comp-model"></i>模型 {{ fmtMs(composition.model) }} · {{ composition.modelCount }} 次</span>
          <span><i class="sw comp-tool"></i>工具 {{ fmtMs(composition.tool) }} · {{ composition.toolCount }} 次</span>
          <span>工具占比 {{ composition.toolShare.toFixed(0) }}%</span>
        </span>
        <div v-if="composition.toolMissing > 0" class="comp-warn">
          有 {{ composition.toolMissing }} 次工具调用没有事件，工具耗时可能偏低
        </div>
      </div>

      <div v-if="hasSide" class="summary-row split" :title="CHAIN_NOTE">
        <span class="split-name">主路</span>
        <span class="split-item">steps {{ skeleton.main?.steps ?? 0 }}</span>
        <span class="split-item">in {{ fmtK(skeleton.main?.usage_in ?? 0) }}</span>
        <span class="split-item">out {{ fmtK(skeleton.main?.usage_out ?? 0) }}</span>
        <span class="split-item">cache {{ fmtK(skeleton.main?.usage_cache ?? 0) }}</span>
        <span class="split-name">旁路 <em>已计入总计</em></span>
        <span class="split-item">steps {{ skeleton.side?.steps ?? 0 }}</span>
        <span class="split-item">in {{ fmtK(skeleton.side?.usage_in ?? 0) }}</span>
        <span class="split-item">out {{ fmtK(skeleton.side?.usage_out ?? 0) }}</span>
        <span class="split-item">cache {{ fmtK(skeleton.side?.usage_cache ?? 0) }}</span>
      </div>
      <div v-if="protocols.length > 1" class="muted small proto-mixed">{{ MIXED_PROTOCOL_NOTE }}</div>
    </div>

    <div class="turn-nav" role="navigation" aria-label="轮次导航">
      <span class="pos">第 {{ turnPosition }} / {{ skeleton.turns.length }} 轮</span>
      <span class="nav-btns">
        <button type="button" aria-label="上一轮" @click="stepTurn(-1)">‹</button>
        <button type="button" aria-label="下一轮" @click="stepTurn(1)">›</button>
      </span>
      <form class="jump" @submit.prevent="jumpTurn">
        <input v-model="jumpValue" inputmode="numeric" placeholder="跳到" aria-label="跳到第几轮" />
      </form>
      <button type="button" class="order" @click="toggleOrder">
        {{ orderDesc ? '倒序' : '正序' }}
      </button>
    </div>

    <div
      v-for="turn in orderedTurns"
      :id="`turn-${turn.index}`"
      :key="turn.index"
      :ref="(el) => setTurnEl(turn.index, el)"
      class="card turn"
    >
      <button
        type="button"
        class="turn-head"
        :aria-expanded="openTurns.includes(turn.index)"
        @click="toggleTurn(turn.index)"
      >
        <span class="caret" aria-hidden="true">{{ openTurns.includes(turn.index) ? '▾' : '▸' }}</span>
        <span class="idx">{{ turn.index }}</span>
        <span
          class="title"
          :class="{ pending: !titleFor(turn.index) }"
          :title="`标题取该轮首个 step 的新增行,需解析 ${parseCostOf(turn)} 行`"
        >
          {{ titleFor(turn.index) || '(标题待填充)' }}
        </span>
        <span class="chips">
          <span class="chip">{{ mainStepsOf(turn).length }} steps</span>
          <span class="chip" :title="START_NOTE">{{ formatFullTime(turn.started_at) }}</span>
          <span v-if="turn.parallel" class="chip infer">⇄ 不相交</span>
          <span v-if="turn.compresses" class="chip compress">⚠ 含压缩 {{ turn.compresses }} 次</span>
          <span v-if="turn.retries" class="chip warn" title="内容完全相同的请求">
            重复 {{ turn.retries }} 次
          </span>
          <span v-if="turn.failures" class="chip danger">⚠ 含失败 {{ turn.failures }} 步</span>
          <span
            v-if="sideStepsOf(turn).length"
            class="chip"
            :title="TURN_CHAIN_NOTE"
          >
            本轮链 {{ sideChainsOf(turn).length }} 条 · 旁路 {{ sideStepsOf(turn).length }} 步
          </span>
          <span
            v-if="turnMeta[turn.index]?.calls === undefined"
            class="chip lazy"
            :title="`解析本轮新增 ${parseCostOf(turn)} 行后填充`"
          >
            工具 · 解析 {{ parseCostOf(turn) }} 行
          </span>
        </span>
      </button>

      <div v-if="storyOf(turn).length" class="story">
        <template v-for="(part, i) in storyOf(turn)" :key="`${i}-${part.text}`">
          <span v-if="i" class="arrow">→</span>
          <button v-if="part.seq !== undefined" class="part" @click.stop="jumpToPart(part)">{{ part.text }}</button>
          <span v-else class="part">{{ part.text }}</span>
        </template>
        <span class="tail">
          · {{ durationLabel(turn) }} · in {{ fmtK(turnUsage(turn, 'in')) }} / out {{ fmtK(turnUsage(turn, 'out')) }}
        </span>
      </div>

      <div v-if="openTurns.includes(turn.index) && sideStepsOf(turn).length" class="turn-tabs">
        <button :class="{ on: effectiveTab(turn) === 0 }" @click.stop="turnTab = 0">
          主路 {{ mainStepsOf(turn).length }}
        </button>
        <button
          v-for="chain in sideChainsOf(turn)"
          :key="chain"
          :class="{ on: effectiveTab(turn) === chain }"
          @click.stop="turnTab = chain"
        >
          旁路 #{{ chain }} · {{ chainStepCount(turn, chain) }}
        </button>
      </div>
      <div v-if="openTurns.includes(turn.index)" class="steps">
        <div v-if="failedStepsOf(turn).length" class="failures">
          <div v-for="step in failedStepsOf(turn)" :key="step.seq" class="failure">
            <span class="chip danger">失败</span>
            <span class="where">
              #{{ step.seq }}<template v-if="step.side">(旁路 #{{ step.side_chain ?? '—' }})</template>
            </span>
            <span class="why">{{ failureText(step) }}</span>
            <button class="link" @click.stop="jumpTo(step.seq)">查看</button>
          </div>
        </div>

        <div v-if="stepsHaveAxis(turn)" class="axis">
          <div class="step-head axis-row">
            <span class="caret" aria-hidden="true"></span>
            <span class="seq"></span>
            <span class="ts">时间轴</span>
            <span class="track">
              <i class="axis-end left">0s</i>
              <i class="axis-end right">{{ durationLabel(turn) }}</i>
            </span>
          </div>
          <p class="axis-note">{{ AXIS_NOTE }}</p>
        </div>

        <div v-for="step in displayStepsOf(turn)" :key="step.seq" class="step" :class="{ open: openSteps.includes(step.seq) }">
          <button
            :id="stepDomId(step.seq)"
            type="button"
            class="step-head"
            :aria-expanded="openSteps.includes(step.seq)"
            @click="toggleStep(step.seq)"
          >
            <span class="caret" aria-hidden="true">{{ openSteps.includes(step.seq) ? '▾' : '▸' }}</span>
            <span class="seq">#{{ step.seq }}</span>
            <span class="ts">{{ formatFullTime(step.ts) }}</span>
            <span class="track" :title="`偏移 ${fmtMs(step.at_ms ?? 0)} · 本步 ${fmtMs(step.latency_ms)}`">
              <i class="fill" :class="{ side: step.side, failed: isFailedStep(step) }" :style="barStyle(step, turn)"></i>
              <i class="hatch" :style="harnessStyle(step, turn)" :title="harnessTitle(step, turn)"></i>
              <i
                v-if="step.tool_ms"
                class="toolfill"
                :style="toolStyle(step, turn)"
                :title="`工具 ${fmtMs(step.tool_ms)}(事件边界,含客户端处理)`"
              ></i>
            </span>
            <span class="num w-code" :class="statusClass(step.status_code)">{{ step.status_code }}</span>
            <span class="lat">{{ fmtMs(step.latency_ms) }}</span>
            <span v-if="step.tool_ms" class="toolms" title="工具占用的墙钟时间,含客户端处理">tool {{ fmtMs(step.tool_ms) }}</span>
            <span class="harness" :title="HARNESS_NOTE">harness {{ harnessLabel(step) }}</span>
            <span class="tokens">in {{ fmtK(step.usage_in) }} / out {{ fmtK(step.usage_out) }} / cache {{ fmtK(step.usage_cache) }}</span>
            <span
              v-if="toolsFor(step.seq)"
              class="tools"
              :class="{ pending: toolsFor(step.seq) === '…' }"
              title="本步发起的工具调用(需解析该步新增行,可渐进填充)"
            >
              {{ toolsFor(step.seq) }}
            </span>
            <span v-if="step.error_kind" class="chip danger" title="网关侧归因">{{ step.error_kind }}</span>
            <span v-if="protocols.length > 1" class="chip" :title="USAGE_NOTE">{{ step.protocol }}</span>
            <span v-for="label in hotLabels(step.seq)" :key="label" class="chip hot">{{ label }}</span>
            <span class="badge" :class="kindClass(step)" :title="step.delta.note">{{ kindLabel(step) }}</span>
            <span
              v-if="step.retry.of > 1"
              class="badge warn"
              title="与前一次内容完全相同"
            >
              第 {{ step.retry.n }}/{{ step.retry.of }} 次相同
            </span>
          </button>

          <div v-if="openSteps.includes(step.seq)" class="detail">
            <p class="note">
              <b>判定理由</b> · {{ step.delta.note }}
              <span class="muted"> · 协议 {{ step.protocol }}(决定用量列的口径)</span>
            </p>

            <template v-if="details[step.seq]">
              <section class="region">
                <h4 class="region-title">请求</h4>
              <div v-if="details[step.seq].compress" class="sect">
                <div class="label">上下文压缩 · step {{ details[step.seq].compress!.from_seq }} → step {{ step.seq }}</div>
                <div class="cmp">
                  <div class="cmp-col">
                    <div class="cmp-head">压缩前 · {{ details[step.seq].compress!.before_lines }} 行</div>
                    <div v-for="(m, i) in details[step.seq].compress!.before ?? []" :key="'b' + i" class="cmp-row">
                      <span class="role">{{ m.role }}</span><span>{{ msgText(m) }}</span>
                    </div>
                  </div>
                  <div class="cmp-col">
                    <div class="cmp-head">压缩后 · {{ details[step.seq].compress!.after_lines }} 行</div>
                    <div v-for="(m, i) in details[step.seq].compress!.after ?? []" :key="'a' + i" class="cmp-row">
                      <span class="role">{{ m.role }}</span><span>{{ msgText(m) }}</span>
                    </div>
                  </div>
                </div>
                <p class="hint">
                  保留 {{ details[step.seq].compress!.kept }} · 移除 {{ details[step.seq].compress!.removed }} ·
                  新增 {{ details[step.seq].compress!.added }} 行
                </p>
              </div>

              <div v-if="details[step.seq].diff?.length && effectiveMsgView(step.seq) === 'diff'" class="sect">
                <div class="label">
                  与同链上一步的差异 · {{ details[step.seq].diff!.length }} 个 hunk
                  <span class="viewsw">
                    <button class="on">差异</button>
                    <button @click.stop="msgView = 'added'">新增消息</button>
                  </span>
                  <span class="spacer"></span>
                  <span class="ctrl">
                    <button class="link" @click.stop="expandContext(step.seq, 0)">
                      收起上下文
                    </button>
                    <button class="link" @click.stop="expandContext(step.seq, 20)">展开更多上下文</button>
                    <button class="link" @click.stop="toggleDiffView">
                      {{ diffView === 'unified' ? '切换为双栏' : '切换为单栏' }}
                    </button>
                  </span>
                </div>

                <template v-for="(hunk, hi) in details[step.seq].diff" :key="hi">
                  <!-- 块间未变更行数:一眼看出两块隔了多远 -->
                  <div v-if="hunk.unchanged_before" class="gap">
                    ⋯ 未变更 {{ hunk.unchanged_before }} 行 ⋯
                  </div>
                  <details class="hunk" :open="details[step.seq].diff!.length === 1">
                    <summary>
                      <span class="hunk-kind" :class="hunk.kind">{{ hunkKindLabel(hunk.kind) }}</span>
                      <span class="hunk-range mono">{{ hunk.header }}</span>
                      <span class="spacer"></span>
                      <span class="sz">{{ fmtBytes(hunk.prev_bytes ?? 0) }} → {{ fmtBytes(hunk.cur_bytes ?? 0) }}</span>
                    </summary>

                    <!-- 单栏:标准 unified diff 顺序(前置上下文 → 删 → 增 → 后置上下文) -->
                    <div v-if="diffView === 'unified'" class="udiff mono">
                      <div v-for="(row, ci) in hunk.ctx_before ?? []" :key="'b' + ci" class="uline ctx">
                        <span class="sign"> </span>
                        <span class="ln">#{{ row.cur_line + 1 }}</span>
                        <span class="sz2">{{ fmtBytes(row.bytes) }}</span>
                        <span class="txt">{{ row.message ? msgText(row.message) : '(无内容)' }}</span>
                      </div>
                      <div v-for="(pair, pi) in pairRows(hunk)" :key="'d' + pi">
                        <div v-if="pair.prev" class="uline del">
                          <span class="sign">-</span>
                          <span class="ln">#{{ pair.prev.line + 1 }}</span>
                          <span class="sz2">{{ fmtBytes(pair.prev.bytes) }}</span>
                          <span class="txt" v-html="inlineHTML(pair.inline?.prev, pair.prev)"></span>
                        </div>
                        <div v-if="pair.cur" class="uline add">
                          <span class="sign">+</span>
                          <span class="ln">#{{ pair.cur.line + 1 }}</span>
                          <span class="sz2">{{ fmtBytes(pair.cur.bytes) }}</span>
                          <span class="txt" v-html="inlineHTML(pair.inline?.cur, pair.cur)"></span>
                        </div>
                      </div>
                      <div v-for="(row, ci) in hunk.ctx_after ?? []" :key="'a' + ci" class="uline ctx">
                        <span class="sign"> </span>
                        <span class="ln">#{{ row.cur_line + 1 }}</span>
                        <span class="sz2">{{ fmtBytes(row.bytes) }}</span>
                        <span class="txt">{{ row.message ? msgText(row.message) : '(无内容)' }}</span>
                      </div>
                    </div>

                    <!-- 双栏:改动行左右并排 -->
                    <div v-else class="udiff split mono">
                      <div v-for="(pair, pi) in pairRows(hunk)" :key="'s' + pi" class="srow">
                        <div class="scell" :class="{ del: pair.prev }">
                          <template v-if="pair.prev">
                            <span class="ln">#{{ pair.prev.line + 1 }}</span>
                            <span class="txt" v-html="inlineHTML(pair.inline?.prev, pair.prev)"></span>
                          </template>
                        </div>
                        <div class="scell" :class="{ add: pair.cur }">
                          <template v-if="pair.cur">
                            <span class="ln">#{{ pair.cur.line + 1 }}</span>
                            <span class="txt" v-html="inlineHTML(pair.inline?.cur, pair.cur)"></span>
                          </template>
                        </div>
                      </div>
                    </div>
                  </details>
                </template>
              </div>

              <div class="sect">
                <div class="label">
                  请求 messages · {{ fullMode[step.seq] ? '完整上下文' : '本轮新增' }}
                  {{ (details[step.seq].messages ?? []).length }} 条
                  <span v-if="details[step.seq].diff?.length" class="viewsw">
                    <button @click.stop="msgView = 'diff'">差异</button>
                    <button class="on">新增消息</button>
                  </span>
                  <button
                    class="link"
                    :title="
                      fullMode[step.seq]
                        ? '只显示相对上一请求新增的消息'
                        : '显示该请求携带的完整 messages(无状态协议下每轮重发全量历史)'
                    "
                    @click.stop="switchFull(step.seq)"
                  >
                    {{ fullMode[step.seq] ? '只看本轮新增' : '查看完整上下文' }}
                  </button>
                </div>
                <div v-if="effectiveMsgView(step.seq) !== 'added'" class="hint">
                  本轮新增的消息已在上方「与同链上一步的差异」中呈现
                  (差异的 <code>+</code> 侧即新增行)。
                  <button class="link" @click.stop="msgView = 'added'">在此逐条查看 →</button>
                </div>
                <div v-if="effectiveMsgView(step.seq) === 'added' && !fullMode[step.seq] && details[step.seq].inherited" class="inherited">
                  ↑ 该请求还携带 {{ details[step.seq].inherited }} 行历史消息(未显示)
                </div>
                <div
                  v-for="(m, i) in effectiveMsgView(step.seq) === 'added' ? (details[step.seq].messages ?? []) : []"
                  :key="i"
                  class="msg"
                >
                  <button
                    type="button"
                    class="msg-head"
                    :aria-expanded="isMsgOpen(step.seq, i)"
                    @click="toggleMsg(step.seq, i)"
                  >
                    <span class="caret" aria-hidden="true">{{ isMsgOpen(step.seq, i) ? '▾' : '▸' }}</span>
                    <span class="idx" :title="fullMode[step.seq] ? '该消息在完整上下文中的序号' : '该消息在完整上下文中的序号(前若干条未显示)'">
                      #{{ msgOrdinal(step.seq, i) }}
                    </span>
                    <span class="role" :class="m.role">{{ m.role }}</span>
                    <span class="blk">{{ blocksSummary(m.blocks) }}</span>
                    <span class="spacer"></span>
                    <span class="sz">{{ sizeLabel(m) }}</span>
                  </button>
                  <div v-if="isMsgOpen(step.seq, i)" class="msg-body">
                    <div
                      v-for="(b, j) in m.blocks"
                      :key="j"
                      class="blk-item"
                      :class="'kind-' + String(b.type ?? 'unknown')"
                    >
                      <div class="blk-tag">{{ b.type ?? 'unknown' }}</div>
                      <MarkdownView v-if="blockText(b)" :text="blockText(b)" />
                      <pre v-else class="raw">{{ fmtValue(b) }}</pre>
                    </div>
                    <p v-if="!m.blocks.length" class="muted small">(无内容)</p>
                  </div>
                </div>
              </div>
              </section>

              <section v-if="respOf(step.seq)" class="region">
                <h4 class="region-title">响应</h4>
                <p v-if="!respRecorded(step.seq)" class="hint">
                  该步响应未记录：早于采集版本，或请求未完成
                </p>
                <template v-else>
                  <div class="msg">
                  <button
                    type="button"
                    class="msg-head"
                    :aria-expanded="isRespOpen(step.seq)"
                    @click.stop="toggleResp(step.seq)"
                  >
                    <span class="caret" aria-hidden="true">{{ isRespOpen(step.seq) ? '▾' : '▸' }}</span>
                    <span class="role assistant">assistant</span>
                    <span class="blk">{{ respSummary(step.seq) }}</span>
                    <span class="spacer"></span>
                    <span class="num">{{ respStop(step.seq) || 'stop —' }}</span>
                    <span class="num w-size">in {{ fmtK(respIn(step.seq)) }} / out {{ fmtK(respOut(step.seq)) }}</span>
                  </button>
                  <div v-if="isRespOpen(step.seq)" class="msg-body">
                    <div
                      v-for="(block, bi) in respBlocks(step.seq)"
                      :key="bi"
                      class="blk-item"
                      :class="'kind-' + block.type"
                    >
                      <div class="blk-tag">{{ block.type }}</div>
                      <pre v-if="block.type === 'tool_use'" class="raw">{{ block.name }} {{ blockBody(block) }}</pre>
                      <MarkdownView v-else-if="blockBody(block)" :text="blockBody(block)" />
                      <p v-else class="muted small">(空块)</p>
                    </div>
                    <p v-if="resspEmpty(step.seq)" class="hint">(无内容)</p>
                    <p v-if="respTruncated(step.seq) || respIncomplete(step.seq)" class="hint">
                      <span v-if="respTruncated(step.seq)">已截断</span>
                      <span v-if="respIncomplete(step.seq)"> · 不完整</span>
                    </p>
                  </div>
                  </div>
                </template>
              </section>

              <section v-if="details[step.seq].tools.length" class="region">
                <h4 class="region-title">工具</h4>
                <template v-for="(tool, i) in details[step.seq].tools" :key="i">
                  <div class="msg">
                  <button
                    type="button"
                    class="msg-head"
                    :aria-expanded="isToolOpen(toolKey(step.seq, i))"
                    @click.stop="toggleTool(toolKey(step.seq, i))"
                  >
                    <span class="caret" aria-hidden="true">{{ isToolOpen(toolKey(step.seq, i)) ? '▾' : '▸' }}</span>
                    <span class="name">{{ tool.name }}</span>
                    <span class="blk mono">{{ toolArgs(tool) }}</span>
                    <span class="spacer"></span>
                    <span class="num w-time">{{ toolCost(tool) }}</span>
                    <span class="state" :class="tool.status === 'ok' ? 'ok' : tool.status === 'error' ? 'danger' : 'unknown'">
                      {{ toolStatusLabel(tool.status) }}
                    </span>
                  </button>
                  <div v-if="isToolOpen(toolKey(step.seq, i))" class="msg-body">
                    <div class="io">
                      <span class="k">入参</span>
                      <span v-if="reportedOf(tool)?.input_truncated" class="warn small">已截断</span>
                      <div v-if="structuredText(toolInputOf(tool))" class="json-window">
                        <pre>{{ structuredText(toolInputOf(tool)) }}</pre>
                      </div>
                      <pre v-else>{{ fmtValue(toolInputOf(tool)) }}</pre>
                    </div>
                    <div class="io">
                      <span class="k">出参</span>
                      <span v-if="reportedOf(tool)?.result_truncated" class="warn small">已截断</span>
                      <div v-if="structuredText(toolResultOf(tool))" class="json-window">
                        <pre>{{ structuredText(toolResultOf(tool)) }}</pre>
                      </div>
                      <pre v-else>{{ fmtValue(toolResultOf(tool)) }}</pre>
                    </div>
                  </div>
                  </div>
                </template>
              </section>
            </template>
            <p v-else-if="detailErrors[step.seq]" class="error" role="alert">{{ detailErrors[step.seq] }}</p>
            <p v-else class="muted" role="status" aria-live="polite">加载中…</p>
          </div>
        </div>
      </div>
    </div>

    <div v-if="skeleton.turns.length > 3" class="minimap" role="navigation" aria-label="轮次位置">
      <button
        v-for="turn in skeleton.turns"
        :key="turn.index"
        type="button"
        :class="{ on: currentTurn === turn.index }"
        :style="{ flexGrow: Math.max(1, turn.steps.length) }"
        :title="`第 ${turn.index} 轮 · ${turn.steps.length} 步`"
        @click="focusTurn(turn.index)"
      ></button>
    </div>

    <div class="quick" :class="{ show: showQuick || openAny }">
      <button type="button" @click="collapseAll">⇈ 收起全部</button>
      <button type="button" @click="scrollTop">↑ 顶部</button>
    </div>
  </template>
</template>

<style scoped>
/* 展开行是原生 button:清掉按钮默认外观,布局仍由下方各自的规则给出 */
.turn-head,
.step-head,
.msg-head {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 4px 10px;
  width: 100%;
  border: 0;
  background: none;
  color: inherit;
  font: inherit;
  text-align: left;
  padding: 5px 8px;
}

.msg-head .blk {
  flex: 1 1 200px;
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.msg-head:hover {
  background: var(--surface-2);
}

/* 数值列统一格式:等宽、tabular、右对齐、固定宽度 */
.ts,
.lat,
.tokens,
.seq,
.status,
.harness,
.toolms,
.stat .v {
  font-family: var(--mono);
  font-variant-numeric: tabular-nums;
  font-size: var(--fs-11);
  color: var(--muted);
  text-align: right;
  white-space: nowrap;
}

.lat,
.harness,
.toolms,
.tokens {
  min-width: 64px;
}

.turn-tabs {
  display: flex;
  gap: 6px;
  padding: 10px 16px 6px 26px;
  border-top: 1px solid var(--line);
}
.turn-tabs button {
  border: 1px solid var(--line);
  background: var(--surface);
  color: var(--muted);
  border-radius: var(--r-md);
  padding: 3px 12px;
  font-size: 12px;
  cursor: pointer;
}
.turn-tabs button:hover {
  background: var(--surface-2);
}
.turn-tabs button.on {
  background: var(--accent-soft);
  border-color: var(--accent);
  color: var(--accent-strong);
  font-weight: 600;
}
/* 统计分两行:计数器在上,分类分布独占一行,避免挤在一行 */
.summary {
  display: flex;
  flex-direction: column;
  gap: 10px;
}

.summary-row {
  display: flex;
  flex-wrap: wrap;
  align-items: flex-end;
  gap: 10px 22px;
}

/* 拆分行:主路与旁路并列,旁路标明是总计的子集 */
.split {
  align-items: center;
  gap: 6px 14px;
  padding-top: 8px;
  border-top: 1px dashed var(--line);
  font-size: var(--fs-12);
  color: var(--muted);
}

.split-name {
  font-weight: 600;
  color: var(--text);
}

.split-name em {
  font-style: normal;
  font-weight: 400;
  color: var(--muted);
}

.split-item {
  font-family: var(--mono);
  font-variant-numeric: tabular-nums;
}

/* 时间构成条:模型、工具、人三段 */
.composition {
  align-items: center;
  gap: 8px 14px;
}

.comp-label {
  font-weight: 600;
}

.comp-model {
  background: var(--accent);
}

.comp-tool {
  background: var(--tool);
}

.comp-warn {
  flex: 1 0 100%;
  font-size: var(--fs-12);
  color: var(--warn);
}


.comp-legend {
  display: flex;
  flex-wrap: wrap;
  gap: 6px 14px;
  font-size: var(--fs-12);
  color: var(--muted);
  font-variant-numeric: tabular-nums;
}

/* 图例色块与轴上分段同色,读者不必猜颜色 */
.comp-legend .sw {
  display: inline-block;
  width: 8px;
  height: 8px;
  border-radius: 2px;
  margin-right: 5px;
  vertical-align: baseline;
}

/* 步时间轴上的工具段:接在模型条之后 */
.toolfill {
  position: absolute;
  top: 0;
  bottom: 0;
  background: var(--tool);
  opacity: 0.75;
  border-radius: 3px;
}

.toolms {
  font-size: var(--fs-11);
  color: var(--tool);
  border-bottom: 1px dashed var(--tool);
  cursor: help;
}
.stat {
  display: flex;
  flex-direction: column;
}
.stat .k {
  font-size: var(--fs-11);
  color: var(--muted);
  text-transform: uppercase;
}
.stat .v {
  font-size: 16px;
  font-weight: 600;
}
/* 推断值用虚线下划线标出:口径随值走,不靠颜色区分 */
.stat .v.infer {
  border-bottom: 1px dashed var(--line);
  cursor: help;
}
/* 退化说明是文案不是数值:用弱化样式,避免读者把它当成计数 */
.stat .v.note {
  font-size: 12px;
  font-weight: 400;
  color: var(--muted);
}
/* 协议名较长:用小一号字,避免把统计行撑开 */
.stat .v.proto {
  font-size: 12px;
  font-weight: 500;
}
.proto-mixed {
  flex: 1 0 100%;
}

/* 一行叙述:每一段指向对应步,点击即跳转 */
.story {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: 6px;
  padding: 0 16px 10px;
  font-size: 12px;
  color: var(--muted);
}
.story .part {
  border: 0;
  background: none;
  padding: 0;
  font: inherit;
  color: var(--accent);
  cursor: pointer;
}
.story .part:hover {
  text-decoration: underline;
}
.story span.part {
  color: var(--muted);
  cursor: default;
}
.story .arrow {
  color: var(--line);
}
.story .tail {
  color: var(--muted);
}

/* 失败优先:清单固定在轮次顶部,每条可跳到该步 */
.failures {
  display: flex;
  flex-direction: column;
  gap: 6px;
  padding: 8px 16px;
  border-bottom: 1px dashed var(--danger-soft);
  background: var(--danger-soft);
}
.failure {
  display: flex;
  align-items: center;
  gap: 8px;
  font-size: 12px;
}
.failure .where {
  font-family: var(--mono);
  color: var(--danger);
}
.failure .why {
  color: var(--muted);
}

/* 时间轴:每步一根条,共用一条轴。轨道列定宽,各行才能对齐 */
.axis {
  border-bottom: 1px dashed var(--line);
}
.axis-row {
  cursor: default;
}
.axis-note {
  margin: 0;
  padding: 0 16px 8px 26px;
  font-size: var(--fs-11);
  color: var(--muted);
  line-height: 1.6;
}
.track {
  position: relative;
  flex: 0 0 170px;
  height: 10px;
  /* 用页面底色加深色描边:行 hover 时仍是同一块可见区域 */
  background: var(--bg);
  box-shadow: inset 0 0 0 1px var(--line);
  border-radius: 3px;
}
.axis-row .track {
  background: transparent;
}
.track .fill {
  position: absolute;
  top: 0;
  bottom: 0;
  background: var(--accent);
  border-radius: 3px;
}
/* 旁路步在轴上弱化配色;失败步用危险色 */
.track .fill.side {
  background: var(--infer);
}
.track .fill.failed {
  background: var(--danger);
}
/* harness 侧时间是推算值:斜纹段,不沿用实测条的实心样式 */
.track .hatch {
  position: absolute;
  top: 0;
  bottom: 0;
  border-radius: 3px;
  background-image: repeating-linear-gradient(45deg, var(--line) 0 3px, transparent 3px 6px);
  cursor: help;
}
/* 数值同样按推算样式弱化:虚线下划线,与实测的 lat 区分 */
.harness {
  font-size: var(--fs-11);
  color: var(--muted);
  border-bottom: 1px dashed var(--line);
  cursor: help;
}
.track .axis-end {
  position: absolute;
  top: 50%;
  transform: translateY(-50%);
  font-size: 10px;
  font-style: normal;
  color: var(--muted);
}
.track .axis-end.left {
  left: 0;
}
.track .axis-end.right {
  right: 0;
}

/* 极值标注:一轮只标最慢与最多 token */
.chip.hot {
  background: var(--warn-soft);
  border-color: var(--warn);
  color: var(--warn);
}

/* 步行的前三列定宽:轨道列才能在各行之间对齐 */
.step-head .caret {
  flex: 0 0 8px;
  font-size: 10px;
}
/* 序号按内容占宽,不再预留 46px 的空白槽 */
.step-head .seq {
  flex: 0 0 30px;
  text-align: right;
}
.step-head .ts {
  flex: 0 0 70px;
}
.turn {
  padding: 0;
  overflow: hidden;
}
.turn-head {
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 12px 16px;
  cursor: pointer;
}
.turn-head:hover {
  background: var(--surface-2);
}
.caret {
  color: var(--muted);
  font-size: var(--fs-11);
}
.idx {
  width: 22px;
  height: 22px;
  border-radius: var(--r-md);
  background: var(--surface-2);
  font-size: 12px;
  font-weight: 600;
  display: inline-flex;
  align-items: center;
  justify-content: center;
}
.title {
  font-weight: 600;
}
.title.pending {
  border-bottom: 1px dashed var(--line);
  color: var(--muted);
}
.chips {
  margin-left: auto;
  display: flex;
  gap: 8px;
  flex-wrap: wrap;
  justify-content: flex-end;
}
.chip {
  font-size: 12px;
  color: var(--muted);
  background: var(--surface-2);
  border: 1px solid var(--line);
  border-radius: 999px;
  padding: 1px 8px;
  white-space: nowrap;
}
.chip.lazy {
  border-style: dashed;
  color: var(--muted);
}
.chip.infer {
  border-style: dashed;
  border-color: var(--infer);
  background: var(--infer-soft);
  color: var(--infer);
}
.chip.compress {
  border-color: var(--warn);
  background: var(--warn-soft);
  color: var(--warn);
  font-weight: 600;
}
.chip.warn {
  border-color: var(--warn);
  background: var(--warn-soft);
  color: var(--warn);
}
.chip.danger {
  border-color: var(--danger);
  background: var(--danger-soft);
  color: var(--danger);
}
.steps {
  position: relative;
  border-top: 1px solid var(--line);
  padding-left: 18px;
}

/* 轮次到 step 的引导线:一条竖线把整组 step 挂在轮次下面 */
.steps::before {
  content: '';
  position: absolute;
  left: 10px;
  top: 0;
  bottom: 0;
  width: 1px;
  background: var(--line);
}
.step-head {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 4px 6px;
  padding: 8px 12px 8px 4px;
  cursor: pointer;
  border-left: 2px solid transparent;
}

.step-head .track {
  flex: 0 0 170px;
}
.step-head:hover {
  background: var(--surface-2);
}
.step.open .step-head {
  background: var(--surface-2);
  border-left-color: var(--accent);
}
.seq,
.ts {
  font-family: var(--mono);
}
.ts {
  color: var(--muted);
  font-size: 12px;
}
.status {
  font-size: 12px;
  font-weight: 600;
  padding: 1px 7px;
  border-radius: 5px;
}
.status.ok {
  background: var(--ok-soft);
  color: var(--ok);
}
.status.warn {
  background: var(--warn-soft);
  color: var(--warn);
}
.status.danger {
  background: var(--danger-soft);
  color: var(--danger);
}
.status.muted {
  background: var(--surface-2);
  color: var(--muted);
}
.lat {
  min-width: 56px;
  text-align: right;
}
.tokens {
  font-family: var(--mono);
  font-size: 12px;
  color: var(--muted);
}
.step .tools {
  font-family: var(--mono);
  font-size: var(--fs-11);
  color: var(--muted);
  background: var(--surface-2);
  border: 1px dashed var(--line);
  border-radius: var(--r-sm);
  padding: 1px 6px;
  white-space: nowrap;
}
.step .tools.pending {
  color: var(--muted);
}
.spacer {
  margin-left: auto;
}
.badge {
  font-size: var(--fs-11);
  padding: 1px 7px;
  border-radius: 5px;
  border: 1px dashed var(--infer-soft);
  background: var(--infer-soft);
  color: var(--infer);
  white-space: nowrap;
}
.badge.added {
  border-color: var(--line);
  background: var(--surface-2);
  color: var(--muted);
}
.badge.retry,
.badge.warn {
  border-color: var(--warn);
  background: var(--warn-soft);
  color: var(--warn);
}
.badge.new {
  border-color: var(--accent);
  background: var(--accent-soft);
  color: var(--accent);
}
.badge.fork {
  border-color: var(--warn);
  background: var(--warn-soft);
  color: var(--warn);
}
.badge.compress {
  border-style: solid;
  border-color: var(--warn);
  background: var(--warn-soft);
  color: var(--warn);
  font-weight: 600;
}
.detail {
  margin-left: 12px;
  padding: 4px 12px 12px 12px;
  border-left: 1px solid var(--line);
}
.note {
  font-size: 12px;
  color: var(--muted);
  margin: 0 0 10px;
}
.sect {
  margin-top: 10px;
}
.label {
  font-size: var(--fs-11);
  color: var(--muted);
  text-transform: uppercase;
  letter-spacing: 0.05em;
  margin-bottom: 6px;
}
.link {
  border: 0;
  background: none;
  color: var(--accent);
  cursor: pointer;
  font-size: 12px;
  margin-left: 8px;
  padding: 0;
}
.cmp {
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: 10px;
}
.cmp-col {
  border: 1px solid var(--line);
  border-radius: var(--r-md);
  overflow: hidden;
}
.cmp-head {
  padding: 6px 10px;
  background: var(--surface-2);
  border-bottom: 1px solid var(--line);
  font-size: 12px;
  color: var(--muted);
}
.cmp-row {
  display: flex;
  gap: 8px;
  padding: 5px 10px;
  font-size: 12px;
  font-family: var(--mono);
  border-top: 1px solid var(--surface-2);
}
.cmp-row .role {
  min-width: 68px;
  color: var(--muted);
}
.cmp-row .sz {
  min-width: 62px;
  color: var(--warn);
  font-variant-numeric: tabular-nums;
}
.cmp-row .sz2 {
  min-width: 56px;
  color: var(--muted);
  font-variant-numeric: tabular-nums;
}
/* 差异块:每块可独立折叠,块头给出类型与两侧行号范围。 */
.hunk {
  border: 1px solid var(--line);
  border-radius: var(--r-md);
  margin-bottom: 8px;
  overflow: hidden;
}
.hunk > summary {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 7px 10px;
  cursor: pointer;
  background: var(--surface-2);
  list-style: none;
}
.hunk > summary::-webkit-details-marker {
  display: none;
}
.hunk > summary:hover {
  background: var(--surface-2);
}
.hunk-kind {
  font-size: var(--fs-11);
  font-weight: 600;
  padding: 1px 7px;
  border-radius: var(--r-sm);
  border: 1px solid var(--line);
  color: var(--muted);
  background: var(--surface-2);
}
.hunk-kind.delete {
  color: var(--danger);
  background: var(--danger-soft);
  border-color: var(--danger);
}
.hunk-kind.insert {
  color: var(--ok);
  background: var(--ok-soft);
  border-color: var(--ok);
}
.hunk-kind.replace {
  color: var(--warn);
  background: var(--warn-soft);
  border-color: var(--warn);
}
.hunk .cmp {
  padding: 8px;
  border-top: 1px solid var(--line);
}
/* 统一 diff 视图:单栏,`-` 旧侧 / `+` 新侧,行首带行号与字节数 */
.udiff {
  border-top: 1px solid var(--line);
  font-size: 12px;
  max-height: 420px;
  overflow: auto;
}
.uline {
  display: flex;
  align-items: baseline;
  gap: 8px;
  padding: 3px 10px;
  white-space: pre-wrap;
  word-break: break-word;
}
.uline.del {
  background: var(--danger-soft);
}
.uline.add {
  background: var(--ok-soft);
}
.uline .sign {
  width: 10px;
  flex: none;
  font-weight: 700;
}
.uline.del .sign {
  color: var(--danger);
}
.uline.add .sign {
  color: var(--ok);
}
.uline .ln {
  min-width: 42px;
  flex: none;
  color: var(--muted);
  font-variant-numeric: tabular-nums;
}
.uline .txt {
  color: var(--text);
}
.hunk-range {
  font-size: 12px;
  color: var(--muted);
}
.uline.ctx {
  background: var(--surface-2);
  color: var(--muted);
}
.uline.ctx .txt {
  color: var(--muted);
}
/* 行内字符级高亮:只标记真正变化的中段 */
.udiff mark.chg {
  background: var(--danger-soft);
  color: var(--danger);
  border-radius: 2px;
  padding: 0 1px;
}
.uline.add mark.chg {
  background: var(--ok-soft);
  color: var(--ok);
}
.udiff .omitted {
  color: var(--muted);
  font-style: italic;
  margin: 0 4px;
}
/* 块间未变更行数 */
.gap {
  text-align: center;
  font-size: 11.5px;
  color: var(--muted);
  background: var(--surface-2);
  border: 1px dashed var(--line);
  border-radius: var(--r-md);
  padding: 3px 0;
  margin-bottom: 8px;
}
/* 双栏视图:改动行左右并排 */
.udiff.split .srow {
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: 0;
}
.udiff.split .scell {
  display: flex;
  align-items: baseline;
  gap: 8px;
  padding: 3px 10px;
  min-height: 22px;
  border-right: 1px solid var(--surface-2);
}
.udiff.split .scell:last-child {
  border-right: 0;
}
.udiff.split .scell.del {
  background: var(--danger-soft);
}
.udiff.split .scell.add {
  background: var(--ok-soft);
}
.udiff.split .scell .txt {
  white-space: pre-wrap;
  word-break: break-word;
}
.ctrl {
  display: inline-flex;
  gap: 10px;
  margin-left: 8px;
}
.ctrl .link {
  font-size: 12px;
}
/* 消息区的呈现切换:差异 / 新增消息(两者内容重叠,同时铺开会重复行号) */
.viewsw {
  display: inline-flex;
  border: 1px solid var(--line);
  border-radius: var(--r-md);
  overflow: hidden;
  margin-left: 8px;
  vertical-align: middle;
}
.viewsw button {
  border: 0;
  border-left: 1px solid var(--line);
  background: var(--surface);
  color: var(--muted);
  font-size: 11.5px;
  padding: 1px 9px;
  cursor: pointer;
}
.viewsw button:first-child {
  border-left: 0;
}
.viewsw button.on {
  background: var(--accent-soft);
  color: var(--accent-strong);
  font-weight: 600;
}
.hint {
  font-size: 12px;
  color: var(--muted);
  background: var(--surface-2);
  border: 1px solid var(--line);
  border-radius: var(--r-md);
  padding: 7px 10px;
  margin-bottom: 6px;
}
.hint code {
  font-family: var(--mono);
  font-size: 11.5px;
  background: var(--surface-2);
  border-radius: 3px;
  padding: 0 4px;
}
.mono {
  font-family: var(--mono);
}
.shrink {
  color: var(--warn);
}
.region {
  display: block;
  padding: 8px 0 10px;
  border-top: 1px dashed var(--line);
  border-top: 1px dashed var(--line);
}

.region .sect + .sect {
  margin-top: 10px;
  padding-top: 10px;
  border-top: 1px dashed var(--line);
}

.region-title {
  margin: 0 0 8px;
  font-size: var(--fs-12);
  letter-spacing: 0.08em;
  color: var(--muted);
}

.resp-meta {
  display: flex;
  flex-wrap: wrap;
  gap: 10px;
  font-size: var(--fs-11);
  color: var(--muted);
  font-variant-numeric: tabular-nums;
}

.resp-meta .warn {
  color: var(--warn);
}

.tool {
  border: 1px solid var(--line);
  border-radius: var(--r-md);
  margin-bottom: 8px;
  padding: 8px 10px;
}
.tool summary {
  display: flex;
  align-items: center;
  gap: 10px;
  cursor: pointer;
}
/* 参数摘要占剩余宽度,过长时省略而不是换行 */
.tool-args {
  flex: 1;
  min-width: 0;
  font-family: var(--mono);
  font-size: var(--fs-12);
  color: var(--muted);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.tool-ms {
  font-size: var(--fs-11);
  color: var(--tool);
  font-variant-numeric: tabular-nums;
}
.io {
  margin-top: 8px;
}
.io .k {
  font-size: var(--fs-11);
  color: var(--muted);
}
.io pre {
  margin: 2px 0 0;
  padding: 8px;
  background: var(--surface-2);
  border-radius: var(--r-md);
  font-size: 12px;
  white-space: pre-wrap;
  word-break: break-word;
  max-height: 260px;
  overflow: auto;
}
.inherited {
  font-size: 12px;
  color: var(--muted);
  padding: 6px 10px;
  background: var(--surface-2);
  border: 1px solid var(--line);
  border-radius: var(--r-md);
  margin-bottom: 6px;
}
.msg {
  border: 1px solid var(--surface-2);
  border-radius: var(--r-md);
  margin-bottom: 4px;
  overflow: hidden;
}
.msg-head {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 5px 8px;
  cursor: pointer;
}
.msg-head:hover {
  background: var(--surface-2);
}
.msg .idx {
  font-family: var(--mono);
  font-size: var(--fs-11);
  color: var(--muted);
  min-width: 34px;
  font-variant-numeric: tabular-nums;
}
.msg .blk {
  font-family: var(--mono);
  font-size: var(--fs-11);
  color: var(--muted);
}
.msg .sz {
  font-size: var(--fs-11);
  color: var(--muted);
  font-variant-numeric: tabular-nums;
}
.json-window {
  margin: 6px 0;
  padding: 8px 10px;
  max-height: 320px;
  overflow: auto;
  /* 与其它代码块同底:避免浅色主题下出现纯白一块 */
  background: var(--surface-2);
  border: 1px solid var(--line);
  border-radius: var(--r-md);
}

.json-window pre {
  margin: 0;
  background: transparent;
  color: var(--text);
  font-family: var(--mono);
  font-size: var(--fs-11);
  line-height: 1.55;
  white-space: pre;
}

.msg-body {
  padding: 6px 10px 10px 12px;
  border-top: 1px solid var(--surface-2);
  background: var(--surface-2);
}
.msg-body .raw {
  margin: 6px 0;
  padding: 8px 10px;
  background: var(--surface-2);
  border: 1px solid var(--line);
  border-radius: var(--r-md);
  font-size: 12px;
  white-space: pre-wrap;
  word-break: break-word;
  max-height: 320px;
  overflow: auto;
}
.blk-item + .blk-item {
  margin-top: 10px;
  padding-top: 10px;
  border-top: 1px dashed var(--line);
}
.blk-tag {
  font-family: var(--mono);
  font-size: 10px;
  color: var(--muted);
  margin-bottom: 3px;
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
.role {
  min-width: 74px;
  font-family: var(--mono);
  font-size: 12px;
  color: var(--muted);
}

.role.assistant {
  color: var(--ok);
}

.role.system {
  color: var(--infer);
}

.role.user {
  color: var(--accent);
}

.role.tool {
  color: var(--tool);
}

.msg .role {
  min-width: 74px;
  font-family: var(--mono);
  font-size: 12px;
  color: var(--muted);
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
.msg .txt {
  font-family: var(--mono);
  font-size: 12px;
  white-space: pre-wrap;
  word-break: break-word;
}
.hint {
  margin: 6px 0 0;
}

/* 轮次导航:滚动时固定,给出位置与换轮入口 */
.turn-nav {
  position: sticky;
  top: 0;
  z-index: 6;
  display: flex;
  align-items: center;
  gap: 10px;
  margin: 10px 0 12px;
  padding: 6px 10px;
  background: var(--surface);
  border: 1px solid var(--line);
  border-radius: var(--r-md);
  font-size: var(--fs-12);
}

.turn-nav .pos {
  font-family: var(--mono);
  color: var(--muted);
}

.turn-nav .nav-btns {
  display: inline-flex;
  gap: 4px;
}

.turn-nav .nav-btns button,
.turn-nav .order {
  min-width: 24px;
  height: 24px;
  padding: 0 8px;
  border: 1px solid var(--line);
  border-radius: var(--r-sm);
  background: var(--surface-2);
  color: var(--text);
  font-size: 12px;
  cursor: pointer;
}

.turn-nav .jump input {
  width: 58px;
  padding: 2px 6px;
  border: 1px solid var(--line);
  border-radius: var(--r-sm);
  background: var(--surface-2);
  color: var(--text);
  font-family: var(--mono);
  font-size: var(--fs-11);
}

/* 浮动按钮:收起全部与回顶 */
.quick {
  position: fixed;
  right: 18px;
  bottom: 18px;
  z-index: 20;
  display: flex;
  gap: 8px;
  opacity: 0;
  pointer-events: none;
  transform: translateY(6px);
  transition: opacity 0.15s ease, transform 0.15s ease;
}

.quick.show {
  opacity: 1;
  pointer-events: auto;
  transform: none;
}

.quick button {
  padding: 6px 12px;
  border: 1px solid var(--line);
  border-radius: 999px;
  background: var(--surface);
  color: var(--text);
  font-size: var(--fs-12);
  box-shadow: var(--shadow-card);
  cursor: pointer;
}

/* 迷你地图:每轮一根刻度,高度按步数加权 */
.minimap {
  position: fixed;
  right: 6px;
  top: 50%;
  transform: translateY(-50%);
  z-index: 15;
  display: flex;
  flex-direction: column;
  gap: 2px;
  width: 10px;
  height: min(46vh, 420px);
  opacity: 0.45;
}

.minimap:hover {
  opacity: 1;
}

.minimap button {
  border: 0;
  padding: 0;
  min-height: 3px;
  background: var(--line);
  border-radius: 2px;
  cursor: pointer;
}

.minimap button.on {
  background: var(--accent);
}
</style>
