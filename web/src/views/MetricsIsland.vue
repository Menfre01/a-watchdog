<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { api, type RequestMetrics } from '../api'
import { fmtK, fmtMs } from '../format'

const windows = [
  { value: '1h', label: '1 小时' },
  { value: '24h', label: '24 小时' },
  { value: '7d', label: '7 天' },
  { value: 'all', label: '全部' },
]

const current = ref('24h')
const data = ref<RequestMetrics | null>(null)
const loading = ref(false)
const error = ref('')

async function load(): Promise<void> {
  loading.value = true
  error.value = ''
  try {
    data.value = await api.get<RequestMetrics>(`/console/api/metrics?window=${current.value}`)
  } catch (e) {
    error.value = e instanceof Error ? e.message : String(e)
  } finally {
    loading.value = false
  }
}

function pick(value: string): void {
  current.value = value
  void load()
}

onMounted(load)

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
</script>

<template>
  <section class="island">
    <div class="island-head">
      <h2>请求指标</h2>
      <div class="segmented" role="group" aria-label="统计窗口">
        <button
          v-for="w in windows"
          :key="w.value"
          type="button"
          :class="{ active: current === w.value }"
          :aria-pressed="current === w.value"
          @click="pick(w.value)"
        >
          {{ w.label }}
        </button>
      </div>
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
    <p class="hint">分位数按窗口内全部请求计时;少于 20 个样本时 p95 接近最大值,参考意义有限。</p>
  </section>
</template>

<style scoped>
.kpis {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(140px, 1fr));
  gap: 12px;
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
</style>
