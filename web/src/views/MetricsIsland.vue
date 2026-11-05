<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { api, type RequestMetrics } from '../api'
import { fmtK, fmtMs } from '../format'

const props = defineProps<{
  range: string
  keyId: string
  sessionPrefix: string
  revision: number
}>()

const data = ref<RequestMetrics | null>(null)
const loading = ref(false)
const error = ref('')

async function load(): Promise<void> {
  loading.value = true
  error.value = ''
  try {
    const params = new URLSearchParams({ window: props.range })
    if (props.keyId) params.set('key_id', props.keyId)
    if (props.sessionPrefix) params.set('q', props.sessionPrefix)
    data.value = await api.get<RequestMetrics>(`/console/api/metrics?${params.toString()}`)
  } catch (e) {
    error.value = e instanceof Error ? e.message : String(e)
  } finally {
    loading.value = false
  }
}

watch(() => [props.range, props.keyId, props.sessionPrefix, props.revision], load, { immediate: true })

const cards = computed(() => {
  const m = data.value
  if (!m) return []
  return [
    { label: '请求数', value: String(m.count) },
    { label: '错误率', value: `${(m.error_rate * 100).toFixed(1)}%`, sub: `${m.errors} 次` },
    { label: 'p50', value: fmtMs(m.p50_ms) },
    { label: 'p95', value: fmtMs(m.p95_ms) },
    { label: '缓存命中', value: `${(m.cache_rate * 100).toFixed(1)}%` },
    { label: 'token 输入 / 输出', value: `${fmtK(m.usage_in)} / ${fmtK(m.usage_out)}` },
  ]
})

function errorKindLabel(kind: string): string {
  return kind || '未归因'
}

const errorSummary = computed(() => {
  const m = data.value
  if (!m) return ''
  if (m.errors === 0) return '无错误'
  const parts = [`4xx ${m.errors_4xx ?? 0}`, `5xx ${m.errors_5xx ?? 0}`]
  const rows = m.error_kinds ?? []
  for (const row of rows.slice(0, 6)) {
    parts.push(`${errorKindLabel(row.kind)} ${row.count}`)
  }
  const rest = rows.slice(6).reduce((n, row) => n + row.count, 0)
  if (rest > 0) parts.push(`其他 ${rest}`)
  return parts.join(' · ')
})

const successSummary = computed(() => {
  const m = data.value
  if (!m || (m.success_count ?? 0) === 0) return '—'
  return `p95 ${fmtMs(m.success_p95_ms ?? 0)} · p99 ${fmtMs(m.success_p99_ms ?? 0)}（样本 ${m.success_count}）`
})
</script>

<template>
  <section class="island">
    <div class="island-head">
      <h2>请求指标</h2>
    </div>
    <p v-if="error" class="error" role="alert">{{ error }}</p>
    <p v-else-if="loading && !data" class="hint">加载中…</p>
    <div v-else class="kpis">
      <div v-for="c in cards" :key="c.label" class="kpi">
        <span class="kpi-label">{{ c.label }}</span>
        <strong class="kpi-value">{{ c.value }}</strong>
        <span v-if="c.sub" class="kpi-sub">{{ c.sub }}</span>
      </div>
    </div>
    <div v-if="data" class="metrics-detail">
      <div class="detail-row"><span class="detail-key">错误构成</span><span class="detail-val">{{ errorSummary }}</span></div>
      <div class="detail-row"><span class="detail-key">成功延迟</span><span class="detail-val">{{ successSummary }}</span></div>
    </div>
    <p v-if="sessionPrefix" class="hint">按 session 筛选时，未生成快照的请求不计入。</p>
    <p v-if="data && data.success_count < 20" class="hint">成功请求少于 20 个。p95 与 p99 参考意义有限。</p>
    <p v-if="data && data.count < 20" class="hint">窗口内请求少于 20 个。p50 与 p95 接近极值，参考意义有限。</p>
  </section>
</template>

<style scoped>
.kpis {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(140px, 1fr));
  gap: 16px;
}

.kpi {
  background: var(--surface-2);
  border: 1px solid var(--line);
  border-radius: var(--r-lg);
  padding: 10px 12px;
  display: grid;
  gap: 4px;
}

.kpi-label {
  font-size: var(--fs-11);
  color: var(--muted);
}

.kpi-value {
  font-size: var(--fs-16);
  font-variant-numeric: tabular-nums;
}

.kpi-sub {
  font-size: var(--fs-11);
  color: var(--muted);
}

.metrics-detail {
  margin-top: 12px;
  padding-top: 10px;
  border-top: 1px solid var(--line);
  display: grid;
  gap: 4px;
}

.detail-row {
  display: flex;
  flex-wrap: wrap;
  gap: 4px 10px;
  font-size: var(--fs-12);
}

.detail-key {
  min-width: 64px;
  color: var(--muted);
}

.detail-val {
  color: var(--text);
}
</style>
