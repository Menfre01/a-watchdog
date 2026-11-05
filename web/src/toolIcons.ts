// 工具名到线性图标的映射。工具名来自客户端,未知名称统一用扳手。
export function toolIconName(name: string): string {
  const n = name.toLowerCase()
  if (n.startsWith('mcp__')) return 'plug'
  if (n.includes('notebook')) return 'notebook'
  if (n.includes('todo') || n.includes('plan')) return 'list'
  if (n.includes('read') || n.includes('file')) return 'file'
  if (n.includes('edit') || n.includes('write')) return 'pencil'
  if (n.includes('bash') || n.includes('shell') || n.includes('terminal')) return 'terminal'
  if (n.includes('grep') || n.includes('glob') || n.includes('search')) return 'search'
  if (n.includes('web') || n.includes('fetch') || n.includes('browser')) return 'globe'
  if (n.includes('task') || n.includes('agent')) return 'bot'
  return 'wrench'
}
