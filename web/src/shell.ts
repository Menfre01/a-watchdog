// 外壳状态:侧栏折叠与否。键与读写集中在这里,组件不直接碰 localStorage。

// ShellItem 是侧栏导航项:id 对应视图,icon 为 24×24 线框图的 path 数据。
export interface ShellItem<Id extends string = string> {
  id: Id
  label: string
  icon: string
}
const STORAGE_KEY = 'dsctx.sidebar'

// sidebarCollapsed 返回上次的折叠状态;默认展开。
export function sidebarCollapsed(): boolean {
  try {
    return localStorage.getItem(STORAGE_KEY) === 'collapsed'
  } catch {
    return false
  }
}

// setSidebarCollapsed 持久化折叠状态。
export function setSidebarCollapsed(collapsed: boolean): void {
  try {
    localStorage.setItem(STORAGE_KEY, collapsed ? 'collapsed' : 'expanded')
  } catch {
    // 存储不可用时不持久化,本次会话内仍生效。
  }
}
