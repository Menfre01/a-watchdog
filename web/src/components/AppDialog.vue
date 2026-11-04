<script setup lang="ts">
import { onBeforeUnmount, ref, useId, watch } from 'vue'

const props = defineProps<{ open: boolean; title: string }>()
const emit = defineEmits<{ close: []; submit: [] }>()

const dialog = ref<HTMLDialogElement | null>(null)
const titleId = useId()
// returnFocus 记录打开前的焦点元素,关闭后把焦点还回去。
let returnFocus: HTMLElement | null = null
let focusFrame = 0

function show(el: HTMLDialogElement): void {
  returnFocus = document.activeElement instanceof HTMLElement ? document.activeElement : null
  document.body.style.overflow = 'hidden'
  el.showModal()
  focusFrame = requestAnimationFrame(() => {
    el.querySelector<HTMLElement>('input, select, textarea, button')?.focus()
  })
}

function hide(el: HTMLDialogElement): void {
  cancelAnimationFrame(focusFrame)
  document.body.style.overflow = ''
  if (el.open) el.close()
  returnFocus?.focus()
  returnFocus = null
}

watch(
  () => props.open,
  (open) => {
    const el = dialog.value
    if (!el) return
    if (open) show(el)
    else hide(el)
  },
)

// 原生关闭(含 Esc)只在父级仍认为打开时上报,避免回声。
function handleClose(): void {
  if (props.open) emit('close')
}

onBeforeUnmount(() => {
  cancelAnimationFrame(focusFrame)
  document.body.style.overflow = ''
})
</script>

<template>
  <dialog
    ref="dialog"
    class="app-dialog"
    :aria-labelledby="titleId"
    @cancel.prevent="emit('close')"
    @close="handleClose"
  >
    <form class="dialog-form" @submit.prevent="emit('submit')">
      <header class="dialog-head">
        <h2 :id="titleId">{{ props.title }}</h2>
      </header>
      <div class="dialog-content">
        <slot />
      </div>
      <footer class="dialog-foot">
        <slot name="footer" />
      </footer>
    </form>
  </dialog>
</template>

<style scoped>
.app-dialog {
  width: min(560px, calc(100vw - 32px));
  padding: 0;
  border: 1px solid var(--line);
  border-radius: var(--r-md);
  background: var(--surface);
  color: var(--text);
}

.app-dialog::backdrop {
  background: var(--backdrop);
}

.dialog-form {
  display: flex;
  flex-direction: column;
}

.dialog-head {
  padding: var(--sp-4) var(--sp-5) var(--sp-2);
}

.dialog-head h2 {
  margin: 0;
  font-size: var(--fs-16);
}

.dialog-content {
  padding: var(--sp-3) var(--sp-5);
  display: flex;
  flex-direction: column;
  gap: 10px;
}

.dialog-foot {
  display: flex;
  justify-content: flex-end;
  gap: var(--sp-2);
  padding: var(--sp-4) var(--sp-5);
  border-top: 1px solid var(--line);
}
</style>
