// 管理 API 客户端:同源携带会话 cookie;变更类请求统一携带 CSRF 头;
// 错误统一抛出后端返回的 message。
export interface Provider {
  id: number
  name: string
  protocol: string
  base_url: string
  api_key: string
  probe_model: string
  enabled: boolean
}

export interface Model {
  id: number
  name: string
  provider_id: number
  upstream_model: string
  enabled: boolean
}

export interface ClientKey {
  id: number
  name: string
  key_prefix: string
  enabled: boolean
  created_at: string
  key?: string
}

export interface Setting {
  key: string
  value: string
}

export interface ProbeResult {
  ok: boolean
  detail: string
  status?: number
}

// ---- 可观测(轨迹) ----

export interface TraceSession {
  session_id: string
  session_source: string
  session_note: string
  first_seen: string
  last_seen: string
  steps: number
  usage_in: number
  usage_out: number
  usage_cache: number
}

export interface TraceUsage {
  in: number | null
  out: number | null
  cache: number | null
}

export interface TraceDelta {
  kind: string
  added: number
  removed: number
  kept: number
  first_diff: number
  note: string
}

export interface TraceStep {
  seq: number
  ts: string
  model: string
  // protocol 决定用量列的口径:Anthropic 的 in 不含缓存,OpenAI 兼容的 in 含缓存命中。
  protocol: string
  status_code: number
  error_kind?: string
  latency_ms: number
  stream: boolean
  usage_in: number | null
  usage_out: number | null
  usage_cache: number | null
  delta: TraceDelta
  compress?: { before_lines: number; after_lines: number; from_seq: number }
  cleared?: boolean
  // tail_update 标记末行内容被就地更新(行数不变、只剩末行不同)。
  // 方向看 tail_before / tail_after:客户端刷新末尾注入内容、旁路请求重写
  // 自己的转录行都属于此形态,扩展与收缩都出现,不是一个"出错"信号。
  tail_update?: boolean
  tail_before?: number
  tail_after?: number
  // diff_lines 是与上一步差异的行数(骨架层即可显示「分叉 N 行」)。
  diff_lines?: number
  // side 标记旁路步(客户端监控 / 子 agent):内联在轮次里但弱化呈现。
  side?: boolean
  // side_chain 是旁路链的编号(1 起),用于区分不同调用方。
  side_chain?: number
  // at_ms 是该步相对本轮时间轴起点的偏移(毫秒)。轴由服务端算好:
  // 前端解析时间戳会重复实现口径。缺失表示该轮时间戳不可解析,此时不画轴。
  at_ms?: number
  // harness_ms 是该步之后 harness 侧耗时(毫秒):同链下一步的开始时间 − 本步结束时间。
  // 这是推算值,含工具在客户端的执行、客户端自身的处理与人的思考,三者不可拆分。
  // 链末步没有后继,该字段缺失(界面显示「—」)。
  harness_ms?: number
  retry: { n: number; of: number }
}

// TraceStoryPart 是轮次叙述的一段;带 seq 的段可跳转到对应步。
export interface TraceStoryPart {
  text: string
  seq?: number
}

// TraceKindCount 是分类分布的一段:分类名与步数。
export interface TraceKindCount {
  kind: string
  steps: number
}

// TraceKinds 是会话层的分类分布。分类分项之和等于 steps;
// marks 是分类内部的形态,单独计数,不并入分项。
export interface TraceKinds {
  counts: TraceKindCount[]
  marks: { compressed: number; cleared: number; tail_update: number }
  steps: number
}

// TraceChains 是会话层的链汇总。链归属由指纹比对推断:
// 同一条链的判据是 system 指纹与首条 user 指纹两级相同。
// degraded 为真表示缺 system 指纹,归属退化为不拆分:此时不给链数,
// 界面显示「—」并说明原因(退化的「1 条链」不是事实)。
export interface TraceChains {
  degraded?: boolean
  note?: string
  main?: number
  side?: number
  side_steps?: number
  side_usage_in?: number
  side_usage_out?: number
}

export interface TraceTurn {
  index: number
  started_at: string
  // duration_ms 是该轮时间轴的总时长(含旁路请求);0 表示不可用。
  duration_ms?: number
  parallel: boolean
  compresses: number
  retries: number
  failures: number
  side_count?: number
  story?: TraceStoryPart[]
  steps: TraceStep[]
}

