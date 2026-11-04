// 可观测模块的地址栏状态:片段路径带会话标识,查询参数带面板。
// 不引入路由库:两段状态用 hash 足够,也避免为一次导航付一个依赖。
export type Panel = 'trace' | 'tools'

export interface ObservabilityRoute {
  sessionID: string
  panel: Panel
}

const PREFIX = '#/observability/'

// parseHash 解析地址;空片段或不是可观测路径时返回 null(列表态)。
export function parseHash(hash: string): ObservabilityRoute | null {
  if (!hash.startsWith(PREFIX)) return null
  const rest = hash.slice(PREFIX.length)
  const [encodedID, query = ''] = rest.split('?')
  if (!encodedID) return null
  let sessionID = ''
  try {
    sessionID = decodeURIComponent(encodedID)
  } catch {
    return null
  }
  if (!sessionID) return null
  const panel: Panel = new URLSearchParams(query).get('panel') === 'tools' ? 'tools' : 'trace'
  return { sessionID, panel }
}

// buildHash 构造会话详情地址。
export function buildHash(sessionID: string, panel: Panel): string {
  return `${PREFIX}${encodeURIComponent(sessionID)}?panel=${panel}`
}

// listHash 是列表态地址。
export const listHash = '#/observability'

// replaceHash 只改地址不新增历史:面板切换用它,避免后退键被面板切换淹没。
export function replaceHash(hash: string): void {
  history.replaceState(null, '', hash)
}

// subscribe 监听片段变化,返回取消订阅函数。
export function subscribe(listener: () => void): () => void {
  window.addEventListener('hashchange', listener)
  return () => window.removeEventListener('hashchange', listener)
}
