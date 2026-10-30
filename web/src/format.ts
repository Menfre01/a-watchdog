// 时间格式化:后端以 RFC3339Nano(UTC) 返回,界面统一为
// `YYYY-MM-DD HH:mm(:ss)` 的本地时刻(去掉 T / Z / 毫秒,保留完整日期)。

/** pad2 补零到两位。 */
function pad2(n: number): string {
  return String(n).padStart(2, '0')
}

/**
 * formatDateTime 输出 `YYYY-MM-DD HH:mm`(本地时区)。
 * 用于会话列表与详情头部:秒在此粒度下无意义。
 */
export function formatDateTime(ts: string): string {
  const d = new Date(ts)
  if (Number.isNaN(d.getTime())) return ts
  return `${d.getFullYear()}-${pad2(d.getMonth() + 1)}-${pad2(d.getDate())} ${pad2(d.getHours())}:${pad2(d.getMinutes())}`
}

/**
 * formatFullTime 输出 `YYYY-MM-DD HH:mm:ss`(本地时区)。
 * 用于轮次与 step:秒是「间隔」口径的必要精度。
 */
export function formatFullTime(ts: string): string {
  const d = new Date(ts)
  if (Number.isNaN(d.getTime())) return ts
  return `${formatDateTime(ts)}:${pad2(d.getSeconds())}`
}

/**
 * formatSpan 输出两个时刻之间的跨度(如 `14m40s`)。
 * 依据是首末请求的进入时刻,不含最后一次的响应耗时。
 */
export function formatSpan(from: string, to: string): string {
  const a = new Date(from).getTime()
  const b = new Date(to).getTime()
  if (Number.isNaN(a) || Number.isNaN(b) || b < a) return ''
  const ms = b - a
  const minutes = Math.floor(ms / 60000)
  const seconds = Math.round((ms % 60000) / 1000)
  if (minutes > 0) return `${minutes}m${pad2(seconds)}s`
  return `${seconds}s`
}