export interface TraceSkeleton {
  session_id: string
  session_source: string
  session_note: string
  first_seen: string
  last_seen: string
  steps: number
  usage_in: number
  usage_out: number
  usage_cache: number
  turns: TraceTurn[]
  kinds?: TraceKinds
  chains?: TraceChains
}

export interface TraceToolAgg {
  name: string
  source: string
  calls: number
  ok: number
  err: number
  unseen: number
  gaps_ms: number[]
  turns: number[]
}

export interface TraceDigest {
  by_turn: { turn: number; title: string; calls: number; by_name: Record<string, number> }[]
  // by_step 供 step 行显示工具标签:只有发起了调用的步才会出现。
  by_step?: { seq: number; calls: number; by_name: Record<string, number> }[]
  totals: { calls: number; ok: number; err: number; unseen: number }
  tools: TraceToolAgg[]
}

export interface TraceMessage {
  role: string
  blocks: Record<string, unknown>[]
}

export interface TraceToolCall {
  name: string
  status: string
  input: unknown
  result: unknown
  issued_seq: number
  result_seq: number
}

export interface TraceCompress {
  before_lines: number
  after_lines: number
  from_seq: number
  kept: number
  removed: number
  added: number
  before?: TraceMessage[]
  after?: TraceMessage[]
}

// TraceDiffRow 是差异块某一侧的一行:完整上下文中的行号、字节数与消息。
export interface TraceDiffRow {
  line: number
  bytes: number
  message?: TraceMessage
}

// TraceDiffHunk 是一处差异:由 LCS 求出,而不是逐位置对齐——
// 消息被插入或删除后其后所有行都会偏移,逐位置对齐会报出一大片无关的「不同」。
// kind:replace(两侧都有)/ delete(只有旧侧)/ insert(只有新侧)。
export interface TraceDiffHunk {
  kind: 'replace' | 'delete' | 'insert'
  // header 是服务端算好的标准块头 `@@ -32,1 +32,2 @@`,范围含上下文行。
  header: string
  prev_start: number
  prev_count: number
  cur_start: number
  cur_count: number
  // ctx_before / ctx_after 是变更前后的上下文行(各至多 3 行,与 git 默认一致)。
  ctx_before?: TraceDiffContext[]
  ctx_after?: TraceDiffContext[]
  // unchanged_before 是本块与上一块之间未变更的行数(首块无此字段)。
  unchanged_before?: number
  prev?: TraceDiffRow[]
  cur?: TraceDiffRow[]
  prev_bytes?: number
  cur_bytes?: number
}

// TraceDiffContext 是一行上下文:两侧行号(0 基)与该行的字节数、内容。
export interface TraceDiffContext {
  prev_line: number
  cur_line: number
  bytes: number
  message?: TraceMessage
}

export interface TraceStepDetail {
  seq: number
  ts: string
  delta: { kind: string; note: string }
  inherited: number
  messages: TraceMessage[]
  tools: TraceToolCall[]
  compress?: TraceCompress
  diff?: TraceDiffHunk[]
  diff_lines?: number
  // ctx_lines 是本次返回实际使用的上下文行数。
  ctx_lines?: number
}

async function request<T>(method: string, path: string, body?: unknown): Promise<T> {
  const resp = await fetch(path, {
    method,
    credentials: 'same-origin',
    headers: {
      'X-DS-Context-CSRF': '1',
      ...(body !== undefined ? { 'Content-Type': 'application/json' } : {}),
    },
    body: body !== undefined ? JSON.stringify(body) : undefined,
  })
  if (resp.status === 204) return undefined as T
  const data = (await resp.json().catch(() => ({}))) as { error?: string }
  if (!resp.ok) {
    throw new Error(data.error ?? `请求失败(${resp.status})`)
  }
  return data as T
}

export const api = {
  get: <T>(path: string) => request<T>('GET', path),
  post: <T>(path: string, body?: unknown) => request<T>('POST', path, body),
  put: <T>(path: string, body?: unknown) => request<T>('PUT', path, body),
  del: (path: string) => request<void>('DELETE', path),
}
