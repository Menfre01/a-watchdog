// 主题读写:localStorage 键与 DOM、浏览器元信息的同步集中在这里,
// 组件只调用函数,不直接碰属性。
export type Theme = 'dark' | 'light'

const STORAGE_KEY = 'dsctx.theme'

// current 读当前生效主题,以 DOM 上的属性为准。
export function current(): Theme {
  return document.documentElement.dataset.theme === 'light' ? 'light' : 'dark'
}

// syncMeta 把 theme-color 对齐到当前主题的 --bg。
// 颜色只在 tokens.css 定义,这里取计算值,避免第二份色值。
export function syncMeta(): void {
  const bg = getComputedStyle(document.documentElement).getPropertyValue('--bg').trim()
  if (!bg) return
  let meta = document.querySelector('meta[name="theme-color"]')
  if (!meta) {
    meta = document.createElement('meta')
    meta.setAttribute('name', 'theme-color')
    document.head.appendChild(meta)
  }
  meta.setAttribute('content', bg)
}

// set 让主题立即生效并写入存储,刷新后保持。
export function set(theme: Theme): void {
  document.documentElement.dataset.theme = theme
  syncMeta()
  try {
    localStorage.setItem(STORAGE_KEY, theme)
  } catch {
    // 存储不可用(隐私模式)时只在本次会话生效。
  }
}

// toggle 在暗色与亮色之间切换,返回切换后的主题。
export function toggle(): Theme {
  const next: Theme = current() === 'dark' ? 'light' : 'dark'
  set(next)
  return next
}
