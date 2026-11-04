// 通知队列:列表级结果用浮层提示,存在时间有限,可手动关闭。
// 表单错误不走这里:它必须留在弹窗内,与字段一起看。
import { ref } from 'vue'

export type ToastKind = 'ok' | 'info' | 'danger'

export interface Toast {
  id: number
  text: string
  kind: ToastKind
}

const items = ref<Toast[]>([])
let seq = 0
const timers = new Map<number, ReturnType<typeof setTimeout>>()

// 成功与提示留 4 秒,失败留 8 秒:读错误原因需要更长时间。
const TTL: Record<ToastKind, number> = { ok: 4000, info: 4000, danger: 8000 }

// pushToast 入队一条通知并安排自动消失,返回可手动关闭的 id。
export function pushToast(text: string, kind: ToastKind = 'info'): number {
  const id = ++seq
  items.value = [...items.value, { id, text, kind }]
  timers.set(
    id,
    setTimeout(() => dismissToast(id), TTL[kind]),
  )
  return id
}

// dismissToast 立刻移除一条通知,重复调用无副作用。
export function dismissToast(id: number): void {
  const timer = timers.get(id)
  if (timer !== undefined) {
    clearTimeout(timer)
    timers.delete(id)
  }
  items.value = items.value.filter((item) => item.id !== id)
}

export { items as toastItems }
