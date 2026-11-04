// 时间格式化:后端以 RFC3339Nano(UTC) 返回,界面统一为本地时刻。
// 用 Intl.DateTimeFormat('sv-SE') 得到 `YYYY-MM-DD HH:mm(:ss)`,不手写补零。
const dateTime = new Intl.DateTimeFormat('sv-SE', {
  year: 'numeric',
  month: '2-digit',
  day: '2-digit',
  hour: '2-digit',
  minute: '2-digit',
  hourCycle: 'h23',
})

const dateTimeSeconds = new Intl.DateTimeFormat('sv-SE', {
  year: 'numeric',
  month: '2-digit',
  day: '2-digit',
  hour: '2-digit',
  minute: '2-digit',
  second: '2-digit',
  hourCycle: 'h23',
})

/**
 * formatDateTime 输出 `YYYY-MM-DD HH:mm`(本地时区)。
 * 用于会话列表与详情头部:秒在此粒度下无意义。
 */
export function formatDateTime(ts: string): string {
  const d = new Date(ts)
  if (Number.isNaN(d.getTime())) return ts
  return dateTime.format(d)
}

/**
 * formatFullTime 输出 `YYYY-MM-DD HH:mm:ss`(本地时区)。
 * 用于轮次与 step:秒是「间隔」口径的必要精度。
 */
export function formatFullTime(ts: string): string {
  const d = new Date(ts)
  if (Number.isNaN(d.getTime())) return ts
  return dateTimeSeconds.format(d)
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
  if (minutes > 0) return `${minutes}m${String(seconds).padStart(2, '0')}s`
  return `${seconds}s`
}

// fmtK 把大数压成 k 单位;null 显示为破折号。
export function fmtK(n: number | null): string {
  if (n === null) return '—'
  if (n >= 1000) return (n / 1000).toFixed(n >= 10000 ? 0 : 1) + 'k'
  return String(n)
}

// fmtMs 按量级选择 ms / s / m+s,供延迟与时长共用。
export function fmtMs(ms: number): string {
  if (ms >= 60000) return Math.floor(ms / 60000) + 'm' + Math.round((ms % 60000) / 1000) + 's'
  if (ms >= 1000) return (ms / 1000).toFixed(1) + 's'
  return ms + 'ms'
}
